package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

const runColumns = `
	id, channel, business_date, status, channel_total_cny_cents,
	platform_total_cny_cents, difference_cny_cents, matched_count,
	missing_count, extra_count, started_at, finished_at, error_message,
	created_at, updated_at
`

// CreateRun reserves the (channel, business_date) reconciliation slot.
//
// The unique index means one run per channel and day. A second attempt is
// reported as ErrRunAlreadyExists so the caller can retry the existing run
// instead of producing competing totals for the same day.
func (r *PostgresRepository) CreateRun(ctx context.Context, run domain.ReconciliationRun) error {
	_, err := r.database.Exec(ctx, `
		INSERT INTO billing_reconciliation_runs (
			id, channel, business_date, status, channel_total_cny_cents,
			platform_total_cny_cents, difference_cny_cents, matched_count,
			missing_count, extra_count, started_at, finished_at, error_message,
			created_at, updated_at
		) VALUES (
			$1, $2, $3::date, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		)
	`,
		run.ID,
		string(run.Channel),
		businessDate(run.BusinessDate),
		string(run.Status),
		int64(run.ChannelTotalCents),
		int64(run.PlatformTotalCents),
		int64(run.DifferenceCents),
		run.MatchedCount,
		run.MissingCount,
		run.ExtraCount,
		run.StartedAt,
		run.FinishedAt,
		run.ErrorMessage,
		run.CreatedAt,
		run.UpdatedAt,
	)
	if err != nil {
		if uniqueViolationOn(err, "billing_reconciliation_runs_unique_idx") {
			return fmt.Errorf("%w: %s %s", ErrRunAlreadyExists, run.Channel, businessDate(run.BusinessDate))
		}
		return fmt.Errorf("insert billing reconciliation run: %w", err)
	}
	return nil
}

// FinishRun writes the final state of a reconciliation run.
//
// A completed run must carry finished_at; failure keeps the caller's message so
// the console can explain why the day was not verified.
func (r *PostgresRepository) FinishRun(ctx context.Context, run domain.ReconciliationRun) error {
	if run.Status != domain.ReconciliationStatusFailed &&
		run.Status != domain.ReconciliationStatusCompleted {
		return fmt.Errorf("%w: run cannot finish as %q",
			domain.ErrInvalidOrderTransition, run.Status)
	}
	finishedAt := run.FinishedAt
	if finishedAt == nil {
		now := run.UpdatedAt
		finishedAt = &now
	}
	return execExpectingRow(ctx, r.database, ErrRunNotFound, `
		UPDATE billing_reconciliation_runs
		SET status = $2,
		    channel_total_cny_cents = $3,
		    platform_total_cny_cents = $4,
		    difference_cny_cents = $5,
		    matched_count = $6,
		    missing_count = $7,
		    extra_count = $8,
		    finished_at = $9,
		    error_message = $10,
		    updated_at = $11
		WHERE id = $1
	`, run.ID,
		string(run.Status),
		int64(run.ChannelTotalCents),
		int64(run.PlatformTotalCents),
		int64(run.DifferenceCents),
		run.MatchedCount,
		run.MissingCount,
		run.ExtraCount,
		finishedAt,
		run.ErrorMessage,
		run.UpdatedAt,
	)
}

// ListRuns returns the most recent reconciliation runs, newest first.
func (r *PostgresRepository) ListRuns(ctx context.Context, limit int, offset int) ([]domain.ReconciliationRun, error) {
	limit, offset = normalizePage(limit, offset)
	rows, err := r.database.Query(ctx, `
		SELECT `+runColumns+`
		FROM billing_reconciliation_runs
		ORDER BY business_date DESC, channel ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list billing reconciliation runs: %w", err)
	}
	defer rows.Close()
	runs := make([]domain.ReconciliationRun, 0)
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate billing reconciliation runs: %w", err)
	}
	return runs, nil
}

// GetLatestRun returns the newest run for one channel, or nil when none exists.
func (r *PostgresRepository) GetLatestRun(
	ctx context.Context,
	channel domain.PaymentChannel,
) (*domain.ReconciliationRun, error) {
	run, err := scanRun(r.database.QueryRow(ctx, `
		SELECT `+runColumns+`
		FROM billing_reconciliation_runs
		WHERE channel = $1
		ORDER BY business_date DESC
		LIMIT 1
	`, string(channel)))
	if errors.Is(err, domain.ErrOrderNotFound) {
		return nil, nil
	}
	return run, err
}

// InsertItem records one reconciled transaction inside a run.
func (r *PostgresRepository) InsertItem(ctx context.Context, tx pgx.Tx, item domain.ReconciliationItem) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO billing_reconciliation_items (
			id, run_id, channel_trade_no, order_no, channel_cny_cents,
			platform_cny_cents, discrepancy, resolved, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		item.ID,
		item.RunID,
		item.ChannelTradeNo,
		item.OrderNo,
		int64(item.ChannelCents),
		int64(item.PlatformCents),
		string(item.Discrepancy),
		item.Resolved,
		item.CreatedAt,
		item.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert billing reconciliation item: %w", err)
	}
	return nil
}

// ListItems returns one run's items, unresolved discrepancies first so an
// operator works the money that does not match before the rest.
func (r *PostgresRepository) ListItems(
	ctx context.Context,
	runID string,
	unresolvedOnly bool,
) ([]domain.ReconciliationItem, error) {
	rows, err := r.database.Query(ctx, `
		SELECT id, run_id, channel_trade_no, order_no, channel_cny_cents,
		       platform_cny_cents, discrepancy, resolved, created_at, updated_at
		FROM billing_reconciliation_items
		WHERE run_id = $1
		  AND ($2 = FALSE OR resolved = FALSE)
		ORDER BY resolved ASC, created_at ASC
	`, runID, unresolvedOnly)
	if err != nil {
		return nil, fmt.Errorf("list billing reconciliation items: %w", err)
	}
	defer rows.Close()
	items := make([]domain.ReconciliationItem, 0)
	for rows.Next() {
		var item domain.ReconciliationItem
		var channelCents int64
		var platformCents int64
		var discrepancy string
		if scanErr := rows.Scan(
			&item.ID,
			&item.RunID,
			&item.ChannelTradeNo,
			&item.OrderNo,
			&channelCents,
			&platformCents,
			&discrepancy,
			&item.Resolved,
			&item.CreatedAt,
			&item.UpdatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("scan billing reconciliation item: %w", scanErr)
		}
		item.ChannelCents = domain.Cents(channelCents)
		item.PlatformCents = domain.Cents(platformCents)
		item.Discrepancy = domain.ReconciliationDiscrepancy(discrepancy)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate billing reconciliation items: %w", err)
	}
	return items, nil
}

// MarkItemResolved records that an operator investigated one discrepancy.
func (r *PostgresRepository) MarkItemResolved(
	ctx context.Context,
	itemID string,
	resolved bool,
	now time.Time,
) error {
	return execExpectingRow(ctx, r.database, ErrItemNotFound, `
		UPDATE billing_reconciliation_items
		SET resolved = $2,
		    updated_at = $3
		WHERE id = $1
	`, itemID, resolved, now)
}

// ListPaymentsForReconciliation returns the platform's settled payments of one
// channel on one business day, keyed by provider trade number.
//
// Only succeeded payments carry a trade number, and only they represent money
// the platform believes the channel settled, so failed attempts are excluded
// rather than reported as platform-only.
func (r *PostgresRepository) ListPaymentsForReconciliation(
	ctx context.Context,
	channel domain.PaymentChannel,
	from time.Time,
	to time.Time,
) ([]SettledPayment, error) {
	rows, err := r.database.Query(ctx, `
		SELECT payments.channel_trade_no, orders.order_no, payments.amount_cny_cents,
		       payments.paid_at
		FROM billing_payments AS payments
		JOIN billing_orders AS orders ON orders.id = payments.order_id
		WHERE payments.channel = $1
		  AND payments.status = 'succeeded'
		  AND payments.paid_at >= $2
		  AND payments.paid_at < $3
		ORDER BY payments.paid_at ASC
	`, string(channel), from, to)
	if err != nil {
		return nil, fmt.Errorf("list settled payments for reconciliation: %w", err)
	}
	defer rows.Close()
	settled := make([]SettledPayment, 0)
	for rows.Next() {
		var payment SettledPayment
		var amount int64
		var paidAt *time.Time
		if scanErr := rows.Scan(&payment.ChannelTradeNo, &payment.OrderNo, &amount, &paidAt); scanErr != nil {
			return nil, fmt.Errorf("scan settled payment: %w", scanErr)
		}
		payment.AmountCents = domain.Cents(amount)
		if paidAt != nil {
			payment.PaidAt = *paidAt
		}
		settled = append(settled, payment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settled payments: %w", err)
	}
	return settled, nil
}

// GetOrderStatusesByNumbers resolves the status of several orders at once, used
// to tell "the callback is still in flight" from "the platform never saw it".
func (r *PostgresRepository) GetOrderStatusesByNumbers(
	ctx context.Context,
	orderNumbers []string,
) (map[string]domain.OrderStatus, error) {
	statuses := make(map[string]domain.OrderStatus, len(orderNumbers))
	if len(orderNumbers) == 0 {
		return statuses, nil
	}
	rows, err := r.database.Query(ctx, `
		SELECT order_no, status
		FROM billing_orders
		WHERE order_no = ANY($1::text[])
	`, orderNumbers)
	if err != nil {
		return nil, fmt.Errorf("list order statuses for reconciliation: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var orderNo string
		var status string
		if scanErr := rows.Scan(&orderNo, &status); scanErr != nil {
			return nil, fmt.Errorf("scan order status for reconciliation: %w", scanErr)
		}
		statuses[orderNo] = domain.OrderStatus(status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order statuses: %w", err)
	}
	return statuses, nil
}

// SettledPayment is one platform payment the reconciliation algorithm compares
// against the channel statement.
type SettledPayment struct {
	ChannelTradeNo string
	OrderNo        string
	AmountCents    domain.Cents
	PaidAt         time.Time
}

// businessDate renders a run's day as a DATE literal input.
func businessDate(value time.Time) string {
	return value.UTC().Format(time.DateOnly)
}

// scanRun reads one reconciliation run row.
func scanRun(row rowScanner) (*domain.ReconciliationRun, error) {
	var run domain.ReconciliationRun
	var channelValue string
	var statusValue string
	var channelTotal int64
	var platformTotal int64
	var difference int64
	err := row.Scan(
		&run.ID,
		&channelValue,
		&run.BusinessDate,
		&statusValue,
		&channelTotal,
		&platformTotal,
		&difference,
		&run.MatchedCount,
		&run.MissingCount,
		&run.ExtraCount,
		&run.StartedAt,
		&run.FinishedAt,
		&run.ErrorMessage,
		&run.CreatedAt,
		&run.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan billing reconciliation run: %w", err)
	}
	run.Channel = domain.PaymentChannel(channelValue)
	run.Status = domain.ReconciliationStatus(statusValue)
	run.ChannelTotalCents = domain.Cents(channelTotal)
	run.PlatformTotalCents = domain.Cents(platformTotal)
	run.DifferenceCents = domain.Cents(difference)
	return &run, nil
}
