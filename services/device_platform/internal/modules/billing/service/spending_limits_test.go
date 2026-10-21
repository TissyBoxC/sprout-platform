package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
)

// seedMonthlySpend records a paid order for the family at a fixed instant, which
// is what the monthly total is derived from.
func (h *testHarness) seedMonthlySpend(
	t *testing.T,
	parentAccountID string,
	payableCents int64,
	paidAt time.Time,
) domain.Order {
	t.Helper()
	orderNo := "SP20261020ABCDEFGH"
	order := domain.Order{
		ID:              "order-" + orderNo + paidAt.Format("150405"),
		OrderNo:         orderNo,
		ParentAccountID: parentAccountID,
		PlanID:          "plan-seed",
		PlanCode:        "seed_plan",
		PlanDisplayName: "历史订单",
		BillingPeriod:   domain.BillingPeriodMonthly,
		PeriodMonths:    1,
		UnitPriceCents:  domain.Cents(payableCents),
		PayableCents:    domain.Cents(payableCents),
		PaidCents:       domain.Cents(payableCents),
		Status:          domain.OrderStatusPaid,
		IdempotencyKey:  "seed-" + paidAt.Format("150405"),
		PaidAt:          &paidAt,
		CreatedAt:       paidAt,
		UpdatedAt:       paidAt,
	}
	h.repository.mu.Lock()
	h.repository.orders[order.ID] = &order
	h.repository.mu.Unlock()
	return order
}

// TestSetSpendingLimitStoresAndReportsTheCap proves a saved cap round-trips
// with the current month's spend and the derived remaining amount.
func TestSetSpendingLimitStoresAndReportsTheCap(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedMonthlySpend(t, "parent-1", 5000, harness.clock.now.Add(-time.Hour))

	view, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(19900))
	if err != nil {
		t.Fatalf("set spending limit: %v", err)
	}
	if view.Limit.MonthlyLimitCents != 19900 {
		t.Fatalf("limit = %d, want 19900", int64(view.Limit.MonthlyLimitCents))
	}
	if view.Spending.ConsumedCents != 5000 {
		t.Fatalf("consumed = %d, want 5000", int64(view.Spending.ConsumedCents))
	}
	if remaining := view.Spending.RemainingCents(view.Limit.MonthlyLimitCents); remaining != 14900 {
		t.Fatalf("remaining = %d, want 14900", int64(remaining))
	}
	if view.Limit.UpdatedBy != "parent-1" {
		t.Fatalf("updated_by = %q, want the acting account", view.Limit.UpdatedBy)
	}
}

// TestSetSpendingLimitRecordsAudit proves the guardian's change is auditable,
// including the value it replaced.
func TestSetSpendingLimitRecordsAudit(t *testing.T) {
	audit := &recordingAuditRecorder{}
	harness := newHarnessWithAudit(t, audit)
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(10000)); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(20000)); err != nil {
		t.Fatalf("second set: %v", err)
	}
	if len(audit.events) < 2 {
		t.Fatalf("audit events = %d, want at least 2", len(audit.events))
	}
	last := audit.events[len(audit.events)-1]
	if last.Action != "billing.spending_limit.updated" {
		t.Fatalf("audit action = %q", last.Action)
	}
	if last.Detail["monthly_limit_cents"] != int64(20000) {
		t.Fatalf("audit detail = %#v", last.Detail)
	}
	if last.Detail["previous_monthly_limit_cents"] != int64(10000) {
		t.Fatalf("previous limit not recorded: %#v", last.Detail)
	}
}

// TestSetSpendingLimitRejectsInvalidAmounts keeps a malformed guardian payload
// out of storage.
func TestSetSpendingLimitRejectsInvalidAmounts(t *testing.T) {
	harness := newHarness(t, nil)
	for _, amount := range []domain.Cents{-1, domain.MonthlySpendingLimitMaxCents + 1} {
		_, err := harness.spendingLimits.SetLimit(context.Background(), "parent-1", "parent-1", amount)
		if !errors.Is(err, domain.ErrInvalidSpendingLimit) {
			t.Fatalf("expected %d to be refused, got %v", int64(amount), err)
		}
	}
	harness.repository.mu.Lock()
	stored := len(harness.repository.spendingLimits)
	harness.repository.mu.Unlock()
	if stored != 0 {
		t.Fatalf("stored limits = %d, want none", stored)
	}
}

// TestCreateOrderRejectsOverLimitCheckout proves the cap is enforced on the
// checkout path, not only on the settings screen.
func TestCreateOrderRejectsOverLimitCheckout(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(10000)); err != nil {
		t.Fatalf("set spending limit: %v", err)
	}

	_, err := harness.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: "parent-1",
		PlanCode:        "sprout_year",
		IdempotencyKey:  "key-over-limit",
		ClientIP:        "127.0.0.1",
	})
	if !errors.Is(err, domain.ErrSpendingLimitExceeded) {
		t.Fatalf("expected ErrSpendingLimitExceeded, got %v", err)
	}
	harness.repository.mu.Lock()
	orderCount := len(harness.repository.orders)
	rejections := len(harness.repository.spendingRejections)
	harness.repository.mu.Unlock()
	if orderCount != 0 {
		t.Fatalf("orders = %d, want none", orderCount)
	}
	if rejections != 1 {
		t.Fatalf("rejections = %d, want 1", rejections)
	}
}

// TestCreateOrderRejectsWhenMonthlySpendIsAtTheCap proves the running total is
// counted, not just the size of the new order.
func TestCreateOrderRejectsWhenMonthlySpendIsAtTheCap(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_month", 9900, "5.000000")
	harness.grantConsent(t, "parent-1")
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(19900)); err != nil {
		t.Fatalf("set spending limit: %v", err)
	}
	harness.seedMonthlySpend(t, "parent-1", 19900, harness.clock.now.Add(-2*time.Hour))

	_, err := harness.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: "parent-1",
		PlanCode:        "sprout_month",
		IdempotencyKey:  "key-at-cap",
	})
	if !errors.Is(err, domain.ErrSpendingLimitExceeded) {
		t.Fatalf("expected ErrSpendingLimitExceeded, got %v", err)
	}
}

// TestCreateOrderAllowsCheckoutWithinTheCap proves the guard is not a blanket
// refusal.
func TestCreateOrderAllowsCheckoutWithinTheCap(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_month", 9900, "5.000000")
	harness.grantConsent(t, "parent-1")
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(19900)); err != nil {
		t.Fatalf("set spending limit: %v", err)
	}
	harness.seedMonthlySpend(t, "parent-1", 5000, harness.clock.now.Add(-time.Hour))

	order := harness.createOrder(t, "parent-1", "sprout_month")
	if order.PayableCents != 9900 {
		t.Fatalf("payable = %d, want 9900", int64(order.PayableCents))
	}
	harness.repository.mu.Lock()
	rejections := len(harness.repository.spendingRejections)
	harness.repository.mu.Unlock()
	if rejections != 0 {
		t.Fatalf("rejections = %d, want none", rejections)
	}
}

// TestCreateOrderIgnoresAPreviousMonthPayment proves the window is calendar
// scoped: last month's payment must not consume this month's cap.
func TestCreateOrderIgnoresAPreviousMonthPayment(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_month", 9900, "5.000000")
	harness.grantConsent(t, "parent-1")
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(10000)); err != nil {
		t.Fatalf("set spending limit: %v", err)
	}
	previousMonth := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	harness.seedMonthlySpend(t, "parent-1", 9900, previousMonth)

	order := harness.createOrder(t, "parent-1", "sprout_month")
	if order.PayableCents != 9900 {
		t.Fatalf("payable = %d, want 9900", int64(order.PayableCents))
	}
}

// TestCreateOrderUnlimitedCapNeverRefuses proves a zero cap stays unlimited.
func TestCreateOrderUnlimitedCapNeverRefuses(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(0)); err != nil {
		t.Fatalf("set spending limit: %v", err)
	}
	harness.seedMonthlySpend(t, "parent-1", 1000000, harness.clock.now.Add(-time.Hour))

	harness.createOrder(t, "parent-1", "sprout_year")
}

// TestCreateOrderReusedOrderIsNotRefusedByTheCap pins the retry contract: a
// retried idempotency key returns the order the guardian already created even
// though that order now fills the cap.
func TestCreateOrderReusedOrderIsNotRefusedByTheCap(t *testing.T) {
	harness := newHarness(t, nil)
	harness.seedPlan(t, "sprout_year", 19900, "20.000000")
	harness.grantConsent(t, "parent-1")
	first := harness.createOrder(t, "parent-1", "sprout_year")
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(19900)); err != nil {
		t.Fatalf("set spending limit: %v", err)
	}
	harness.seedMonthlySpend(t, "parent-1", 19900, harness.clock.now.Add(-time.Hour))

	result, err := harness.orders.CreateOrder(context.Background(), CreateOrderRequest{
		ParentAccountID: "parent-1",
		PlanCode:        "sprout_year",
		IdempotencyKey:  "key-sprout_year",
	})
	if err != nil {
		t.Fatalf("retried checkout must reuse the existing order: %v", err)
	}
	if result.Order.ID != first.ID || !result.Reused {
		t.Fatalf("expected the original order %s to be reused", first.ID)
	}
}

// TestCheckOrderSpendingRecordsRejectionAudit proves the refusal is auditable
// with the amounts that justified it.
func TestCheckOrderSpendingRecordsRejectionAudit(t *testing.T) {
	audit := &recordingAuditRecorder{}
	harness := newHarnessWithAudit(t, audit)
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(5000)); err != nil {
		t.Fatalf("set spending limit: %v", err)
	}
	if err := harness.spendingLimits.CheckOrderSpending(
		context.Background(), "parent-1", domain.Cents(9900), "sprout_month"); !errors.Is(err, domain.ErrSpendingLimitExceeded) {
		t.Fatalf("expected ErrSpendingLimitExceeded, got %v", err)
	}
	found := false
	for _, event := range audit.events {
		if event.Action != "billing.spending_limit.exceeded" {
			continue
		}
		found = true
		if event.Detail["monthly_limit_cents"] != int64(5000) {
			t.Fatalf("audit detail = %#v", event.Detail)
		}
		if event.Detail["requested_cents"] != int64(9900) {
			t.Fatalf("audit detail = %#v", event.Detail)
		}
	}
	if !found {
		t.Fatal("over-limit refusal was not audited")
	}
}

// TestListRejectionsReturnsTheCurrentMonth proves the operator view is scoped
// to one month and reports the window it used.
func TestListRejectionsReturnsTheCurrentMonth(t *testing.T) {
	harness := newHarness(t, nil)
	if _, err := harness.repository.SpendingLimitMetrics(context.Background()); err != nil {
		t.Fatalf("metrics: %v", err)
	}
	harness.seedMonthlySpend(t, "parent-1", 0, harness.clock.now)
	if _, err := harness.spendingLimits.SetLimit(
		context.Background(), "parent-1", "parent-1", domain.Cents(1000)); err != nil {
		t.Fatalf("set spending limit: %v", err)
	}
	for index := 0; index < 2; index++ {
		if err := harness.spendingLimits.CheckOrderSpending(
			context.Background(), "parent-1", domain.Cents(9900), "sprout_month"); !errors.Is(err, domain.ErrSpendingLimitExceeded) {
			t.Fatalf("expected refusal %d, got %v", index, err)
		}
	}

	rejections, total, period, err := harness.spendingLimits.ListRejections(
		context.Background(), "parent-1", time.Time{}, Page{})
	if err != nil {
		t.Fatalf("list rejections: %v", err)
	}
	if total != 2 || len(rejections) != 2 {
		t.Fatalf("rejections = %d/%d, want 2", len(rejections), total)
	}
	if period.Start() != time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("period = %s, want 2026-10", period.Start())
	}
}
