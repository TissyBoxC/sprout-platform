package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

// Detail keys that mark a purchase grant whose quota was booked locally but not
// pushed to the provider, because no quota service was wired up.
//
// This is the honest representation of a half-done grant: the ledger row exists
// so the money and the quota it bought cannot be lost or silently skipped, and
// its detail says the upstream balance was never raised and that the recorded
// balance_after is not backed by provider money yet. A job that later gets a
// quota service can query exactly these rows and finish them.
const (
	LedgerDetailGrantState           = "grant_state"
	LedgerGrantStatePendingProvider  = "pending_provider_grant"
	LedgerGrantStateApplied          = "applied"
	LedgerDetailBalanceAfterUnbacked = "balance_after_unbacked"
)

const quotaLedgerColumns = `
	id, parent_account_id, entry_type, amount_usd::text, balance_after_usd::text,
	observed_provider_balance_usd::text, COALESCE(order_id::text, ''),
	COALESCE(refund_id::text, ''), COALESCE(operator_account_id::text, ''),
	reason, detail, created_at
`

// QuotaLedgerTotal is the ledger view of one family's quota movements.
type QuotaLedgerTotal struct {
	// SumAmountUSD is the exact sum of every signed movement.
	SumAmountUSD string
	// LatestBalanceAfterUSD is the balance recorded on the newest entry, and
	// "0" when the family has no entry at all.
	LatestBalanceAfterUSD string
	// EntryCount is how many entries exist, so callers can tell an empty ledger
	// from a ledger that nets to zero.
	EntryCount int
}

// LockQuotaLedgerEntry locks one ledger row for a state transition, such as
// confirming a grant that was recorded without a quota service.
func (r *PostgresRepository) LockQuotaLedgerEntry(
	ctx context.Context,
	tx pgx.Tx,
	entryID string,
) (*domain.QuotaLedgerEntry, error) {
	entry, _, err := scanLedgerEntry(tx.QueryRow(ctx,
		`SELECT `+quotaLedgerColumns+` FROM ai_quota_ledger WHERE id = $1 FOR UPDATE`,
		entryID), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLedgerEntryNotFound
	}
	return entry, err
}

// AppendQuotaLedgerEntry writes one signed quota movement.
//
// It runs on the caller's transaction because a purchase grant must commit with
// the order settlement: a ledger row without a paid order, or a paid order
// without a ledger row, breaks the audit trail the design depends on. The
// partial unique indexes on order_id and refund_id turn a replay into a
// conflict instead of a second credit.
func (r *PostgresRepository) AppendQuotaLedgerEntry(
	ctx context.Context,
	tx pgx.Tx,
	entry domain.QuotaLedgerEntry,
) error {
	detail, err := json.Marshal(orEmptyObject(entry.Detail))
	if err != nil {
		return fmt.Errorf("encode quota ledger detail: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO ai_quota_ledger (
			id, parent_account_id, entry_type, amount_usd, balance_after_usd,
			observed_provider_balance_usd, order_id, refund_id,
			operator_account_id, reason, quota_expires_at, detail, created_at
		) VALUES (
			$1, $2, $3, $4::numeric, $5::numeric, $6::numeric, $7, $8, $9,
			$10, $11, $12::jsonb, $13
		)
	`,
		entry.ID,
		entry.ParentAccountID,
		string(entry.EntryType),
		entry.AmountUSD,
		entry.BalanceAfterUSD,
		nullableUSD(entry.ObservedProviderBalanceUSD),
		normalizeUUID(entry.OrderID),
		normalizeUUID(entry.RefundID),
		normalizeUUID(entry.OperatorAccountID),
		entry.Reason,
		entry.QuotaExpiresAt,
		string(detail),
		entry.CreatedAt,
	)
	if err != nil {
		if uniqueViolationOn(err,
			"ai_quota_ledger_purchase_unique_idx",
			"ai_quota_ledger_clawback_unique_idx") {
			return fmt.Errorf("%w: quota entry for order %q refund %q already exists",
				ErrAlreadySettled, entry.OrderID, entry.RefundID)
		}
		return fmt.Errorf("append AI quota ledger entry: %w", err)
	}
	return nil
}

// ListQuotaLedger returns one family's quota movements, newest first.
func (r *PostgresRepository) ListQuotaLedger(
	ctx context.Context,
	parentAccountID string,
	limit int,
	offset int,
) ([]domain.QuotaLedgerEntry, int, error) {
	limit, offset = normalizePage(limit, offset)
	rows, err := r.database.Query(ctx, `
		SELECT `+quotaLedgerColumns+`, COUNT(*) OVER () AS total
		FROM ai_quota_ledger
		WHERE parent_account_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, parentAccountID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list AI quota ledger: %w", err)
	}
	defer rows.Close()
	entries := make([]domain.QuotaLedgerEntry, 0)
	total := 0
	for rows.Next() {
		entry, rowTotal, scanErr := scanLedgerEntry(rows, true)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		total = rowTotal
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate AI quota ledger: %w", err)
	}
	return entries, total, nil
}

// SumQuotaLedgerForParent returns the exact signed total of one family's quota
// movements and the balance recorded on its newest entry.
//
// PostgreSQL performs the SUM on NUMERIC, so the total is exact; it is handed
// back as text and never as a float, because a float would introduce drift that
// the provider-balance consistency check would then report as a discrepancy.
func (r *PostgresRepository) SumQuotaLedgerForParent(
	ctx context.Context,
	parentAccountID string,
) (QuotaLedgerTotal, error) {
	var sumUSD string
	var latest *string
	var entryCount int
	err := r.database.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_usd), 0)::text,
		       COUNT(*),
		       (SELECT balance_after_usd::text
		        FROM ai_quota_ledger
		        WHERE parent_account_id = $1
		        ORDER BY created_at DESC, id DESC
		        LIMIT 1)
		FROM ai_quota_ledger
		WHERE parent_account_id = $1
	`, parentAccountID).Scan(&sumUSD, &entryCount, &latest)
	if err != nil {
		return QuotaLedgerTotal{}, fmt.Errorf("sum AI quota ledger: %w", err)
	}
	total := QuotaLedgerTotal{
		SumAmountUSD:          sumUSD,
		LatestBalanceAfterUSD: "0",
		EntryCount:            entryCount,
	}
	if latest != nil {
		total.LatestBalanceAfterUSD = *latest
	}
	return total, nil
}

// LatestObservedProviderBalance returns the provider balance seen by the newest
// provider_sync entry. found is false when the family has never been synced,
// which the consistency check must treat as "unknown", never as "balanced".
func (r *PostgresRepository) LatestObservedProviderBalance(
	ctx context.Context,
	parentAccountID string,
) (balanceUSD string, found bool, err error) {
	var observed *string
	queryErr := r.database.QueryRow(ctx, `
		SELECT observed_provider_balance_usd::text
		FROM ai_quota_ledger
		WHERE parent_account_id = $1
		  AND entry_type = 'provider_sync'
		  AND observed_provider_balance_usd IS NOT NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, parentAccountID).Scan(&observed)
	if errors.Is(queryErr, pgx.ErrNoRows) {
		return "", false, nil
	}
	if queryErr != nil {
		return "", false, fmt.Errorf("latest observed provider balance: %w", queryErr)
	}
	if observed == nil {
		return "", false, nil
	}
	return *observed, true, nil
}

// MarkQuotaLedgerGrantState rewrites the detail grant_state of a pending
// purchase grant once its provider balance has finally been raised.
//
// The predicate pins the previous state, so two recovery workers racing on the
// same entry cannot both succeed and grant the family twice. ai_quota_ledger is
// append-only and has no updated_at column, so the state lives in detail alone.
func (r *PostgresRepository) MarkQuotaLedgerGrantState(
	ctx context.Context,
	tx Querier,
	entryID string,
	state string,
	now time.Time,
) error {
	return execExpectingRow(ctx, tx, ErrAlreadySettled, `
		UPDATE ai_quota_ledger
		SET detail = jsonb_set(detail, '{`+LedgerDetailGrantState+`}', to_jsonb($2::text))
		WHERE id = $1
		  AND entry_type = 'purchase_grant'
		  AND detail->>'`+LedgerDetailGrantState+`' = '`+LedgerGrantStatePendingProvider+`'
	`, entryID, state)
}

// orEmptyObject substitutes an empty JSON object for a nil detail map, because
// the ai_quota_ledger_detail_object check rejects a JSON null.
func orEmptyObject(detail map[string]any) map[string]any {
	if detail == nil {
		return map[string]any{}
	}
	return detail
}

// nullableUSD renders an empty observed balance as SQL NULL.
func nullableUSD(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// scanLedgerEntry reads one quota ledger row, optionally with a windowed total.
func scanLedgerEntry(row rowScanner, withTotal bool) (*domain.QuotaLedgerEntry, int, error) {
	var entry domain.QuotaLedgerEntry
	var entryType string
	var detail []byte
	var total int
	targets := []any{
		&entry.ID,
		&entry.ParentAccountID,
		&entryType,
		&entry.AmountUSD,
		&entry.BalanceAfterUSD,
		&entry.ObservedProviderBalanceUSD,
		&entry.OrderID,
		&entry.RefundID,
		&entry.OperatorAccountID,
		&entry.Reason,
		&detail,
		&entry.CreatedAt,
	}
	if withTotal {
		targets = append(targets, &total)
	}
	if err := row.Scan(targets...); err != nil {
		return nil, 0, fmt.Errorf("scan AI quota ledger entry: %w", err)
	}
	entry.EntryType = domain.QuotaLedgerEntryType(entryType)
	if len(detail) > 0 {
		if err := json.Unmarshal(detail, &entry.Detail); err != nil {
			return nil, 0, fmt.Errorf("decode AI quota ledger detail: %w", err)
		}
	}
	return &entry, total, nil
}
