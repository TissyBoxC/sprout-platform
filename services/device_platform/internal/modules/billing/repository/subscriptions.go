package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

const subscriptionColumns = `
	id, parent_account_id, plan_id, plan_code, plan_display_name, status,
	current_period_start, current_period_end, source_order_id, auto_renew,
	created_at, updated_at
`

// UpsertActiveSubscription opens or renews the family's active period for a plan.
//
// The partial unique index allows one active row per (family, plan_code), so a
// renewal updates that row instead of stacking a second period. The new period
// starts at the later of now and the current period end, which extends an
// unexpired subscription and starts a fresh one after a lapse.
func (r *PostgresRepository) UpsertActiveSubscription(
	ctx context.Context,
	tx pgx.Tx,
	subscription domain.Subscription,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO billing_subscriptions (
			id, parent_account_id, plan_id, plan_code, plan_display_name, status,
			current_period_start, current_period_end, source_order_id, auto_renew,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, 'active', $6, $7, $8, $9, $10, $11
		)
		ON CONFLICT (parent_account_id, plan_code) WHERE status = 'active'
		DO UPDATE SET
			plan_id = EXCLUDED.plan_id,
			plan_display_name = EXCLUDED.plan_display_name,
			current_period_start = GREATEST(
				billing_subscriptions.current_period_start,
				LEAST(EXCLUDED.current_period_start, billing_subscriptions.current_period_end)
			),
			current_period_end = GREATEST(
				billing_subscriptions.current_period_end,
				EXCLUDED.current_period_end
			),
			source_order_id = EXCLUDED.source_order_id,
			auto_renew = EXCLUDED.auto_renew,
			updated_at = EXCLUDED.updated_at
	`,
		subscription.ID,
		subscription.ParentAccountID,
		normalizeUUID(subscription.PlanID),
		subscription.PlanCode,
		subscription.PlanDisplayName,
		subscription.CurrentPeriodStart,
		subscription.CurrentPeriodEnd,
		normalizeUUID(subscription.SourceOrderID),
		subscription.AutoRenew,
		subscription.CreatedAt,
		subscription.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert billing subscription: %w", err)
	}
	return nil
}

// GetActiveSubscriptions returns every active subscription of one family.
func (r *PostgresRepository) GetActiveSubscriptions(
	ctx context.Context,
	parentAccountID string,
	now time.Time,
) ([]domain.Subscription, error) {
	rows, err := r.database.Query(ctx, `
		SELECT `+subscriptionColumns+`
		FROM billing_subscriptions
		WHERE parent_account_id = $1
		  AND status = 'active'
		  AND current_period_end > $2
		ORDER BY current_period_end DESC
	`, parentAccountID, now)
	if err != nil {
		return nil, fmt.Errorf("list active billing subscriptions: %w", err)
	}
	defer rows.Close()
	subscriptions := make([]domain.Subscription, 0)
	for rows.Next() {
		subscription, scanErr := scanSubscription(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		subscriptions = append(subscriptions, *subscription)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate billing subscriptions: %w", err)
	}
	return subscriptions, nil
}

// ExpireDueSubscriptions moves active subscriptions whose period ended to
// expired and returns how many changed.
func (r *PostgresRepository) ExpireDueSubscriptions(ctx context.Context, now time.Time) (int, error) {
	tag, err := r.database.Exec(ctx, `
		UPDATE billing_subscriptions
		SET status = 'expired',
		    updated_at = $1
		WHERE status = 'active'
		  AND current_period_end <= $1
	`, now)
	if err != nil {
		return 0, fmt.Errorf("expire billing subscriptions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// scanSubscription reads one subscription row.
func scanSubscription(row rowScanner) (*domain.Subscription, error) {
	var subscription domain.Subscription
	var planID *string
	var statusValue string
	var sourceOrderID *string
	err := row.Scan(
		&subscription.ID,
		&subscription.ParentAccountID,
		&planID,
		&subscription.PlanCode,
		&subscription.PlanDisplayName,
		&statusValue,
		&subscription.CurrentPeriodStart,
		&subscription.CurrentPeriodEnd,
		&sourceOrderID,
		&subscription.AutoRenew,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan billing subscription: %w", err)
	}
	if planID != nil {
		subscription.PlanID = *planID
	}
	if sourceOrderID != nil {
		subscription.SourceOrderID = *sourceOrderID
	}
	subscription.Status = domain.SubscriptionStatus(statusValue)
	return &subscription, nil
}
