package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LedgerOnlyGrantRetention is how long a quota granted without a quota service
// stays claimable by a later ConfirmGrant.
//
// A pending grant is a promise recorded but not delivered upstream. Keeping it
// claimable for a bounded window lets an operator wire up a quota service and
// finish the backlog, while an indefinitely claimable record would turn the
// ledger into a second, silently diverging source of truth.
const LedgerOnlyGrantRetention = 30 * 24 * time.Hour

// QuotaGrantState is what actually happened to the provider balance behind one
// purchase or refund.
//
// The distinction is the point of the type: "applied" means the gateway
// accepted the movement and the provider pool really changed, while
// "pending_provider_grant" means the family was charged, the ledger booked the
// quota, and the provider balance was never raised. Reporting the second as the
// first would tell a paying guardian they have quota they cannot use.
type QuotaGrantState string

const (
	// QuotaGrantApplied means the movement reached the provider balance.
	QuotaGrantApplied QuotaGrantState = "applied"
	// QuotaGrantPendingProvider means the ledger booked the movement but no
	// quota service was wired up, so the provider balance must still change.
	QuotaGrantPendingProvider QuotaGrantState = "pending_provider_grant"
)

// QuotaGrantReceipt reports the outcome of one quota movement.
type QuotaGrantReceipt struct {
	EntryID string
	State   QuotaGrantState
	// AmountUSD is the booked amount in the provider's decimal form.
	AmountUSD string
	// Message explains a pending outcome in operator language and is empty when
	// the movement was applied.
	Message string
}

// QuotaLedgerService records every model-quota movement beside the purchase or
// refund that caused it, and moves the provider balance in step when a quota
// service is available.
//
// The ledger is an audit trail, not the balance: ai_accounts.balance_usd stays
// the live provider value (design doc §1.1). What this service guarantees is
// that no movement is lost and that an undelivered one says so.
type QuotaLedgerService struct {
	base
}

// NewQuotaLedgerService creates the quota ledger service.
func NewQuotaLedgerService(options Options) (*QuotaLedgerService, error) {
	shared, err := newBase(options)
	if err != nil {
		return nil, err
	}
	return &QuotaLedgerService{base: shared}, nil
}

// GrantPurchaseQuota books the quota an order bought and raises the provider
// balance, inside the caller's transaction so a rolled-back settlement leaves
// no quota behind.
//
// With no quota service configured the entry is still written, marked pending
// upstream, and the receipt says so: the money is real, so dropping the grant
// would lose what the guardian paid for, and claiming success would be a lie.
func (s *QuotaLedgerService) GrantPurchaseQuota(
	ctx context.Context,
	tx pgx.Tx,
	order domain.Order,
	now time.Time,
) (QuotaGrantReceipt, error) {
	amount, err := quotaToMicroUnits(order.AIQuotaUSD)
	if err != nil {
		return QuotaGrantReceipt{}, fmt.Errorf("purchase quota for order %s: %w", order.OrderNo, err)
	}
	if amount.Sign() == 0 {
		// A zero-quota plan is legal, and the ledger rejects a zero entry, so
		// there is simply no quota movement to record.
		return QuotaGrantReceipt{State: QuotaGrantApplied, AmountUSD: "0"}, nil
	}

	detail := map[string]any{
		repository.LedgerDetailGrantState:           string(QuotaGrantPendingProvider),
		repository.LedgerDetailBalanceAfterUnbacked: true,
	}
	receipt := QuotaGrantReceipt{
		State:     QuotaGrantPendingProvider,
		AmountUSD: formatQuota(amount),
		Message:   "no AI quota service is configured; the provider balance was not raised",
	}
	if s.quota != nil {
		amountUSD, ok := quotaUSD(amount)
		if !ok {
			return QuotaGrantReceipt{}, fmt.Errorf(
				"%w: quota %s for order %s cannot be represented exactly",
				repository.ErrInvalidUSD, order.AIQuotaUSD, order.OrderNo)
		}
		if grantErr := s.quota.GrantQuota(
			ctx, order.ParentAccountID, amountUSD, "purchase "+order.OrderNo,
		); grantErr != nil {
			return QuotaGrantReceipt{}, fmt.Errorf(
				"grant AI quota for order %s: %w", order.OrderNo, grantErr)
		}
		detail = map[string]any{repository.LedgerDetailGrantState: string(QuotaGrantApplied)}
		receipt.State = QuotaGrantApplied
		receipt.Message = ""
	}

	balanceAfter, err := s.projectedBalanceAfter(ctx, order.ParentAccountID, amount)
	if err != nil {
		return QuotaGrantReceipt{}, err
	}
	entry := domain.QuotaLedgerEntry{
		ID:              uuid.NewString(),
		ParentAccountID: order.ParentAccountID,
		EntryType:       domain.QuotaEntryPurchaseGrant,
		AmountUSD:       formatQuota(amount),
		BalanceAfterUSD: balanceAfter,
		OrderID:         order.ID,
		// The plan's window is snapshotted onto the order, so the deadline is
		// resolved from the order rather than from the plan as it stands now.
		// A zero window means the grant never lapses.
		QuotaExpiresAt: quotaExpiryFor(order.QuotaValidityDays, now),
		Detail:         detail,
		CreatedAt:      now,
	}
	if err := domain.ValidateQuotaLedgerEntry(entry); err != nil {
		return QuotaGrantReceipt{}, fmt.Errorf("purchase quota entry: %w", err)
	}
	if err := s.repository.AppendQuotaLedgerEntry(ctx, tx, entry); err != nil {
		return QuotaGrantReceipt{}, err
	}
	receipt.EntryID = entry.ID
	return receipt, nil
}

// quotaExpiryFor resolves when a purchased grant stops being usable.
//
// A zero or negative window means "never expires", which is why it returns nil
// rather than a timestamp in the past: a plan with no stated window must not
// silently expire the quota a guardian just paid for.
func quotaExpiryFor(validityDays int, now time.Time) *time.Time {
	if validityDays <= 0 {
		return nil
	}
	expiresAt := now.AddDate(0, 0, validityDays)
	return &expiresAt
}

// ClawBackRefundQuota removes the quota a refund returned and lowers the
// provider balance by the same share the refund holds of the order.
//
// The platform granted order.AIQuotaUSD for order.PaidCents, so the returned
// share is exact proportional arithmetic rounded down: a partial refund never
// takes back more quota than the money it returns. It runs in the caller's
// transaction so the clawback and the refund commit together.
func (s *QuotaLedgerService) ClawBackRefundQuota(
	ctx context.Context,
	tx pgx.Tx,
	order domain.Order,
	refund domain.Refund,
	now time.Time,
) (QuotaGrantReceipt, error) {
	granted, err := quotaToMicroUnits(order.AIQuotaUSD)
	if err != nil {
		return QuotaGrantReceipt{}, fmt.Errorf("refund clawback for order %s: %w", order.OrderNo, err)
	}
	if granted.Sign() <= 0 || order.PaidCents <= 0 {
		return QuotaGrantReceipt{State: QuotaGrantApplied, AmountUSD: "0"}, nil
	}
	clawback := new(big.Int).Mul(granted, big.NewInt(int64(refund.AmountCents)))
	clawback.Quo(clawback, big.NewInt(int64(order.PaidCents)))
	if clawback.Sign() == 0 {
		return QuotaGrantReceipt{State: QuotaGrantApplied, AmountUSD: "0"}, nil
	}
	clawback.Neg(clawback)

	detail := map[string]any{
		repository.LedgerDetailGrantState: string(QuotaGrantPendingProvider),
	}
	receipt := QuotaGrantReceipt{
		State:     QuotaGrantPendingProvider,
		AmountUSD: formatQuota(clawback),
		Message:   "no AI quota service is configured; the provider balance was not lowered",
	}
	if s.quota != nil {
		amountUSD, ok := quotaUSD(clawback)
		if !ok {
			return QuotaGrantReceipt{}, fmt.Errorf(
				"%w: quota clawback for refund %s cannot be represented exactly",
				repository.ErrInvalidUSD, refund.RefundNo)
		}
		// clawback is already negative, so it crosses the seam as a signed
		// removal unchanged; negating it here would hand quota back instead.
		if clawErr := s.quota.ClawBackQuota(
			ctx, order.ParentAccountID, amountUSD, "refund "+refund.RefundNo,
		); clawErr != nil {
			return QuotaGrantReceipt{}, fmt.Errorf(
				"claw back AI quota for refund %s: %w", refund.RefundNo, clawErr)
		}
		detail = map[string]any{repository.LedgerDetailGrantState: string(QuotaGrantApplied)}
		receipt.State = QuotaGrantApplied
		receipt.Message = ""
	}

	balanceAfter, err := s.projectedBalanceAfter(ctx, order.ParentAccountID, clawback)
	if err != nil {
		return QuotaGrantReceipt{}, err
	}
	entry := domain.QuotaLedgerEntry{
		ID:              uuid.NewString(),
		ParentAccountID: order.ParentAccountID,
		EntryType:       domain.QuotaEntryRefundClawback,
		AmountUSD:       formatQuota(clawback),
		BalanceAfterUSD: balanceAfter,
		RefundID:        refund.ID,
		Detail:          detail,
		CreatedAt:       now,
	}
	if err := domain.ValidateQuotaLedgerEntry(entry); err != nil {
		return QuotaGrantReceipt{}, fmt.Errorf("refund clawback entry: %w", err)
	}
	if err := s.repository.AppendQuotaLedgerEntry(ctx, tx, entry); err != nil {
		return QuotaGrantReceipt{}, err
	}
	receipt.EntryID = entry.ID
	return receipt, nil
}

// ConfirmGrant finishes a purchase grant that was recorded while no quota
// service existed, by raising the provider balance now.
//
// The ledger row already exists, so this only moves the provider balance and
// flips the row's detail to applied. The state predicate makes it idempotent:
// a second call finds an applied grant and fails instead of crediting twice.
func (s *QuotaLedgerService) ConfirmGrant(
	ctx context.Context,
	entryID string,
) (QuotaGrantReceipt, error) {
	if s.quota == nil {
		return QuotaGrantReceipt{}, fmt.Errorf("%w: %w",
			ErrPaymentNotOpenYet, ErrQuotaServiceUnavailable)
	}
	var receipt QuotaGrantReceipt
	err := s.repository.WithTx(ctx, func(tx pgx.Tx) error {
		entry, loadErr := s.repository.LockQuotaLedgerEntry(ctx, tx, entryID)
		if loadErr != nil {
			return loadErr
		}
		if entry.EntryType != domain.QuotaEntryPurchaseGrant {
			return fmt.Errorf("%w: ledger entry %s is a %s, not a purchase grant",
				ErrInvalidRequest, entryID, entry.EntryType)
		}
		state, _ := entry.Detail[repository.LedgerDetailGrantState].(string)
		if state != repository.LedgerGrantStatePendingProvider {
			return fmt.Errorf("%w: grant %s is %q", ErrAlreadyResolved, entryID, state)
		}
		if entry.CreatedAt.Add(LedgerOnlyGrantRetention).Before(s.now()) {
			return fmt.Errorf("%w: grant %s is older than %s",
				ErrHoldExpired, entryID, LedgerOnlyGrantRetention)
		}
		amount, parseErr := quotaToMicroUnits(entry.AmountUSD)
		if parseErr != nil {
			return parseErr
		}
		amountUSD, ok := quotaUSD(amount)
		if !ok {
			return fmt.Errorf("%w: grant %s cannot be represented exactly",
				repository.ErrInvalidUSD, entryID)
		}
		if grantErr := s.quota.GrantQuota(
			ctx, entry.ParentAccountID, amountUSD, "purchase "+entry.OrderID,
		); grantErr != nil {
			return fmt.Errorf("grant AI quota for entry %s: %w", entryID, grantErr)
		}
		if markErr := s.repository.MarkQuotaLedgerGrantState(
			ctx, tx, entryID, repository.LedgerGrantStateApplied, s.now(),
		); markErr != nil {
			return markErr
		}
		receipt = QuotaGrantReceipt{
			EntryID:   entryID,
			State:     QuotaGrantApplied,
			AmountUSD: entry.AmountUSD,
		}
		return nil
	})
	if err != nil {
		return QuotaGrantReceipt{}, err
	}
	return receipt, nil
}

// ListLedger returns one family's quota movements for the guardian app.
func (s *QuotaLedgerService) ListLedger(
	ctx context.Context,
	parentAccountID string,
	page Page,
) ([]domain.QuotaLedgerEntry, int, error) {
	if parentAccountID == "" {
		return nil, 0, invalidRequest("parent account is required")
	}
	return s.repository.ListQuotaLedger(ctx, parentAccountID, page.Limit, page.Offset)
}

// formatQuota renders exact micro units in the provider's six-decimal form.
//
// Trailing zeros are kept because this is the value written into NUMERIC(18, 6)
// and read back by the AI gateway, and an operator comparing the ledger against
// the provider console expects the same shape on both sides.
func formatQuota(units *big.Int) string {
	text := repository.FormatUSDMicroUnits(units)
	whole, fraction, found := strings.Cut(text, ".")
	if !found {
		return text + "." + strings.Repeat("0", quotaDecimals)
	}
	if len(fraction) >= quotaDecimals {
		return text
	}
	return whole + "." + fraction + strings.Repeat("0", quotaDecimals-len(fraction))
}

// quotaDecimals mirrors the NUMERIC(18, 6) scale of the provider quota column.
const quotaDecimals = 6

// projectedBalanceAfter computes the balance recorded on a new entry.
//
// The ledger is additive: the new balance is the newest recorded balance plus
// the movement. A family with no entry yet anchors on the last provider balance
// the ledger observed; with no quota service at all it anchors on zero and the
// entry is flagged as unbacked, because there is no provider balance to trust.
func (s *QuotaLedgerService) projectedBalanceAfter(
	ctx context.Context,
	parentAccountID string,
	movement *big.Int,
) (string, error) {
	total, err := s.repository.SumQuotaLedgerForParent(ctx, parentAccountID)
	if err != nil {
		return "", err
	}
	anchor := new(big.Int)
	if total.EntryCount > 0 {
		latest, parseErr := quotaToMicroUnits(total.LatestBalanceAfterUSD)
		if parseErr != nil {
			return "", fmt.Errorf("latest ledger balance: %w", parseErr)
		}
		anchor = latest
	} else if s.quota != nil {
		observed, found, readErr := s.repository.LatestObservedProviderBalance(ctx, parentAccountID)
		if readErr != nil {
			return "", readErr
		}
		if found {
			parsed, parseErr := quotaToMicroUnits(observed)
			if parseErr != nil {
				return "", fmt.Errorf("observed provider balance: %w", parseErr)
			}
			anchor = parsed
		}
	}
	next := new(big.Int).Add(anchor, movement)
	if next.Sign() < 0 {
		// The ledger must never record a negative pool: the provider cannot
		// spend what it does not hold, and a negative projection would hide the
		// overdraw behind arithmetic.
		return "", fmt.Errorf("%w: balance %s plus %s would be negative",
			domain.ErrInvalidQuotaLedgerEntry,
			repository.FormatUSDMicroUnits(anchor),
			repository.FormatUSDMicroUnits(movement))
	}
	return formatQuota(next), nil
}

// ErrAlreadyResolved reports an action on a record that already reached its
// final state.
var ErrAlreadyResolved = errors.New("billing: record is already resolved")

// ErrQuotaServiceUnavailable reports that a ledger-only grant cannot be
// delivered because no quota service is wired up.
var ErrQuotaServiceUnavailable = errors.New("billing: AI quota service is not configured")

// ErrHoldExpired reports a pending grant past its claimable window.
var ErrHoldExpired = errors.New("billing: quota grant has expired")
