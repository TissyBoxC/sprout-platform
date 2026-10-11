// Package repository persists the guardian learning-visualization policy and
// the bounded per-device visualization status.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/learning_visualization/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for the learning_visualization
// module.
type Repository interface {
	// Current returns the family's policy or the package default.
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	// Save upserts the family policy and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	// RecordStatus upserts the latest device visualization status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed learning_visualization
// repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a learning_visualization repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a learning_visualization repository
// with an injected time source for deterministic tests.
func NewPostgresRepositoryWithClock(
	pool *pgxpool.Pool,
	timeSource clock.Clock,
) *PostgresRepository {
	repository := NewPostgresRepository(pool)
	if timeSource != nil {
		repository.clock = timeSource
	}
	return repository
}

// Current returns the family policy row or the default when it is absent.
func (r *PostgresRepository) Current(
	ctx context.Context,
	familyID string,
) (domain.Policy, error) {
	policy := domain.Default()
	var revision int
	var updatedAt = policy.UpdatedAt
	err := r.pool.QueryRow(ctx, `
		SELECT schema_version,
			enabled,
			feedback_style,
			animation_level,
			touch_feedback_enabled,
			reward_effects_enabled,
			lesson_linkage_enabled,
			max_session_minutes,
			revision,
			updated_at
		FROM learning_visualization_policies
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.SchemaVersion,
		&policy.Enabled,
		&policy.FeedbackStyle,
		&policy.AnimationLevel,
		&policy.TouchFeedbackEnabled,
		&policy.RewardEffectsEnabled,
		&policy.LessonLinkageEnabled,
		&policy.MaxSessionMinutes,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read learning visualization policy: %w", err)
	}
	policy.Revision = revision
	policy.UpdatedAt = updatedAt
	return policy, nil
}

// Save upserts the family policy and bumps its revision.
func (r *PostgresRepository) Save(
	ctx context.Context,
	familyID string,
	input domain.Input,
) (domain.Policy, error) {
	policy := domain.Default()
	err := r.pool.QueryRow(ctx, `
		INSERT INTO learning_visualization_policies (
			family_id,
			schema_version,
			enabled,
			feedback_style,
			animation_level,
			touch_feedback_enabled,
			reward_effects_enabled,
			lesson_linkage_enabled,
			max_session_minutes,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			enabled = EXCLUDED.enabled,
			feedback_style = EXCLUDED.feedback_style,
			animation_level = EXCLUDED.animation_level,
			touch_feedback_enabled = EXCLUDED.touch_feedback_enabled,
			reward_effects_enabled = EXCLUDED.reward_effects_enabled,
			lesson_linkage_enabled = EXCLUDED.lesson_linkage_enabled,
			max_session_minutes = EXCLUDED.max_session_minutes,
			revision = learning_visualization_policies.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.Enabled,
		input.FeedbackStyle,
		input.AnimationLevel,
		input.TouchFeedbackEnabled,
		input.RewardEffectsEnabled,
		input.LessonLinkageEnabled,
		input.MaxSessionMinutes,
	).Scan(&policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save learning visualization policy: %w", err)
	}
	policy.SchemaVersion = domain.SchemaVersion
	policy.Enabled = input.Enabled
	policy.FeedbackStyle = input.FeedbackStyle
	policy.AnimationLevel = input.AnimationLevel
	policy.TouchFeedbackEnabled = input.TouchFeedbackEnabled
	policy.RewardEffectsEnabled = input.RewardEffectsEnabled
	policy.LessonLinkageEnabled = input.LessonLinkageEnabled
	policy.MaxSessionMinutes = input.MaxSessionMinutes
	return policy, nil
}

// RecordStatus upserts the latest device visualization status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_learning_visualization_status (
			device_id,
			family_id,
			visualization_supported,
			touch_supported,
			linked_lesson_id,
			visual_state,
			feedback_style,
			animation_level,
			feedback_events,
			session_minutes,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			visualization_supported = EXCLUDED.visualization_supported,
			touch_supported = EXCLUDED.touch_supported,
			linked_lesson_id = EXCLUDED.linked_lesson_id,
			visual_state = EXCLUDED.visual_state,
			feedback_style = EXCLUDED.feedback_style,
			animation_level = EXCLUDED.animation_level,
			feedback_events = EXCLUDED.feedback_events,
			session_minutes = EXCLUDED.session_minutes,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		status.VisualizationSupported,
		status.TouchSupported,
		status.LinkedLessonID,
		status.VisualState,
		status.FeedbackStyle,
		status.AnimationLevel,
		status.FeedbackEvents,
		status.SessionMinutes,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record learning visualization device status: %w", err)
	}
	return nil
}

// Statuses returns every device status in a family.
func (r *PostgresRepository) Statuses(
	ctx context.Context,
	familyID string,
) ([]domain.Status, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT device_id,
			family_id,
			visualization_supported,
			touch_supported,
			linked_lesson_id,
			visual_state,
			feedback_style,
			animation_level,
			feedback_events,
			session_minutes,
			policy_revision,
			reported_at
		FROM device_learning_visualization_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list learning visualization device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&status.VisualizationSupported,
			&status.TouchSupported,
			&status.LinkedLessonID,
			&status.VisualState,
			&status.FeedbackStyle,
			&status.AnimationLevel,
			&status.FeedbackEvents,
			&status.SessionMinutes,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan learning visualization device status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate learning visualization device status: %w", err)
	}
	return statuses, nil
}

// AdminStats returns a bounded fleet summary over the requested window. A
// device is counted as outdated when its applied policy revision trails the
// current family revision; a family without a stored policy row is treated as
// revision 1, so a fresh device that reported revision 1 is up to date.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE s.visualization_supported),
			COUNT(*) FILTER (WHERE s.touch_supported),
			COUNT(*) FILTER (WHERE s.linked_lesson_id <> ''),
			COUNT(*) FILTER (
				WHERE s.policy_revision < COALESCE(p.revision, 1)
			),
			COALESCE(SUM(s.feedback_events), 0)
		FROM device_learning_visualization_status s
		LEFT JOIN learning_visualization_policies p
			ON p.family_id = s.family_id
		WHERE s.reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.SupportedCount,
		&stats.TouchCount,
		&stats.LessonLinkedCount,
		&stats.OutdatedCount,
		&stats.FeedbackEvents,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read learning visualization stats: %w", err)
	}

	stateBuckets, err := r.buckets(ctx, windowDays, "visual_state")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.StateBuckets = stateBuckets

	styleBuckets, err := r.buckets(ctx, windowDays, "feedback_style")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.StyleBuckets = styleBuckets
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

// buckets returns one column's histogram over the window. The column is chosen
// from a fixed allow-list so it can never be attacker-controlled.
func (r *PostgresRepository) buckets(
	ctx context.Context,
	windowDays int,
	column string,
) ([]domain.Count, error) {
	query := ""
	switch column {
	case "visual_state":
		query = `
			SELECT visual_state AS bucket, COUNT(*)
			FROM device_learning_visualization_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "feedback_style":
		query = `
			SELECT feedback_style AS bucket, COUNT(*)
			FROM device_learning_visualization_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	default:
		return nil, fmt.Errorf("unsupported learning visualization bucket column %q", column)
	}
	rows, err := r.pool.Query(ctx, query, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read learning visualization %s buckets: %w", column, err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 6)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan learning visualization %s bucket: %w", column, err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate learning visualization %s buckets: %w", column, err)
	}
	return buckets, nil
}
