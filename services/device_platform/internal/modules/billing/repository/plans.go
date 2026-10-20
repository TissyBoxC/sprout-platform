package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

// PlanFilter narrows an administrative plan listing. Zero values mean "any".
type PlanFilter struct {
	Status domain.PlanStatus
	// Search matches the code or display name case-insensitively.
	Search string
	Limit  int
	Offset int
}

// CreatePlan inserts one plan in its current status.
func (r *PostgresRepository) CreatePlan(ctx context.Context, plan domain.Plan) error {
	_, err := r.database.Exec(ctx, `
		INSERT INTO billing_plans (
			id, code, display_name, description, billing_period, period_months,
			price_cny_cents, original_price_cny_cents, ai_quota_usd,
			quota_validity_days, status,
			sort_order, version, published_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10, $11, $12, $13, $14, $15, $16
		)
	`,
		plan.ID,
		plan.Code,
		plan.DisplayName,
		plan.Description,
		string(plan.BillingPeriod),
		plan.PeriodMonths,
		int64(plan.PriceCents),
		int64(plan.OriginalPriceCents),
		plan.AIQuotaUSD,
		plan.QuotaValidityDays,
		string(plan.Status),
		plan.SortOrder,
		plan.Version,
		plan.PublishedAt,
		plan.CreatedAt,
		plan.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: plan code %q", ErrPlanCodeTaken, plan.Code)
		}
		return fmt.Errorf("insert billing plan: %w", err)
	}
	return nil
}

// UpdatePlan rewrites the editable plan fields and bumps the version, but only
// when expectedVersion still matches the stored row.
//
// The version predicate is the whole point: two operators editing one published
// price must not silently overwrite each other, so a mismatched version returns
// ErrVersionConflict and touches nothing.
func (r *PostgresRepository) UpdatePlan(
	ctx context.Context,
	plan domain.Plan,
	expectedVersion int,
) error {
	tag, err := r.database.Exec(ctx, `
		UPDATE billing_plans
		SET display_name = $2,
		    description = $3,
		    billing_period = $4,
		    period_months = $5,
		    price_cny_cents = $6,
		    original_price_cny_cents = $7,
		    ai_quota_usd = $8::numeric,
		    quota_validity_days = $9,
		    status = $10,
		    sort_order = $11,
		    published_at = $12,
		    version = version + 1,
		    updated_at = $13
		WHERE id = $1
		  AND version = $14
	`,
		plan.ID,
		plan.DisplayName,
		plan.Description,
		string(plan.BillingPeriod),
		plan.PeriodMonths,
		int64(plan.PriceCents),
		int64(plan.OriginalPriceCents),
		plan.AIQuotaUSD,
		plan.QuotaValidityDays,
		string(plan.Status),
		plan.SortOrder,
		plan.PublishedAt,
		plan.UpdatedAt,
		expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update billing plan: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	// Zero rows is either a stale version or a missing plan; distinguishing the
	// two lets the API answer 409 for a conflict and 404 for a bad id.
	var existingCode string
	var existingVersion int
	lookupErr := r.database.QueryRow(ctx,
		`SELECT code, version FROM billing_plans WHERE id = $1`, plan.ID,
	).Scan(&existingCode, &existingVersion)
	if errors.Is(lookupErr, pgx.ErrNoRows) {
		return domain.ErrPlanNotFound
	}
	if lookupErr != nil {
		return fmt.Errorf("read billing plan version: %w", lookupErr)
	}
	return fmt.Errorf("%w: plan %q is at version %d, not %d",
		ErrVersionConflict, existingCode, existingVersion, expectedVersion)
}

// ArchivePlan moves a plan to the archived status, keeping its code reserved so
// historical orders and subscriptions still resolve.
func (r *PostgresRepository) ArchivePlan(ctx context.Context, planID string, now time.Time) error {
	tag, err := r.database.Exec(ctx, `
		UPDATE billing_plans
		SET status = 'archived',
		    version = version + 1,
		    updated_at = $2
		WHERE id = $1
		  AND status <> 'archived'
	`, planID, now)
	if err != nil {
		return fmt.Errorf("archive billing plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either the plan does not exist or it was already archived; both are
		// reported as not found so a repeated archive is not an error.
		return domain.ErrPlanNotFound
	}
	return nil
}

// collectPlans drains a plan result set and closes it.

const planColumns = `
	id, code, display_name, description, billing_period, period_months,
	price_cny_cents, original_price_cny_cents, ai_quota_usd::text,
	quota_validity_days, status,
	sort_order, version, published_at, created_at, updated_at
`

// GetPlanByID returns the plan with the given id, or domain.ErrPlanNotFound.
func (r *PostgresRepository) GetPlanByID(ctx context.Context, planID string) (*domain.Plan, error) {
	return scanPlan(r.database.QueryRow(ctx,
		`SELECT `+planColumns+` FROM billing_plans WHERE id = $1`, planID))
}

// GetPlanByCode returns the plan with the given stable business code.
func (r *PostgresRepository) GetPlanByCode(ctx context.Context, code string) (*domain.Plan, error) {
	return scanPlan(r.database.QueryRow(ctx,
		`SELECT `+planColumns+` FROM billing_plans WHERE code = $1`, code))
}

// ListActivePlans returns the purchasable catalogue in display order.
func (r *PostgresRepository) ListActivePlans(ctx context.Context) ([]domain.Plan, error) {
	rows, err := r.database.Query(ctx, `
		SELECT `+planColumns+`
		FROM billing_plans
		WHERE status = 'active'
		ORDER BY sort_order ASC, created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list active billing plans: %w", err)
	}
	return collectPlans(rows)
}

// ListPlans returns a page of plans for the operator console.
func (r *PostgresRepository) ListPlans(
	ctx context.Context,
	filter PlanFilter,
) ([]domain.Plan, int, error) {
	limit, offset := normalizePage(filter.Limit, filter.Offset)
	// One statement lists and counts so the page and the total cannot come from
	// two different snapshots.
	rows, err := r.database.Query(ctx, `
		SELECT `+planColumns+`, COUNT(*) OVER () AS total
		FROM billing_plans
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR code ILIKE '%' || $2 || '%'
		       OR display_name ILIKE '%' || $2 || '%')
		ORDER BY sort_order ASC, created_at ASC
		LIMIT $3 OFFSET $4
	`, string(filter.Status), filter.Search, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list billing plans: %w", err)
	}
	defer rows.Close()
	plans := make([]domain.Plan, 0)
	total := 0
	for rows.Next() {
		plan, rowTotal, scanErr := scanPlanWithTotal(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		total = rowTotal
		plans = append(plans, *plan)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate billing plans: %w", err)
	}
	return plans, total, nil
}

// scanPlan reads one plan row, mapping a missing row to the domain error.
func scanPlan(row pgx.Row) (*domain.Plan, error) {
	var plan domain.Plan
	var billingPeriod string
	var status string
	var price int64
	var originalPrice int64
	if err := row.Scan(
		&plan.ID,
		&plan.Code,
		&plan.DisplayName,
		&plan.Description,
		&billingPeriod,
		&plan.PeriodMonths,
		&price,
		&originalPrice,
		&plan.AIQuotaUSD,
		&plan.QuotaValidityDays,
		&status,
		&plan.SortOrder,
		&plan.Version,
		&plan.PublishedAt,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPlanNotFound
		}
		return nil, fmt.Errorf("scan billing plan: %w", err)
	}
	plan.BillingPeriod = domain.BillingPeriod(billingPeriod)
	plan.Status = domain.PlanStatus(status)
	plan.PriceCents = domain.Cents(price)
	plan.OriginalPriceCents = domain.Cents(originalPrice)
	return &plan, nil
}

// scanPlanWithTotal reads a plan row that also carries the windowed total.
func scanPlanWithTotal(row rowScanner) (*domain.Plan, int, error) {
	var plan domain.Plan
	var billingPeriod string
	var status string
	var price int64
	var originalPrice int64
	var total int
	if err := row.Scan(
		&plan.ID,
		&plan.Code,
		&plan.DisplayName,
		&plan.Description,
		&billingPeriod,
		&plan.PeriodMonths,
		&price,
		&originalPrice,
		&plan.AIQuotaUSD,
		&plan.QuotaValidityDays,
		&status,
		&plan.SortOrder,
		&plan.Version,
		&plan.PublishedAt,
		&plan.CreatedAt,
		&plan.UpdatedAt,
		&total,
	); err != nil {
		return nil, 0, fmt.Errorf("scan billing plan: %w", err)
	}
	plan.BillingPeriod = domain.BillingPeriod(billingPeriod)
	plan.Status = domain.PlanStatus(status)
	plan.PriceCents = domain.Cents(price)
	plan.OriginalPriceCents = domain.Cents(originalPrice)
	return &plan, total, nil
}

// collectPlans drains a plan result set and closes it.
func collectPlans(rows pgx.Rows) ([]domain.Plan, error) {
	defer rows.Close()
	plans := make([]domain.Plan, 0)
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate billing plans: %w", err)
	}
	return plans, nil
}
