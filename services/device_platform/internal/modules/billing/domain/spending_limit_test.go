package domain

import (
	"errors"
	"testing"
	"time"
)

// TestValidateMonthlySpendingLimitAcceptsAndRejectsRanges pins the accepted cap
// range: zero means unlimited, and both a negative value and an absurd amount
// are refused before storage.
func TestValidateMonthlySpendingLimitAcceptsAndRejectsRanges(t *testing.T) {
	for _, value := range []Cents{0, 1, 19900, MonthlySpendingLimitMaxCents} {
		if err := ValidateMonthlySpendingLimit(value); err != nil {
			t.Fatalf("expected %d to be valid, got %v", int64(value), err)
		}
	}
	for _, value := range []Cents{-1, -19900, MonthlySpendingLimitMaxCents + 1} {
		if err := ValidateMonthlySpendingLimit(value); !errors.Is(err, ErrInvalidSpendingLimit) {
			t.Fatalf("expected %d to be rejected, got %v", int64(value), err)
		}
	}
}

// TestFamilySpendingLimitValidateRequiresFamily keeps a row from being written
// without an owner: the cap is family-scoped and has no meaning otherwise.
func TestFamilySpendingLimitValidateRequiresFamily(t *testing.T) {
	limit := FamilySpendingLimit{MonthlyLimitCents: 19900}
	if err := limit.Validate(); !errors.Is(err, ErrInvalidSpendingLimit) {
		t.Fatalf("expected a missing family to be rejected, got %v", err)
	}
	limit.FamilyID = "family-1"
	if err := limit.Validate(); err != nil {
		t.Fatalf("expected a valid limit, got %v", err)
	}
}

// TestExceedsLimitAllowsLandingExactlyOnTheCap encodes the comparison rule: the
// guardian authorised the capped amount, so spending exactly the cap is allowed
// and one cent more is not.
func TestExceedsLimitAllowsLandingExactlyOnTheCap(t *testing.T) {
	tests := []struct {
		name      string
		limit     Cents
		consumed  Cents
		requested Cents
		want      bool
	}{
		{name: "unlimited never exceeds", limit: 0, consumed: 5000000, requested: 19900, want: false},
		{name: "lands on the cap", limit: 19900, consumed: 0, requested: 19900, want: false},
		{name: "one cent over", limit: 19900, consumed: 0, requested: 19901, want: true},
		{name: "already at the cap", limit: 19900, consumed: 19900, requested: 1, want: true},
		{name: "room left", limit: 19900, consumed: 10000, requested: 9900, want: false},
	}
	for _, test := range tests {
		if got := ExceedsLimit(test.limit, test.consumed, test.requested); got != test.want {
			t.Fatalf("%s: ExceedsLimit = %v, want %v", test.name, got, test.want)
		}
	}
}

// TestMonthlySpendingRemainingCentsNeverGoesNegative guards the guardian-facing
// remaining figure: an over-run reports zero, and unlimited reports -1.
func TestMonthlySpendingRemainingCentsNeverGoesNegative(t *testing.T) {
	spending := MonthlySpending{
		FamilyID:      "family-1",
		Period:        time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		ConsumedCents: 25000,
	}
	if remaining := spending.RemainingCents(19900); remaining != 0 {
		t.Fatalf("remaining = %d, want 0 when already over the cap", int64(remaining))
	}
	if remaining := spending.RemainingCents(0); remaining != -1 {
		t.Fatalf("remaining = %d, want -1 for an unlimited cap", int64(remaining))
	}
	if remaining := spending.RemainingCents(30000); remaining != 5000 {
		t.Fatalf("remaining = %d, want 5000", int64(remaining))
	}
}

// TestFamilySpendingLimitIsUnlimited covers the zero-means-unlimited rule that
// the checkout guard and the wire response both rely on.
func TestFamilySpendingLimitIsUnlimited(t *testing.T) {
	if !(FamilySpendingLimit{}).IsUnlimited() {
		t.Fatal("a zero cap must read as unlimited")
	}
	if (FamilySpendingLimit{MonthlyLimitCents: 1}).IsUnlimited() {
		t.Fatal("a positive cap must not read as unlimited")
	}
}
