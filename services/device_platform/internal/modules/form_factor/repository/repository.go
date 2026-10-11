// Package repository persists device form-factor profiles.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/form_factor/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for form-factor profiles.
type Repository interface {
	// Record upserts the latest device profile.
	Record(ctx context.Context, profile domain.Profile) error
	// Profile returns one device profile within a family.
	Profile(ctx context.Context, familyID string, deviceID string) (domain.Profile, bool, error)
	// Statuses returns every device profile in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Profile, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed form-factor repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a form-factor repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a form-factor repository with an
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

// Record upserts the latest device form-factor profile.
func (r *PostgresRepository) Record(
	ctx context.Context,
	profile domain.Profile,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_form_factor (
			device_id,
			family_id,
			technical_name,
			capability_mask,
			action_mask,
			schema_version,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			technical_name = EXCLUDED.technical_name,
			capability_mask = EXCLUDED.capability_mask,
			action_mask = EXCLUDED.action_mask,
			schema_version = EXCLUDED.schema_version,
			reported_at = NOW()
	`,
		profile.DeviceID,
		profile.FamilyID,
		profile.TechnicalName,
		profile.CapabilityMask,
		profile.ActionMask,
		profile.SchemaVersion,
	)
	if err != nil {
		return fmt.Errorf("record form factor profile: %w", err)
	}
	return nil
}

// Profile returns one device profile within a family.
func (r *PostgresRepository) Profile(
	ctx context.Context,
	familyID string,
	deviceID string,
) (domain.Profile, bool, error) {
	var profile domain.Profile
	err := r.pool.QueryRow(ctx, `
		SELECT device_id,
			family_id,
			technical_name,
			capability_mask,
			action_mask,
			schema_version,
			reported_at
		FROM device_form_factor
		WHERE family_id = $1 AND device_id = $2
	`, familyID, deviceID).Scan(
		&profile.DeviceID,
		&profile.FamilyID,
		&profile.TechnicalName,
		&profile.CapabilityMask,
		&profile.ActionMask,
		&profile.SchemaVersion,
		&profile.ReportedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Profile{}, false, nil
	}
	if err != nil {
		return domain.Profile{}, false, fmt.Errorf("read form factor profile: %w", err)
	}
	return profile, true, nil
}

// Statuses returns every device profile in a family.
func (r *PostgresRepository) Statuses(
	ctx context.Context,
	familyID string,
) ([]domain.Profile, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT device_id,
			family_id,
			technical_name,
			capability_mask,
			action_mask,
			schema_version,
			reported_at
		FROM device_form_factor
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list form factor profiles: %w", err)
	}
	defer rows.Close()
	profiles := make([]domain.Profile, 0, 8)
	for rows.Next() {
		var profile domain.Profile
		if err := rows.Scan(
			&profile.DeviceID,
			&profile.FamilyID,
			&profile.TechnicalName,
			&profile.CapabilityMask,
			&profile.ActionMask,
			&profile.SchemaVersion,
			&profile.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan form factor profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate form factor profiles: %w", err)
	}
	return profiles, nil
}

// AdminStats returns a bounded fleet summary over the requested window.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE (capability_mask & $2) <> 0),
			COUNT(*) FILTER (WHERE (action_mask & $3) <> 0),
			COUNT(*) FILTER (WHERE (action_mask & $4) <> 0),
			COUNT(*) FILTER (WHERE (capability_mask & $5) <> 0)
		FROM device_form_factor
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`,
		windowDays,
		domain.CapLocomotion,
		domain.ActionVoice,
		domain.ActionVideoCall,
		domain.CapWearable,
	).Scan(
		&stats.DeviceCount,
		&stats.LocomotionCount,
		&stats.VoiceCount,
		&stats.VideoCallCount,
		&stats.WearableCount,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read form factor stats: %w", err)
	}

	nameBuckets, err := r.nameBuckets(ctx, windowDays)
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.NameBuckets = nameBuckets

	capabilityCounts, err := r.capabilityCounts(ctx, windowDays)
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.CapabilityCounts = capabilityCounts
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

// nameBuckets returns the count of devices per technical name over the window.
func (r *PostgresRepository) nameBuckets(
	ctx context.Context,
	windowDays int,
) ([]domain.Count, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT technical_name, COUNT(*)
		FROM device_form_factor
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
		GROUP BY technical_name
		ORDER BY technical_name
	`, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read form factor name buckets: %w", err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 4)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan form factor name bucket: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate form factor name buckets: %w", err)
	}
	return buckets, nil
}

// capabilityCounts returns the count of devices advertising each capability.
//
// The capability bits come from a fixed allow-list, so the join is bounded and
// no label can be attacker-controlled.
func (r *PostgresRepository) capabilityCounts(
	ctx context.Context,
	windowDays int,
) ([]domain.Count, error) {
	bits := []uint32{
		domain.CapDisplay,
		domain.CapCamera,
		domain.CapMicrophone,
		domain.CapSpeaker,
		domain.CapTouch,
		domain.CapLocomotion,
		domain.CapServo,
		domain.CapCellular,
		domain.CapBattery,
		domain.CapWearable,
	}
	rows, err := r.pool.Query(ctx, `
		SELECT bits.bit,
			COUNT(dev.device_id)
		FROM (VALUES
			(1), (2), (4), (8), (16), (32), (64), (128), (256), (512)
		) AS bits(bit)
		LEFT JOIN device_form_factor AS dev
			ON (dev.capability_mask & bits.bit) <> 0
			AND dev.reported_at >= NOW() - ($1::int * INTERVAL '1 day')
		GROUP BY bits.bit
		ORDER BY bits.bit
	`, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read form factor capability counts: %w", err)
	}
	defer rows.Close()
	counts := make([]domain.Count, 0, len(bits))
	for rows.Next() {
		var bit int
		var count uint64
		if err := rows.Scan(&bit, &count); err != nil {
			return nil, fmt.Errorf("scan form factor capability count: %w", err)
		}
		counts = append(counts, domain.Count{
			Label: domain.CapabilityName(uint32(bit)),
			Count: count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate form factor capability counts: %w", err)
	}
	return counts, nil
}
