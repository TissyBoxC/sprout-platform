package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateOrderAmountsAcceptsConsistentMoney(t *testing.T) {
	t.Parallel()

	cases := []Order{
		{UnitPriceCents: 990, PayableCents: 990},
		{UnitPriceCents: 990, DiscountCents: 100, PayableCents: 890},
		{UnitPriceCents: 990, DiscountCents: 990, PayableCents: 0},
		{UnitPriceCents: 1990, DiscountCents: 100, PayableCents: 1890, PaidCents: 1890},
		{UnitPriceCents: 1990, DiscountCents: 100, PayableCents: 1890, PaidCents: 1890, RefundedCents: 1890},
	}
	for index, order := range cases {
		if err := ValidateOrderAmounts(order); err != nil {
			t.Fatalf("case %d rejected: %v", index, err)
		}
	}
}

func TestValidateOrderAmountsRejectionPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		order Order
		want  error
	}{
		{
			name:  "payable below unit minus discount",
			order: Order{UnitPriceCents: 990, DiscountCents: 100, PayableCents: 880},
			want:  ErrInvalidOrderAmounts,
		},
		{
			name:  "payable above unit minus discount",
			order: Order{UnitPriceCents: 990, DiscountCents: 0, PayableCents: 991},
			want:  ErrInvalidOrderAmounts,
		},
		{
			name:  "discount exceeds unit price",
			order: Order{UnitPriceCents: 990, DiscountCents: 1000, PayableCents: -10},
			want:  ErrInvalidOrderAmounts,
		},
		{
			name:  "negative unit price",
			order: Order{UnitPriceCents: -1, PayableCents: -1},
			want:  ErrInvalidOrderAmounts,
		},
		{
			name:  "negative discount",
			order: Order{UnitPriceCents: 990, DiscountCents: -100, PayableCents: 1090},
			want:  ErrInvalidOrderAmounts,
		},
		{
			name:  "negative payable",
			order: Order{UnitPriceCents: 0, DiscountCents: 0, PayableCents: -1},
			want:  ErrInvalidOrderAmounts,
		},
		{
			name:  "negative paid",
			order: Order{UnitPriceCents: 990, PayableCents: 990, PaidCents: -1},
			want:  ErrInvalidOrderAmounts,
		},
		{
			name:  "paid above payable",
			order: Order{UnitPriceCents: 990, PayableCents: 990, PaidCents: 991},
			want:  ErrInvalidOrderAmounts,
		},
		{
			name:  "negative refunded",
			order: Order{UnitPriceCents: 990, PayableCents: 990, PaidCents: 990, RefundedCents: -1},
			want:  ErrInvalidOrderAmounts,
		},
		{
			name:  "refunded above paid",
			order: Order{UnitPriceCents: 990, PayableCents: 990, PaidCents: 500, RefundedCents: 501},
			want:  ErrInvalidOrderAmounts,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateOrderAmounts(testCase.order); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestValidateRefundAmountRejectsOverRefund(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		refund          Cents
		paid            Cents
		alreadyRefunded Cents
		want            error
	}{
		{name: "first partial refund", refund: 100, paid: 990, alreadyRefunded: 0},
		{name: "exactly the remainder", refund: 890, paid: 990, alreadyRefunded: 100},
		{name: "full refund", refund: 990, paid: 990, alreadyRefunded: 0},
		{name: "zero refund", refund: 0, paid: 990, want: ErrInvalidRefundAmount},
		{name: "negative refund", refund: -100, paid: 990, want: ErrInvalidRefundAmount},
		{name: "negative paid", refund: 100, paid: -1, want: ErrInvalidRefundAmount},
		{name: "negative already refunded", refund: 100, paid: 990, alreadyRefunded: -1, want: ErrInvalidRefundAmount},
		{name: "already refunded above paid", refund: 1, paid: 100, alreadyRefunded: 101, want: ErrOverRefund},
		{
			name:            "one fen over the remainder",
			refund:          101,
			paid:            990,
			alreadyRefunded: 890,
			want:            ErrOverRefund,
		},
		{
			name:            "second full refund",
			refund:          990,
			paid:            990,
			alreadyRefunded: 990,
			want:            ErrOverRefund,
		},
		{
			name:            "overflow on the running total",
			refund:          1,
			paid:            0,
			alreadyRefunded: 0,
			want:            ErrOverRefund,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRefundAmount(testCase.refund, testCase.paid, testCase.alreadyRefunded)
			if testCase.want == nil {
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestValidateRefundOverflowDoesNotPanic(t *testing.T) {
	t.Parallel()

	// alreadyRefunded + refund overflows int64; it must be reported, not wrap.
	err := ValidateRefundAmount(1<<62, 1<<62, 1<<62)
	if !errors.Is(err, ErrInvalidRefundAmount) && !errors.Is(err, ErrOverRefund) {
		t.Fatalf("error = %v, want a refund rejection", err)
	}
}

func TestValidateRefundMirrorsOperatorConstraint(t *testing.T) {
	t.Parallel()

	approvedAt := time.Now()
	refund := Refund{AmountCents: 100, Status: RefundStatusApproved}
	if err := ValidateRefund(refund, 990, 0); err == nil {
		t.Fatal("approved refund without operator must be rejected")
	}
	refund.OperatorAccountID = "operator-1"
	if err := ValidateRefund(refund, 990, 0); err == nil {
		t.Fatal("approved refund without approval time must be rejected")
	}
	refund.ApprovedAt = &approvedAt
	if err := ValidateRefund(refund, 990, 0); err != nil {
		t.Fatalf("complete approved refund rejected: %v", err)
	}

	// requested and rejected need no operator, matching the SQL constraint.
	for _, status := range []RefundStatus{RefundStatusRequested, RefundStatusRejected} {
		if err := ValidateRefund(Refund{AmountCents: 100, Status: status}, 990, 0); err != nil {
			t.Fatalf("%q refund rejected: %v", status, err)
		}
	}
	succeeded := Refund{
		AmountCents:       100,
		Status:            RefundStatusSucceeded,
		OperatorAccountID: "operator-1",
		ApprovedAt:        &approvedAt,
	}
	if err := ValidateRefund(succeeded, 990, 0); err == nil {
		t.Fatal("succeeded refund without completion time must be rejected")
	}
	succeeded.CompletedAt = &approvedAt
	if err := ValidateRefund(succeeded, 990, 0); err != nil {
		t.Fatalf("complete succeeded refund rejected: %v", err)
	}
	if err := ValidateRefund(succeeded, 990, 900); !errors.Is(err, ErrOverRefund) {
		t.Fatalf("over refund error = %v, want ErrOverRefund", err)
	}
	if err := ValidateRefund(Refund{AmountCents: 1, Status: RefundStatus("pending")}, 990, 0); err == nil {
		t.Fatal("unknown refund status must be rejected")
	}
}

func TestValidatePaymentAmountRejectsCallbackMismatch(t *testing.T) {
	t.Parallel()

	order := Order{PayableCents: 990}
	if err := ValidatePaymentAmount(990, order); err != nil {
		t.Fatalf("exact payment rejected: %v", err)
	}
	for _, wrong := range []Cents{0, 989, 991, 1990} {
		if err := ValidatePaymentAmount(wrong, order); !errors.Is(err, ErrAmountMismatch) {
			t.Fatalf("payment %d error = %v, want ErrAmountMismatch", int64(wrong), err)
		}
	}
}

func TestValidateOrderPayable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(30 * time.Minute)
	pending := Order{Status: OrderStatusPending, PayableCents: 990, ExpiresAt: &deadline}
	if err := ValidateOrderPayable(pending, now); err != nil {
		t.Fatalf("pending order rejected: %v", err)
	}
	if err := ValidateOrderPayable(pending, deadline); !errors.Is(err, ErrOrderExpired) {
		t.Fatalf("deadline-boundary error = %v, want ErrOrderExpired", err)
	}
	if err := ValidateOrderPayable(pending, deadline.Add(time.Second)); !errors.Is(err, ErrOrderExpired) {
		t.Fatalf("past-deadline error = %v, want ErrOrderExpired", err)
	}
	paid := pending
	paid.Status = OrderStatusPaid
	if err := ValidateOrderPayable(paid, now); !errors.Is(err, ErrOrderNotPayable) {
		t.Fatalf("paid order error = %v, want ErrOrderNotPayable", err)
	}
	zero := pending
	zero.PayableCents = 0
	if err := ValidateOrderPayable(zero, now); !errors.Is(err, ErrOrderNotPayable) {
		t.Fatalf("zero-payable order error = %v, want ErrOrderNotPayable", err)
	}
}

func TestValidatePlan(t *testing.T) {
	t.Parallel()

	valid := Plan{
		Code:          "starter_monthly",
		DisplayName:   "初芽体验包",
		BillingPeriod: BillingPeriodMonthly,
		PeriodMonths:  1,
		PriceCents:    1990,
		AIQuotaUSD:    "5",
		Status:        PlanStatusActive,
		Version:       1,
	}
	if err := ValidatePlan(valid); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}

	oneTime := valid
	oneTime.Code = "starter_onetime"
	oneTime.BillingPeriod = BillingPeriodOneTime
	oneTime.PeriodMonths = 0
	if err := ValidatePlan(oneTime); err != nil {
		t.Fatalf("valid one_time plan rejected: %v", err)
	}

	draft := valid
	draft.Status = PlanStatusDraft
	draft.PriceCents = 0
	draft.AIQuotaUSD = "0"
	if err := ValidatePlan(draft); err != nil {
		t.Fatalf("free draft rejected: %v", err)
	}

	archived := draft
	archived.Status = PlanStatusArchived
	if err := ValidatePlan(archived); err != nil {
		t.Fatalf("zero-price archived plan rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(plan *Plan)
	}{
		{name: "blank code", mutate: func(plan *Plan) { plan.Code = "  " }},
		{name: "camel case code", mutate: func(plan *Plan) { plan.Code = "StarterMonthly" }},
		{name: "code with double underscore", mutate: func(plan *Plan) { plan.Code = "starter__monthly" }},
		{name: "code starting with digit", mutate: func(plan *Plan) { plan.Code = "1starter" }},
		{name: "code with trailing underscore", mutate: func(plan *Plan) { plan.Code = "starter_" }},
		{name: "blank display name", mutate: func(plan *Plan) { plan.DisplayName = "" }},
		{name: "unknown billing period", mutate: func(plan *Plan) { plan.BillingPeriod = "weekly" }},
		{name: "monthly with zero months", mutate: func(plan *Plan) { plan.PeriodMonths = 0 }},
		{name: "one_time with months", mutate: func(plan *Plan) {
			plan.BillingPeriod = BillingPeriodOneTime
			plan.PeriodMonths = 1
		}},
		{name: "too many months", mutate: func(plan *Plan) { plan.PeriodMonths = MaxPlanPeriodMonths + 1 }},
		{name: "negative price", mutate: func(plan *Plan) { plan.PriceCents = -1 }},
		{name: "negative original price", mutate: func(plan *Plan) { plan.OriginalPriceCents = -1 }},
		{name: "active plan without price", mutate: func(plan *Plan) { plan.PriceCents = 0 }},
		{name: "unparseable quota", mutate: func(plan *Plan) { plan.AIQuotaUSD = "five" }},
		{name: "negative sort order", mutate: func(plan *Plan) { plan.SortOrder = -1 }},
		{name: "negative version", mutate: func(plan *Plan) { plan.Version = -1 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			plan := valid
			testCase.mutate(&plan)
			if err := ValidatePlan(plan); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("error = %v, want ErrInvalidPlan", err)
			}
		})
	}
}

func TestValidatePlanCodeMatchesMigrationPattern(t *testing.T) {
	t.Parallel()

	// billing_plans_code_format uses '^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$'.
	accepted := []string{"a", "starter", "starter_monthly", "plan2_year", "abc_1def"}
	for _, code := range accepted {
		if !isPlanCode(code) {
			t.Fatalf("code %q should be accepted", code)
		}
	}
	rejected := []string{"", "_starter", "Starter", "starter-monthly", "starter__monthly", "starter_", "starter_1_"}
	for _, code := range rejected {
		if isPlanCode(code) {
			t.Fatalf("code %q should be rejected", code)
		}
	}
}

func TestOrderRemainingRefundableFloorsAtZero(t *testing.T) {
	t.Parallel()

	if got := (Order{PaidCents: 990, RefundedCents: 100}).RemainingRefundableCents(); got != 890 {
		t.Fatalf("remaining = %d, want 890", int64(got))
	}
	if got := (Order{PaidCents: 100, RefundedCents: 200}).RemainingRefundableCents(); got != 0 {
		t.Fatalf("remaining = %d, want 0", int64(got))
	}
}

func TestErrorMessagesStayActionable(t *testing.T) {
	t.Parallel()

	err := ValidateRefundAmount(300, 200, 0)
	if !errors.Is(err, ErrOverRefund) {
		t.Fatalf("error = %v, want ErrOverRefund", err)
	}
	for _, fragment := range []string{"¥3.00", "¥2.00"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error %q must mention %q", err.Error(), fragment)
		}
	}
}
