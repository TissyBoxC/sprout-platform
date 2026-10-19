// Package repository persists guardian screen policies, device status, the
// family point-to-read dictionary and the device video playback state.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/screen/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for the screen module.
type Repository interface {
	// Current returns the family's policy or the package default.
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	// Save upserts the family policy and bumps its revision.
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	// RecordStatus upserts the latest device screen status.
	RecordStatus(ctx context.Context, status domain.Status) error
	// Statuses returns every device status in a family.
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)

	// Entries returns the family point-to-read dictionary.
	Entries(ctx context.Context, familyID string) ([]domain.DictionaryEntry, error)
	// EntryByTerm returns one entry by its normalized term.
	EntryByTerm(ctx context.Context, familyID string, term string) (domain.DictionaryEntry, bool, error)
	// SaveEntry upserts one dictionary entry.
	SaveEntry(ctx context.Context, entry domain.DictionaryEntry) error
	// DeleteEntry removes one dictionary entry.
	DeleteEntry(ctx context.Context, familyID string, entryID string) error
	// EntryCount returns how many entries a family currently stores.
	EntryCount(ctx context.Context, familyID string) (int, error)

	// Playback returns the current video playback plan for a device.
	Playback(ctx context.Context, familyID string, deviceID string) (domain.PlaybackPlan, bool, error)
	// SavePlayback upserts the video playback plan for a device.
	SavePlayback(ctx context.Context, familyID string, deviceID string, plan domain.PlaybackPlan) error

	// AdminStats returns a bounded fleet summary over the window.
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed screen repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a screen repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a screen repository with an injected
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
			default_expression,
			animation_enabled,
			point_read_enabled,
			video_enabled,
			video_max_fps,
			video_max_bitrate_kbps,
			animation_frame_ms,
			revision,
			updated_at
		FROM screen_policies
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.SchemaVersion,
		&policy.DefaultExpression,
		&policy.AnimationEnabled,
		&policy.PointReadEnabled,
		&policy.VideoEnabled,
		&policy.VideoMaxFPS,
		&policy.VideoMaxBitrateKBS,
		&policy.AnimationFrameMS,
		&revision,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read screen policy: %w", err)
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
		INSERT INTO screen_policies (
			family_id,
			schema_version,
			default_expression,
			animation_enabled,
			point_read_enabled,
			video_enabled,
			video_max_fps,
			video_max_bitrate_kbps,
			animation_frame_ms,
			revision,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			schema_version = EXCLUDED.schema_version,
			default_expression = EXCLUDED.default_expression,
			animation_enabled = EXCLUDED.animation_enabled,
			point_read_enabled = EXCLUDED.point_read_enabled,
			video_enabled = EXCLUDED.video_enabled,
			video_max_fps = EXCLUDED.video_max_fps,
			video_max_bitrate_kbps = EXCLUDED.video_max_bitrate_kbps,
			animation_frame_ms = EXCLUDED.animation_frame_ms,
			revision = screen_policies.revision + 1,
			updated_at = NOW()
		RETURNING revision, updated_at
	`,
		familyID,
		domain.SchemaVersion,
		input.DefaultExpression,
		input.AnimationEnabled,
		input.PointReadEnabled,
		input.VideoEnabled,
		input.VideoMaxFPS,
		input.VideoMaxBitrateKBS,
		input.AnimationFrameMS,
	).Scan(&policy.Revision, &policy.UpdatedAt)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save screen policy: %w", err)
	}
	policy.SchemaVersion = domain.SchemaVersion
	policy.DefaultExpression = input.DefaultExpression
	policy.AnimationEnabled = input.AnimationEnabled
	policy.PointReadEnabled = input.PointReadEnabled
	policy.VideoEnabled = input.VideoEnabled
	policy.VideoMaxFPS = input.VideoMaxFPS
	policy.VideoMaxBitrateKBS = input.VideoMaxBitrateKBS
	policy.AnimationFrameMS = input.AnimationFrameMS
	return policy, nil
}

// RecordStatus upserts the latest device screen status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.Status,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO screen_device_status (
			device_id,
			family_id,
			screen_supported,
			expressions_supported,
			animation_supported,
			point_read_supported,
			video_supported,
			expression,
			animation,
			video_state,
			policy_revision,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			screen_supported = EXCLUDED.screen_supported,
			expressions_supported = EXCLUDED.expressions_supported,
			animation_supported = EXCLUDED.animation_supported,
			point_read_supported = EXCLUDED.point_read_supported,
			video_supported = EXCLUDED.video_supported,
			expression = EXCLUDED.expression,
			animation = EXCLUDED.animation,
			video_state = EXCLUDED.video_state,
			policy_revision = EXCLUDED.policy_revision,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		status.ScreenSupported,
		status.ExpressionsSupported,
		status.AnimationSupported,
		status.PointReadSupported,
		status.VideoSupported,
		status.Expression,
		status.Animation,
		status.VideoState,
		status.PolicyRevision,
	)
	if err != nil {
		return fmt.Errorf("record screen device status: %w", err)
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
			screen_supported,
			expressions_supported,
			animation_supported,
			point_read_supported,
			video_supported,
			expression,
			animation,
			video_state,
			policy_revision,
			reported_at
		FROM screen_device_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list screen device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.Status, 0, 8)
	for rows.Next() {
		var status domain.Status
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&status.ScreenSupported,
			&status.ExpressionsSupported,
			&status.AnimationSupported,
			&status.PointReadSupported,
			&status.VideoSupported,
			&status.Expression,
			&status.Animation,
			&status.VideoState,
			&status.PolicyRevision,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan screen device status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate screen device status: %w", err)
	}
	return statuses, nil
}

// Entries returns the family point-to-read dictionary ordered by term.
func (r *PostgresRepository) Entries(
	ctx context.Context,
	familyID string,
) ([]domain.DictionaryEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, family_id, term, text, created_at, updated_at
		FROM screen_point_read_entries
		WHERE family_id = $1
		ORDER BY term
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list point-read entries: %w", err)
	}
	defer rows.Close()
	entries := make([]domain.DictionaryEntry, 0, 8)
	for rows.Next() {
		var entry domain.DictionaryEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.FamilyID,
			&entry.Term,
			&entry.Text,
			&entry.CreatedAt,
			&entry.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan point-read entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate point-read entries: %w", err)
	}
	return entries, nil
}

// EntryByTerm returns one dictionary entry by its normalized term.
func (r *PostgresRepository) EntryByTerm(
	ctx context.Context,
	familyID string,
	term string,
) (domain.DictionaryEntry, bool, error) {
	var entry domain.DictionaryEntry
	err := r.pool.QueryRow(ctx, `
		SELECT id, family_id, term, text, created_at, updated_at
		FROM screen_point_read_entries
		WHERE family_id = $1 AND term = $2
	`, familyID, term).Scan(
		&entry.ID,
		&entry.FamilyID,
		&entry.Term,
		&entry.Text,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DictionaryEntry{}, false, nil
	}
	if err != nil {
		return domain.DictionaryEntry{}, false, fmt.Errorf("read point-read entry: %w", err)
	}
	return entry, true, nil
}

// SaveEntry upserts one dictionary entry by family and term.
func (r *PostgresRepository) SaveEntry(
	ctx context.Context,
	entry domain.DictionaryEntry,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO screen_point_read_entries (
			id, family_id, term, text, created_at, updated_at
		) VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (family_id, term)
		DO UPDATE SET
			text = EXCLUDED.text,
			updated_at = NOW()
	`, entry.ID, entry.FamilyID, entry.Term, entry.Text)
	if err != nil {
		return fmt.Errorf("save point-read entry: %w", err)
	}
	return nil
}

// DeleteEntry removes one dictionary entry.
func (r *PostgresRepository) DeleteEntry(
	ctx context.Context,
	familyID string,
	entryID string,
) error {
	commandTag, err := r.pool.Exec(ctx, `
		DELETE FROM screen_point_read_entries
		WHERE family_id = $1 AND id = $2
	`, familyID, entryID)
	if err != nil {
		return fmt.Errorf("delete point-read entry: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return domain.ErrDictionaryNotFound
	}
	return nil
}

// EntryCount returns how many entries a family currently stores.
func (r *PostgresRepository) EntryCount(
	ctx context.Context,
	familyID string,
) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM screen_point_read_entries WHERE family_id = $1
	`, familyID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count point-read entries: %w", err)
	}
	return count, nil
}

// Playback returns the current video playback plan for a device.
func (r *PostgresRepository) Playback(
	ctx context.Context,
	familyID string,
	deviceID string,
) (domain.PlaybackPlan, bool, error) {
	var plan domain.PlaybackPlan
	err := r.pool.QueryRow(ctx, `
		SELECT bitrate_kbps, fps
		FROM screen_video_playback
		WHERE family_id = $1 AND device_id = $2
	`, familyID, deviceID).Scan(&plan.BitrateKBS, &plan.FPS)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PlaybackPlan{}, false, nil
	}
	if err != nil {
		return domain.PlaybackPlan{}, false, fmt.Errorf("read video playback: %w", err)
	}
	return plan, true, nil
}

// SavePlayback upserts the video playback plan for a device.
func (r *PostgresRepository) SavePlayback(
	ctx context.Context,
	familyID string,
	deviceID string,
	plan domain.PlaybackPlan,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO screen_video_playback (
			family_id, device_id, bitrate_kbps, fps, updated_at
		) VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (family_id, device_id)
		DO UPDATE SET
			bitrate_kbps = EXCLUDED.bitrate_kbps,
			fps = EXCLUDED.fps,
			updated_at = NOW()
	`, familyID, deviceID, plan.BitrateKBS, plan.FPS)
	if err != nil {
		return fmt.Errorf("save video playback: %w", err)
	}
	return nil
}

// AdminStats returns a bounded fleet summary over the requested window.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE video_supported),
			COUNT(*) FILTER (WHERE point_read_supported)
		FROM screen_device_status
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.VideoDeviceCount,
		&stats.PointReadDeviceCount,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read screen stats: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT video_state, COUNT(*)
		FROM screen_device_status
		WHERE reported_at >= NOW() - ($1::int * INTERVAL '1 day')
		GROUP BY video_state
		ORDER BY video_state
	`, windowDays)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read screen video states: %w", err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 6)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return domain.AdminStats{}, fmt.Errorf("scan screen video state: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return domain.AdminStats{}, fmt.Errorf("iterate screen video states: %w", err)
	}
	stats.StateBuckets = buckets
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

// Readiness aggregates active devices that advertise a display using the
// richer screen contract.
func (r *PostgresRepository) Readiness(
	ctx context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	return featurehealth.ReadDeviceStateReadiness(ctx, featurehealth.DeviceStateQuery{
		Pool:            r.pool,
		Capability:      "display",
		StateTable:      "screen_device_status",
		TimestampColumn: "reported_at",
		FaultCondition:  "NOT status.screen_supported OR status.video_state = 'failed'",
	}, freshness)
}
