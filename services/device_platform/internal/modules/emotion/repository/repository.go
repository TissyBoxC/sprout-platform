// Package repository persists mood sensing settings and samples.
//
// A row here is a label, a confidence, and an instant. There is no column for
// an image, an audio clip, a transcript, or a sentence fragment, so there is
// nothing in this schema that could reconstruct what a child was doing when a
// sample was taken. Reads for the guardian side are aggregates only: the
// per-sample rows exist for retention and trend arithmetic, and no query in
// this package returns an individual observation to a caller outside the
// service.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/domain"
)

var (
	// ErrSettingsNotFound means the family never stored a setting. It is a
	// normal state, not a failure: the caller answers with the disabled default.
	ErrSettingsNotFound = errors.New("emotion settings not found")
	// ErrDeviceNotBound means the device is not currently bound to the reported
	// child, so the sample would describe a child this device cannot see.
	ErrDeviceNotBound = errors.New("emotion child is not bound to this device")
)

// Repository stores emotion settings and observations.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds a repository over the shared pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const settingsColumns = `family_id::text, is_enabled, retention_days, updated_at`

// GetSettings returns the family's stored setting, or ErrSettingsNotFound when
// the guardian never changed it.
func (repository *Repository) GetSettings(
	ctx context.Context,
	familyID string,
) (domain.Settings, error) {
	row := repository.pool.QueryRow(ctx,
		`SELECT `+settingsColumns+` FROM emotion_settings WHERE family_id = $1::uuid`,
		familyID,
	)
	var settings domain.Settings
	if err := row.Scan(
		&settings.FamilyID, &settings.Enabled, &settings.RetentionDays, &settings.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Settings{}, ErrSettingsNotFound
		}
		return domain.Settings{}, fmt.Errorf("read emotion settings: %w", err)
	}
	return settings, nil
}

// SaveSettings stores the family's choice, creating the row on first write.
//
// The upsert is what makes a first enable and a later change the same
// operation. Turning sensing off keeps the row rather than deleting it, so the
// guardian's retention choice survives a temporary opt-out.
func (repository *Repository) SaveSettings(ctx context.Context, settings domain.Settings) error {
	_, err := repository.pool.Exec(ctx,
		`INSERT INTO emotion_settings (family_id, is_enabled, retention_days, updated_at)
		 VALUES ($1::uuid, $2, $3, NOW())
		 ON CONFLICT (family_id) DO UPDATE
		 SET is_enabled = EXCLUDED.is_enabled,
		     retention_days = EXCLUDED.retention_days,
		     updated_at = NOW()`,
		settings.FamilyID, settings.Enabled, settings.RetentionDays,
	)
	if err != nil {
		return fmt.Errorf("save emotion settings: %w", err)
	}
	return nil
}

// DeviceFamily returns the family that owns the device, or an empty string when
// the device is unknown or unbound.
//
// An unknown device is not an error here; the service answers "not found", so a
// caller cannot tell an unbound device from another household's device.
func (repository *Repository) DeviceFamily(ctx context.Context, deviceID string) (string, error) {
	var familyID string
	err := repository.pool.QueryRow(ctx,
		`SELECT COALESCE(parent_account_id::text, '') FROM device_bindings
		 WHERE device_id = $1::text`,
		deviceID,
	).Scan(&familyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("resolve emotion device family: %w", err)
	}
	return familyID, nil
}

// DeviceChildBinding reports whether the device currently serves childID in
// familyID.
//
// Both the device and the family are part of the predicate rather than checked
// afterwards: a binding that points at another household's child can never load
// a row, so the answer for a cross-family child is the same as for a child that
// does not exist.
func (repository *Repository) DeviceChildBinding(
	ctx context.Context,
	familyID, deviceID, childID string,
) (bool, error) {
	var exists bool
	err := repository.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM device_child_bindings binding
			WHERE binding.device_id = $1::text
			  AND binding.is_active
			  AND binding.family_id = $2::uuid
			  AND binding.child_id = NULLIF($3, '')::uuid
		 )`,
		deviceID, familyID, childID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check emotion child binding: %w", err)
	}
	return exists, nil
}

// InsertObservation stores one accepted sample.
//
// The insert re-checks the binding inside the same statement as the write, so a
// device that was unbound between the service's check and this call cannot
// record a sample about a child it no longer serves.
func (repository *Repository) InsertObservation(
	ctx context.Context,
	observation domain.Observation,
) error {
	tag, err := repository.pool.Exec(ctx,
		`INSERT INTO emotion_observations
			(id, family_id, child_id, device_id, label, confidence, observed_at)
		 SELECT $1::uuid, $2::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7
		 WHERE $3 = ''
		    OR EXISTS (
				SELECT 1 FROM device_child_bindings binding
				WHERE binding.device_id = $4::text
				  AND binding.is_active
				  AND binding.family_id = $2::uuid
				  AND binding.child_id = NULLIF($3, '')::uuid
		    )`,
		observation.ID, observation.FamilyID, observation.ChildID,
		observation.DeviceID, string(observation.Label),
		observation.Confidence, observation.ObservedAt,
	)
	if err != nil {
		return fmt.Errorf("insert emotion observation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDeviceNotBound
	}
	return nil
}

// CountLabels aggregates samples per label between two instants.
//
// The query returns counts and confidence sums only. It never returns an
// observation instant, an identifier, or a device, so nothing derived from it
// can locate a child's mood in time.
func (repository *Repository) CountLabels(
	ctx context.Context,
	familyID, childID string,
	from, to time.Time,
) (map[domain.Label]domain.LabelCount, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT label, COUNT(*), COALESCE(SUM(confidence), 0)
		 FROM emotion_observations
		 WHERE family_id = $1::uuid
		   AND ($2 = '' OR child_id = NULLIF($2, '')::uuid)
		   AND observed_at >= $3
		   AND observed_at < $4
		 GROUP BY label`,
		familyID, childID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("count emotion labels: %w", err)
	}
	defer rows.Close()

	counts := make(map[domain.Label]domain.LabelCount, len(domain.AllLabels()))
	for rows.Next() {
		var label string
		var count int
		var confidenceSum int64
		if err := rows.Scan(&label, &count, &confidenceSum); err != nil {
			return nil, fmt.Errorf("scan emotion label count: %w", err)
		}
		counts[domain.Label(label)] = domain.LabelCount{
			Label:         domain.Label(label),
			Count:         count,
			ConfidenceSum: int(confidenceSum),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate emotion label counts: %w", err)
	}
	return counts, nil
}

// DeleteBefore removes samples observed before cutoff and reports how many were
// removed.
func (repository *Repository) DeleteBefore(
	ctx context.Context,
	familyID string,
	cutoff time.Time,
) (int, error) {
	tag, err := repository.pool.Exec(ctx,
		`DELETE FROM emotion_observations
		 WHERE family_id = $1::uuid AND observed_at < $2`,
		familyID, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("delete expired emotion observations: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ListEnabledFamilies returns the families with sensing turned on, at most
// limit of them.
//
// The retention worker iterates this list rather than scanning every family,
// because a family that never enabled the feature has nothing to retain.
func (repository *Repository) ListEnabledFamilies(
	ctx context.Context,
	limit int,
) ([]domain.Settings, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT `+settingsColumns+` FROM emotion_settings
		 WHERE is_enabled
		 ORDER BY family_id
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list emotion families: %w", err)
	}
	defer rows.Close()

	settings := make([]domain.Settings, 0, limit)
	for rows.Next() {
		var item domain.Settings
		if err := rows.Scan(
			&item.FamilyID, &item.Enabled, &item.RetentionDays, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan emotion settings: %w", err)
		}
		settings = append(settings, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate emotion settings: %w", err)
	}
	return settings, nil
}

// CountEnabledFamilies reports how many families turned sensing on.
func (repository *Repository) CountEnabledFamilies(ctx context.Context) (int, error) {
	var count int
	err := repository.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM emotion_settings WHERE is_enabled`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count enabled emotion families: %w", err)
	}
	return count, nil
}

// CountObservationsSince counts samples across every family since an instant.
//
// It is the platform-wide activity number the feature center reports, so it is
// deliberately not family-scoped and returns no label breakdown.
func (repository *Repository) CountObservationsSince(
	ctx context.Context,
	since time.Time,
) (int, error) {
	var count int
	err := repository.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM emotion_observations WHERE observed_at >= $1`,
		since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count emotion observations: %w", err)
	}
	return count, nil
}
