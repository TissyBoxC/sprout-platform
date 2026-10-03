// Package repository persists parent policies.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for parent policies.
type Repository interface {
	Create(ctx context.Context, policy *domain.Policy) error
	GetByChildID(ctx context.Context, childID string) (*domain.Policy, error)
	GetByFamilyID(
		ctx context.Context,
		familyID string,
		childID string,
	) (*domain.Policy, error)
	ListByFamilyID(ctx context.Context, familyID string) ([]domain.Policy, error)
	Update(ctx context.Context, policy *domain.Policy) error
	DeleteByChildID(ctx context.Context, childID string) error
}

// PostgresRepository is the PostgreSQL-backed parent policy repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a parent policy repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create inserts one policy row. One child has exactly one active policy.
func (r *PostgresRepository) Create(
	ctx context.Context,
	policy *domain.Policy,
) error {
	disabledPeriods, err := json.Marshal(policy.DisabledPeriods)
	if err != nil {
		return fmt.Errorf("encode disabled periods: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO parent_policies (
			id,
			family_id,
			child_id,
			policy_version,
			daily_limit_minutes,
			allowed_categories,
			disabled_periods,
			max_volume_percent,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		policy.ID,
		policy.FamilyID,
		policy.ChildID,
		policy.PolicyVersion,
		policy.DailyLimitMinutes,
		policy.AllowedCategories,
		disabledPeriods,
		policy.MaxVolumePercent,
		policy.CreatedAt,
		policy.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert parent policy: %w", err)
	}
	return nil
}

// GetByChildID loads one policy without family scoping for internal callers.
func (r *PostgresRepository) GetByChildID(
	ctx context.Context,
	childID string,
) (*domain.Policy, error) {
	row := r.pool.QueryRow(ctx, parentPolicySelect+`
		WHERE child_id = $1
	`, childID)
	policy, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	return policy, nil
}

// GetByFamilyID loads one policy and enforces family ownership.
func (r *PostgresRepository) GetByFamilyID(
	ctx context.Context,
	familyID string,
	childID string,
) (*domain.Policy, error) {
	row := r.pool.QueryRow(ctx, parentPolicySelect+`
		WHERE family_id = $1
		  AND child_id = $2
	`, familyID, childID)
	policy, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	return policy, nil
}

// ListByFamilyID returns every policy owned by one guardian.
func (r *PostgresRepository) ListByFamilyID(
	ctx context.Context,
	familyID string,
) ([]domain.Policy, error) {
	rows, err := r.pool.Query(ctx, parentPolicySelect+`
		WHERE family_id = $1
		ORDER BY created_at ASC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list parent policies: %w", err)
	}
	defer rows.Close()

	policies := make([]domain.Policy, 0)
	for rows.Next() {
		policy, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		policies = append(policies, *policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate parent policies: %w", err)
	}
	return policies, nil
}

// Update replaces the editable policy fields and bumps the policy version.
func (r *PostgresRepository) Update(
	ctx context.Context,
	policy *domain.Policy,
) error {
	disabledPeriods, err := json.Marshal(policy.DisabledPeriods)
	if err != nil {
		return fmt.Errorf("encode disabled periods: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE parent_policies
		SET policy_version = $3,
		    daily_limit_minutes = $4,
		    allowed_categories = $5,
		    disabled_periods = $6,
		    max_volume_percent = $7,
		    updated_at = $8
		WHERE family_id = $1
		  AND child_id = $2
	`,
		policy.FamilyID,
		policy.ChildID,
		policy.PolicyVersion,
		policy.DailyLimitMinutes,
		policy.AllowedCategories,
		disabledPeriods,
		policy.MaxVolumePercent,
		policy.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update parent policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrPolicyNotFound
	}
	return nil
}

// DeleteByChildID removes one dependent policy.
func (r *PostgresRepository) DeleteByChildID(
	ctx context.Context,
	childID string,
) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM parent_policies
		WHERE child_id = $1
	`, childID)
	if err != nil {
		return fmt.Errorf("delete parent policy: %w", err)
	}
	return nil
}

const parentPolicySelect = `
	SELECT
		id,
		family_id,
		child_id,
		policy_version,
		daily_limit_minutes,
		allowed_categories,
		disabled_periods,
		max_volume_percent,
		created_at,
		updated_at
	FROM parent_policies
`

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanPolicy(row rowScanner) (*domain.Policy, error) {
	var policy domain.Policy
	var disabledPeriods []byte
	if err := row.Scan(
		&policy.ID,
		&policy.FamilyID,
		&policy.ChildID,
		&policy.PolicyVersion,
		&policy.DailyLimitMinutes,
		&policy.AllowedCategories,
		&disabledPeriods,
		&policy.MaxVolumePercent,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(disabledPeriods, &policy.DisabledPeriods); err != nil {
		return nil, fmt.Errorf("decode disabled periods: %w", err)
	}
	if policy.DisabledPeriods == nil {
		policy.DisabledPeriods = []domain.DisabledPeriod{}
	}
	return &policy, nil
}
