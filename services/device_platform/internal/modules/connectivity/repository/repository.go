// Package repository persists guardian cellular policies and device status.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/connectivity/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for cellular policy and status.
type Repository interface {
	// Current returns the family's policy or the package default.
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	// Save upserts the family policy and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	// RecordStatus upserts the latest device connectivity status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed connectivity repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a connectivity repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a connectivity repository with an
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
	var preference string
	var revision int
	var updatedAt = policy.UpdatedAt
	err := r.pool.QueryRow(ctx, `
		SELECT schema_version,
			enabled,
			roaming_allowed,
			remote_wake_enabled,
			metered_upload_limit,
			preference,
			monthly_quota_mb,
			low_quota_warn_percent,
			keepalive_seconds,
			revision,
			updated_at
		FROM cellular_policies
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.SchemaVersion,
		&policy.Enabled,
		&policy.RoamingAllowed,
		&policy.RemoteWakeEnabled,
		&policy.MeteredUploadLimit,
		&preference,
		&policy.MonthlyQuotaMB,
		&policy.LowQuotaWarnPercent,
		&policy.KeepaliveSeconds,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read cellular policy: %w", err)
	}
	policy.Preference = domain.Preference(preference)
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
		INSERT INTO cellular_policies (
			family_id,
			schema_version,
			enabled,
			roaming_allowed,
			remote_wake_enabled,
			metered_upload_limit,
			preference,
			monthly_quota_mb,
			low_quota_warn_percent,
			keepalive_seconds,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			enabled = EXCLUDED.enabled,
			roaming_allowed = EXCLUDED.roaming_allowed,
			remote_wake_enabled = EXCLUDED.remote_wake_enabled,
			metered_upload_limit = EXCLUDED.metered_upload_limit,
			preference = EXCLUDED.preference,
			monthly_quota_mb = EXCLUDED.monthly_quota_mb,
			low_quota_warn_percent = EXCLUDED.low_quota_warn_percent,
			keepalive_seconds = EXCLUDED.keepalive_seconds,
			revision = cellular_policies.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.Enabled,
		input.RoamingAllowed,
		input.RemoteWakeEnabled,
		input.MeteredUploadLimit,
		string(input.Preference),
		input.MonthlyQuotaMB,
		input.LowQuotaWarnPercent,
		input.KeepaliveSeconds,
	).Scan(&policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save cellular policy: %w", err)
	}
	policy.SchemaVersion = domain.SchemaVersion
	policy.Enabled = input.Enabled
	policy.RoamingAllowed = input.RoamingAllowed
	policy.RemoteWakeEnabled = input.RemoteWakeEnabled
	policy.MeteredUploadLimit = input.MeteredUploadLimit
	policy.Preference = input.Preference
	policy.MonthlyQuotaMB = input.MonthlyQuotaMB
	policy.LowQuotaWarnPercent = input.LowQuotaWarnPercent
	policy.KeepaliveSeconds = input.KeepaliveSeconds
	return policy, nil
}

// RecordStatus upserts the latest device connectivity status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_connectivity_status (
			device_id,
			family_id,
			bearer,
			roaming,
			esim_state,
			signal_percent,
			used_mb,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			bearer = EXCLUDED.bearer,
			roaming = EXCLUDED.roaming,
			esim_state = EXCLUDED.esim_state,
			signal_percent = EXCLUDED.signal_percent,
			used_mb = EXCLUDED.used_mb,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		string(status.Bearer),
		status.Roaming,
		string(status.ESIMState),
		status.SignalPercent,
		status.UsedMB,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record connectivity device status: %w", err)
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
			bearer,
			roaming,
			esim_state,
			signal_percent,
			used_mb,
			policy_revision,
			reported_at
		FROM device_connectivity_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list connectivity device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		var bearer string
		var esimState string
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&bearer,
			&status.Roaming,
			&esimState,
			&status.SignalPercent,
			&status.UsedMB,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan connectivity device status: %w", err)
		}
		status.Bearer = domain.Bearer(bearer)
		status.ESIMState = domain.ESIMState(esimState)
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connectivity device status: %w", err)
	}
	return statuses, nil
}

// AdminStats returns a bounded fleet summary over the requested window.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	var averageSignal *float64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE bearer = 'cellular'),
			COUNT(*) FILTER (WHERE roaming),
			COALESCE(AVG(signal_percent), 0)
		FROM device_connectivity_status
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.CellularCount,
		&stats.RoamingCount,
		&averageSignal,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read connectivity stats: %w", err)
	}
	if averageSignal != nil {
		stats.AverageSignal = int(*averageSignal + 0.5)
	}

	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM device_connectivity_status AS status
		LEFT JOIN cellular_policies AS policy
			ON policy.family_id = status.family_id
		WHERE status.reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			AND COALESCE(policy.monthly_quota_mb, 0) > 0
			AND status.used_mb >= COALESCE(policy.monthly_quota_mb, 0)
	`, windowDays).Scan(&stats.QuotaExhausted); err != nil {
		return domain.AdminStats{}, fmt.Errorf("read connectivity quota: %w", err)
	}

	bearerBuckets, err := r.buckets(ctx, windowDays, "bearer")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.BearerBuckets = bearerBuckets

	esimBuckets, err := r.buckets(ctx, windowDays, "esim_state")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.ESIMBuckets = esimBuckets

	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

func (r *PostgresRepository) buckets(
	ctx context.Context,
	windowDays int,
	column string,
) ([]domain.Count, error) {
	// The column name is a closed set chosen by the caller, never user input, so
	// it is safe to format into the statement.
	var query string
	switch column {
	case "bearer":
		query = `
			SELECT bearer AS bucket, COUNT(*)
			FROM device_connectivity_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "esim_state":
		query = `
			SELECT esim_state AS bucket, COUNT(*)
			FROM device_connectivity_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	default:
		return nil, fmt.Errorf("unsupported bucket column %q", column)
	}
	rows, err := r.pool.Query(ctx, query, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read connectivity %s buckets: %w", column, err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 4)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan connectivity %s bucket: %w", column, err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connectivity %s buckets: %w", column, err)
	}
	return buckets, nil
}

// Readiness aggregates active devices that advertise cellular. A device that
// advertises cellular but reports the absent eSIM state is a fault.
func (r *PostgresRepository) Readiness(
	ctx context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	return featurehealth.ReadDeviceStateReadiness(ctx, featurehealth.DeviceStateQuery{
		Pool:            r.pool,
		Capability:      "cellular_4g",
		StateTable:      "device_connectivity_status",
		TimestampColumn: "reported_at",
		FaultCondition:  "status.esim_state = 'absent'",
	}, freshness)
}
