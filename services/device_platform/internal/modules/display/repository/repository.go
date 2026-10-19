// Package repository persists guardian display policies and device status.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/display/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for display policy and status.
type Repository interface {
	// Current returns the family's policy or the package default.
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	// Save upserts the family policy and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	// RecordStatus upserts the latest device display status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed display repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a display repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a display repository with an injected
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
			brightness_percent,
			color_temperature_kelvin,
			min_view_distance_cm,
			daily_limit_minutes,
			break_interval_minutes,
			dim_after_seconds,
			auto_brightness,
			night_light,
			distance_reminder,
			revision,
			updated_at
		FROM display_policies
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.SchemaVersion,
		&policy.BrightnessPercent,
		&policy.ColorTemperatureKelvin,
		&policy.MinViewDistanceCM,
		&policy.DailyLimitMinutes,
		&policy.BreakIntervalMinutes,
		&policy.DimAfterSeconds,
		&policy.AutoBrightness,
		&policy.NightLight,
		&policy.DistanceReminder,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read display policy: %w", err)
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
		INSERT INTO display_policies (
			family_id,
			schema_version,
			brightness_percent,
			color_temperature_kelvin,
			min_view_distance_cm,
			daily_limit_minutes,
			break_interval_minutes,
			dim_after_seconds,
			auto_brightness,
			night_light,
			distance_reminder,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			brightness_percent = EXCLUDED.brightness_percent,
			color_temperature_kelvin = EXCLUDED.color_temperature_kelvin,
			min_view_distance_cm = EXCLUDED.min_view_distance_cm,
			daily_limit_minutes = EXCLUDED.daily_limit_minutes,
			break_interval_minutes = EXCLUDED.break_interval_minutes,
			dim_after_seconds = EXCLUDED.dim_after_seconds,
			auto_brightness = EXCLUDED.auto_brightness,
			night_light = EXCLUDED.night_light,
			distance_reminder = EXCLUDED.distance_reminder,
			revision = display_policies.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.BrightnessPercent,
		input.ColorTemperatureKelvin,
		input.MinViewDistanceCM,
		input.DailyLimitMinutes,
		input.BreakIntervalMinutes,
		input.DimAfterSeconds,
		input.AutoBrightness,
		input.NightLight,
		input.DistanceReminder,
	).Scan(&policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save display policy: %w", err)
	}
	policy.SchemaVersion = domain.SchemaVersion
	policy.BrightnessPercent = input.BrightnessPercent
	policy.ColorTemperatureKelvin = input.ColorTemperatureKelvin
	policy.MinViewDistanceCM = input.MinViewDistanceCM
	policy.DailyLimitMinutes = input.DailyLimitMinutes
	policy.BreakIntervalMinutes = input.BreakIntervalMinutes
	policy.DimAfterSeconds = input.DimAfterSeconds
	policy.AutoBrightness = input.AutoBrightness
	policy.NightLight = input.NightLight
	policy.DistanceReminder = input.DistanceReminder
	return policy, nil
}

// RecordStatus upserts the latest device display status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO display_device_status (
			device_id,
			family_id,
			brightness_percent,
			screen_width,
			screen_height,
			touch_supported,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			brightness_percent = EXCLUDED.brightness_percent,
			screen_width = EXCLUDED.screen_width,
			screen_height = EXCLUDED.screen_height,
			touch_supported = EXCLUDED.touch_supported,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		status.BrightnessPercent,
		status.ScreenWidth,
		status.ScreenHeight,
		status.TouchSupported,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record display device status: %w", err)
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
			brightness_percent,
			screen_width,
			screen_height,
			touch_supported,
			policy_revision,
			reported_at
		FROM display_device_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list display device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&status.BrightnessPercent,
			&status.ScreenWidth,
			&status.ScreenHeight,
			&status.TouchSupported,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan display device status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate display device status: %w", err)
	}
	return statuses, nil
}

// AdminStats returns a bounded fleet summary over the requested window.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	var averageBrightness *float64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE touch_supported),
			COALESCE(AVG(brightness_percent), 0)
		FROM display_device_status
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.TouchDeviceCount,
		&averageBrightness,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read display stats: %w", err)
	}
	if averageBrightness != nil {
		stats.AverageBrightness = int(*averageBrightness + 0.5)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT
			CASE
				WHEN screen_width = 0 OR screen_height = 0 THEN 'unknown'
				WHEN screen_width >= 1024 THEN 'large'
				WHEN screen_width >= 480 THEN 'medium'
				ELSE 'small'
			END AS bucket,
			COUNT(*)
		FROM display_device_status
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
		GROUP BY bucket
		ORDER BY bucket
	`, windowDays)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read display screen buckets: %w", err)
	}
	defer rows.Close()
	buckets := make([]domain.ScreenBucket, 0, 4)
	for rows.Next() {
		var bucket domain.ScreenBucket
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return domain.AdminStats{}, fmt.Errorf("scan display screen bucket: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return domain.AdminStats{}, fmt.Errorf("iterate display screen buckets: %w", err)
	}
	stats.ScreenSizeBuckets = buckets
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

// Readiness aggregates active devices that advertise a display. Missing or
// stale reports degrade the capability; invalid geometry is a fault.
func (r *PostgresRepository) Readiness(
	ctx context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	return featurehealth.ReadDeviceStateReadiness(ctx, featurehealth.DeviceStateQuery{
		Pool:            r.pool,
		Capability:      "display",
		StateTable:      "display_device_status",
		TimestampColumn: "reported_at",
		FaultCondition:  "status.screen_width <= 0 OR status.screen_height <= 0",
	}, freshness)
}
