package service

import (
	"context"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	"github.com/jackc/pgx/v5"
)

// fakeRepository is an in-memory billing store that reproduces the predicates
// the Postgres repository enforces.
//
// The point is behavioural fidelity, not SQL: every guard that decides whether
// money moves (unique idempotency key, notification claim, status predicate on a
// settlement, refund total against paid) is implemented here, so a service test
// that credits twice would fail here exactly as it would against PostgreSQL.
type fakeRepository struct {
	mu sync.Mutex

	runError   error
	statements []string

	plans               map[string]*domain.Plan
	orders              map[string]*domain.Order
	orderByNo           map[string]string
	payments            map[string]*domain.Payment
	notifications       map[string]*domain.PaymentNotification
	notificationByClaim map[string]string
	refunds             map[string]*domain.Refund
	subscriptions       map[string]*domain.Subscription
	consents            map[string]*domain.SpendingConsent
	runs                map[string]*domain.ReconciliationRun
	items               map[string][]domain.ReconciliationItem
	ledger              []domain.QuotaLedgerEntry
	ledgerState         map[string]string
	settled             []repository.SettledPayment
	spendingLimits      map[string]*domain.FamilySpendingLimit
	spendingRejections  []domain.SpendingLimitRejection

	// grantFailures makes the quota path deterministic for the rollback test.
	grantFailures int
}

// newFakeRepository returns an empty store.
func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		plans:               map[string]*domain.Plan{},
		orders:              map[string]*domain.Order{},
		orderByNo:           map[string]string{},
		payments:            map[string]*domain.Payment{},
		notifications:       map[string]*domain.PaymentNotification{},
		notificationByClaim: map[string]string{},
		refunds:             map[string]*domain.Refund{},
		subscriptions:       map[string]*domain.Subscription{},
		consents:            map[string]*domain.SpendingConsent{},
		runs:                map[string]*domain.ReconciliationRun{},
		items:               map[string][]domain.ReconciliationItem{},
		ledgerState:         map[string]string{},
		spendingLimits:      map[string]*domain.FamilySpendingLimit{},
	}
}

// WithTx runs the callback against the same fake store. The store snapshots its
// state first and restores it on error, which is what makes a rollback assertion
// meaningful: a test can prove nothing was credited.
func (f *fakeRepository) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	f.mu.Lock()
	snapshot := f.snapshotLocked()
	f.statements = append(f.statements, "BEGIN")
	f.mu.Unlock()

	err := fn(nil)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		f.statements = append(f.statements, "ROLLBACK")
		f.restoreLocked(snapshot)
		return err
	}
	f.statements = append(f.statements, "COMMIT")
	return nil
}

// recording is the fake's internal snapshot of every mutable map.
type recording struct {
	orders        map[string]domain.Order
	payments      map[string]domain.Payment
	notifications map[string]domain.PaymentNotification
	refunds       map[string]domain.Refund
	subscriptions map[string]domain.Subscription
	ledger        []domain.QuotaLedgerEntry
	ledgerState   map[string]string
}

// snapshotLocked copies the money-bearing state. The caller holds the lock.
func (f *fakeRepository) snapshotLocked() recording {
	copied := recording{
		orders:        map[string]domain.Order{},
		payments:      map[string]domain.Payment{},
		notifications: map[string]domain.PaymentNotification{},
		refunds:       map[string]domain.Refund{},
		subscriptions: map[string]domain.Subscription{},
		ledger:        append([]domain.QuotaLedgerEntry(nil), f.ledger...),
		ledgerState:   map[string]string{},
	}
	for key, value := range f.orders {
		copied.orders[key] = *value
	}
	for key, value := range f.payments {
		copied.payments[key] = *value
	}
	for key, value := range f.notifications {
		copied.notifications[key] = *value
	}
	for key, value := range f.refunds {
		copied.refunds[key] = *value
	}
	for key, value := range f.subscriptions {
		copied.subscriptions[key] = *value
	}
	for key, value := range f.ledgerState {
		copied.ledgerState[key] = value
	}
	return copied
}

// restoreLocked rolls the money-bearing state back. The caller holds the lock.
func (f *fakeRepository) restoreLocked(saved recording) {
	f.orders = map[string]*domain.Order{}
	for key, value := range saved.orders {
		order := value
		f.orders[key] = &order
	}
	f.payments = map[string]*domain.Payment{}
	for key, value := range saved.payments {
		payment := value
		f.payments[key] = &payment
	}
	f.notifications = map[string]*domain.PaymentNotification{}
	f.notificationByClaim = map[string]string{}
	for key, value := range saved.notifications {
		notification := value
		f.notifications[key] = &notification
		f.notificationByClaim[notificationClaim(notification.Channel, notification.NotificationID)] = key
	}
	f.refunds = map[string]*domain.Refund{}
	for key, value := range saved.refunds {
		refund := value
		f.refunds[key] = &refund
	}
	f.subscriptions = map[string]*domain.Subscription{}
	for key, value := range saved.subscriptions {
		subscription := value
		f.subscriptions[key] = &subscription
	}
	f.ledger = saved.ledger
	f.ledgerState = saved.ledgerState
}

// notificationClaim is the unique key the notification table enforces.
func notificationClaim(channelValue domain.PaymentChannel, notificationID string) string {
	return string(channelValue) + "\x00" + notificationID
}

// ===== plans =====

// CreatePlan stores a plan and rejects a duplicate code.
func (f *fakeRepository) CreatePlan(ctx context.Context, plan domain.Plan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runError != nil {
		return f.runError
	}
	for _, existing := range f.plans {
		if existing.Code == plan.Code {
			return repository.ErrPlanCodeTaken
		}
	}
	stored := plan
	f.plans[plan.ID] = &stored
	return nil
}

// UpdatePlan applies an edit only at the expected version.
func (f *fakeRepository) UpdatePlan(ctx context.Context, plan domain.Plan, expectedVersion int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.plans[plan.ID]
	if !ok {
		return domain.ErrPlanNotFound
	}
	if existing.Version != expectedVersion {
		return repository.ErrVersionConflict
	}
	stored := plan
	stored.Version = expectedVersion + 1
	f.plans[plan.ID] = &stored
	return nil
}

// ArchivePlan marks a plan archived.
func (f *fakeRepository) ArchivePlan(ctx context.Context, planID string, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.plans[planID]
	if !ok {
		return domain.ErrPlanNotFound
	}
	existing.Status = domain.PlanStatusArchived
	existing.UpdatedAt = now
	existing.Version++
	return nil
}

// GetPlanByID returns one plan.
func (f *fakeRepository) GetPlanByID(ctx context.Context, planID string) (*domain.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.plans[planID]
	if !ok {
		return nil, domain.ErrPlanNotFound
	}
	copied := *existing
	return &copied, nil
}

// GetPlanByCode returns one plan by its stable code.
func (f *fakeRepository) GetPlanByCode(ctx context.Context, code string) (*domain.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.plans {
		if existing.Code == code {
			copied := *existing
			return &copied, nil
		}
	}
	return nil, domain.ErrPlanNotFound
}

// ListActivePlans returns purchasable plans in display order.
func (f *fakeRepository) ListActivePlans(ctx context.Context) ([]domain.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	plans := []domain.Plan{}
	for _, existing := range f.plans {
		if existing.Status == domain.PlanStatusActive {
			plans = append(plans, *existing)
		}
	}
	sort.Slice(plans, func(left, right int) bool { return plans[left].SortOrder < plans[right].SortOrder })
	return plans, nil
}

// ListPlans returns a filtered page of plans.
func (f *fakeRepository) ListPlans(
	ctx context.Context,
	filter repository.PlanFilter,
) ([]domain.Plan, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	plans := []domain.Plan{}
	for _, existing := range f.plans {
		if filter.Status != "" && existing.Status != filter.Status {
			continue
		}
		if filter.Search != "" && !strings.Contains(existing.Code, filter.Search) &&
			!strings.Contains(existing.DisplayName, filter.Search) {
			continue
		}
		plans = append(plans, *existing)
	}
	total := len(plans)
	return paginate(plans, filter.Limit, filter.Offset), total, nil
}

// paginate applies a window to an already ordered slice.
func paginate[T any](values []T, limit, offset int) []T {
	if offset >= len(values) {
		return []T{}
	}
	values = values[offset:]
	if limit > 0 && limit < len(values) {
		return values[:limit]
	}
	return values
}

// ===== orders =====

// CreateOrder stores an order, and returns the existing one when the family
// retries with the same idempotency key.
func (f *fakeRepository) CreateOrder(ctx context.Context, order domain.Order) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.orders {
		if existing.ParentAccountID == order.ParentAccountID &&
			existing.IdempotencyKey == order.IdempotencyKey {
			copied := *existing
			return &copied, nil
		}
	}
	stored := order
	f.orders[order.ID] = &stored
	f.orderByNo[order.OrderNo] = order.ID
	copied := stored
	return &copied, nil
}

// GetOrderByID returns one order.
func (f *fakeRepository) GetOrderByID(ctx context.Context, orderID string) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.orders[orderID]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	copied := *existing
	return &copied, nil
}

// GetOrderByOrderNo returns one order by its public number.
func (f *fakeRepository) GetOrderByOrderNo(ctx context.Context, orderNo string) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	orderID, ok := f.orderByNo[orderNo]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	copied := *f.orders[orderID]
	return &copied, nil
}

// GetOrderByIdentityOrIdempotency resolves a retried checkout.
func (f *fakeRepository) GetOrderByIdentityOrIdempotency(
	ctx context.Context,
	parentAccountID string,
	idempotencyKey string,
) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.orders {
		if existing.ParentAccountID == parentAccountID && existing.IdempotencyKey == idempotencyKey {
			copied := *existing
			return &copied, nil
		}
	}
	return nil, domain.ErrOrderNotFound
}

// ListOrdersByParent returns one family's orders.
func (f *fakeRepository) ListOrdersByParent(
	ctx context.Context,
	parentAccountID string,
	limit int,
	offset int,
) ([]domain.Order, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	orders := []domain.Order{}
	for _, existing := range f.orders {
		if existing.ParentAccountID == parentAccountID {
			orders = append(orders, *existing)
		}
	}
	return paginate(orders, limit, offset), len(orders), nil
}

// ListOrders returns a filtered page of orders.
func (f *fakeRepository) ListOrders(
	ctx context.Context,
	filter repository.OrderFilter,
) ([]domain.Order, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	orders := []domain.Order{}
	for _, existing := range f.orders {
		if filter.Status != "" && existing.Status != filter.Status {
			continue
		}
		orders = append(orders, *existing)
	}
	return paginate(orders, filter.Limit, filter.Offset), len(orders), nil
}

// LockOrderForSettlement returns an order for update.
func (f *fakeRepository) LockOrderForSettlement(
	ctx context.Context,
	tx pgx.Tx,
	orderNo string,
) (*domain.Order, error) {
	return f.GetOrderByOrderNo(ctx, orderNo)
}

// LockOrderByID returns an order for update.
func (f *fakeRepository) LockOrderByID(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
) (*domain.Order, error) {
	return f.GetOrderByID(ctx, orderID)
}

// MarkOrderPaid settles a pending order exactly once.
func (f *fakeRepository) MarkOrderPaid(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
	paidCents domain.Cents,
	paidAt time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.orders[orderID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	if existing.Status != domain.OrderStatusPending {
		return repository.ErrAlreadySettled
	}
	existing.Status = domain.OrderStatusPaid
	existing.PaidCents = paidCents
	existing.PaidAt = &paidAt
	return nil
}

// CancelPendingOrder cancels a pending order of the family.
func (f *fakeRepository) CancelPendingOrder(
	ctx context.Context,
	parentAccountID string,
	orderNo string,
	now time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	orderID, ok := f.orderByNo[orderNo]
	if !ok {
		return domain.ErrOrderNotFound
	}
	existing := f.orders[orderID]
	if existing.ParentAccountID != parentAccountID {
		return domain.ErrCrossFamilyAccess
	}
	if existing.Status != domain.OrderStatusPending {
		return repository.ErrAlreadySettled
	}
	existing.Status = domain.OrderStatusCancelled
	existing.CancelledAt = &now
	return nil
}

// ExpireStaleOrders expires due pending orders.
func (f *fakeRepository) ExpireStaleOrders(ctx context.Context, now time.Time, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	expired := []string{}
	for _, existing := range f.orders {
		if existing.Status != domain.OrderStatusPending || existing.ExpiresAt == nil {
			continue
		}
		if existing.ExpiresAt.After(now) {
			continue
		}
		existing.Status = domain.OrderStatusExpired
		closedAt := now
		existing.ClosedAt = &closedAt
		expired = append(expired, existing.ID)
	}
	return expired, nil
}

// CountPendingOrders reports the pending backlog.
func (f *fakeRepository) CountPendingOrders(ctx context.Context, now time.Time) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pending, overdue := 0, 0
	for _, existing := range f.orders {
		if existing.Status != domain.OrderStatusPending {
			continue
		}
		pending++
		if existing.ExpiresAt != nil && existing.ExpiresAt.Before(now) {
			overdue++
		}
	}
	return pending, overdue, nil
}

// ===== payments =====

// CreatePayment stores a payment attempt and rejects a repeated trade number.
func (f *fakeRepository) CreatePayment(ctx context.Context, payment domain.Payment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if payment.ChannelTradeNo != "" {
		for _, existing := range f.payments {
			if existing.Channel == payment.Channel && existing.ChannelTradeNo == payment.ChannelTradeNo {
				return domain.ErrDuplicateNotification
			}
		}
	}
	stored := payment
	f.payments[payment.ID] = &stored
	return nil
}

// GetPaymentByID returns one attempt.
func (f *fakeRepository) GetPaymentByID(ctx context.Context, paymentID string) (*domain.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.payments[paymentID]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	copied := *existing
	return &copied, nil
}

// GetPaymentByChannelTradeNo returns one attempt by the provider reference.
func (f *fakeRepository) GetPaymentByChannelTradeNo(
	ctx context.Context,
	channelValue domain.PaymentChannel,
	channelTradeNo string,
) (*domain.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.payments {
		if existing.Channel == channelValue && existing.ChannelTradeNo == channelTradeNo {
			copied := *existing
			return &copied, nil
		}
	}
	return nil, domain.ErrOrderNotFound
}

// FindSucceededPayment returns the settled attempt of an order.
func (f *fakeRepository) FindSucceededPayment(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
) (*domain.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.payments {
		if existing.OrderID == orderID && existing.Status == domain.PaymentStatusSucceeded {
			copied := *existing
			return &copied, nil
		}
	}
	return nil, domain.ErrOrderNotFound
}

// ListPaymentsByOrder returns every attempt of an order.
func (f *fakeRepository) ListPaymentsByOrder(
	ctx context.Context,
	orderID string,
) ([]domain.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	payments := []domain.Payment{}
	for _, existing := range f.payments {
		if existing.OrderID == orderID {
			payments = append(payments, *existing)
		}
	}
	return payments, nil
}

// UpdatePaymentProcessing moves a created attempt to processing.
func (f *fakeRepository) UpdatePaymentProcessing(
	ctx context.Context,
	payment domain.Payment,
	channelTradeNo string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.payments[payment.ID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	if existing.Status != domain.PaymentStatusCreated {
		return repository.ErrAlreadySettled
	}
	existing.Status = domain.PaymentStatusProcessing
	existing.ChannelTradeNo = channelTradeNo
	existing.ProviderPayload = payment.ProviderPayload
	existing.UpdatedAt = payment.UpdatedAt
	return nil
}

// MarkPaymentSucceeded settles one attempt, honouring one success per order.
func (f *fakeRepository) MarkPaymentSucceeded(
	ctx context.Context,
	tx pgx.Tx,
	paymentID string,
	channelTradeNo string,
	paidAt time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.payments[paymentID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	for _, other := range f.payments {
		if other.ID != paymentID && other.OrderID == existing.OrderID &&
			other.Status == domain.PaymentStatusSucceeded {
			return repository.ErrAlreadySettled
		}
	}
	if existing.Status == domain.PaymentStatusSucceeded {
		return repository.ErrAlreadySettled
	}
	existing.Status = domain.PaymentStatusSucceeded
	if channelTradeNo != "" {
		existing.ChannelTradeNo = channelTradeNo
	}
	existing.PaidAt = &paidAt
	return nil
}

// MarkPaymentFailed closes an attempt as failed.
func (f *fakeRepository) MarkPaymentFailed(
	ctx context.Context,
	paymentID string,
	code string,
	message string,
	now time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.payments[paymentID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	existing.Status = domain.PaymentStatusFailed
	existing.FailureCode = code
	existing.FailureMessage = message
	existing.UpdatedAt = now
	return nil
}

// MarkPaymentClosed closes an attempt as closed.
func (f *fakeRepository) MarkPaymentClosed(
	ctx context.Context,
	paymentID string,
	now time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.payments[paymentID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	existing.Status = domain.PaymentStatusClosed
	existing.UpdatedAt = now
	return nil
}

// InsertNotification claims a callback exactly once.
func (f *fakeRepository) InsertNotification(
	ctx context.Context,
	notification domain.PaymentNotification,
) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	claim := notificationClaim(notification.Channel, notification.NotificationID)
	if _, exists := f.notificationByClaim[claim]; exists {
		return true, nil
	}
	stored := notification
	f.notifications[notification.ID] = &stored
	f.notificationByClaim[claim] = notification.ID
	return false, nil
}

// GetNotification returns a recorded callback.
func (f *fakeRepository) GetNotification(
	ctx context.Context,
	channelValue domain.PaymentChannel,
	notificationID string,
) (*domain.PaymentNotification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.notificationByClaim[notificationClaim(channelValue, notificationID)]
	if !ok {
		return nil, domain.ErrDuplicateNotification
	}
	copied := *f.notifications[id]
	return &copied, nil
}

// MarkNotificationProcessed records the result of a received callback.
func (f *fakeRepository) MarkNotificationProcessed(
	ctx context.Context,
	notificationID string,
	result domain.NotificationResult,
	processedAt *time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.notifications[notificationID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	if existing.ProcessedAt != nil {
		return domain.ErrDuplicateNotification
	}
	existing.Result = result
	existing.ProcessedAt = processedAt
	return nil
}

// ===== refunds =====

// CreateRefund stores a refund request.
func (f *fakeRepository) CreateRefund(ctx context.Context, tx pgx.Tx, refund domain.Refund) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := refund
	f.refunds[refund.ID] = &stored
	return nil
}

// GetRefundByID returns one refund.
func (f *fakeRepository) GetRefundByID(ctx context.Context, refundID string) (*domain.Refund, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.refunds[refundID]
	if !ok {
		return nil, repository.ErrRefundNotFound
	}
	copied := *existing
	return &copied, nil
}

// GetRefundByNo returns one refund by its public number.
func (f *fakeRepository) GetRefundByNo(ctx context.Context, refundNo string) (*domain.Refund, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.refunds {
		if existing.RefundNo == refundNo {
			copied := *existing
			return &copied, nil
		}
	}
	return nil, repository.ErrRefundNotFound
}

// LockRefund returns a refund for update.
func (f *fakeRepository) LockRefund(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
) (*domain.Refund, error) {
	return f.GetRefundByID(ctx, refundID)
}

// ListRefundsByOrder returns every refund of an order.
func (f *fakeRepository) ListRefundsByOrder(
	ctx context.Context,
	orderID string,
) ([]domain.Refund, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	refunds := []domain.Refund{}
	for _, existing := range f.refunds {
		if existing.OrderID == orderID {
			refunds = append(refunds, *existing)
		}
	}
	return refunds, nil
}

// SumRefundCentsByStatus totals the refunds of an order in the given states.
func (f *fakeRepository) SumRefundCentsByStatus(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
	statuses []domain.RefundStatus,
) (domain.Cents, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := domain.Cents(0)
	for _, existing := range f.refunds {
		if existing.OrderID != orderID {
			continue
		}
		for _, status := range statuses {
			if existing.Status == status {
				total += existing.AmountCents
				break
			}
		}
	}
	return total, nil
}

// SumSucceededRefundCents totals only settled refunds.
func (f *fakeRepository) SumSucceededRefundCents(
	ctx context.Context,
	orderID string,
) (domain.Cents, error) {
	return f.SumRefundCentsByStatus(ctx, nil, orderID, []domain.RefundStatus{domain.RefundStatusSucceeded})
}

// ApproveRefund records an operator approval.
func (f *fakeRepository) ApproveRefund(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	operatorAccountID string,
	now time.Time,
) error {
	if strings.TrimSpace(operatorAccountID) == "" {
		return repository.ErrOperatorRequired
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.refunds[refundID]
	if !ok {
		return repository.ErrRefundNotFound
	}
	if existing.Status != domain.RefundStatusRequested {
		return domain.ErrInvalidOrderTransition
	}
	existing.Status = domain.RefundStatusApproved
	existing.OperatorAccountID = operatorAccountID
	existing.ApprovedAt = &now
	return nil
}

// RejectRefund records an operator rejection.
func (f *fakeRepository) RejectRefund(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	operatorAccountID string,
	now time.Time,
) error {
	if strings.TrimSpace(operatorAccountID) == "" {
		return repository.ErrOperatorRequired
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.refunds[refundID]
	if !ok {
		return repository.ErrRefundNotFound
	}
	existing.Status = domain.RefundStatusRejected
	existing.OperatorAccountID = operatorAccountID
	existing.UpdatedAt = now
	return nil
}

// MarkRefundProcessing records a channel call in flight.
func (f *fakeRepository) MarkRefundProcessing(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	channelRefundNo string,
	now time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.refunds[refundID]
	if !ok {
		return repository.ErrRefundNotFound
	}
	existing.Status = domain.RefundStatusProcessing
	if channelRefundNo != "" {
		existing.ChannelRefundNo = channelRefundNo
	}
	existing.UpdatedAt = now
	return nil
}

// MarkRefundSucceeded records a completed refund.
func (f *fakeRepository) MarkRefundSucceeded(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	channelRefundNo string,
	now time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.refunds[refundID]
	if !ok {
		return repository.ErrRefundNotFound
	}
	existing.Status = domain.RefundStatusSucceeded
	if channelRefundNo != "" {
		existing.ChannelRefundNo = channelRefundNo
	}
	existing.CompletedAt = &now
	return nil
}

// MarkRefundFailed records a failed channel call.
func (f *fakeRepository) MarkRefundFailed(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	now time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.refunds[refundID]
	if !ok {
		return repository.ErrRefundNotFound
	}
	existing.Status = domain.RefundStatusFailed
	existing.UpdatedAt = now
	return nil
}

// ApplyOrderRefund adds a refund to an order, refusing to exceed the paid total.
func (f *fakeRepository) ApplyOrderRefund(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
	refundCents domain.Cents,
	now time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.orders[orderID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	if existing.RefundedCents+refundCents > existing.PaidCents {
		return domain.ErrOverRefund
	}
	existing.RefundedCents += refundCents
	if existing.RefundedCents == existing.PaidCents {
		existing.Status = domain.OrderStatusRefunded
	} else {
		existing.Status = domain.OrderStatusPartiallyRefunded
	}
	existing.UpdatedAt = now
	return nil
}

// ===== subscriptions and consents =====

// UpsertActiveSubscription opens or extends a period.
func (f *fakeRepository) UpsertActiveSubscription(
	ctx context.Context,
	tx pgx.Tx,
	subscription domain.Subscription,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.subscriptions {
		if existing.ParentAccountID == subscription.ParentAccountID &&
			existing.PlanCode == subscription.PlanCode &&
			existing.Status == domain.SubscriptionStatusActive {
			if subscription.CurrentPeriodStart.Before(existing.CurrentPeriodStart) {
				existing.CurrentPeriodStart = subscription.CurrentPeriodStart
			}
			if subscription.CurrentPeriodEnd.After(existing.CurrentPeriodEnd) {
				existing.CurrentPeriodEnd = subscription.CurrentPeriodEnd
			}
			existing.SourceOrderID = subscription.SourceOrderID
			return nil
		}
	}
	stored := subscription
	f.subscriptions[subscription.ID] = &stored
	return nil
}

// GetActiveSubscriptions returns the family's live subscriptions.
func (f *fakeRepository) GetActiveSubscriptions(
	ctx context.Context,
	parentAccountID string,
	now time.Time,
) ([]domain.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	subscriptions := []domain.Subscription{}
	for _, existing := range f.subscriptions {
		if existing.ParentAccountID == parentAccountID &&
			existing.Status == domain.SubscriptionStatusActive &&
			existing.CurrentPeriodEnd.After(now) {
			subscriptions = append(subscriptions, *existing)
		}
	}
	return subscriptions, nil
}

// ExpireDueSubscriptions expires subscriptions whose period ended.
func (f *fakeRepository) ExpireDueSubscriptions(ctx context.Context, now time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	expired := 0
	for _, existing := range f.subscriptions {
		if existing.Status == domain.SubscriptionStatusActive &&
			!existing.CurrentPeriodEnd.After(now) {
			existing.Status = domain.SubscriptionStatusExpired
			expired++
		}
	}
	return expired, nil
}

// GrantSpendingConsent stores a consent unless a live one exists.
func (f *fakeRepository) GrantSpendingConsent(
	ctx context.Context,
	consent domain.SpendingConsent,
) (*domain.SpendingConsent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.consents {
		if existing.ParentAccountID == consent.ParentAccountID &&
			existing.ConsentVersion == consent.ConsentVersion &&
			existing.RevokedAt == nil {
			copied := *existing
			return &copied, nil
		}
	}
	stored := consent
	f.consents[consent.ID] = &stored
	copied := stored
	return &copied, nil
}

// RevokeSpendingConsent withdraws a live consent and reports whether one existed.
func (f *fakeRepository) RevokeSpendingConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
	now time.Time,
) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.consents {
		if existing.ParentAccountID == parentAccountID &&
			existing.ConsentVersion == consentVersion &&
			existing.RevokedAt == nil {
			existing.RevokedAt = &now
			return true, nil
		}
	}
	return false, nil
}

// GetActiveSpendingConsent returns the live consent, or nil.
func (f *fakeRepository) GetActiveSpendingConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
) (*domain.SpendingConsent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.consents {
		if existing.ParentAccountID == parentAccountID &&
			existing.ConsentVersion == consentVersion &&
			existing.RevokedAt == nil {
			copied := *existing
			return &copied, nil
		}
	}
	return nil, nil
}

// GetLatestSpendingConsent returns the newest consent row regardless of state.
func (f *fakeRepository) GetLatestSpendingConsent(
	ctx context.Context,
	parentAccountID string,
) (*domain.SpendingConsent, error) {
	return f.GetActiveSpendingConsent(ctx, parentAccountID, SpendingConsentVersion)
}

// ===== reconciliation =====

// CreateRun reserves a reconciliation run, one per channel and day.
func (f *fakeRepository) CreateRun(ctx context.Context, run domain.ReconciliationRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.runs {
		if existing.Channel == run.Channel &&
			existing.BusinessDate.Format(time.DateOnly) == run.BusinessDate.Format(time.DateOnly) {
			return repository.ErrRunAlreadyExists
		}
	}
	stored := run
	f.runs[run.ID] = &stored
	return nil
}

// FinishRun closes a run with its outcome.
func (f *fakeRepository) FinishRun(ctx context.Context, run domain.ReconciliationRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.runs[run.ID]; !ok {
		return repository.ErrRunNotFound
	}
	stored := run
	f.runs[run.ID] = &stored
	return nil
}

// ListRuns returns recorded runs.
func (f *fakeRepository) ListRuns(
	ctx context.Context,
	limit int,
	offset int,
) ([]domain.ReconciliationRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	runs := []domain.ReconciliationRun{}
	for _, existing := range f.runs {
		runs = append(runs, *existing)
	}
	return paginate(runs, limit, offset), nil
}

// GetLatestRun returns the newest run of a channel.
func (f *fakeRepository) GetLatestRun(
	ctx context.Context,
	channelValue domain.PaymentChannel,
) (*domain.ReconciliationRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *domain.ReconciliationRun
	for _, existing := range f.runs {
		if existing.Channel != channelValue {
			continue
		}
		copied := *existing
		if latest == nil || copied.BusinessDate.After(latest.BusinessDate) {
			latest = &copied
		}
	}
	if latest == nil {
		return nil, repository.ErrRunNotFound
	}
	return latest, nil
}

// InsertItem stores one reconciliation item.
func (f *fakeRepository) InsertItem(
	ctx context.Context,
	tx pgx.Tx,
	item domain.ReconciliationItem,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[item.RunID] = append(f.items[item.RunID], item)
	return nil
}

// ListItems returns a run's items.
func (f *fakeRepository) ListItems(
	ctx context.Context,
	runID string,
	unresolvedOnly bool,
) ([]domain.ReconciliationItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := []domain.ReconciliationItem{}
	for _, existing := range f.items[runID] {
		if unresolvedOnly && existing.Resolved {
			continue
		}
		items = append(items, existing)
	}
	return items, nil
}

// MarkItemResolved marks a discrepancy investigated.
func (f *fakeRepository) MarkItemResolved(
	ctx context.Context,
	itemID string,
	resolved bool,
	now time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for runID, items := range f.items {
		for index := range items {
			if items[index].ID != itemID {
				continue
			}
			items[index].Resolved = resolved
			items[index].UpdatedAt = now
			f.items[runID] = items
			return nil
		}
	}
	return repository.ErrItemNotFound
}

// ListPaymentsForReconciliation returns the platform's settled payments.
func (f *fakeRepository) ListPaymentsForReconciliation(
	ctx context.Context,
	channelValue domain.PaymentChannel,
	from time.Time,
	to time.Time,
) ([]repository.SettledPayment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]repository.SettledPayment(nil), f.settled...), nil
}

// GetOrderStatusesByNumbers returns order statuses by number.
func (f *fakeRepository) GetOrderStatusesByNumbers(
	ctx context.Context,
	orderNos []string,
) (map[string]domain.OrderStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	statuses := map[string]domain.OrderStatus{}
	for _, orderNo := range orderNos {
		if orderID, ok := f.orderByNo[orderNo]; ok {
			statuses[orderNo] = f.orders[orderID].Status
		}
	}
	return statuses, nil
}

// ===== quota ledger =====

// AppendQuotaLedgerEntry appends one signed movement, once per order or refund.
func (f *fakeRepository) AppendQuotaLedgerEntry(
	ctx context.Context,
	tx pgx.Tx,
	entry domain.QuotaLedgerEntry,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.ledger {
		if entry.OrderID != "" && existing.OrderID == entry.OrderID &&
			existing.EntryType == domain.QuotaEntryPurchaseGrant {
			return repository.ErrAlreadySettled
		}
		if entry.RefundID != "" && existing.RefundID == entry.RefundID &&
			existing.EntryType == domain.QuotaEntryRefundClawback {
			return repository.ErrAlreadySettled
		}
	}
	f.ledger = append(f.ledger, entry)
	return nil
}

// LockQuotaLedgerEntry returns one ledger row for update.
func (f *fakeRepository) LockQuotaLedgerEntry(
	ctx context.Context,
	tx pgx.Tx,
	entryID string,
) (*domain.QuotaLedgerEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.ledger {
		if existing.ID == entryID {
			copied := existing
			return &copied, nil
		}
	}
	return nil, repository.ErrLedgerEntryNotFound
}

// MarkQuotaLedgerGrantState rewrites the grant state of a pending entry.
func (f *fakeRepository) MarkQuotaLedgerGrantState(
	ctx context.Context,
	tx repository.Querier,
	entryID string,
	state string,
	now time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range f.ledger {
		if f.ledger[index].ID != entryID {
			continue
		}
		current, _ := f.ledger[index].Detail[repository.LedgerDetailGrantState].(string)
		if current != repository.LedgerGrantStatePendingProvider {
			return repository.ErrAlreadySettled
		}
		f.ledger[index].Detail[repository.LedgerDetailGrantState] = state
		f.ledgerState[entryID] = state
		return nil
	}
	return repository.ErrLedgerEntryNotFound
}

// ListQuotaLedger returns one family's movements.
func (f *fakeRepository) ListQuotaLedger(
	ctx context.Context,
	parentAccountID string,
	limit int,
	offset int,
) ([]domain.QuotaLedgerEntry, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries := []domain.QuotaLedgerEntry{}
	for _, existing := range f.ledger {
		if existing.ParentAccountID == parentAccountID {
			entries = append(entries, existing)
		}
	}
	return paginate(entries, limit, offset), len(entries), nil
}

// SumQuotaLedgerForParent totals a family's movements.
func (f *fakeRepository) SumQuotaLedgerForParent(
	ctx context.Context,
	parentAccountID string,
) (repository.QuotaLedgerTotal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := repository.QuotaLedgerTotal{}
	sum := new(big.Int)
	for _, existing := range f.ledger {
		if existing.ParentAccountID != parentAccountID {
			continue
		}
		amount, err := repository.ParseUSDMicroUnits(existing.AmountUSD)
		if err != nil {
			return repository.QuotaLedgerTotal{}, err
		}
		sum.Add(sum, amount)
		total.EntryCount++
		total.LatestBalanceAfterUSD = existing.BalanceAfterUSD
	}
	if total.EntryCount > 0 {
		total.SumAmountUSD = repository.FormatUSDMicroUnits(sum)
	}
	return total, nil
}

// LatestObservedProviderBalance returns the newest provider-synced balance.
func (f *fakeRepository) LatestObservedProviderBalance(
	ctx context.Context,
	parentAccountID string,
) (string, bool, error) {
	return "", false, nil
}

// GetSpendingLimit returns the stored monthly cap, mirroring the Postgres
// repository's "no row means not found" contract.
func (f *fakeRepository) GetSpendingLimit(
	ctx context.Context,
	familyID string,
) (*domain.FamilySpendingLimit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	limit, ok := f.spendingLimits[familyID]
	if !ok {
		return nil, repository.ErrSpendingLimitNotFound
	}
	copied := *limit
	return &copied, nil
}

// UpsertSpendingLimit stores the cap, keeping the first created_at exactly as
// the ON CONFLICT clause does.
func (f *fakeRepository) UpsertSpendingLimit(
	ctx context.Context,
	limit domain.FamilySpendingLimit,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.spendingLimits[limit.FamilyID]; ok {
		limit.CreatedAt = existing.CreatedAt
	}
	copied := limit
	f.spendingLimits[limit.FamilyID] = &copied
	return nil
}

// SumMonthlyPaidCents sums the payable amount of the family's paid orders in
// the half-open window.
func (f *fakeRepository) SumMonthlyPaidCents(
	ctx context.Context,
	familyID string,
	from time.Time,
	to time.Time,
) (domain.Cents, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := domain.Cents(0)
	for _, order := range f.orders {
		if order.ParentAccountID != familyID || order.PaidAt == nil {
			continue
		}
		paidAt := *order.PaidAt
		if paidAt.Before(from) || !paidAt.Before(to) {
			continue
		}
		switch order.Status {
		case domain.OrderStatusPaid,
			domain.OrderStatusPartiallyRefunded,
			domain.OrderStatusRefunded:
			total += order.PayableCents
		}
	}
	return total, nil
}

// AppendSpendingLimitRejection records one refused checkout.
func (f *fakeRepository) AppendSpendingLimitRejection(
	ctx context.Context,
	rejection domain.SpendingLimitRejection,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spendingRejections = append(f.spendingRejections, rejection)
	return nil
}

// ListSpendingLimitRejections returns the family's refusals in the window,
// newest first.
func (f *fakeRepository) ListSpendingLimitRejections(
	ctx context.Context,
	familyID string,
	from time.Time,
	to time.Time,
	limit int,
	offset int,
) ([]domain.SpendingLimitRejection, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	matching := make([]domain.SpendingLimitRejection, 0)
	for _, rejection := range f.spendingRejections {
		if rejection.FamilyID != familyID {
			continue
		}
		if rejection.CreatedAt.Before(from) || !rejection.CreatedAt.Before(to) {
			continue
		}
		matching = append(matching, rejection)
	}
	sort.Slice(matching, func(left int, right int) bool {
		return matching[left].CreatedAt.After(matching[right].CreatedAt)
	})
	total := len(matching)
	if offset >= total {
		return []domain.SpendingLimitRejection{}, total, nil
	}
	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}
	return matching[offset:end], total, nil
}

// SpendingLimitMetrics reports the configured family count and the current
// month's rejection count, matching the repository query.
func (f *fakeRepository) SpendingLimitMetrics(
	ctx context.Context,
) (repository.SpendingLimitMetrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	metrics := repository.SpendingLimitMetrics{}
	for _, limit := range f.spendingLimits {
		if limit.MonthlyLimitCents > 0 {
			metrics.ConfiguredFamilyCount++
		}
	}
	return metrics, nil
}

// SpendingLimitReadiness reports the fake store as readable.
func (f *fakeRepository) SpendingLimitReadiness(ctx context.Context) error {
	return nil
}
