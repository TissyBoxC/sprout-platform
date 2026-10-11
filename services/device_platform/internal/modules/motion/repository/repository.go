// Package repository persists guardian motion policies and device status.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/motion/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for motion policy and status.
type Repository interface {
	// Current returns the family's policy or the package default.
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	// Save upserts the family policy and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	// RecordStatus upserts the latest device motion status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed motion repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a motion repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a motion repository with an injected
// time source for deterministic tests.
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
			require_stop_ack,
			allow_locomotion,
			max_speed_percent,
			max_runtime_ms,
			stop_grace_ms,
			revision,
			updated_at
		FROM motion_policies
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.SchemaVersion,
		&policy.Enabled,
		&policy.RequireStopAck,
		&policy.AllowLocomotion,
		&policy.MaxSpeedPercent,
		&policy.MaxRuntimeMS,
		&policy.StopGraceMS,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read motion policy: %w", err)
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
		INSERT INTO motion_policies (
			family_id,
			schema_version,
			enabled,
			require_stop_ack,
			allow_locomotion,
			max_speed_percent,
			max_runtime_ms,
			stop_grace_ms,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			enabled = EXCLUDED.enabled,
			require_stop_ack = EXCLUDED.require_stop_ack,
			allow_locomotion = EXCLUDED.allow_locomotion,
			max_speed_percent = EXCLUDED.max_speed_percent,
			max_runtime_ms = EXCLUDED.max_runtime_ms,
			stop_grace_ms = EXCLUDED.stop_grace_ms,
			revision = motion_policies.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.Enabled,
		input.RequireStopAck,
		input.AllowLocomotion,
		input.MaxSpeedPercent,
		input.MaxRuntimeMS,
		input.StopGraceMS,
	).Scan(&policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save motion policy: %w", err)
	}
	policy.SchemaVersion = domain.SchemaVersion
	policy.Enabled = input.Enabled
	policy.RequireStopAck = input.RequireStopAck
	policy.AllowLocomotion = input.AllowLocomotion
	policy.MaxSpeedPercent = input.MaxSpeedPercent
	policy.MaxRuntimeMS = input.MaxRuntimeMS
	policy.StopGraceMS = input.StopGraceMS
	return policy, nil
}

// RecordStatus upserts the latest device motion status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_motion_status (
			device_id,
			family_id,
			state,
			action,
			actuator_count,
			faulted,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			state = EXCLUDED.state,
			action = EXCLUDED.action,
			actuator_count = EXCLUDED.actuator_count,
			faulted = EXCLUDED.faulted,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		string(status.State),
		string(status.Action),
		status.ActuatorCount,
		status.Faulted,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record motion device status: %w", err)
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
			state,
			action,
			actuator_count,
			faulted,
			policy_revision,
			reported_at
		FROM device_motion_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list motion device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		var state string
		var action string
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&state,
			&action,
			&status.ActuatorCount,
			&status.Faulted,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan motion device status: %w", err)
		}
		status.State = domain.State(state)
		status.Action = domain.Action(action)
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate motion device status: %w", err)
	}
	return statuses, nil
}

// AdminStats returns a bounded fleet summary over the requested window.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE state = 'running'),
			COUNT(*) FILTER (WHERE faulted),
			COUNT(*) FILTER (WHERE state = 'idle')
		FROM device_motion_status
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.RunningCount,
		&stats.FaultedCount,
		&stats.IdleCount,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read motion stats: %w", err)
	}

	actionBuckets, err := r.countBy(ctx, windowDays, "action")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.ActionBuckets = actionBuckets

	stateBuckets, err := r.countBy(ctx, windowDays, "state")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.StateBuckets = stateBuckets
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

// countBy returns one column's histogram over the window. The column is chosen
// from a fixed allow-list so it can never be attacker-controlled.
func (r *PostgresRepository) countBy(
	ctx context.Context,
	windowDays int,
	column string,
) ([]domain.Count, error) {
	query := ""
	switch column {
	case "action":
		query = `
			SELECT action AS bucket, COUNT(*)
			FROM device_motion_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "state":
		query = `
			SELECT state AS bucket, COUNT(*)
			FROM device_motion_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	default:
		return nil, fmt.Errorf("unsupported motion bucket column %q", column)
	}
	rows, err := r.pool.Query(ctx, query, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read motion %s buckets: %w", column, err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 5)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan motion %s bucket: %w", column, err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate motion %s buckets: %w", column, err)
	}
	return buckets, nil
}
