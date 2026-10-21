// Package service owns billing business rules: what a guardian may buy, when an
// order may be paid, what a payment callback is allowed to credit, how much may
// be refunded, and how the AI quota a purchase bought is granted.
//
// Boundary: the package never writes SQL. It depends on repository.Repository
// for storage and on channel.Channel for provider calls, and it takes every
// money decision inside a single transaction so an order, its payment, its
// subscription, and its quota grant move together or not at all.
//
// Money: every CNY amount is domain.Cents (int64 分). The AI quota is a decimal
// string in the provider's USD unit and is never converted to or from cents.
package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrIdempotencyKeyRequired rejects a checkout without a retry key: without
	// one a double tap would create two orders.
	ErrIdempotencyKeyRequired = errors.New("billing: idempotency key is required")
	// ErrInvalidRequest rejects a malformed request before any state changes.
	ErrInvalidRequest = errors.New("billing: invalid request")
	// ErrPaymentNotOpenYet reports that the requested channel cannot take money
	// yet. It wraps domain.ErrChannelNotConfigured and is what the handler
	// renders as "payment is not open yet".
	ErrPaymentNotOpenYet = errors.New("billing: payment is not open yet")
	// ErrChannelUnavailable reports that the provider answer was not usable, so
	// nothing was charged and nothing was credited.
	ErrChannelUnavailable = errors.New("billing: payment channel call failed")
	// ErrOrderNotCancellable rejects a cancel for an order that is no longer
	// pending; a paid order moves through a refund instead.
	ErrOrderNotCancellable = errors.New("billing: only a pending order can be cancelled")
	// ErrRefundNotRequestable rejects a refund for an order that is not paid.
	ErrRefundNotRequestable = errors.New("billing: order is not refundable")
	// ErrOperatorRequired rejects an administrative money action without an
	// administrator identity.
	ErrOperatorRequired = errors.New("billing: operator account is required")
	// ErrNotificationFailed reports a well-formed callback saying the payment
	// did not succeed. Nothing is credited.
	ErrNotificationFailed = errors.New("billing: payment notification reports a failed payment")
	// ErrNotificationStillProcessing reports a redelivery of a callback whose
	// first delivery has not finished, so the provider should retry later.
	ErrNotificationStillProcessing = errors.New("billing: payment notification is still processing")
	// ErrNotificationRejected reports a verified callback that settled nothing:
	// the order was missing, already settled, or its amount did not match.
	ErrNotificationRejected = errors.New("billing: payment notification was not credited")
	// ErrReconciliationUnavailable reports that the channel statement could not
	// be downloaded, so the day stays unverified rather than balanced.
	ErrReconciliationUnavailable = errors.New("billing: channel statement unavailable")
	// ErrStatementEmpty reports an empty statement for a day with no platform
	// settlement either: a zero-row answer proves nothing and must not read as
	// "all matched".
	ErrStatementEmpty = errors.New("billing: channel statement is empty")
	// ErrStatementNotBalanced reports a statement whose entries do not add up to
	// its reported total.
	ErrStatementNotBalanced = errors.New("billing: channel statement total does not match its entries")
	// ErrRefundNotCompletable rejects completion of a refund no operator
	// approved; money must not move on an unreviewed request.
	ErrRefundNotCompletable = errors.New("billing: refund is not approved for completion")
)

// SpendingConsentVersion is the terms version stored with a guardian consent.
//
// Consent is evidence: the version identifies which text a guardian agreed to,
// so changing the policy text needs a new version rather than silently reusing
// the old row.
const SpendingConsentVersion = "2026-10-v1"

// ConsentMethodAppConfirm is the only confirmation channel the schema allows.
const ConsentMethodAppConfirm = "app_confirm"

// OrderPaymentWindow is how long a guardian has to pay before the order expires
// (design doc §5.1). It bounds how long a price can be held.
const OrderPaymentWindow = 30 * time.Minute

// AIQuotaGrafter is the seam to the AI gateway's quota.
//
// The billing package must not import the ai_gateway module: the gateway owns
// the provider balance and its transport, and a direct import would point the
// dependency arrow the wrong way. The composition root adapts the gateway
// service to this interface. A nil implementation means no quota service is
// wired up, which the service records honestly (see QuotaLedger) instead of
// skipping.
type AIQuotaGrafter interface {
	GrantQuota(ctx context.Context, parentAccountID string, amountUSD float64, reason string) error
	ClawBackQuota(ctx context.Context, parentAccountID string, amountUSD float64, reason string) error
}

// AuditRecorder records billing actions for the operator audit trail.
//
// It is a one-method seam for the same reason as AIQuotaGrafter: billing should
// not import the audit module, and a nil recorder means the deployment records
// audit elsewhere.
type AuditRecorder interface {
	RecordBillingAudit(ctx context.Context, event BillingAuditEvent) error
}

// BillingAuditEvent is one billing action recorded for operators.
type BillingAuditEvent struct {
	// Action is a stable dotted identifier such as "billing.payment.accepted".
	Action          string
	ActorAccountID  string
	ParentAccountID string
	OrderNo         string
	RefundNo        string
	// Detail holds non-secret context only: no payload, key, or certificate.
	Detail    map[string]any
	CreatedAt time.Time
}

// ChannelLookup resolves a registered provider by name.
type ChannelLookup interface {
	Get(name string) (channel.Channel, bool)
}

// Options contains the dependencies every billing service shares.
type Options struct {
	Repository repository.Repository
	Channels   ChannelLookup
	Quota      AIQuotaGrafter
	Audit      AuditRecorder
	Clock      clock.Clock
	// PaymentEndpoint is the public base URL providers call back on, such as
	// "https://api.example.com". Empty leaves the callback URL to the provider
	// adapter's own configuration.
	PaymentEndpoint string
}

// base carries the shared dependencies and the small helpers every billing
// service uses.
type base struct {
	repository      repository.Repository
	channels        ChannelLookup
	quota           AIQuotaGrafter
	audit           AuditRecorder
	clock           clock.Clock
	paymentEndpoint string
}

// newBase validates the required dependencies and applies defaults.
func newBase(options Options) (base, error) {
	if options.Repository == nil {
		return base{}, errors.New("billing repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return base{
		repository:      options.Repository,
		channels:        options.Channels,
		quota:           options.Quota,
		audit:           options.Audit,
		clock:           timeSource,
		paymentEndpoint: strings.TrimRight(strings.TrimSpace(options.PaymentEndpoint), "/"),
	}, nil
}

// now returns the current time in UTC so stored timestamps compare across
// replicas regardless of host timezone.
func (b base) now() time.Time {
	return b.clock.Now().UTC()
}

// recordAudit writes one audit event. Audit failure never fails a completed
// money movement: the movement already committed, and reporting it as failed
// would invite a retry that credits twice.
func (b base) recordAudit(ctx context.Context, event BillingAuditEvent) {
	if b.audit == nil {
		return
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = b.now()
	}
	_ = b.audit.RecordBillingAudit(ctx, event)
}

// withTx runs fn in one transaction so a settlement and every side effect it
// writes commit or roll back together.
func (b base) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return b.repository.WithTx(ctx, fn)
}

// paymentCallbackURL builds the provider callback endpoint for a channel, or ""
// when no public endpoint is configured and the adapter must supply its own.
func (b base) paymentCallbackURL(channelName string) string {
	if b.paymentEndpoint == "" {
		return ""
	}
	return b.paymentEndpoint + "/api/v1/billing/notifications/" + channelName
}

// resolveChannel returns the registered provider for name.
func (b base) resolveChannel(name string) (channel.Channel, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return nil, invalidRequest("channel is required")
	}
	if b.channels == nil {
		return nil, notConfigured(nil)
	}
	provider, exists := b.channels.Get(normalized)
	if !exists {
		return nil, notConfigured(nil)
	}
	return provider, nil
}

// invalidRequest wraps a request-validation message with ErrInvalidRequest.
func invalidRequest(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, arguments...))
}

// notOpenYet wraps a request-validation message with ErrPaymentNotOpenYet.
func notOpenYet(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrPaymentNotOpenYet, fmt.Sprintf(format, arguments...))
}

// notConfigured wraps a provider-absence failure as ErrPaymentNotOpenYet so the
// handler renders a guardian-friendly message instead of a provider error.
func notConfigured(err error) error {
	if err == nil {
		return ErrPaymentNotOpenYet
	}
	return joinSentinel(ErrPaymentNotOpenYet, err)
}

// isChannelNotConfigured reports whether any layer said the provider is absent.
//
// Both the channel package and the domain package define the sentinel by
// design: the channel layer compiles without importing the domain. Either one
// means "not configured", never success.
func isChannelNotConfigured(err error) bool {
	return errors.Is(err, domain.ErrChannelNotConfigured) ||
		errors.Is(err, channel.ErrChannelNotConfigured)
}

// isDuplicateNotification reports a callback already recorded under the same
// provider id, from either sentinel.
func isDuplicateNotification(err error) bool {
	return errors.Is(err, domain.ErrDuplicateNotification) ||
		errors.Is(err, channel.ErrDuplicateNotification)
}

// isSignatureInvalid reports a callback whose signature did not verify.
func isSignatureInvalid(err error) bool {
	return errors.Is(err, domain.ErrSignatureInvalid) ||
		errors.Is(err, channel.ErrSignatureInvalid)
}

// joinSentinel returns an error that matches both sentinels.
func joinSentinel(primary error, detail error) error {
	return errors.Join(primary, detail)
}

// quotaToMicroUnits parses a provider quota amount into exact 1e-6 USD units.
func quotaToMicroUnits(text string) (*big.Int, error) {
	return repository.ParseUSDMicroUnits(text)
}

// quotaUSD converts exact quota units to the float the AIQuotaGrafter seam
// takes.
//
// The seam is fixed at float64 by the gateway's own API, so the conversion
// happens only at that call; every stored amount stays an exact decimal string.
// A value float64 cannot represent is refused rather than rounded, because a
// rounded grant would make the ledger disagree with the provider balance.
func quotaUSD(units *big.Int) (float64, bool) {
	return repository.USDMicroUnitsAsFloat(units)
}

// Service is the composition-root facade: it owns the seven billing services
// and exposes one method per use case, so the HTTP transport depends on a
// single value instead of holding seven pointers of its own.
//
// Every method forwards to the owning service; this type adds no business rule.
// It is safe for concurrent use because every embedded service is.
type Service struct {
	repository     repository.Repository
	Consents       *SpendingConsentService
	Plans          *PlanService
	Orders         *OrderService
	Payments       *PaymentService
	Refunds        *RefundService
	Reconciliation *ReconciliationService
	Expiry         *ExpiryService
	QuotaLedger    *QuotaLedgerService
	SpendingLimits *SpendingLimitService
}

// New builds every billing service over one set of Options.
//
// The payment and refund services build their own quota ledger from the same
// options, so the ledger constructor is only called here to fail fast on a
// missing repository before any service is handed out.
func New(options Options) (*Service, error) {
	consents, err := NewSpendingConsentService(options)
	if err != nil {
		return nil, fmt.Errorf("create spending consent service: %w", err)
	}
	plans, err := NewPlanService(options)
	if err != nil {
		return nil, fmt.Errorf("create plan service: %w", err)
	}
	orders, err := NewOrderService(options)
	if err != nil {
		return nil, fmt.Errorf("create order service: %w", err)
	}
	spendingLimits, err := NewSpendingLimitService(options)
	if err != nil {
		return nil, fmt.Errorf("create spending limit service: %w", err)
	}
	// The checkout path enforces the family cap, so the order service must hold
	// the same instance the guardian routes write through. Wiring it here keeps
	// one enforcement object instead of a copy per transport.
	orders.spendingLimits = spendingLimits
	quotaLedger, err := NewQuotaLedgerService(options)
	if err != nil {
		return nil, fmt.Errorf("create quota ledger service: %w", err)
	}
	payments, err := NewPaymentService(options)
	if err != nil {
		return nil, fmt.Errorf("create payment service: %w", err)
	}
	refunds, err := NewRefundService(options)
	if err != nil {
		return nil, fmt.Errorf("create refund service: %w", err)
	}
	reconciliation, err := NewReconciliationService(options)
	if err != nil {
		return nil, fmt.Errorf("create reconciliation service: %w", err)
	}
	expiry, err := NewExpiryService(options)
	if err != nil {
		return nil, fmt.Errorf("create expiry service: %w", err)
	}
	return &Service{
		repository:     options.Repository,
		Consents:       consents,
		Plans:          plans,
		Orders:         orders,
		Payments:       payments,
		Refunds:        refunds,
		Reconciliation: reconciliation,
		Expiry:         expiry,
		QuotaLedger:    quotaLedger,
		SpendingLimits: spendingLimits,
	}, nil
}

// ListActivePlans returns the plans a guardian may buy.
func (s *Service) ListActivePlans(ctx context.Context) ([]domain.Plan, error) {
	return s.Plans.ListActivePlans(ctx)
}

// ListPlans returns a page of plans for the operator console.
func (s *Service) ListPlans(
	ctx context.Context,
	status domain.PlanStatus,
	search string,
	page Page,
) ([]domain.Plan, int, error) {
	return s.Plans.ListPlans(ctx, status, search, page)
}

// CreatePlan inserts one plan.
func (s *Service) CreatePlan(ctx context.Context, input PlanInput) (*domain.Plan, error) {
	return s.Plans.CreatePlan(ctx, input)
}

// UpdatePlan saves one plan edit under optimistic concurrency.
func (s *Service) UpdatePlan(
	ctx context.Context,
	planID string,
	expectedVersion int,
	input PlanInput,
) (*domain.Plan, error) {
	return s.Plans.UpdatePlan(ctx, planID, expectedVersion, input)
}

// CreateOrder starts one guardian checkout.
func (s *Service) CreateOrder(
	ctx context.Context,
	request CreateOrderRequest,
) (CreateOrderResult, error) {
	return s.Orders.CreateOrder(ctx, request)
}

// GetOrderForParent returns one order that must belong to the calling family.
func (s *Service) GetOrderForParent(
	ctx context.Context,
	parentAccountID string,
	orderNo string,
) (*domain.Order, error) {
	return s.Orders.GetOrderForParent(ctx, parentAccountID, orderNo)
}

// ListOrdersForParent returns one family's orders with the total count.
func (s *Service) ListOrdersForParent(
	ctx context.Context,
	parentAccountID string,
	page Page,
) ([]domain.Order, int, error) {
	return s.Orders.ListOrdersForParent(ctx, parentAccountID, page)
}

// ListOrders returns a page of orders for the operator console.
func (s *Service) ListOrders(
	ctx context.Context,
	filter repository.OrderFilter,
) ([]domain.Order, int, error) {
	return s.Orders.ListOrders(ctx, filter)
}

// CancelOrder cancels a pending order of the calling family.
func (s *Service) CancelOrder(
	ctx context.Context,
	parentAccountID string,
	orderNo string,
) (*domain.Order, error) {
	return s.Orders.CancelOrder(ctx, parentAccountID, orderNo)
}

// FindOrderByNumber returns one order by its public number without a family
// scope. Only the operator console uses it: a guardian route must always go
// through GetOrderForParent so ownership is checked.
func (s *Service) FindOrderByNumber(ctx context.Context, orderNo string) (*domain.Order, error) {
	return s.repository.GetOrderByOrderNo(ctx, strings.TrimSpace(orderNo))
}

// StartPayment opens one payment attempt for a pending order.
func (s *Service) StartPayment(
	ctx context.Context,
	parentAccountID string,
	orderNo string,
	channelName string,
) (StartPaymentResult, error) {
	return s.Payments.StartPayment(ctx, parentAccountID, orderNo, channelName)
}

// HandleNotification verifies, records, and settles one provider callback.
func (s *Service) HandleNotification(
	ctx context.Context,
	channelName string,
	request channel.NotificationRequest,
) (NotificationOutcome, error) {
	return s.Payments.HandleNotification(ctx, channelName, request)
}

// RequestRefund records a guardian refund request.
func (s *Service) RequestRefund(
	ctx context.Context,
	request RequestRefundRequest,
) (*domain.Refund, error) {
	return s.Refunds.RequestRefund(ctx, request)
}

// ApproveRefund approves a requested refund in the operator's name.
func (s *Service) ApproveRefund(
	ctx context.Context,
	operatorAccountID string,
	refundNo string,
) (*domain.Refund, error) {
	return s.Refunds.ApproveRefund(ctx, operatorAccountID, refundNo)
}

// RejectRefund rejects a refund that has not moved money yet.
func (s *Service) RejectRefund(
	ctx context.Context,
	operatorAccountID string,
	refundNo string,
) (*domain.Refund, error) {
	return s.Refunds.RejectRefund(ctx, operatorAccountID, refundNo)
}

// GetSpendingLimit returns one family's monthly cap with this month's spend.
func (s *Service) GetSpendingLimit(
	ctx context.Context,
	familyID string,
) (SpendingLimitView, error) {
	return s.SpendingLimits.GetLimit(ctx, familyID)
}

// SetSpendingLimit stores a guardian-set monthly cap for one family.
func (s *Service) SetSpendingLimit(
	ctx context.Context,
	familyID string,
	actorAccountID string,
	monthlyLimitCents domain.Cents,
) (SpendingLimitView, error) {
	return s.SpendingLimits.SetLimit(ctx, familyID, actorAccountID, monthlyLimitCents)
}

// ListSpendingLimitRejections returns one family's over-limit refusals for the
// calendar month containing moment (the current month when zero).
func (s *Service) ListSpendingLimitRejections(
	ctx context.Context,
	familyID string,
	moment time.Time,
	page Page,
) ([]domain.SpendingLimitRejection, int, MonthPeriod, error) {
	return s.SpendingLimits.ListRejections(ctx, familyID, moment, page)
}

// GetActiveConsent returns the family's live spending consent, or nil.
func (s *Service) GetActiveConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
) (*domain.SpendingConsent, error) {
	return s.Consents.GetActive(ctx, parentAccountID, consentVersion)
}

// GrantConsent records the guardian's spending consent.
func (s *Service) GrantConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
	clientIP string,
) (*domain.SpendingConsent, error) {
	return s.Consents.GrantConsent(ctx, parentAccountID, consentVersion, clientIP)
}

// RevokeConsent withdraws the guardian's spending consent.
func (s *Service) RevokeConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
) (bool, error) {
	return s.Consents.RevokeConsent(ctx, parentAccountID, consentVersion)
}

// RunReconciliation compares one channel business day.
func (s *Service) RunReconciliation(
	ctx context.Context,
	channelName string,
	businessDate time.Time,
) (*domain.ReconciliationRun, error) {
	return s.Reconciliation.RunReconciliation(ctx, channelName, businessDate)
}

// ListReconciliationRuns returns the recorded reconciliation runs.
func (s *Service) ListReconciliationRuns(
	ctx context.Context,
	page Page,
) ([]domain.ReconciliationRun, error) {
	return s.Reconciliation.ListRuns(ctx, page)
}

// ListReconciliationItems returns one run's discrepancy items.
func (s *Service) ListReconciliationItems(
	ctx context.Context,
	runID string,
	unresolvedOnly bool,
) ([]domain.ReconciliationItem, error) {
	return s.Reconciliation.ListItems(ctx, runID, unresolvedOnly)
}
