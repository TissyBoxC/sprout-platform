package domain

import "fmt"

// MonthlySpendingLimitMaxCents bounds a guardian-set monthly cap at
// ¥100,000.00.
//
// The bound exists so a mistyped amount cannot hold the family to an
// effectively infinite cap, and so a stored value always stays inside the range
// the persistence layer accepts.
const MonthlySpendingLimitMaxCents Cents = 10_000_000

// ValidateMonthlySpendingLimit checks one guardian-supplied monthly cap.
//
// Zero is accepted and means unlimited; a negative value or a value above
// MonthlySpendingLimitMaxCents is refused so an invalid amount never reaches
// storage. The error wraps ErrInvalidSpendingLimit.
func ValidateMonthlySpendingLimit(value Cents) error {
	if value < 0 {
		return fmt.Errorf("%w: amount must not be negative", ErrInvalidSpendingLimit)
	}
	if value > MonthlySpendingLimitMaxCents {
		return fmt.Errorf(
			"%w: amount exceeds %s", ErrInvalidSpendingLimit, MonthlySpendingLimitMaxCents.Format())
	}
	return nil
}

// Validate checks the stored invariants of one limit row.
//
// It is used on every write so a caller cannot bypass the amount rules by
// constructing the entity directly.
func (limit FamilySpendingLimit) Validate() error {
	if limit.FamilyID == "" {
		return fmt.Errorf("%w: family is required", ErrInvalidSpendingLimit)
	}
	return ValidateMonthlySpendingLimit(limit.MonthlyLimitCents)
}

// ExceedsLimit reports whether adding requested cents to the family's already
// paid total would take it past the cap.
//
// An unlimited cap never exceeds. The comparison is ">" rather than ">=" so a
// checkout that lands exactly on the cap is allowed: the guardian authorised
// that amount.
func ExceedsLimit(limit Cents, consumed Cents, requested Cents) bool {
	if limit <= 0 {
		return false
	}
	return consumed+requested > limit
}
