package domain

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ErrInvalidQuotaLedgerEntry reports a malformed or contradictory quota-ledger
// entry.
var ErrInvalidQuotaLedgerEntry = errors.New("billing: invalid quota ledger entry")

// quotaScale is the number of decimal places of the provider's NUMERIC(18, 6)
// quota unit. Amounts are compared as scaled integers so no float64 enters the
// calculation.
const quotaScale = 6

// quota is a fixed-point USD amount held as an integer count of 1e-6 USD.
type quota struct {
	units int64
}

// parseQuota parses a decimal quota string such as "3.5" or "-0.000001" into
// scaled micro-units.
//
// It rejects empty input, a bare sign, a bare separator, an empty integer part
// such as ".5", more than six decimals, exponent notation, and any value
// outside int64 when scaled.
func parseQuota(text string) (quota, error) {
	value := strings.TrimSpace(text)
	negative := false
	if trimmed := strings.TrimPrefix(value, "-"); trimmed != value {
		negative = true
		value = trimmed
	}
	if strings.HasPrefix(value, "+") {
		value = strings.TrimPrefix(value, "+")
	}
	if value == "" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		return quota{}, fmt.Errorf("%w: %q has an invalid sign", ErrInvalidQuotaLedgerEntry, text)
	}

	// Exponent and hex forms would parse for some readers and not others;
	// accepting only plain decimals keeps writer and reader in agreement.
	if strings.ContainsAny(value, "eE") {
		return quota{}, fmt.Errorf("%w: %q must be a plain decimal", ErrInvalidQuotaLedgerEntry, text)
	}
	whole, fraction, hasSeparator := strings.Cut(value, ".")
	if whole == "" || !isDigits(whole) {
		return quota{}, fmt.Errorf("%w: %q has a non-numeric integer part", ErrInvalidQuotaLedgerEntry, text)
	}
	if len(fraction) > quotaScale {
		return quota{}, fmt.Errorf("%w: %q has more than %d decimals", ErrInvalidQuotaLedgerEntry, text, quotaScale)
	}
	if hasSeparator && fraction != "" && !isDigits(fraction) {
		return quota{}, fmt.Errorf("%w: %q has a non-numeric decimal part", ErrInvalidQuotaLedgerEntry, text)
	}

	scaled := big.NewInt(0)
	wholeValue := new(big.Int)
	if _, ok := wholeValue.SetString(whole, 10); !ok {
		return quota{}, fmt.Errorf("%w: %q is out of range", ErrInvalidQuotaLedgerEntry, text)
	}
	scaled.Mul(wholeValue, big.NewInt(pow10(quotaScale)))
	fractionValue := new(big.Int)
	if fraction != "" {
		if _, ok := fractionValue.SetString(fraction+strings.Repeat("0", quotaScale-len(fraction)), 10); !ok {
			return quota{}, fmt.Errorf("%w: %q is out of range", ErrInvalidQuotaLedgerEntry, text)
		}
		scaled.Add(scaled, fractionValue)
	}
	if negative {
		scaled.Neg(scaled)
	}
	if !scaled.IsInt64() {
		return quota{}, fmt.Errorf("%w: %q is out of range", ErrInvalidQuotaLedgerEntry, text)
	}
	return quota{units: scaled.Int64()}, nil
}

// parseUSD parses a ledger amount and reports its direction as +1, -1, or 0.
func parseUSD(text string) (int64, int, error) {
	parsed, err := parseQuota(text)
	if err != nil {
		return 0, 0, err
	}
	sign := 0
	switch {
	case parsed.units > 0:
		sign = 1
	case parsed.units < 0:
		sign = -1
	}
	return parsed.units, sign, nil
}

// addQuota returns left plus right, reporting overflow.
func addQuota(left, right int64) (int64, bool) {
	sum := left + right
	if (left > 0 && right > 0 && sum < 0) || (left < 0 && right < 0 && sum > 0) {
		return 0, false
	}
	return sum, true
}

// pow10 returns 10**exponent for the small exponents this package uses.
func pow10(exponent int) int64 {
	result := int64(1)
	for index := 0; index < exponent; index++ {
		result *= 10
	}
	return result
}

// FormatUSD renders a scaled quota back as a plain decimal string, kept for
// error messages and tests.
func (amount quota) FormatUSD() string {
	units := amount.units
	sign := ""
	if units < 0 {
		sign = "-"
		units = -units
	}
	whole := units / pow10(quotaScale)
	fraction := units % pow10(quotaScale)
	text := fmt.Sprintf("%s%d.%06d", sign, whole, fraction)
	text = strings.TrimRight(text, "0")
	text = strings.TrimSuffix(text, ".")
	if text == "" || text == "-" {
		return "0"
	}
	return text
}

// ValidateQuotaLedgerEntry checks one ledger entry against the invariants the
// audit trail depends on.
//
// It rejects an unknown entry type, an unparseable or zero amount, a direction
// the type does not allow, an adjustment without a reason, a provider sync
// whose observed balance does not reproduce its recorded delta, and a missing
// source reference for the two types that must name one.
func ValidateQuotaLedgerEntry(entry QuotaLedgerEntry) error {
	if entry.ParentAccountID == "" {
		return fmt.Errorf("%w: parent account is required", ErrInvalidQuotaLedgerEntry)
	}
	if !entry.EntryType.IsKnown() {
		return fmt.Errorf("%w: unknown entry type %q", ErrInvalidQuotaLedgerEntry, entry.EntryType)
	}
	amount, err := parseQuota(entry.AmountUSD)
	if err != nil {
		return err
	}
	if amount.units == 0 {
		return fmt.Errorf("%w: amount must not be zero", ErrInvalidQuotaLedgerEntry)
	}
	balanceAfter, err := parseQuota(entry.BalanceAfterUSD)
	if err != nil {
		return fmt.Errorf("%w: balance_after_usd: %v", ErrInvalidQuotaLedgerEntry, err)
	}
	if balanceAfter.units < 0 {
		return fmt.Errorf("%w: balance after an entry must not be negative", ErrInvalidQuotaLedgerEntry)
	}

	canAdd, canRemove := entry.EntryType.AllowedDirection()
	if amount.units > 0 && !canAdd {
		return fmt.Errorf("%w: %q cannot add quota", ErrInvalidQuotaLedgerEntry, entry.EntryType)
	}
	if amount.units < 0 && !canRemove {
		return fmt.Errorf("%w: %q cannot remove quota", ErrInvalidQuotaLedgerEntry, entry.EntryType)
	}

	// A manual adjustment is a human act with no source row, so the reason is
	// the only record of why quota moved.
	if entry.EntryType == QuotaEntryAdminAdjustment && strings.TrimSpace(entry.Reason) == "" {
		return fmt.Errorf("%w: admin adjustment requires a reason", ErrInvalidQuotaLedgerEntry)
	}
	if entry.EntryType == QuotaEntryPurchaseGrant && entry.OrderID == "" {
		return fmt.Errorf("%w: purchase grant requires an order", ErrInvalidQuotaLedgerEntry)
	}
	if entry.EntryType == QuotaEntryRefundClawback {
		if entry.RefundID == "" {
			return fmt.Errorf("%w: refund clawback requires a refund", ErrInvalidQuotaLedgerEntry)
		}
		// A clawback returns quota a purchase granted, so it can never be
		// larger than that purchase; the service checks the exact amount
		// against the order, this only blocks the nonsensical direction.
		if amount.units > 0 {
			return fmt.Errorf("%w: refund clawback must remove quota", ErrInvalidQuotaLedgerEntry)
		}
	}

	if entry.EntryType != QuotaEntryProviderSync {
		if entry.ObservedProviderBalanceUSD != "" {
			return fmt.Errorf("%w: only a provider sync records an observed balance", ErrInvalidQuotaLedgerEntry)
		}
		return nil
	}
	observed, err := parseQuota(entry.ObservedProviderBalanceUSD)
	if err != nil {
		return fmt.Errorf("%w: observed_provider_balance_usd: %v", ErrInvalidQuotaLedgerEntry, err)
	}
	if observed.units < 0 {
		return fmt.Errorf("%w: observed provider balance must not be negative", ErrInvalidQuotaLedgerEntry)
	}
	// Replaying the entry must land exactly on the balance the sync observed;
	// otherwise the recorded delta and the recorded balance disagree and the
	// trail no longer sums to reality.
	replayed, ok := addQuota(balanceAfter.units, amount.units)
	if !ok {
		return fmt.Errorf("%w: sync replay overflows", ErrInvalidQuotaLedgerEntry)
	}
	if replayed != observed.units {
		return fmt.Errorf(
			"%w: sync observed %s but balance after plus amount is %s",
			ErrInvalidQuotaLedgerEntry,
			observed.FormatUSD(),
			quota{units: replayed}.FormatUSD(),
		)
	}
	return nil
}

// ValidateQuotaBalance reproduces the projected balance by applying the entry
// to the balance before it, given in the provider's decimal form.
//
// The service uses it to prove that the entry it is about to append stays
// consistent with the provider balance; a removal that would drive the balance
// below zero is rejected because the provider cannot spend a negative pool.
func ValidateQuotaBalance(entry QuotaLedgerEntry, balanceBeforeUSD string) error {
	before, err := parseQuota(balanceBeforeUSD)
	if err != nil {
		return fmt.Errorf("%w: balance before: %v", ErrInvalidQuotaLedgerEntry, err)
	}
	amount, err := parseQuota(entry.AmountUSD)
	if err != nil {
		return err
	}
	after, ok := addQuota(before.units, amount.units)
	if !ok {
		return fmt.Errorf("%w: balance overflow", ErrInvalidQuotaLedgerEntry)
	}
	if after < 0 {
		return fmt.Errorf(
			"%w: balance %s plus %s would be negative",
			ErrInvalidQuotaLedgerEntry,
			before.FormatUSD(),
			amount.FormatUSD(),
		)
	}
	return nil
}

// VerifyQuotaChain applies entries in order and reports whether each one's
// recorded balance matches the recomputed balance, which is the audit property
// the consistency job depends on.
//
// It is a test and diagnostics helper, not part of the write path.
func VerifyQuotaChain(openingBalanceUSD string, entries []QuotaLedgerEntry) error {
	balance, err := parseQuota(openingBalanceUSD)
	if err != nil {
		return fmt.Errorf("%w: opening balance: %v", ErrInvalidQuotaLedgerEntry, err)
	}
	for index, entry := range entries {
		amount, amountErr := parseQuota(entry.AmountUSD)
		if amountErr != nil {
			return fmt.Errorf("entry %d: %w", index, amountErr)
		}
		next, ok := addQuota(balance.units, amount.units)
		if !ok {
			return fmt.Errorf("%w: entry %d overflows the balance", ErrInvalidQuotaLedgerEntry, index)
		}
		recorded, recordedErr := parseQuota(entry.BalanceAfterUSD)
		if recordedErr != nil {
			return fmt.Errorf("entry %d: %w", index, recordedErr)
		}
		if recorded.units != next {
			return fmt.Errorf(
				"%w: entry %d recorded balance %s but replay gives %s",
				ErrInvalidQuotaLedgerEntry,
				index,
				recorded.FormatUSD(),
				quota{units: next}.FormatUSD(),
			)
		}
		balance = quota{units: next}
	}
	return nil
}
