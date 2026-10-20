package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
)

// fixedClock is a clock the tests control, so an expiry decision is a fact and
// not a race against wall time.
type fixedClock struct {
	now time.Time
}

// Now returns the current test time.
func (c *fixedClock) Now() time.Time { return c.now }

// advance moves the clock forward.
func (c *fixedClock) advance(duration time.Duration) { c.now = c.now.Add(duration) }

// registry is the channel lookup the services use, without the real adapters.
type registry struct {
	channels map[string]channel.Channel
}

// Get returns a registered channel.
func (r registry) Get(name string) (channel.Channel, bool) {
	value, ok := r.channels[name]
	return value, ok
}

// testHarness wires the services to one fake store and one scripted clock.
type testHarness struct {
	repository *fakeRepository
	clock      *fixedClock
	channels   registry
	orders     *OrderService
	plans      *PlanService
	payments   *PaymentService
	refunds    *RefundService
	consents   *SpendingConsentService
	quota      *QuotaLedgerService
	expiry     *ExpiryService
	reconcile  *ReconciliationService
}

// quotaRecorder captures the provider-side quota calls, so a test can prove the
// gateway was or was not asked for something.
type quotaRecorder struct {
	grants    []float64
	clawbacks []float64
	failWith  error
}

// GrantQuota records one grant.
func (r *quotaRecorder) GrantQuota(ctx context.Context, parentAccountID string, amountUSD float64, reason string) error {
	if r.failWith != nil {
		return r.failWith
	}
	r.grants = append(r.grants, amountUSD)
	return nil
}

// ClawBackQuota records one clawback.
func (r *quotaRecorder) ClawBackQuota(ctx context.Context, parentAccountID string, amountUSD float64, reason string) error {
	if r.failWith != nil {
		return r.failWith
	}
	r.clawbacks = append(r.clawbacks, amountUSD)
	return nil
}

// grantTotal sums every granted amount.
func (r *quotaRecorder) grantTotal() float64 {
	total := 0.0
	for _, amount := range r.grants {
		total += amount
	}
	return total
}

// newHarness builds a service set over a fake store; quota may be nil.
func newHarness(t *testing.T, quota AIQuotaGrafter, channels ...channel.Channel) *testHarness {
	t.Helper()
	store := newFakeRepository()
	clockValue := &fixedClock{now: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	registered := registry{channels: map[string]channel.Channel{}}
	for _, provider := range channels {
		registered.channels[provider.Name()] = provider
	}
	options := Options{
		Repository:      store,
		Channels:        registered,
		Quota:           quota,
		Clock:           clockValue,
		PaymentEndpoint: "https://api.example.test",
	}
	harness := &testHarness{repository: store, clock: clockValue, channels: registered}
	var err error
	if harness.orders, err = NewOrderService(options); err != nil {
		t.Fatalf("build order service: %v", err)
	}
	if harness.plans, err = NewPlanService(options); err != nil {
		t.Fatalf("build plan service: %v", err)
	}
	if harness.payments, err = NewPaymentService(options); err != nil {
		t.Fatalf("build payment service: %v", err)
	}
	if harness.refunds, err = NewRefundService(options); err != nil {
		t.Fatalf("build refund service: %v", err)
	}
	if harness.consents, err = NewSpendingConsentService(options); err != nil {
		t.Fatalf("build consent service: %v", err)
	}
	if harness.quota, err = NewQuotaLedgerService(options); err != nil {
		t.Fatalf("build quota service: %v", err)
	}
	if harness.expiry, err = NewExpiryService(options); err != nil {
		t.Fatalf("build expiry service: %v", err)
	}
	if harness.reconcile, err = NewReconciliationService(options); err != nil {
		t.Fatalf("build reconciliation service: %v", err)
	}
	return harness
}

// seedPlan inserts an active plan a test can buy.
func (h *testHarness) seedPlan(t *testing.T, code string, priceCents int64, quotaUSD string) domain.Plan {
	t.Helper()
	return h.seedPlanWithValidity(t, code, priceCents, quotaUSD, 0)
}

// seedPlanWithValidity inserts an active plan that bounds its quota grant.
//
// A zero validityDays means the grant never expires, which is what every other
// test relies on so an unrelated assertion is not disturbed by a deadline.
func (h *testHarness) seedPlanWithValidity(
	t *testing.T,
	code string,
	priceCents int64,
	quotaUSD string,
	validityDays int,
) domain.Plan {
	t.Helper()
	plan := domain.Plan{
		ID:                "plan-" + code,
		Code:              code,
		DisplayName:       "初芽年度陪伴",
		BillingPeriod:     domain.BillingPeriodYearly,
		PeriodMonths:      12,
		PriceCents:        domain.Cents(priceCents),
		AIQuotaUSD:        quotaUSD,
		QuotaValidityDays: validityDays,
		Status:            domain.PlanStatusActive,
		SortOrder:         1,
		Version:           1,
		CreatedAt:         h.clock.now,
		UpdatedAt:         h.clock.now,
	}
	h.repository.mu.Lock()
	h.repository.plans[plan.ID] = &plan
	h.repository.mu.Unlock()
	return plan
}

// grantConsent records a live spending consent for the family.
func (h *testHarness) grantConsent(t *testing.T, parentAccountID string) {
	t.Helper()
	if _, err := h.consents.GrantConsent(context.Background(), parentAccountID, "", "127.0.0.1"); err != nil {
		t.Fatalf("grant consent: %v", err)
	}
}

// createOrder creates and returns a pending order for the family.
func (h *testHarness) createOrder(t *testing.T, parentAccountID, planCode string) domain.Order {
	t.Helper()
	result, err := h.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: parentAccountID,
		PlanCode:        planCode,
		IdempotencyKey:  "key-" + planCode,
		ClientIP:        "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	return result.Order
}

// ===== orders =====

// TestCreateOrderRequiresSpendingConsent proves a family without a guardian
// consent cannot create an order at all.
func TestCreateOrderRequiresSpendingConsent(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")

	_, err := harness.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: "parent-1",
		PlanCode:        "sprout_year",
		IdempotencyKey:  "key-1",
	})
	if !errors.Is(err, domain.ErrSpendingConsentRequired) {
		t.Fatalf("expected ErrSpendingConsentRequired, got %v", err)
	}
	harness.repository.mu.Lock()
	orderCount := len(harness.repository.orders)
	harness.repository.mu.Unlock()
	if orderCount != 0 {
		t.Fatalf("expected no order to be created, found %d", orderCount)
	}
}

// TestCreateOrderIsIdempotent proves a retried checkout returns the original
// order instead of creating a second one.
func TestCreateOrderIsIdempotent(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")

	first := harness.createOrder(t, "parent-1", "sprout_year")
	second, err := harness.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: "parent-1",
		PlanCode:        "sprout_year",
		IdempotencyKey:  "key-sprout_year",
	})
	if err != nil {
		t.Fatalf("retried create order: %v", err)
	}
	if second.Order.ID != first.ID {
		t.Fatalf("expected the original order %s, got %s", first.ID, second.Order.ID)
	}
	if !second.Reused {
		t.Fatal("expected the retry to report a reused order")
	}
	harness.repository.mu.Lock()
	orderCount := len(harness.repository.orders)
	harness.repository.mu.Unlock()
	if orderCount != 1 {
		t.Fatalf("expected exactly one order, found %d", orderCount)
	}
}

// TestCreateOrderSnapshotsPlanFields proves the order keeps the plan values it
// was sold at, so a later price change cannot rewrite history.
func TestCreateOrderSnapshotsPlanFields(t *testing.T) {
	harness := newHarness(t, nil)
	plan := harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	if order.UnitPriceCents != plan.PriceCents ||
		order.PayableCents != plan.PriceCents ||
		order.AIQuotaUSD != plan.AIQuotaUSD ||
		order.PlanDisplayName != plan.DisplayName ||
		order.PeriodMonths != plan.PeriodMonths {
		t.Fatalf("order did not snapshot the plan: %+v", order)
	}
	if order.ExpiresAt == nil || !order.ExpiresAt.Equal(harness.clock.now.Add(OrderPaymentWindow)) {
		t.Fatalf("expected a %s payment window, got %v", OrderPaymentWindow, order.ExpiresAt)
	}
}

// TestCreateOrderRejectsInactivePlan proves an archived plan cannot be bought.
func TestCreateOrderRejectsInactivePlan(t *testing.T) {
	harness := newHarness(t, nil)
	plan := harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	harness.repository.mu.Lock()
	harness.repository.plans[plan.ID].Status = domain.PlanStatusArchived
	harness.repository.mu.Unlock()

	_, err := harness.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: "parent-1",
		PlanCode:        "sprout_year",
		IdempotencyKey:  "key-1",
	})
	if !errors.Is(err, domain.ErrPlanNotActive) {
		t.Fatalf("expected ErrPlanNotActive, got %v", err)
	}
}

// TestCreateOrderRequiresIdempotencyKey proves the client must supply a retry key,
// because without one a retried checkout cannot be told apart from a new sale.
func TestCreateOrderRequiresIdempotencyKey(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")

	_, err := harness.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: "parent-1",
		PlanCode:        "sprout_year",
	})
	if !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("expected ErrIdempotencyKeyRequired, got %v", err)
	}
}

// TestGetOrderForParentCrossFamily proves reading another family's order is
// refused as a security violation rather than reported as a missing order.
func TestGetOrderForParentCrossFamily(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	harness.grantConsent(t, "parent-2")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	owner, err := harness.orders.GetOrderForParent(context.Background(), "parent-1", order.OrderNo)
	if err != nil {
		t.Fatalf("owner read: %v", err)
	}
	if owner.ID != order.ID {
		t.Fatalf("expected order %s, got %s", order.ID, owner.ID)
	}

	_, err = harness.orders.GetOrderForParent(context.Background(), "parent-2", order.OrderNo)
	if !errors.Is(err, domain.ErrCrossFamilyAccess) {
		t.Fatalf("expected ErrCrossFamilyAccess, got %v", err)
	}
}

// TestCancelOrderRefusesForeignOrder proves cancellation is scoped to the family
// that owns the order.
func TestCancelOrderRefusesForeignOrder(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	harness.grantConsent(t, "parent-2")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	if _, err := harness.orders.CancelOrder(context.Background(), "parent-2", order.OrderNo); !errors.Is(err, domain.ErrCrossFamilyAccess) {
		t.Fatalf("expected ErrCrossFamilyAccess, got %v", err)
	}
	if _, err := harness.orders.CancelOrder(context.Background(), "parent-1", order.OrderNo); err != nil {
		t.Fatalf("owner cancel: %v", err)
	}
}

// ===== plans =====

// TestUpdatePlanStaleVersionConflicts proves two operators editing the same plan
// cannot silently overwrite each other.
func TestUpdatePlanStaleVersionConflicts(t *testing.T) {
	harness := newHarness(t, nil)
	plan := harness.seedPlan(t, "sprout_year", 19900, "20.000000")

	updated, err := harness.plans.UpdatePlan(context.Background(), plan.ID, 1, PlanInput{
		DisplayName:        "初芽年度陪伴",
		BillingPeriod:      domain.BillingPeriodYearly,
		PeriodMonths:       12,
		PriceCents:         29900,
		OriginalPriceCents: 39900,
		AIQuotaUSD:         "30.000000",
		Status:             domain.PlanStatusActive,
	})
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if updated.PriceCents != 29900 || updated.Version != 2 {
		t.Fatalf("unexpected updated plan: %+v", updated)
	}

	_, err = harness.plans.UpdatePlan(context.Background(), plan.ID, 1, PlanInput{
		DisplayName:   "初芽年度陪伴",
		BillingPeriod: domain.BillingPeriodYearly,
		PeriodMonths:  12,
		PriceCents:    19900,
		AIQuotaUSD:    "20.000000",
	})
	if !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict, got %v", err)
	}
}

// TestUpdatePlanRejectsCodeChange proves the stable plan code cannot be renamed,
// because order snapshots and subscriptions refer to it.
func TestUpdatePlanRejectsCodeChange(t *testing.T) {
	harness := newHarness(t, nil)
	plan := harness.seedPlan(t, "sprout_year", 19900, "20.000000")

	_, err := harness.plans.UpdatePlan(context.Background(), plan.ID, 1, PlanInput{
		Code:          "sprout_year_v2",
		DisplayName:   "初芽年度陪伴",
		BillingPeriod: domain.BillingPeriodYearly,
		PeriodMonths:  12,
		PriceCents:    19900,
		AIQuotaUSD:    "20.000000",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

// TestListActivePlansHidesDraftsAndArchived proves the guardian catalogue only
// shows plans that are actually purchasable.
func TestListActivePlansHidesDraftsAndArchived(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	draft := harness.seedPlan(t, "sprout_month", 2900, "3.000000")
	archived := harness.seedPlan(t, "sprout_legacy", 9900, "9.000000")
	harness.repository.mu.Lock()
	harness.repository.plans[draft.ID].Status = domain.PlanStatusDraft
	harness.repository.plans[archived.ID].Status = domain.PlanStatusArchived
	harness.repository.mu.Unlock()

	plans, err := harness.plans.ListActivePlans(context.Background())
	if err != nil {
		t.Fatalf("list active plans: %v", err)
	}
	if len(plans) != 1 || plans[0].Code != "sprout_year" {
		t.Fatalf("expected only sprout_year, got %+v", plans)
	}
}

// ===== payments: notification settlement =====

// TestHandleNotificationDeliveredThreeTimesCreditsOnce is the central
// idempotency proof: the same signed callback arriving three times must settle
// the order once and grant the quota once.
func TestHandleNotificationDeliveredThreeTimesCreditsOnce(t *testing.T) {
	recorder := &quotaRecorder{}
	verifier := testVerifier(t)
	harness := newHarness(t, recorder, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	request := signedNotification(t, verifier, "test-secret", notificationPayload{
		NotificationID: "notify-1",
		ChannelTradeNo: "trade-1",
		OrderNo:        order.OrderNo,
		AmountCents:    19900,
		PaidAt:         harness.clock.now,
		Success:        true,
	})

	for attempt := 1; attempt <= 3; attempt++ {
		outcome, err := harness.payments.HandleNotification(context.Background(), "wechat", request)
		if err != nil {
			t.Fatalf("delivery %d: %v", attempt, err)
		}
		if attempt == 1 && outcome.Result != domain.NotificationResultAccepted {
			t.Fatalf("first delivery result = %s, want accepted", outcome.Result)
		}
		if attempt > 1 && !outcome.Duplicate {
			t.Fatalf("delivery %d should have been reported as a duplicate", attempt)
		}
	}

	harness.repository.mu.Lock()
	stored := harness.repository.orders[order.ID]
	ledgerCount := len(harness.repository.ledger)
	harness.repository.mu.Unlock()
	if stored.Status != domain.OrderStatusPaid {
		t.Fatalf("order status = %s, want paid", stored.Status)
	}
	if stored.PaidCents != 19900 {
		t.Fatalf("paid = %s, want 199.00", stored.PaidCents.Format())
	}
	if ledgerCount != 1 {
		t.Fatalf("quota ledger entries = %d, want exactly 1", ledgerCount)
	}
	if got := recorder.grantTotal(); got != 20 {
		t.Fatalf("granted quota = %v USD, want 20 exactly once", got)
	}
}

// TestHandleNotificationRejectsSignatureWithoutCrediting proves a forged
// callback is recorded as signature_invalid and credits nothing.
func TestHandleNotificationRejectsSignatureWithoutCrediting(t *testing.T) {
	recorder := &quotaRecorder{}
	verifier := testVerifier(t)
	harness := newHarness(t, recorder, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	request := signedNotification(t, verifier, "wrong-secret", notificationPayload{
		NotificationID: "notify-forged",
		ChannelTradeNo: "trade-forged",
		OrderNo:        order.OrderNo,
		AmountCents:    19900,
		PaidAt:         harness.clock.now,
		Success:        true,
	})

	outcome, err := harness.payments.HandleNotification(context.Background(), "wechat", request)
	if err == nil {
		t.Fatal("expected a forged signature to be rejected")
	}
	if outcome.Result != domain.NotificationResultSignatureInvalid {
		t.Fatalf("result = %s, want signature_invalid", outcome.Result)
	}
	if !errors.Is(err, ErrNotificationRejected) {
		t.Fatalf("expected ErrNotificationRejected, got %v", err)
	}
	if len(recorder.grants) != 0 {
		t.Fatalf("a forged callback granted quota: %v", recorder.grants)
	}
	harness.repository.mu.Lock()
	stored := harness.repository.orders[order.ID]
	ledger := harness.repository.notifications
	harness.repository.mu.Unlock()
	if stored.Status != domain.OrderStatusPending {
		t.Fatalf("order status = %s, want pending", stored.Status)
	}
	recorded := false
	for _, notification := range ledger {
		if notification.Result == domain.NotificationResultSignatureInvalid && !notification.SignatureVerified {
			recorded = true
		}
	}
	if !recorded {
		t.Fatal("expected a signature_invalid notification row to be recorded")
	}
}

// TestHandleNotificationAmountMismatchCreditsNothing proves a callback paying a
// different amount is recorded as amount_mismatch and settles nothing.
func TestHandleNotificationAmountMismatchCreditsNothing(t *testing.T) {
	recorder := &quotaRecorder{}
	verifier := testVerifier(t)
	harness := newHarness(t, recorder, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	request := signedNotification(t, verifier, "test-secret", notificationPayload{
		NotificationID: "notify-wrong-amount",
		ChannelTradeNo: "trade-wrong",
		OrderNo:        order.OrderNo,
		AmountCents:    100,
		PaidAt:         harness.clock.now,
		Success:        true,
	})

	outcome, err := harness.payments.HandleNotification(context.Background(), "wechat", request)
	if err == nil {
		t.Fatal("expected an amount mismatch to be refused")
	}
	if outcome.Result != domain.NotificationResultAmountMismatch {
		t.Fatalf("result = %s, want amount_mismatch", outcome.Result)
	}
	if !errors.Is(err, domain.ErrAmountMismatch) {
		t.Fatalf("expected ErrAmountMismatch, got %v", err)
	}
	if len(recorder.grants) != 0 {
		t.Fatalf("an amount mismatch granted quota: %v", recorder.grants)
	}
	harness.repository.mu.Lock()
	stored := harness.repository.orders[order.ID]
	ledgerCount := len(harness.repository.ledger)
	harness.repository.mu.Unlock()
	if stored.Status != domain.OrderStatusPending || stored.PaidCents != 0 {
		t.Fatalf("order was credited: status=%s paid=%s", stored.Status, stored.PaidCents.Format())
	}
	if ledgerCount != 0 {
		t.Fatalf("ledger entries = %d, want 0", ledgerCount)
	}
}

// TestHandleNotificationAfterExpiryCreditsNothing proves a callback that loses
// the race against the expiry sweep does not pay an expired order.
func TestHandleNotificationAfterExpiryCreditsNothing(t *testing.T) {
	recorder := &quotaRecorder{}
	verifier := testVerifier(t)
	harness := newHarness(t, recorder, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	harness.clock.advance(OrderPaymentWindow + time.Minute)
	if _, err := harness.expiry.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("expire orders: %v", err)
	}

	request := signedNotification(t, verifier, "test-secret", notificationPayload{
		NotificationID: "notify-late",
		ChannelTradeNo: "trade-late",
		OrderNo:        order.OrderNo,
		AmountCents:    19900,
		PaidAt:         harness.clock.now,
		Success:        true,
	})
	outcome, err := harness.payments.HandleNotification(context.Background(), "wechat", request)
	if err == nil {
		t.Fatal("expected a callback on an expired order to be refused")
	}
	if outcome.Result != domain.NotificationResultOrderNotFound {
		t.Fatalf("result = %s, want order_not_found", outcome.Result)
	}
	if len(recorder.grants) != 0 {
		t.Fatalf("an expired order granted quota: %v", recorder.grants)
	}
	harness.repository.mu.Lock()
	stored := harness.repository.orders[order.ID]
	harness.repository.mu.Unlock()
	if stored.Status != domain.OrderStatusExpired {
		t.Fatalf("order status = %s, want expired", stored.Status)
	}
}

// TestHandleNotificationSettlementIsAtomic proves a quota grant failure rolls
// back the whole settlement: no paid order, no ledger row, no acceptance.
func TestHandleNotificationSettlementIsAtomic(t *testing.T) {
	recorder := &quotaRecorder{failWith: errors.New("quota service is down")}
	verifier := testVerifier(t)
	harness := newHarness(t, recorder, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	request := signedNotification(t, verifier, "test-secret", notificationPayload{
		NotificationID: "notify-grant-fails",
		ChannelTradeNo: "trade-grant",
		OrderNo:        order.OrderNo,
		AmountCents:    19900,
		PaidAt:         harness.clock.now,
		Success:        true,
	})
	if _, err := harness.payments.HandleNotification(context.Background(), "wechat", request); err == nil {
		t.Fatal("expected the settlement to fail when the quota grant fails")
	}

	harness.repository.mu.Lock()
	stored := harness.repository.orders[order.ID]
	ledgerCount := len(harness.repository.ledger)
	refundsSettled := 0
	for _, payment := range harness.repository.payments {
		if payment.Status == domain.PaymentStatusSucceeded {
			refundsSettled++
		}
	}
	harness.repository.mu.Unlock()
	if stored.Status != domain.OrderStatusPending {
		t.Fatalf("order status = %s, want pending after rollback", stored.Status)
	}
	if ledgerCount != 0 {
		t.Fatalf("ledger entries = %d, want 0 after rollback", ledgerCount)
	}
	if refundsSettled != 0 {
		t.Fatalf("successful payments = %d, want 0 after rollback", refundsSettled)
	}
}

// TestHandleNotificationRenewsSubscription proves a periodic plan opens a
// subscription for the bought period.
func TestHandleNotificationRenewsSubscription(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	request := signedNotification(t, verifier, "test-secret", notificationPayload{
		NotificationID: "notify-sub",
		ChannelTradeNo: "trade-sub",
		OrderNo:        order.OrderNo,
		AmountCents:    19900,
		PaidAt:         harness.clock.now,
		Success:        true,
	})
	if _, err := harness.payments.HandleNotification(context.Background(), "wechat", request); err != nil {
		t.Fatalf("notification: %v", err)
	}
	subscriptions, err := harness.repository.GetActiveSubscriptions(context.Background(), "parent-1", harness.clock.now)
	if err != nil {
		t.Fatalf("get subscriptions: %v", err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("subscriptions = %d, want 1", len(subscriptions))
	}
	wantEnd := harness.clock.now.AddDate(0, 12, 0)
	if !subscriptions[0].CurrentPeriodEnd.Equal(wantEnd) {
		t.Fatalf("period end = %s, want %s", subscriptions[0].CurrentPeriodEnd, wantEnd)
	}
}

// TestHandleNotificationWithoutQuotaServiceRecordsPendingGrant proves the
// honest fallback: with no quota service the purchase is still booked, and the
// ledger says the provider balance was not raised.
func TestHandleNotificationWithoutQuotaServiceRecordsPendingGrant(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	request := signedNotification(t, verifier, "test-secret", notificationPayload{
		NotificationID: "notify-no-quota",
		ChannelTradeNo: "trade-no-quota",
		OrderNo:        order.OrderNo,
		AmountCents:    19900,
		PaidAt:         harness.clock.now,
		Success:        true,
	})
	if _, err := harness.payments.HandleNotification(context.Background(), "wechat", request); err != nil {
		t.Fatalf("notification: %v", err)
	}
	harness.repository.mu.Lock()
	defer harness.repository.mu.Unlock()
	if len(harness.repository.ledger) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(harness.repository.ledger))
	}
	entry := harness.repository.ledger[0]
	if entry.AmountUSD != "20.000000" {
		t.Fatalf("ledger amount = %s, want 20.000000", entry.AmountUSD)
	}
	if state, _ := entry.Detail[repository.LedgerDetailGrantState].(string); state != repository.LedgerGrantStatePendingProvider {
		t.Fatalf("grant state = %q, want %q", state, repository.LedgerGrantStatePendingProvider)
	}
	if unbacked, _ := entry.Detail[repository.LedgerDetailBalanceAfterUnbacked].(bool); !unbacked {
		t.Fatal("expected the entry to be marked as not backed by a provider balance")
	}
}

// TestPurchaseGrantCarriesQuotaExpiryFromThePlanWindow proves the validity
// window reaches the ledger.
//
// The deadline is what later lets the expiry sweep write off unspent quota. If
// the grant row carried no deadline, a monthly plan's allowance would roll over
// forever and the expiry entry could never be produced.
func TestPurchaseGrantCarriesQuotaExpiryFromThePlanWindow(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, newTestChannel(t, verifier))
	harness.seedPlanWithValidity(t, "sprout_month", 3900, "5.000000", 30)
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_month")

	request := signedNotification(t, verifier, "test-secret", notificationPayload{
		NotificationID: "notify-expiry",
		ChannelTradeNo: "trade-no-expiry",
		OrderNo:        order.OrderNo,
		AmountCents:    3900,
		PaidAt:         harness.clock.now,
		Success:        true,
	})
	if _, err := harness.payments.HandleNotification(context.Background(), "wechat", request); err != nil {
		t.Fatalf("notification: %v", err)
	}
	harness.repository.mu.Lock()
	defer harness.repository.mu.Unlock()
	if len(harness.repository.ledger) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(harness.repository.ledger))
	}
	entry := harness.repository.ledger[0]
	if entry.QuotaExpiresAt == nil {
		t.Fatal("expected the purchase grant to carry a quota expiry")
	}
	// 30 days on top of the payment instant, not a fixed calendar date.
	wantExpiry := harness.clock.now.AddDate(0, 0, 30)
	if !entry.QuotaExpiresAt.Equal(wantExpiry) {
		t.Fatalf("quota expiry = %s, want %s", entry.QuotaExpiresAt, wantExpiry)
	}
}

// TestPurchaseGrantWithoutValidityWindowNeverExpires proves a plan that states
// no window does not quietly expire the quota a guardian just paid for.
func TestPurchaseGrantWithoutValidityWindowNeverExpires(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, newTestChannel(t, verifier))
	harness.seedPlanWithValidity(t, "sprout_forever", 9900, "9.000000", 0)
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_forever")

	request := signedNotification(t, verifier, "test-secret", notificationPayload{
		NotificationID: "notify-forever",
		ChannelTradeNo: "trade-no-forever",
		OrderNo:        order.OrderNo,
		AmountCents:    9900,
		PaidAt:         harness.clock.now,
		Success:        true,
	})
	if _, err := harness.payments.HandleNotification(context.Background(), "wechat", request); err != nil {
		t.Fatalf("notification: %v", err)
	}
	harness.repository.mu.Lock()
	defer harness.repository.mu.Unlock()
	if len(harness.repository.ledger) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(harness.repository.ledger))
	}
	if harness.repository.ledger[0].QuotaExpiresAt != nil {
		t.Fatalf("quota expiry = %v, want nil for a plan with no window",
			*harness.repository.ledger[0].QuotaExpiresAt)
	}
}

// TestStartPaymentLeavesOrderPendingWhenChannelUnconfigured proves a missing
// provider fails only the payment attempt, so the guardian can retry the same
// order once the channel is configured.
//
// The order must stay pending: cancelling or failing it here would destroy a
// purchase the guardian still intends to make.
func TestStartPaymentLeavesOrderPendingWhenChannelUnconfigured(t *testing.T) {
	harness := newHarness(t, nil, unconfiguredChannel(t))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	if _, err := harness.payments.StartPayment(context.Background(), "parent-1", order.OrderNo, "wechat"); err == nil {
		t.Fatal("expected an unconfigured channel to fail the payment attempt")
	}

	harness.repository.mu.Lock()
	defer harness.repository.mu.Unlock()
	stored := harness.repository.orders[order.ID]
	if stored.Status != domain.OrderStatusPending {
		t.Fatalf("order status = %s, want pending so payment can be retried", stored.Status)
	}
	if stored.CancelledAt != nil {
		t.Fatal("a failed payment attempt must not cancel the order")
	}
	if stored.ClosedAt != nil {
		t.Fatal("a failed payment attempt must not close the order")
	}
	if len(harness.repository.payments) != 1 {
		t.Fatalf("payment attempts = %d, want 1", len(harness.repository.payments))
	}
	for _, payment := range harness.repository.payments {
		if payment.Status != domain.PaymentStatusFailed {
			t.Fatalf("payment status = %s, want failed", payment.Status)
		}
	}
}

// TestStartPaymentRetrySucceedsOnceChannelConfigured proves the pending order is
// genuinely retryable: the same order can start a payment after the channel is
// configured, which is why its status must survive the earlier failure.
func TestStartPaymentRetrySucceedsOnceChannelConfigured(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, unconfiguredChannel(t))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	if _, err := harness.payments.StartPayment(context.Background(), "parent-1", order.OrderNo, "wechat"); err == nil {
		t.Fatal("expected the first attempt to fail while the channel is unconfigured")
	}

	// The operator configures the channel; the same order must now be payable.
	harness.channels.channels["wechat"] = newTestChannel(t, verifier)
	payments, err := NewPaymentService(Options{
		Repository:      harness.repository,
		Channels:        harness.channels,
		Clock:           harness.clock,
		PaymentEndpoint: "https://api.example.test",
	})
	if err != nil {
		t.Fatalf("rebuild payment service: %v", err)
	}
	result, err := payments.StartPayment(context.Background(), "parent-1", order.OrderNo, "wechat")
	if err != nil {
		t.Fatalf("retried payment after configuring the channel: %v", err)
	}
	if result.Payload["prepay_id"] == "" {
		t.Fatal("expected the retry to return the provider payload")
	}
	harness.repository.mu.Lock()
	defer harness.repository.mu.Unlock()
	if harness.repository.orders[order.ID].Status != domain.OrderStatusPending {
		t.Fatalf("order status = %s, want still pending until the callback settles it",
			harness.repository.orders[order.ID].Status)
	}
}

// TestStartPaymentReportsChannelNotOpen proves an unconfigured channel fails
// loudly and marks the attempt failed instead of looking ready.
func TestStartPaymentReportsChannelNotOpen(t *testing.T) {
	harness := newHarness(t, nil, unconfiguredChannel(t))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	_, err := harness.payments.StartPayment(context.Background(), "parent-1", order.OrderNo, "wechat")
	if !errors.Is(err, ErrPaymentNotOpenYet) {
		t.Fatalf("expected ErrPaymentNotOpenYet, got %v", err)
	}
	if !errors.Is(err, channel.ErrChannelNotConfigured) {
		t.Fatalf("expected the channel sentinel to survive wrapping, got %v", err)
	}
	harness.repository.mu.Lock()
	defer harness.repository.mu.Unlock()
	for _, payment := range harness.repository.payments {
		if payment.Status != domain.PaymentStatusFailed {
			t.Fatalf("payment status = %s, want failed", payment.Status)
		}
		if payment.FailureCode != "channel_not_configured" {
			t.Fatalf("failure code = %q, want channel_not_configured", payment.FailureCode)
		}
	}
}

// TestStartPaymentRefusesForeignOrder proves starting a payment is scoped to the
// owning family.
func TestStartPaymentRefusesForeignOrder(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	harness.grantConsent(t, "parent-2")
	order := harness.createOrder(t, "parent-1", "sprout_year")

	_, err := harness.payments.StartPayment(context.Background(), "parent-2", order.OrderNo, "wechat")
	if !errors.Is(err, domain.ErrCrossFamilyAccess) {
		t.Fatalf("expected ErrCrossFamilyAccess, got %v", err)
	}
}

// ===== refunds =====

// TestRefundTotalCannotExceedPaid proves the over-refund guard rejects a second
// refund beyond the paid total, both at request and at approval time.
func TestRefundTotalCannotExceedPaid(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")
	harness.payOrder(t, verifier, order)

	first, err := harness.refunds.RequestRefund(context.Background(), RequestRefundRequest{
		ParentAccountID: "parent-1",
		OrderNo:         order.OrderNo,
		AmountCents:     15000,
		Reason:          "重复购买",
	})
	if err != nil {
		t.Fatalf("first refund request: %v", err)
	}
	if _, err := harness.refunds.ApproveRefund(context.Background(), "operator-1", first.RefundNo); err != nil {
		t.Fatalf("approve first refund: %v", err)
	}

	// 199.00 paid, 150.00 already approved: 60.00 more is 10.00 too much.
	_, err = harness.refunds.RequestRefund(context.Background(), RequestRefundRequest{
		ParentAccountID: "parent-1",
		OrderNo:         order.OrderNo,
		AmountCents:     6000,
		Reason:          "再次退款",
	})
	if !errors.Is(err, domain.ErrOverRefund) {
		t.Fatalf("expected ErrOverRefund, got %v", err)
	}
	// The exact remainder is still allowed.
	if _, err := harness.refunds.RequestRefund(context.Background(), RequestRefundRequest{
		ParentAccountID: "parent-1",
		OrderNo:         order.OrderNo,
		AmountCents:     4900,
		Reason:          "差额退款",
	}); err != nil {
		t.Fatalf("refund of the remaining balance: %v", err)
	}
}

// TestRefundRequestRequiresOperatorForApproval proves approval and rejection are
// operator actions and refuse to proceed anonymously.
func TestRefundRequestRequiresOperatorForApproval(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")
	harness.payOrder(t, verifier, order)

	refund, err := harness.refunds.RequestRefund(context.Background(), RequestRefundRequest{
		ParentAccountID: "parent-1",
		OrderNo:         order.OrderNo,
		AmountCents:     5000,
		Reason:          "孩子误触购买",
	})
	if err != nil {
		t.Fatalf("request refund: %v", err)
	}
	if _, err := harness.refunds.ApproveRefund(context.Background(), "  ", refund.RefundNo); !errors.Is(err, ErrOperatorRequired) {
		t.Fatalf("expected ErrOperatorRequired, got %v", err)
	}
	if _, err := harness.refunds.RejectRefund(context.Background(), "", refund.RefundNo); !errors.Is(err, ErrOperatorRequired) {
		t.Fatalf("expected ErrOperatorRequired, got %v", err)
	}
	harness.repository.mu.Lock()
	stored := harness.repository.refunds[refund.ID]
	harness.repository.mu.Unlock()
	if stored.Status != domain.RefundStatusRequested {
		t.Fatalf("refund status = %s, want requested", stored.Status)
	}
}

// TestCompleteRefundWithUnconfiguredChannelChangesNothing proves an unconfigured
// channel surfaces an error and leaves the refund approved rather than pretending
// the money moved.
func TestCompleteRefundWithUnconfiguredChannelChangesNothing(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")
	harness.payOrder(t, verifier, order)
	refund := harness.requestAndApproveRefund(t, order, 5000)

	// Replace the working channel with an unconfigured one for the money step.
	harness.channels.channels["wechat"] = unconfiguredChannel(t)
	rebuild := Options{
		Repository: harness.repository,
		Channels:   harness.channels,
		Clock:      harness.clock,
	}
	refunds, err := NewRefundService(rebuild)
	if err != nil {
		t.Fatalf("rebuild refund service: %v", err)
	}

	if _, err := refunds.CompleteRefund(context.Background(), refund.RefundNo); !errors.Is(err, ErrPaymentNotOpenYet) {
		t.Fatalf("expected ErrPaymentNotOpenYet, got %v", err)
	}
	harness.repository.mu.Lock()
	defer harness.repository.mu.Unlock()
	stored := harness.repository.refunds[refund.ID]
	if stored.Status == domain.RefundStatusSucceeded {
		t.Fatal("an unconfigured channel must not report a completed refund")
	}
	storedOrder := harness.repository.orders[order.ID]
	if storedOrder.RefundedCents != 0 {
		t.Fatalf("refunded = %s, want 0", storedOrder.RefundedCents.Format())
	}
}

// TestCompleteRefundMarksOrderRefunded proves a completed refund moves the order
// to refunded and claws the quota back proportionally.
func TestCompleteRefundMarksOrderRefunded(t *testing.T) {
	recorder := &quotaRecorder{}
	verifier := testVerifier(t)
	harness := newHarness(t, recorder, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	order := harness.createOrder(t, "parent-1", "sprout_year")
	harness.payOrder(t, verifier, order)
	refund := harness.requestAndApproveRefund(t, order, 9950)

	completed, err := harness.refunds.CompleteRefund(context.Background(), refund.RefundNo)
	if err != nil {
		t.Fatalf("complete refund: %v", err)
	}
	if completed.Status != domain.RefundStatusSucceeded {
		t.Fatalf("refund status = %s, want succeeded", completed.Status)
	}
	harness.repository.mu.Lock()
	storedOrder := harness.repository.orders[order.ID]
	harness.repository.mu.Unlock()
	if storedOrder.Status != domain.OrderStatusPartiallyRefunded {
		t.Fatalf("order status = %s, want partially_refunded", storedOrder.Status)
	}
	// Half of 20.00 USD bought for 199.00 CNY, so 99.50 CNY returns 10.00 USD.
	if len(recorder.clawbacks) != 1 {
		t.Fatalf("clawbacks = %v, want exactly one", recorder.clawbacks)
	}
	if recorder.clawbacks[0] != -10 {
		t.Fatalf("clawback = %v, want -10", recorder.clawbacks[0])
	}
}

// ===== consents =====

// TestSpendingConsentRevokeStopsCheckout proves withdrawal is immediate: a
// revoked consent blocks the next checkout.
func TestSpendingConsentRevokeStopsCheckout(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")

	active, err := harness.consents.IsActive(context.Background(), "parent-1", "")
	if err != nil {
		t.Fatalf("is active: %v", err)
	}
	if !active {
		t.Fatal("expected the granted consent to be active")
	}
	if revoked, err := harness.consents.RevokeConsent(context.Background(), "parent-1", ""); err != nil || !revoked {
		t.Fatalf("revoke consent: revoked=%v err=%v", revoked, err)
	}
	if active, err := harness.consents.IsActive(context.Background(), "parent-1", ""); err != nil || active {
		t.Fatalf("expected the consent to be inactive: active=%v err=%v", active, err)
	}
	_, err = harness.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: "parent-1",
		PlanCode:        "sprout_year",
		IdempotencyKey:  "key-after-revoke",
	})
	if !errors.Is(err, domain.ErrSpendingConsentRequired) {
		t.Fatalf("expected ErrSpendingConsentRequired after revoke, got %v", err)
	}
}

// ===== expiry =====

// TestExpireOrdersSkipsSettledOrders proves the sweep only closes pending orders.
func TestExpireOrdersSkipsSettledOrders(t *testing.T) {
	verifier := testVerifier(t)
	harness := newHarness(t, nil, newTestChannel(t, verifier))
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")

	paidOrder := harness.createOrder(t, "parent-1", "sprout_year")
	harness.payOrder(t, verifier, paidOrder)
	pendingOrder, err := harness.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: "parent-1",
		PlanCode:        "sprout_year",
		IdempotencyKey:  "key-pending",
	})
	if err != nil {
		t.Fatalf("create second order: %v", err)
	}

	harness.clock.advance(OrderPaymentWindow + time.Minute)
	result, err := harness.expiry.ExpireOrders(context.Background())
	if err != nil {
		t.Fatalf("expire orders: %v", err)
	}
	if len(result.ExpiredOrderIDs) != 1 {
		t.Fatalf("expired %d orders, want 1", len(result.ExpiredOrderIDs))
	}
	harness.repository.mu.Lock()
	defer harness.repository.mu.Unlock()
	if harness.repository.orders[paidOrder.ID].Status != domain.OrderStatusPaid {
		t.Fatalf("paid order status = %s, want paid", harness.repository.orders[paidOrder.ID].Status)
	}
	if harness.repository.orders[pendingOrder.Order.ID].Status != domain.OrderStatusExpired {
		t.Fatalf("pending order status = %s, want expired",
			harness.repository.orders[pendingOrder.Order.ID].Status)
	}
}

// ===== reconciliation =====

// TestReconciliationUnconfiguredChannelRecordsFailure proves an unconfigured
// channel records a failed run and never reports a balanced day.
func TestReconciliationUnconfiguredChannelRecordsFailure(t *testing.T) {
	harness := newHarness(t, nil, unconfiguredChannel(t))
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)

	run, err := harness.reconcile.RunReconciliation(context.Background(), "wechat", day)
	if !errors.Is(err, ErrReconciliationUnavailable) {
		t.Fatalf("expected ErrReconciliationUnavailable, got %v", err)
	}
	if run == nil {
		t.Fatal("expected the failed run to be recorded")
	}
	if run.Status != domain.ReconciliationStatusFailed {
		t.Fatalf("run status = %s, want failed", run.Status)
	}
	if run.FinishedAt == nil {
		t.Fatal("expected a failed run to carry a finished_at time")
	}
	if strings.TrimSpace(run.ErrorMessage) == "" {
		t.Fatal("expected a failed run to explain why")
	}
	if run.Status == domain.ReconciliationStatusCompleted {
		t.Fatal("an unconfigured channel must never report a completed run")
	}
	harness.repository.mu.Lock()
	defer harness.repository.mu.Unlock()
	stored := harness.repository.runs[run.ID]
	if stored == nil || stored.Status != domain.ReconciliationStatusFailed {
		t.Fatal("expected the failed run to be persisted")
	}
	if len(harness.repository.items[run.ID]) != 0 {
		t.Fatal("a failed run must not record reconciliation items")
	}
}

// TestReconciliationClassifiesDiscrepancies proves the matcher reports matched,
// amount_mismatch, platform_only and channel_only separately.
func TestReconciliationClassifiesDiscrepancies(t *testing.T) {
	scripted := newScriptedChannel()
	scripted.statement = channel.Statement{
		BusinessDate: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
		Entries: []channel.StatementEntry{
			{ChannelTradeNo: "trade-matched", OrderNo: "SP1", AmountCents: 19900},
			{ChannelTradeNo: "trade-short", OrderNo: "SP2", AmountCents: 9900},
			{ChannelTradeNo: "trade-extra", OrderNo: "SP3", AmountCents: 2900},
		},
	}
	harness := newHarness(t, nil, scripted)
	harness.repository.settled = []repository.SettledPayment{
		{ChannelTradeNo: "trade-matched", OrderNo: "SP1", AmountCents: 19900},
		{ChannelTradeNo: "trade-short", OrderNo: "SP2", AmountCents: 19900},
		{ChannelTradeNo: "trade-missing", OrderNo: "SP4", AmountCents: 4900},
	}

	run, err := harness.reconcile.RunReconciliation(
		context.Background(), "wechat", time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("run reconciliation: %v", err)
	}
	if run.Status != domain.ReconciliationStatusCompleted {
		t.Fatalf("run status = %s, want completed", run.Status)
	}
	if run.MatchedCount != 1 {
		t.Fatalf("matched = %d, want 1", run.MatchedCount)
	}
	if run.MissingCount != 1 {
		t.Fatalf("platform_only = %d, want 1", run.MissingCount)
	}
	if run.ExtraCount != 1 {
		t.Fatalf("channel_only = %d, want 1", run.ExtraCount)
	}
	// 199.00 + 99.00 + 29.00 channel against 199.00 + 199.00 + 49.00 platform.
	if run.ChannelTotalCents != 32700 {
		t.Fatalf("channel total = %s, want 327.00", run.ChannelTotalCents.Format())
	}
	if run.PlatformTotalCents != 44700 {
		t.Fatalf("platform total = %s, want 447.00", run.PlatformTotalCents.Format())
	}
	if run.DifferenceCents != -12000 {
		t.Fatalf("difference = %s, want -120.00", run.DifferenceCents.Format())
	}

	items, err := harness.reconcile.ListItems(context.Background(), run.ID, false)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	classification := map[string]domain.ReconciliationDiscrepancy{}
	for _, item := range items {
		classification[item.ChannelTradeNo] = item.Discrepancy
	}
	expected := map[string]domain.ReconciliationDiscrepancy{
		"trade-matched": domain.ReconciliationMatched,
		"trade-short":   domain.ReconciliationAmountMismatch,
		"trade-extra":   domain.ReconciliationChannelOnly,
		"trade-missing": domain.ReconciliationPlatformOnly,
	}
	for tradeNo, want := range expected {
		if classification[tradeNo] != want {
			t.Fatalf("%s classified as %q, want %q", tradeNo, classification[tradeNo], want)
		}
	}
}

// TestReconciliationRefusesEmptyStatement proves an empty statement is recorded
// as a failure rather than as a balanced day.
func TestReconciliationRefusesEmptyStatement(t *testing.T) {
	harness := newHarness(t, nil, newScriptedChannel())

	run, err := harness.reconcile.RunReconciliation(
		context.Background(), "wechat", time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
	)
	if !errors.Is(err, ErrStatementEmpty) {
		t.Fatalf("expected ErrStatementEmpty, got %v", err)
	}
	if run.Status != domain.ReconciliationStatusFailed {
		t.Fatalf("run status = %s, want failed", run.Status)
	}
}

// TestReconciliationRefusesDuplicateDay proves one business day is reconciled
// once per channel.
func TestReconciliationRefusesDuplicateDay(t *testing.T) {
	scripted := newScriptedChannel()
	scripted.statement = channel.Statement{
		Entries: []channel.StatementEntry{
			{ChannelTradeNo: "trade-1", OrderNo: "SP1", AmountCents: 19900},
		},
	}
	harness := newHarness(t, nil, scripted)
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	if _, err := harness.reconcile.RunReconciliation(context.Background(), "wechat", day); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := harness.reconcile.RunReconciliation(context.Background(), "wechat", day); !errors.Is(err, repository.ErrRunAlreadyExists) {
		t.Fatalf("expected ErrRunAlreadyExists, got %v", err)
	}
}

// ===== helpers =====

// payOrder settles an order through a real signed callback.
func (h *testHarness) payOrder(t *testing.T, verifier *channel.HMACVerifier, order domain.Order) {
	t.Helper()
	request := signedNotification(t, verifier, "test-secret", notificationPayload{
		NotificationID: "notify-" + order.OrderNo,
		ChannelTradeNo: "trade-" + order.OrderNo,
		OrderNo:        order.OrderNo,
		AmountCents:    int64(order.PayableCents),
		PaidAt:         h.clock.now,
		Success:        true,
	})
	if _, err := h.payments.HandleNotification(context.Background(), "wechat", request); err != nil {
		t.Fatalf("settle order %s: %v", order.OrderNo, err)
	}
}

// requestAndApproveRefund creates and approves one refund.
func (h *testHarness) requestAndApproveRefund(t *testing.T, order domain.Order, amountCents int64) *domain.Refund {
	t.Helper()
	refund, err := h.refunds.RequestRefund(context.Background(), RequestRefundRequest{
		ParentAccountID: order.ParentAccountID,
		OrderNo:         order.OrderNo,
		AmountCents:     domain.Cents(amountCents),
		Reason:          "孩子误触购买",
	})
	if err != nil {
		t.Fatalf("request refund: %v", err)
	}
	approved, err := h.refunds.ApproveRefund(context.Background(), "operator-1", refund.RefundNo)
	if err != nil {
		t.Fatalf("approve refund: %v", err)
	}
	return approved
}
