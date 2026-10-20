package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ErrInvalidPlan reports a plan that could not be published or purchased.
var ErrInvalidPlan = errors.New("billing: invalid plan")

// ErrInvalidOrderAmounts reports an order whose money fields do not add up.
var ErrInvalidOrderAmounts = errors.New("billing: invalid order amounts")

// ErrInvalidRefundAmount reports a refund outside the refundable range.
var ErrInvalidRefundAmount = errors.New("billing: invalid refund amount")

// MaxPlanPeriodMonths is the longest subscription a plan may sell.
//
// It bounds the subscription end date so a typo cannot create a multi-century
// entitlement, and it keeps the month arithmetic far from overflow.
const MaxPlanPeriodMonths = 60

// ValidatePlan checks the catalogue fields a plan needs to be stored.
//
// An active plan must be purchasable, so it needs a positive price. Drafts and
// archived plans may carry zero while being prepared or retired.
func ValidatePlan(plan Plan) error {
	code := strings.TrimSpace(plan.Code)
	if code == "" {
		return fmt.Errorf("%w: code is required", ErrInvalidPlan)
	}
	if !isPlanCode(code) {
		return fmt.Errorf("%w: code %q must be lower_snake_case", ErrInvalidPlan, plan.Code)
	}
	if strings.TrimSpace(plan.DisplayName) == "" {
		return fmt.Errorf("%w: display name is required", ErrInvalidPlan)
	}
	if !isBillingPeriod(plan.BillingPeriod) {
		return fmt.Errorf("%w: unknown billing period %q", ErrInvalidPlan, plan.BillingPeriod)
	}
	if err := validatePeriodMonths(plan.BillingPeriod, plan.PeriodMonths); err != nil {
		return err
	}
	if plan.PriceCents < 0 {
		return fmt.Errorf("%w: price must not be negative", ErrInvalidPlan)
	}
	if plan.OriginalPriceCents < 0 {
		return fmt.Errorf("%w: original price must not be negative", ErrInvalidPlan)
	}
	if plan.Status == PlanStatusActive && plan.PriceCents == 0 {
		return fmt.Errorf("%w: an active plan must have a price", ErrInvalidPlan)
	}
	if _, err := parseQuota(plan.AIQuotaUSD); err != nil {
		return fmt.Errorf("%w: ai quota: %v", ErrInvalidPlan, err)
	}
	if plan.SortOrder < 0 {
		return fmt.Errorf("%w: sort order must not be negative", ErrInvalidPlan)
	}
	// Version 0 is what an unpersisted plan carries; a stored plan starts at 1.
	if plan.Version < 0 {
		return fmt.Errorf("%w: version must not be negative", ErrInvalidPlan)
	}
	return nil
}

// ValidateOrderAmounts checks the money fields of an order.
//
// Payable must equal unit price minus discount, every amount must be
// non-negative, the discount may not exceed the unit price, and neither paid
// nor refunded may exceed the payable amount. This is the same relationship the
// billing_orders constraints enforce, duplicated here so a caller gets a
// domain error instead of a constraint violation.
func ValidateOrderAmounts(order Order) error {
	if order.UnitPriceCents < 0 {
		return fmt.Errorf("%w: unit price must not be negative", ErrInvalidOrderAmounts)
	}
	if order.DiscountCents < 0 {
		return fmt.Errorf("%w: discount must not be negative", ErrInvalidOrderAmounts)
	}
	if order.DiscountCents > order.UnitPriceCents {
		return fmt.Errorf("%w: discount exceeds unit price", ErrInvalidOrderAmounts)
	}
	if order.PayableCents < 0 {
		return fmt.Errorf("%w: payable must not be negative", ErrInvalidOrderAmounts)
	}
	if expected := order.UnitPriceCents - order.DiscountCents; order.PayableCents != expected {
		return fmt.Errorf(
			"%w: payable %s does not equal unit price %s minus discount %s",
			ErrInvalidOrderAmounts,
			order.PayableCents.Format(),
			order.UnitPriceCents.Format(),
			order.DiscountCents.Format(),
		)
	}
	if order.PaidCents < 0 {
		return fmt.Errorf("%w: paid must not be negative", ErrInvalidOrderAmounts)
	}
	if order.PaidCents > order.PayableCents {
		return fmt.Errorf("%w: paid exceeds payable", ErrInvalidOrderAmounts)
	}
	if order.RefundedCents < 0 {
		return fmt.Errorf("%w: refunded must not be negative", ErrInvalidOrderAmounts)
	}
	if order.RefundedCents > order.PaidCents {
		return fmt.Errorf("%w: refunded exceeds paid", ErrInvalidOrderAmounts)
	}
	return nil
}

// ValidateRefundAmount checks one refund against what the order still owes the
// guardian.
//
// paid is the total settled on the order, alreadyRefunded the amount refunded
// so far including refunds in progress, and refund the amount being requested.
// A refund must be positive and the total may never exceed paid.
func ValidateRefundAmount(refund, paid, alreadyRefunded Cents) error {
	if refund <= 0 {
		return fmt.Errorf("%w: refund must be positive", ErrInvalidRefundAmount)
	}
	if paid < 0 || alreadyRefunded < 0 {
		return fmt.Errorf("%w: paid and refunded amounts must not be negative", ErrInvalidRefundAmount)
	}
	if alreadyRefunded > paid {
		return fmt.Errorf(
			"%w: already refunded %s exceeds paid %s",
			ErrOverRefund,
			alreadyRefunded.Format(),
			paid.Format(),
		)
	}
	total, err := refund.Add(alreadyRefunded)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRefundAmount, err)
	}
	if total > paid {
		return fmt.Errorf(
			"%w: refunding %s would total %s against paid %s",
			ErrOverRefund,
			refund.Format(),
			total.Format(),
			paid.Format(),
		)
	}
	return nil
}

// ValidateRefund checks a refund row and its amount together.
//
// It mirrors the billing_refunds constraints: the amount must be positive, and
// an approved-or-later refund must name the operator and the approval time.
func ValidateRefund(refund Refund, paid, alreadyRefunded Cents) error {
	if !refund.Status.IsKnown() {
		return fmt.Errorf("%w: unknown refund status %q", ErrInvalidRefundAmount, refund.Status)
	}
	switch refund.Status {
	case RefundStatusRequested, RefundStatusRejected:
	default:
		if strings.TrimSpace(refund.OperatorAccountID) == "" {
			return fmt.Errorf("%w: an approved refund requires an approving operator", ErrInvalidRefundAmount)
		}
		if refund.ApprovedAt == nil {
			return fmt.Errorf("%w: an approved refund requires an approval time", ErrInvalidRefundAmount)
		}
	}
	if refund.Status == RefundStatusSucceeded && refund.CompletedAt == nil {
		return fmt.Errorf("%w: a succeeded refund requires a completion time", ErrInvalidRefundAmount)
	}
	return ValidateRefundAmount(refund.AmountCents, paid, alreadyRefunded)
}

// ValidatePaymentAmount checks a callback amount against the order.
//
// The callback must settle exactly the payable amount: a short payment would
// under-credit and an overpayment would be invisible to reconciliation. Any
// difference is ErrAmountMismatch and is recorded without crediting.
func ValidatePaymentAmount(payment Cents, order Order) error {
	if payment != order.PayableCents {
		return fmt.Errorf(
			"%w: paid %s but order payable is %s",
			ErrAmountMismatch,
			payment.Format(),
			order.PayableCents.Format(),
		)
	}
	return nil
}

// ValidateOrderPayable checks whether an order may start a payment attempt at
// now.
//
// The deadline is authoritative even for an order still recorded as pending,
// because the expiry sweep may not have run yet.
func ValidateOrderPayable(order Order, now time.Time) error {
	if order.Status != OrderStatusPending {
		return fmt.Errorf("%w: order status is %q", ErrOrderNotPayable, order.Status)
	}
	if order.ExpiresAt != nil && !now.Before(*order.ExpiresAt) {
		return fmt.Errorf("%w: deadline was %s", ErrOrderExpired, order.ExpiresAt.Format(time.RFC3339))
	}
	if order.PayableCents <= 0 {
		return fmt.Errorf("%w: payable amount is not positive", ErrOrderNotPayable)
	}
	return nil
}

// planCodePattern mirrors the billing_plans_code_format check in migration
// 0073, so a code accepted here can never be rejected by the database.
var planCodePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)

// isPlanCode reports whether code is lower_snake_case starting with a letter.
func isPlanCode(code string) bool {
	return planCodePattern.MatchString(code)
}

// isBillingPeriod reports whether period is one of the four declared periods.
func isBillingPeriod(period BillingPeriod) bool {
	for _, known := range BillingPeriods {
		if known == period {
			return true
		}
	}
	return false
}

// validatePeriodMonths enforces the design doc rule that a one_time plan has
// zero months and every recurring plan has at least one.
func validatePeriodMonths(period BillingPeriod, months int) error {
	if period == BillingPeriodOneTime {
		if months != 0 {
			return fmt.Errorf("%w: one_time plans must have 0 period months", ErrInvalidPlan)
		}
		return nil
	}
	if months < 1 {
		return fmt.Errorf("%w: recurring plans need at least one period month", ErrInvalidPlan)
	}
	if months > MaxPlanPeriodMonths {
		return fmt.Errorf("%w: period months %d exceed %d", ErrInvalidPlan, months, MaxPlanPeriodMonths)
	}
	return nil
}
