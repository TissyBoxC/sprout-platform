// Package repository persists guardian touch configurations and device status.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/input/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for touch config and status.
type Repository interface {
	// Current returns the family's configuration or the package default.
	Current(ctx context.Context, familyID string) (domain.Config, error)
	// Save upserts the family configuration and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Config, error)
	// RecordStatus upserts the latest device touch status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed touch repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a touch repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a touch repository with an injected
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

// Current returns the family configuration row or the default when absent.
func (r *PostgresRepository) Current(
	ctx context.Context,
	familyID string,
) (domain.Config, error) {
	config := domain.Default()
	var revision int
	var updatedAt = config.UpdatedAt
	err := r.pool.QueryRow(ctx, `
		SELECT schema_version,
			enabled,
			tap_enabled,
			swipe_enabled,
			long_press_enabled,
			haptic_enabled,
			sensitivity,
			rotation_degrees,
			long_press_ms,
			double_tap_ms,
			swipe_distance_px,
			revision,
			updated_at
		FROM touch_configs
		WHERE family_id = $1
	`, familyID).Scan(
		&config.SchemaVersion,
		&config.Enabled,
		&config.TapEnabled,
		&config.SwipeEnabled,
		&config.LongPressEnabled,
		&config.HapticEnabled,
		&config.Sensitivity,
		&config.RotationDegrees,
		&config.LongPressMS,
		&config.DoubleTapMS,
		&config.SwipeDistancePX,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Config{}, fmt.Errorf("read touch config: %w", err)
	}
	config.Revision = revision
	config.UpdatedAt = updatedAt
	return config, nil
}

// Save upserts the family configuration and bumps its revision.
func (r *PostgresRepository) Save(
	ctx context.Context,
	familyID string,
	input domain.Input,
) (domain.Config, error) {
	config := domain.Default()
	err := r.pool.QueryRow(ctx, `
		INSERT INTO touch_configs (
			family_id,
			schema_version,
			enabled,
			tap_enabled,
			swipe_enabled,
			long_press_enabled,
			haptic_enabled,
			sensitivity,
			rotation_degrees,
			long_press_ms,
			double_tap_ms,
			swipe_distance_px,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			enabled = EXCLUDED.enabled,
			tap_enabled = EXCLUDED.tap_enabled,
			swipe_enabled = EXCLUDED.swipe_enabled,
			long_press_enabled = EXCLUDED.long_press_enabled,
			haptic_enabled = EXCLUDED.haptic_enabled,
			sensitivity = EXCLUDED.sensitivity,
			rotation_degrees = EXCLUDED.rotation_degrees,
			long_press_ms = EXCLUDED.long_press_ms,
			double_tap_ms = EXCLUDED.double_tap_ms,
			swipe_distance_px = EXCLUDED.swipe_distance_px,
			revision = touch_configs.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.Enabled,
		input.TapEnabled,
		input.SwipeEnabled,
		input.LongPressEnabled,
		input.HapticEnabled,
		input.Sensitivity,
		input.RotationDegrees,
		input.LongPressMS,
		input.DoubleTapMS,
		input.SwipeDistancePX,
	).Scan(&config.Revision, &config.UpdatedAt)
	if err != nil {
		return domain.Config{}, fmt.Errorf("save touch config: %w", err)
	}
	config.SchemaVersion = domain.SchemaVersion
	config.Enabled = input.Enabled
	config.TapEnabled = input.TapEnabled
	config.SwipeEnabled = input.SwipeEnabled
	config.LongPressEnabled = input.LongPressEnabled
	config.HapticEnabled = input.HapticEnabled
	config.Sensitivity = input.Sensitivity
	config.RotationDegrees = input.RotationDegrees
	config.LongPressMS = input.LongPressMS
	config.DoubleTapMS = input.DoubleTapMS
	config.SwipeDistancePX = input.SwipeDistancePX
	return config, nil
}

// RecordStatus upserts the latest device touch status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO touch_device_status (
			device_id,
			family_id,
			touch_supported,
			haptic_supported,
			gesture_count,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			touch_supported = EXCLUDED.touch_supported,
			haptic_supported = EXCLUDED.haptic_supported,
			gesture_count = EXCLUDED.gesture_count,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		status.TouchSupported,
		status.HapticSupported,
		status.GestureCount,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record touch device status: %w", err)
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
			touch_supported,
			haptic_supported,
			gesture_count,
			policy_revision,
			reported_at
		FROM touch_device_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list touch device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&status.TouchSupported,
			&status.HapticSupported,
			&status.GestureCount,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan touch device status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate touch device status: %w", err)
	}
	return statuses, nil
}

// AdminStats returns a bounded fleet summary over the requested window.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	var averageSensitivity *float64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE touch_supported),
			COUNT(*) FILTER (WHERE haptic_supported),
			COALESCE(AVG(sensitivity), 0)
		FROM touch_device_status AS status
		LEFT JOIN touch_configs AS config
			ON config.family_id = status.family_id
		WHERE status.reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.TouchDeviceCount,
		&stats.HapticDeviceCount,
		&averageSensitivity,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read touch stats: %w", err)
	}
	if averageSensitivity != nil {
		stats.AverageSensitivity = int(*averageSensitivity + 0.5)
	}

	var outdated uint64
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM touch_device_status AS status
		LEFT JOIN touch_configs AS config
			ON config.family_id = status.family_id
		WHERE status.reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			AND status.policy_revision < COALESCE(config.revision, 0)
	`, windowDays).Scan(&outdated); err != nil {
		return domain.AdminStats{}, fmt.Errorf("read touch outdated: %w", err)
	}
	stats.OutdatedCount = outdated

	rows, err := r.pool.Query(ctx, `
		SELECT preset, COUNT(*)
		FROM (
			SELECT
				CASE
					WHEN config.long_press_enabled THEN 'long_press'
					WHEN config.swipe_enabled THEN 'swipe'
					WHEN config.tap_enabled THEN 'tap'
					ELSE 'tap'
				END AS preset
			FROM touch_device_status AS status
			LEFT JOIN touch_configs AS config
				ON config.family_id = status.family_id
			WHERE status.reported_at >= NOW() - ($1::int * INTERVAL '1 day')
		) AS presets
		GROUP BY preset
		ORDER BY preset
	`, windowDays)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read touch presets: %w", err)
	}
	defer rows.Close()
	presets := make([]domain.PresetCount, 0, 4)
	for rows.Next() {
		var preset domain.PresetCount
		if err := rows.Scan(&preset.Preset, &preset.Count); err != nil {
			return domain.AdminStats{}, fmt.Errorf("scan touch preset: %w", err)
		}
		presets = append(presets, preset)
	}
	if err := rows.Err(); err != nil {
		return domain.AdminStats{}, fmt.Errorf("iterate touch presets: %w", err)
	}
	stats.PresetCoverage = presets
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}
