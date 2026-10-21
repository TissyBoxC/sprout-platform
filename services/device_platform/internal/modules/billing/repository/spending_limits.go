package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

// ErrSpendingLimitNotFound reports a family with no stored spending cap. The
// caller renders it as "unlimited" rather than as a failure.
var ErrSpendingLimitNotFound = errors.New("billing repository: family spending limit not found")

const familySpendingLimitColumns = `
	family_id,
	monthly_limit_cents,
	COALESCE(updated_by::text, ''),
	created_at,
	updated_at
`

// GetSpendingLimit returns one family's stored cap, or ErrSpendingLimitNotFound.
func (r *PostgresRepository) GetSpendingLimit(
	ctx context.Context,
	familyID string,
) (*domain.FamilySpendingLimit, error) {
	limit, err := scanSpendingLimit(r.database.QueryRow(ctx, `
		SELECT `+familySpendingLimitColumns+`
		FROM family_spending_limits
		WHERE family_id = $1
	`, familyID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSpendingLimitNotFound
	}
	if err != nil {
		return nil, err
	}
	return limit, nil
}

// UpsertSpendingLimit stores one family's cap, creating the row on first write.
//
// The family id is the primary key, so a concurrent second save updates the
// same row instead of creating a duplicate cap. created_at keeps the first
// setting time; updated_at always moves.
func (r *PostgresRepository) UpsertSpendingLimit(
	ctx context.Context,
	limit domain.FamilySpendingLimit,
) error {
	_, err := r.database.Exec(ctx, `
		INSERT INTO family_spending_limits (
			family_id,
			monthly_limit_cents,
			updated_by,
			created_at,
			updated_at
		) VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5)
		ON CONFLICT (family_id) DO UPDATE
		SET monthly_limit_cents = EXCLUDED.monthly_limit_cents,
		    updated_by = EXCLUDED.updated_by,
		    updated_at = EXCLUDED.updated_at
	`,
		limit.FamilyID,
		int64(limit.MonthlyLimitCents),
		limit.UpdatedBy,
		limit.CreatedAt,
		limit.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("save family spending limit: %w", err)
	}
	return nil
}

// SumMonthlyPaidCents sums the payable amount of every already-paid order the
// family settled inside [from, to).
//
// Payable, not refunded: the cap bounds what the guardian committed to spend,
// and a refund returns money without re-authorising a new purchase. The window
// is half-open so a payment at exactly the month boundary belongs to one month.
func (r *PostgresRepository) SumMonthlyPaidCents(
	ctx context.Context,
	familyID string,
	from time.Time,
	to time.Time,
) (domain.Cents, error) {
	var total int64
	if err := r.database.QueryRow(ctx, `
		SELECT COALESCE(SUM(payable_cny_cents), 0)
		FROM billing_orders
		WHERE parent_account_id = $1
		  AND paid_at IS NOT NULL
		  AND paid_at >= $2
		  AND paid_at < $3
		  AND status IN ('paid', 'partially_refunded', 'refunded')
	`, familyID, from, to).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum family monthly paid amount: %w", err)
	}
	return domain.Cents(total), nil
}

// AppendSpendingLimitRejection records one refused checkout.
//
// The row is written outside the checkout transaction on purpose: the refusal
// changed no order, and the compliance trail must not be rolled back with a
// request that failed for an unrelated reason after this point.
func (r *PostgresRepository) AppendSpendingLimitRejection(
	ctx context.Context,
	rejection domain.SpendingLimitRejection,
) error {
	_, err := r.database.Exec(ctx, `
		INSERT INTO family_spending_limit_rejections (
			id,
			family_id,
			monthly_limit_cents,
			consumed_cents,
			requested_cents,
			plan_code,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		rejection.ID,
		rejection.FamilyID,
		int64(rejection.MonthlyLimitCents),
		int64(rejection.ConsumedCents),
		int64(rejection.RequestedCents),
		rejection.PlanCode,
		rejection.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record spending limit rejection: %w", err)
	}
	return nil
}

// ListSpendingLimitRejections returns one family's rejections inside
// [from, to) together with the unrestricted total.
func (r *PostgresRepository) ListSpendingLimitRejections(
	ctx context.Context,
	familyID string,
	from time.Time,
	to time.Time,
	limit int,
	offset int,
) ([]domain.SpendingLimitRejection, int, error) {
	limit, offset = normalizePage(limit, offset)
	rows, err := r.database.Query(ctx, `
		SELECT
			id::text,
			family_id::text,
			monthly_limit_cents,
			consumed_cents,
			requested_cents,
			plan_code,
			created_at,
			COUNT(*) OVER () AS total
		FROM family_spending_limit_rejections
		WHERE family_id = $1
		  AND created_at >= $2
		  AND created_at < $3
		ORDER BY created_at DESC, id DESC
		LIMIT $4 OFFSET $5
	`, familyID, from, to, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list spending limit rejections: %w", err)
	}
	defer rows.Close()

	rejections := make([]domain.SpendingLimitRejection, 0)
	total := 0
	for rows.Next() {
		var rejection domain.SpendingLimitRejection
		var limitCents, consumedCents, requestedCents int64
		if err := rows.Scan(
			&rejection.ID,
			&rejection.FamilyID,
			&limitCents,
			&consumedCents,
			&requestedCents,
			&rejection.PlanCode,
			&rejection.CreatedAt,
			&total,
		); err != nil {
			return nil, 0, fmt.Errorf("scan spending limit rejection: %w", err)
		}
		rejection.MonthlyLimitCents = domain.Cents(limitCents)
		rejection.ConsumedCents = domain.Cents(consumedCents)
		rejection.RequestedCents = domain.Cents(requestedCents)
		rejections = append(rejections, rejection)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate spending limit rejections: %w", err)
	}
	return rejections, total, nil
}

// SpendingLimitMetrics returns the two Feature Center figures owned by this
// module: how many families have a cap, and how many checkouts were refused for
// exceeding one in the current calendar month (UTC).
func (r *PostgresRepository) SpendingLimitMetrics(
	ctx context.Context,
) (SpendingLimitMetrics, error) {
	var metrics SpendingLimitMetrics
	if err := r.database.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM family_spending_limits WHERE monthly_limit_cents > 0),
			(SELECT COUNT(*) FROM family_spending_limit_rejections
			 WHERE created_at >= date_trunc('month', NOW()))
	`).Scan(&metrics.ConfiguredFamilyCount, &metrics.MonthlyRejectionCount); err != nil {
		return SpendingLimitMetrics{}, fmt.Errorf("load spending limit metrics: %w", err)
	}
	return metrics, nil
}

// SpendingLimitReadiness probes the two tables the feature depends on by
// reading them. A missing table or an unreadable row reports unavailable
// storage, which the Feature Center must never translate into healthy.
func (r *PostgresRepository) SpendingLimitReadiness(ctx context.Context) error {
	var configuredFamilies int64
	if err := r.database.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM family_spending_limits
		WHERE monthly_limit_cents > 0
	`).Scan(&configuredFamilies); err != nil {
		return fmt.Errorf("read family spending limits: %w", err)
	}
	var recordedRejections int64
	if err := r.database.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM family_spending_limit_rejections
	`).Scan(&recordedRejections); err != nil {
		return fmt.Errorf("read family spending limit rejections: %w", err)
	}
	return nil
}

// SpendingLimitMetrics is the read-only operating view of the spending cap.
type SpendingLimitMetrics struct {
	ConfiguredFamilyCount int64
	MonthlyRejectionCount int64
}

func scanSpendingLimit(row rowScanner) (*domain.FamilySpendingLimit, error) {
	var limit domain.FamilySpendingLimit
	var monthlyLimitCents int64
	if err := row.Scan(
		&limit.FamilyID,
		&monthlyLimitCents,
		&limit.UpdatedBy,
		&limit.CreatedAt,
		&limit.UpdatedAt,
	); err != nil {
		return nil, err
	}
	limit.MonthlyLimitCents = domain.Cents(monthlyLimitCents)
	return &limit, nil
}
