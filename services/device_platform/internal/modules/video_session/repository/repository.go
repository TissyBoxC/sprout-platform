// Package repository persists the guardian video-session media policy and the
// bounded per-device session status.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_session/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for the video_session module.
type Repository interface {
	// Current returns the family's policy or the package default.
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	// Save upserts the family policy and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	// RecordStatus upserts the latest device session status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed video-session repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a video-session repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a video-session repository with an
// injected time source for deterministic tests.
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
			camera_allowed,
			microphone_allowed,
			require_child_consent,
			quality_floor,
			ring_timeout_seconds,
			max_session_seconds,
			revision,
			updated_at
		FROM video_session_policies
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.SchemaVersion,
		&policy.Enabled,
		&policy.CameraAllowed,
		&policy.MicrophoneAllowed,
		&policy.RequireChildConsent,
		&policy.QualityFloor,
		&policy.RingTimeoutSeconds,
		&policy.MaxSessionSeconds,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read video session policy: %w", err)
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
		INSERT INTO video_session_policies (
			family_id,
			schema_version,
			enabled,
			camera_allowed,
			microphone_allowed,
			require_child_consent,
			quality_floor,
			ring_timeout_seconds,
			max_session_seconds,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			enabled = EXCLUDED.enabled,
			camera_allowed = EXCLUDED.camera_allowed,
			microphone_allowed = EXCLUDED.microphone_allowed,
			require_child_consent = EXCLUDED.require_child_consent,
			quality_floor = EXCLUDED.quality_floor,
			ring_timeout_seconds = EXCLUDED.ring_timeout_seconds,
			max_session_seconds = EXCLUDED.max_session_seconds,
			revision = video_session_policies.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.Enabled,
		input.CameraAllowed,
		input.MicrophoneAllowed,
		input.RequireChildConsent,
		input.QualityFloor,
		input.RingTimeoutSeconds,
		input.MaxSessionSeconds,
	).Scan(&policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save video session policy: %w", err)
	}
	policy.SchemaVersion = domain.SchemaVersion
	policy.Enabled = input.Enabled
	policy.CameraAllowed = input.CameraAllowed
	policy.MicrophoneAllowed = input.MicrophoneAllowed
	policy.RequireChildConsent = input.RequireChildConsent
	policy.QualityFloor = input.QualityFloor
	policy.RingTimeoutSeconds = input.RingTimeoutSeconds
	policy.MaxSessionSeconds = input.MaxSessionSeconds
	return policy, nil
}

// RecordStatus upserts the latest device session status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_video_session_status (
			device_id,
			family_id,
			session_supported,
			camera_allowed,
			microphone_allowed,
			call_state,
			quality,
			last_end_reason,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			session_supported = EXCLUDED.session_supported,
			camera_allowed = EXCLUDED.camera_allowed,
			microphone_allowed = EXCLUDED.microphone_allowed,
			call_state = EXCLUDED.call_state,
			quality = EXCLUDED.quality,
			last_end_reason = EXCLUDED.last_end_reason,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		status.SessionSupported,
		status.CameraAllowed,
		status.MicrophoneAllowed,
		status.CallState,
		status.Quality,
		status.LastEndReason,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record video session device status: %w", err)
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
			session_supported,
			camera_allowed,
			microphone_allowed,
			call_state,
			quality,
			last_end_reason,
			policy_revision,
			reported_at
		FROM device_video_session_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list video session device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&status.SessionSupported,
			&status.CameraAllowed,
			&status.MicrophoneAllowed,
			&status.CallState,
			&status.Quality,
			&status.LastEndReason,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan video session device status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate video session device status: %w", err)
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
			COUNT(*) FILTER (WHERE s.session_supported),
			COUNT(*) FILTER (
				WHERE s.session_supported
				AND (s.camera_allowed OR s.microphone_allowed)
			),
			COUNT(*) FILTER (
				WHERE s.policy_revision < COALESCE(p.revision, 1)
			),
			COUNT(*) FILTER (
				WHERE s.quality = 'poor'
			)
		FROM device_video_session_status s
		LEFT JOIN video_session_policies p ON p.family_id = s.family_id
		WHERE s.reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.SupportedCount,
		&stats.CaptureAllowed,
		&stats.OutdatedCount,
		&stats.PoorQualityCount,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read video session stats: %w", err)
	}

	stateBuckets, err := r.buckets(ctx, windowDays, "call_state")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.StateBuckets = stateBuckets

	reasonBuckets, err := r.buckets(ctx, windowDays, "last_end_reason")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.ReasonBuckets = reasonBuckets
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
	case "call_state":
		query = `
			SELECT call_state AS bucket, COUNT(*)
			FROM device_video_session_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "last_end_reason":
		query = `
			SELECT last_end_reason AS bucket, COUNT(*)
			FROM device_video_session_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	default:
		return nil, fmt.Errorf("unsupported video session bucket column %q", column)
	}
	rows, err := r.pool.Query(ctx, query, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read video session %s buckets: %w", column, err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 6)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan video session %s bucket: %w", column, err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate video session %s buckets: %w", column, err)
	}
	return buckets, nil
}

// Readiness aggregates active devices that advertise video calls.
func (r *PostgresRepository) Readiness(
	ctx context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	return featurehealth.ReadDeviceStateReadiness(ctx, featurehealth.DeviceStateQuery{
		Pool:            r.pool,
		Capability:      "video_call",
		StateTable:      "device_video_session_status",
		TimestampColumn: "reported_at",
		FaultCondition:  "status.call_state = 'failed' OR NOT (status.session_supported OR status.camera_allowed OR status.microphone_allowed)",
	}, freshness)
}
