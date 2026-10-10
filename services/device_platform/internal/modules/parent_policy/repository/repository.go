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
	ListByFamilyIDWithRevision(
		ctx context.Context,
		familyID string,
	) ([]domain.Policy, int64, error)
	Update(ctx context.Context, policy *domain.Policy) error
	UpdateWithVersion(
		ctx context.Context,
		policy *domain.Policy,
		expectedVersion int,
	) error
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
			voice_continuous_enabled,
			voice_idle_window_seconds,
			voice_barge_in_enabled,
			voice_far_field_enabled,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`,
		policy.ID,
		policy.FamilyID,
		policy.ChildID,
		policy.PolicyVersion,
		policy.DailyLimitMinutes,
		policy.AllowedCategories,
		disabledPeriods,
		policy.MaxVolumePercent,
		policy.VoiceConversation.ContinuousConversationEnabled,
		policy.VoiceConversation.IdleWindowSeconds,
		policy.VoiceConversation.BargeInEnabled,
		policy.VoiceConversation.FarFieldEnabled,
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
	return r.update(ctx, policy, 0)
}

// UpdateWithVersion updates one policy only when the stored version still
// matches the guardian's snapshot. The SQL predicate is the final race guard.
func (r *PostgresRepository) UpdateWithVersion(
	ctx context.Context,
	policy *domain.Policy,
	expectedVersion int,
) error {
	return r.update(ctx, policy, expectedVersion)
}

func (r *PostgresRepository) update(
	ctx context.Context,
	policy *domain.Policy,
	expectedVersion int,
) error {
	disabledPeriods, err := json.Marshal(policy.DisabledPeriods)
	if err != nil {
		return fmt.Errorf("encode disabled periods: %w", err)
	}
	query := `
		UPDATE parent_policies
		SET policy_version = $3,
		    daily_limit_minutes = $4,
		    allowed_categories = $5,
		    disabled_periods = $6,
		    max_volume_percent = $7,
		    voice_continuous_enabled = $8,
		    voice_idle_window_seconds = $9,
		    voice_barge_in_enabled = $10,
		    voice_far_field_enabled = $11,
		    updated_at = $12
		WHERE family_id = $1
		  AND child_id = $2
	`
	args := []any{
		policy.FamilyID,
		policy.ChildID,
		policy.PolicyVersion,
		policy.DailyLimitMinutes,
		policy.AllowedCategories,
		disabledPeriods,
		policy.MaxVolumePercent,
		policy.VoiceConversation.ContinuousConversationEnabled,
		policy.VoiceConversation.IdleWindowSeconds,
		policy.VoiceConversation.BargeInEnabled,
		policy.VoiceConversation.FarFieldEnabled,
		policy.UpdatedAt,
	}
	if expectedVersion > 0 {
		query += " AND policy_version = $13"
		args = append(args, expectedVersion)
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update parent policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if expectedVersion > 0 {
			if _, lookupErr := r.GetByFamilyID(
				ctx,
				policy.FamilyID,
				policy.ChildID,
			); lookupErr == nil {
				return domain.ErrPolicyVersionConflict
			} else if !errors.Is(lookupErr, domain.ErrPolicyNotFound) {
				return lookupErr
			}
		}
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

// ListByFamilyIDWithRevision returns policies and their revision from one
// repeatable-read snapshot. Reading them separately could pair old policy
// content with a newer revision and cause a device to skip the next update.
func (r *PostgresRepository) ListByFamilyIDWithRevision(
	ctx context.Context,
	familyID string,
) ([]domain.Policy, int64, error) {
	transaction, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("begin parent policy snapshot: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	rows, err := transaction.Query(ctx, parentPolicySelect+`
		WHERE family_id = $1
		ORDER BY created_at ASC
	`, familyID)
	if err != nil {
		return nil, 0, fmt.Errorf("list parent policy snapshot: %w", err)
	}
	policies := make([]domain.Policy, 0)
	for rows.Next() {
		policy, scanErr := scanPolicy(rows)
		if scanErr != nil {
			rows.Close()
			return nil, 0, scanErr
		}
		policies = append(policies, *policy)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("iterate parent policy snapshot: %w", err)
	}
	rows.Close()
	if len(policies) == 0 {
		return nil, 0, domain.ErrPolicyNotFound
	}

	var revision int64
	if err := transaction.QueryRow(ctx, `
		SELECT revision
		FROM parent_policy_revisions
		WHERE family_id = $1
	`, familyID).Scan(&revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, domain.ErrPolicyNotFound
		}
		return nil, 0, fmt.Errorf("read parent policy revision: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit parent policy snapshot: %w", err)
	}
	return policies, revision, nil
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
		voice_continuous_enabled,
		voice_idle_window_seconds,
		voice_barge_in_enabled,
		voice_far_field_enabled,
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
		&policy.VoiceConversation.ContinuousConversationEnabled,
		&policy.VoiceConversation.IdleWindowSeconds,
		&policy.VoiceConversation.BargeInEnabled,
		&policy.VoiceConversation.FarFieldEnabled,
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
