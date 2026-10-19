// Package repository persists the guardian image-privacy policy and the
// bounded per-device image pipeline status.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/image_privacy/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for the image_privacy module.
type Repository interface {
	// Current returns the family's policy or the package default.
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	// Save upserts the family policy and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	// RecordStatus upserts the latest device pipeline status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed image_privacy repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates an image_privacy repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates an image_privacy repository with an
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
			upload_allowed,
			local_processing_only,
			processing_mode,
			upload_scope,
			retention_days,
			redaction_level,
			require_child_consent,
			revision,
			updated_at
		FROM image_privacy_policies
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.SchemaVersion,
		&policy.Enabled,
		&policy.UploadAllowed,
		&policy.LocalProcessingOnly,
		&policy.ProcessingMode,
		&policy.UploadScope,
		&policy.RetentionDays,
		&policy.RedactionLevel,
		&policy.RequireChildConsent,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read image privacy policy: %w", err)
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
		INSERT INTO image_privacy_policies (
			family_id,
			schema_version,
			enabled,
			upload_allowed,
			local_processing_only,
			processing_mode,
			upload_scope,
			retention_days,
			redaction_level,
			require_child_consent,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			enabled = EXCLUDED.enabled,
			upload_allowed = EXCLUDED.upload_allowed,
			local_processing_only = EXCLUDED.local_processing_only,
			processing_mode = EXCLUDED.processing_mode,
			upload_scope = EXCLUDED.upload_scope,
			retention_days = EXCLUDED.retention_days,
			redaction_level = EXCLUDED.redaction_level,
			require_child_consent = EXCLUDED.require_child_consent,
			revision = image_privacy_policies.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.Enabled,
		input.UploadAllowed,
		input.LocalProcessingOnly,
		input.ProcessingMode,
		input.UploadScope,
		input.RetentionDays,
		input.RedactionLevel,
		input.RequireChildConsent,
	).Scan(&policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save image privacy policy: %w", err)
	}
	policy.SchemaVersion = domain.SchemaVersion
	policy.Enabled = input.Enabled
	policy.UploadAllowed = input.UploadAllowed
	policy.LocalProcessingOnly = input.LocalProcessingOnly
	policy.ProcessingMode = input.ProcessingMode
	policy.UploadScope = input.UploadScope
	policy.RetentionDays = input.RetentionDays
	policy.RedactionLevel = input.RedactionLevel
	policy.RequireChildConsent = input.RequireChildConsent
	return policy, nil
}

// RecordStatus upserts the latest device pipeline status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_image_privacy_status (
			device_id,
			family_id,
			camera_supported,
			local_processing,
			upload_allowed,
			processing_mode,
			upload_scope,
			redaction_level,
			processed_count,
			uploaded_count,
			blocked_count,
			last_block_reason,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			camera_supported = EXCLUDED.camera_supported,
			local_processing = EXCLUDED.local_processing,
			upload_allowed = EXCLUDED.upload_allowed,
			processing_mode = EXCLUDED.processing_mode,
			upload_scope = EXCLUDED.upload_scope,
			redaction_level = EXCLUDED.redaction_level,
			processed_count = EXCLUDED.processed_count,
			uploaded_count = EXCLUDED.uploaded_count,
			blocked_count = EXCLUDED.blocked_count,
			last_block_reason = EXCLUDED.last_block_reason,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		status.CameraSupported,
		status.LocalProcessing,
		status.UploadAllowed,
		status.ProcessingMode,
		status.UploadScope,
		status.RedactionLevel,
		status.ProcessedCount,
		status.UploadedCount,
		status.BlockedCount,
		status.LastBlockReason,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record image privacy device status: %w", err)
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
			camera_supported,
			local_processing,
			upload_allowed,
			processing_mode,
			upload_scope,
			redaction_level,
			processed_count,
			uploaded_count,
			blocked_count,
			last_block_reason,
			policy_revision,
			reported_at
		FROM device_image_privacy_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list image privacy device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&status.CameraSupported,
			&status.LocalProcessing,
			&status.UploadAllowed,
			&status.ProcessingMode,
			&status.UploadScope,
			&status.RedactionLevel,
			&status.ProcessedCount,
			&status.UploadedCount,
			&status.BlockedCount,
			&status.LastBlockReason,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan image privacy device status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate image privacy device status: %w", err)
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
			COUNT(*) FILTER (WHERE s.camera_supported),
			COUNT(*) FILTER (WHERE s.local_processing),
			COUNT(*) FILTER (WHERE s.upload_allowed),
			COUNT(*) FILTER (
				WHERE s.policy_revision < COALESCE(p.revision, 1)
			),
			COALESCE(SUM(s.processed_count), 0),
			COALESCE(SUM(s.uploaded_count), 0),
			COALESCE(SUM(s.blocked_count), 0)
		FROM device_image_privacy_status s
		LEFT JOIN image_privacy_policies p ON p.family_id = s.family_id
		WHERE s.reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.CameraCount,
		&stats.LocalOnlyCount,
		&stats.UploadAllowedCount,
		&stats.OutdatedCount,
		&stats.ProcessedTotal,
		&stats.UploadedTotal,
		&stats.BlockedTotal,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read image privacy stats: %w", err)
	}

	modeBuckets, err := r.buckets(ctx, windowDays, "processing_mode")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.ModeBuckets = modeBuckets

	reasonBuckets, err := r.buckets(ctx, windowDays, "last_block_reason")
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
	case "processing_mode":
		query = `
			SELECT processing_mode AS bucket, COUNT(*)
			FROM device_image_privacy_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "last_block_reason":
		query = `
			SELECT last_block_reason AS bucket, COUNT(*)
			FROM device_image_privacy_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	default:
		return nil, fmt.Errorf("unsupported image privacy bucket column %q", column)
	}
	rows, err := r.pool.Query(ctx, query, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read image privacy %s buckets: %w", column, err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 6)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan image privacy %s bucket: %w", column, err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate image privacy %s buckets: %w", column, err)
	}
	return buckets, nil
}

// Readiness aggregates active devices that advertise a camera. A device that
// reports no camera support or an error block reason is a fault. Upload being
// disabled is a valid privacy choice and is not treated as unhealthy.
func (r *PostgresRepository) Readiness(
	ctx context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	return featurehealth.ReadDeviceStateReadiness(ctx, featurehealth.DeviceStateQuery{
		Pool:            r.pool,
		Capability:      "camera",
		StateTable:      "device_image_privacy_status",
		TimestampColumn: "reported_at",
		FaultCondition:  "NOT status.camera_supported OR status.last_block_reason = 'error'",
	}, freshness)
}
