package domain

import "time"

// FamilySpendingLimit caps how much one family may pay for plans in a calendar
// month.
//
// The limit is family-scoped, not child-scoped: the billing ledger records one
// paying account per family, and splitting the cap per child would let the same
// payment count against several caps. Encoded as an integer cent count, never a
// float, matching the rest of the billing money rules.
type FamilySpendingLimit struct {
	FamilyID string
	// MonthlyLimitCents is the cap in CNY cents. Zero means unlimited.
	MonthlyLimitCents Cents
	// UpdatedBy is the account that last changed the cap; empty when the row
	// was created by the platform rather than by a person.
	UpdatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsUnlimited reports whether the family has no monthly cap.
func (limit FamilySpendingLimit) IsUnlimited() bool {
	return limit.MonthlyLimitCents <= 0
}

// MonthlySpending is the family's already-paid total for one calendar month.
//
// The period is resolved on the server in UTC so two app instances in different
// time zones agree on which payments land in which month.
type MonthlySpending struct {
	FamilyID      string
	Period        time.Time
	ConsumedCents Cents
}

// RemainingCents is the cap minus what is already paid, or -1 when unlimited.
//
// A negative result is never returned for a capped family: spending can exceed
// the cap after a manual operator grant, and reporting "-500 left" would make
// clients render a nonsensical value.
func (spending MonthlySpending) RemainingCents(limit Cents) Cents {
	if limit <= 0 {
		return Cents(-1)
	}
	remaining := limit - spending.ConsumedCents
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SpendingLimitRejection is the durable record of one checkout refused because
// the family had already reached its monthly cap.
//
// It is written outside the checkout transaction: the refusal itself did not
// change any order, and the compliance trail must survive a later cap change.
type SpendingLimitRejection struct {
	ID                string
	FamilyID          string
	MonthlyLimitCents Cents
	ConsumedCents     Cents
	RequestedCents    Cents
	PlanCode          string
	CreatedAt         time.Time
}
