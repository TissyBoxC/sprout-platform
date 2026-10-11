// Package repository persists the guardian Bluetooth audio policy and the
// bounded per-device Bluetooth adapter status.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/bluetooth_audio/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for the bluetooth_audio module.
type Repository interface {
	// Current returns the family's policy or the package default.
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	// Save upserts the family policy and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	// RecordStatus upserts the latest device adapter status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed bluetooth_audio repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a bluetooth_audio repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a bluetooth_audio repository with an
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
			speaker_mode_allowed,
			pairing_allowed,
			auto_reconnect,
			preferred_codec,
			max_volume_percent,
			display_name,
			revision,
			updated_at
		FROM bluetooth_audio_policies
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.SchemaVersion,
		&policy.Enabled,
		&policy.SpeakerModeAllowed,
		&policy.PairingAllowed,
		&policy.AutoReconnect,
		&policy.PreferredCodec,
		&policy.MaxVolumePercent,
		&policy.DisplayName,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read bluetooth audio policy: %w", err)
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
		INSERT INTO bluetooth_audio_policies (
			family_id,
			schema_version,
			enabled,
			speaker_mode_allowed,
			pairing_allowed,
			auto_reconnect,
			preferred_codec,
			max_volume_percent,
			display_name,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			enabled = EXCLUDED.enabled,
			speaker_mode_allowed = EXCLUDED.speaker_mode_allowed,
			pairing_allowed = EXCLUDED.pairing_allowed,
			auto_reconnect = EXCLUDED.auto_reconnect,
			preferred_codec = EXCLUDED.preferred_codec,
			max_volume_percent = EXCLUDED.max_volume_percent,
			display_name = EXCLUDED.display_name,
			revision = bluetooth_audio_policies.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.Enabled,
		input.SpeakerModeAllowed,
		input.PairingAllowed,
		input.AutoReconnect,
		input.PreferredCodec,
		input.MaxVolumePercent,
		input.DisplayName,
	).Scan(&policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save bluetooth audio policy: %w", err)
	}
	policy.SchemaVersion = domain.SchemaVersion
	policy.Enabled = input.Enabled
	policy.SpeakerModeAllowed = input.SpeakerModeAllowed
	policy.PairingAllowed = input.PairingAllowed
	policy.AutoReconnect = input.AutoReconnect
	policy.PreferredCodec = input.PreferredCodec
	policy.MaxVolumePercent = input.MaxVolumePercent
	policy.DisplayName = input.DisplayName
	return policy, nil
}

// RecordStatus upserts the latest device adapter status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_bluetooth_audio_status (
			device_id,
			family_id,
			bluetooth_supported,
			speaker_mode,
			adapter_state,
			paired_device_count,
			connected_device_count,
			active_codec,
			volume_percent,
			last_disconnect_reason,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			bluetooth_supported = EXCLUDED.bluetooth_supported,
			speaker_mode = EXCLUDED.speaker_mode,
			adapter_state = EXCLUDED.adapter_state,
			paired_device_count = EXCLUDED.paired_device_count,
			connected_device_count = EXCLUDED.connected_device_count,
			active_codec = EXCLUDED.active_codec,
			volume_percent = EXCLUDED.volume_percent,
			last_disconnect_reason = EXCLUDED.last_disconnect_reason,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		status.BluetoothSupported,
		status.SpeakerMode,
		status.AdapterState,
		status.PairedDeviceCount,
		status.ConnectedDeviceCount,
		status.ActiveCodec,
		status.VolumePercent,
		status.LastDisconnectReason,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record bluetooth audio device status: %w", err)
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
			bluetooth_supported,
			speaker_mode,
			adapter_state,
			paired_device_count,
			connected_device_count,
			active_codec,
			volume_percent,
			last_disconnect_reason,
			policy_revision,
			reported_at
		FROM device_bluetooth_audio_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list bluetooth audio device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&status.BluetoothSupported,
			&status.SpeakerMode,
			&status.AdapterState,
			&status.PairedDeviceCount,
			&status.ConnectedDeviceCount,
			&status.ActiveCodec,
			&status.VolumePercent,
			&status.LastDisconnectReason,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan bluetooth audio device status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bluetooth audio device status: %w", err)
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
			COUNT(*) FILTER (WHERE s.bluetooth_supported),
			COUNT(*) FILTER (WHERE s.speaker_mode),
			COUNT(*) FILTER (WHERE s.adapter_state = 'connected'),
			COALESCE(SUM(s.paired_device_count), 0),
			COUNT(*) FILTER (
				WHERE s.policy_revision < COALESCE(p.revision, 1)
			)
		FROM device_bluetooth_audio_status s
		LEFT JOIN bluetooth_audio_policies p ON p.family_id = s.family_id
		WHERE s.reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.SupportedCount,
		&stats.SpeakerModeCount,
		&stats.ConnectedCount,
		&stats.PairedDeviceTotal,
		&stats.OutdatedCount,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read bluetooth audio stats: %w", err)
	}

	stateBuckets, err := r.buckets(ctx, windowDays, "adapter_state")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.StateBuckets = stateBuckets

	codecBuckets, err := r.buckets(ctx, windowDays, "active_codec")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.CodecBuckets = codecBuckets
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
	case "adapter_state":
		query = `
			SELECT adapter_state AS bucket, COUNT(*)
			FROM device_bluetooth_audio_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "active_codec":
		query = `
			SELECT active_codec AS bucket, COUNT(*)
			FROM device_bluetooth_audio_status
			WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	default:
		return nil, fmt.Errorf("unsupported bluetooth audio bucket column %q", column)
	}
	rows, err := r.pool.Query(ctx, query, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read bluetooth audio %s buckets: %w", column, err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 6)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan bluetooth audio %s bucket: %w", column, err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bluetooth audio %s buckets: %w", column, err)
	}
	return buckets, nil
}
