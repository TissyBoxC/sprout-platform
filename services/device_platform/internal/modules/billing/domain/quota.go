package domain

import "time"

// QuotaLedgerEntryType is the movement kind recorded in the quota ledger
// (design doc §1.1). The ledger is the only authoritative record of model
// quota, and ai_accounts.balance_usd is a projection of its sum.
type QuotaLedgerEntryType string

const (
	// QuotaEntryPurchaseGrant credits the quota an order bought.
	QuotaEntryPurchaseGrant QuotaLedgerEntryType = "purchase_grant"
	// QuotaEntryRefundClawback removes quota returned by a refund.
	QuotaEntryRefundClawback QuotaLedgerEntryType = "refund_clawback"
	// QuotaEntryAdminAdjustment is a manual operator correction. It must state
	// a reason and is always audited.
	QuotaEntryAdminAdjustment QuotaLedgerEntryType = "admin_adjustment"
	// QuotaEntryProviderSync records consumption or a difference reported by
	// the upstream provider. It is bookkeeping only and never overwrites the
	// projected balance.
	QuotaEntryProviderSync QuotaLedgerEntryType = "provider_sync"
	// QuotaEntryExpiry writes off quota that reached its validity end.
	QuotaEntryExpiry QuotaLedgerEntryType = "expiry"
)

// QuotaLedgerEntryTypes lists every declared entry type for exhaustive checks.
var QuotaLedgerEntryTypes = []QuotaLedgerEntryType{
	QuotaEntryPurchaseGrant,
	QuotaEntryRefundClawback,
	QuotaEntryAdminAdjustment,
	QuotaEntryProviderSync,
	QuotaEntryExpiry,
}

// AllowedDirection reports whether the entry type may add or remove quota.
//
// A purchase grant only ever adds and a clawback or expiry only ever removes;
// only a manual adjustment and a provider sync are genuinely signed. Enforcing
// the direction keeps a mislabelled entry from silently draining a paid
// balance while still looking like a legitimate grant.
func (entryType QuotaLedgerEntryType) AllowedDirection() (canAdd, canRemove bool) {
	switch entryType {
	case QuotaEntryPurchaseGrant:
		return true, false
	case QuotaEntryRefundClawback, QuotaEntryExpiry:
		return false, true
	case QuotaEntryAdminAdjustment, QuotaEntryProviderSync:
		return true, true
	default:
		return false, false
	}
}

// IsKnown reports whether the value is a declared entry type.
func (entryType QuotaLedgerEntryType) IsKnown() bool {
	for _, known := range QuotaLedgerEntryTypes {
		if known == entryType {
			return true
		}
	}
	return false
}

// QuotaLedgerEntry is one signed quota movement in the provider's USD unit.
//
// AmountUSD is a decimal string rather than a float64 for the same reason
// cents are integers: the balance is a sum, and a binary float would drift.
// The unit mirrors upstream `balance_usd` exactly and is never a CNY price.
type QuotaLedgerEntry struct {
	ID              string
	ParentAccountID string
	EntryType       QuotaLedgerEntryType
	// AmountUSD is signed: positive grants quota, negative removes it.
	AmountUSD string
	// BalanceAfterUSD is the running balance after this entry, kept for audit.
	BalanceAfterUSD string
	// ObservedProviderBalanceUSD is the provider balance seen when a
	// provider_sync entry was computed, and empty for every other type. The
	// recorded AmountUSD is always observed minus BalanceAfterUSD.
	ObservedProviderBalanceUSD string
	OrderID                    string
	RefundID                   string
	OperatorAccountID          string
	// Reason is required for a manual adjustment and empty otherwise.
	Reason string
	// QuotaExpiresAt is the deadline for a purchase_grant, resolved from the
	// plan's validity window when the order was paid. It stays on the ledger
	// row so the grant stays auditable after the plan is edited or archived.
	// Nil means the grant never expires.
	QuotaExpiresAt *time.Time
	Detail         map[string]any
	CreatedAt      time.Time
}

// IsGrant reports whether the entry direction adds quota. A zero amount is
// neither a grant nor a removal; callers should have rejected it already.
func (entry QuotaLedgerEntry) IsGrant() bool {
	return entry.Sign() > 0
}

// Sign returns +1 for a grant, -1 for a removal, and 0 for a zero or
// unparseable amount.
func (entry QuotaLedgerEntry) Sign() int {
	_, sign, err := parseUSD(entry.AmountUSD)
	if err != nil {
		return 0
	}
	return sign
}

// NeedsBalanceCheck reports whether the entry removes quota, which is the only
// direction that can overdraw a balance and therefore needs a balance check
// under the account row lock.
func (entry QuotaLedgerEntry) NeedsBalanceCheck() bool {
	return entry.Sign() < 0
}
