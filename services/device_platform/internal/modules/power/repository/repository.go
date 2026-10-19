// Package repository persists guardian power policies and device status.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/power/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for power policy and status.
type Repository interface {
	// Current returns the family's policy or the package default.
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	// Save upserts the family policy and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	// RecordStatus upserts the latest device power status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed power repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a power repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a power repository with an injected
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
			low_power_enabled,
			deep_sleep_enabled,
			charge_priority,
			low_battery_warn_percent,
			low_power_enter_percent,
			sleep_after_seconds,
			revision,
			updated_at
		FROM power_policies
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.SchemaVersion,
		&policy.Enabled,
		&policy.LowPowerEnabled,
		&policy.DeepSleepEnabled,
		&policy.ChargePriority,
		&policy.LowBatteryWarnPercent,
		&policy.LowPowerEnterPercent,
		&policy.SleepAfterSeconds,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read power policy: %w", err)
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
		INSERT INTO power_policies (
			family_id,
			schema_version,
			enabled,
			low_power_enabled,
			deep_sleep_enabled,
			charge_priority,
			low_battery_warn_percent,
			low_power_enter_percent,
			sleep_after_seconds,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			enabled = EXCLUDED.enabled,
			low_power_enabled = EXCLUDED.low_power_enabled,
			deep_sleep_enabled = EXCLUDED.deep_sleep_enabled,
			charge_priority = EXCLUDED.charge_priority,
			low_battery_warn_percent = EXCLUDED.low_battery_warn_percent,
			low_power_enter_percent = EXCLUDED.low_power_enter_percent,
			sleep_after_seconds = EXCLUDED.sleep_after_seconds,
			revision = power_policies.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.Enabled,
		input.LowPowerEnabled,
		input.DeepSleepEnabled,
		input.ChargePriority,
		input.LowBatteryWarnPercent,
		input.LowPowerEnterPercent,
		input.SleepAfterSeconds,
	).Scan(&policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save power policy: %w", err)
	}
	policy.SchemaVersion = domain.SchemaVersion
	policy.Enabled = input.Enabled
	policy.LowPowerEnabled = input.LowPowerEnabled
	policy.DeepSleepEnabled = input.DeepSleepEnabled
	policy.ChargePriority = input.ChargePriority
	policy.LowBatteryWarnPercent = input.LowBatteryWarnPercent
	policy.LowPowerEnterPercent = input.LowPowerEnterPercent
	policy.SleepAfterSeconds = input.SleepAfterSeconds
	return policy, nil
}

// RecordStatus upserts the latest device power status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_power_status (
			device_id,
			family_id,
			charge_state,
			power_mode,
			battery_percent,
			millivolts,
			charging,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			charge_state = EXCLUDED.charge_state,
			power_mode = EXCLUDED.power_mode,
			battery_percent = EXCLUDED.battery_percent,
			millivolts = EXCLUDED.millivolts,
			charging = EXCLUDED.charging,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		string(status.ChargeState),
		string(status.PowerMode),
		status.BatteryPercent,
		status.Millivolts,
		status.Charging,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record power device status: %w", err)
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
			charge_state,
			power_mode,
			battery_percent,
			millivolts,
			charging,
			policy_revision,
			reported_at
		FROM device_power_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list power device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		var chargeState string
		var powerMode string
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&chargeState,
			&powerMode,
			&status.BatteryPercent,
			&status.Millivolts,
			&status.Charging,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan power device status: %w", err)
		}
		status.ChargeState = domain.ChargeState(chargeState)
		status.PowerMode = domain.PowerMode(powerMode)
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate power device status: %w", err)
	}
	return statuses, nil
}

// AdminStats returns a bounded fleet summary over the requested window.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	var averagePercent *float64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE charging),
			COUNT(*) FILTER (WHERE battery_percent <= 20),
			COUNT(*) FILTER (WHERE power_mode = 'low_power'),
			COUNT(*) FILTER (WHERE power_mode = 'deep_sleep'),
			COALESCE(AVG(battery_percent), 0)
		FROM device_power_status
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.ChargingCount,
		&stats.LowBatteryCount,
		&stats.LowPowerCount,
		&stats.DeepSleepCount,
		&averagePercent,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read power stats: %w", err)
	}
	if averagePercent != nil {
		stats.AveragePercent = int(*averagePercent + 0.5)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT
			CASE
				WHEN battery_percent >= 80 THEN 'high'
				WHEN battery_percent >= 50 THEN 'good'
				WHEN battery_percent >= 20 THEN 'medium'
				WHEN battery_percent > 0 THEN 'low'
				ELSE 'unknown'
			END AS bucket,
			COUNT(*)
		FROM device_power_status
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
		GROUP BY bucket
		ORDER BY bucket
	`, windowDays)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read power percentage buckets: %w", err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 4)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return domain.AdminStats{}, fmt.Errorf("scan power percentage bucket: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return domain.AdminStats{}, fmt.Errorf("iterate power percentage buckets: %w", err)
	}
	stats.PercentageBuckets = buckets
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

// Readiness aggregates active devices that advertise a battery. An unknown
// charge state on an advertising device is a fault signal.
func (r *PostgresRepository) Readiness(
	ctx context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	return featurehealth.ReadDeviceStateReadiness(ctx, featurehealth.DeviceStateQuery{
		Pool:            r.pool,
		Capability:      "battery",
		StateTable:      "device_power_status",
		TimestampColumn: "reported_at",
		FaultCondition:  "status.charge_state = 'unknown'",
	}, freshness)
}
