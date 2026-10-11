// Package repository persists wearable location policy, positions, emergency
// contacts, the home geofence and SOS events.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/wearable/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for the wearable module.
type Repository interface {
	Policy(ctx context.Context, familyID string) (domain.LocationPolicy, bool, error)
	SavePolicy(ctx context.Context, policy domain.LocationPolicy) error
	ClearFamilyLocations(ctx context.Context, familyID string) error

	CreateContact(ctx context.Context, contact domain.EmergencyContact) error
	UpdateContact(ctx context.Context, contact domain.EmergencyContact) error
	DeleteContact(ctx context.Context, familyID string, contactID string) error
	Contacts(ctx context.Context, familyID string) ([]domain.EmergencyContact, error)
	Contact(ctx context.Context, familyID string, contactID string) (domain.EmergencyContact, bool, error)

	SaveGeofence(ctx context.Context, geofence domain.Geofence) error
	Geofence(ctx context.Context, familyID string) (domain.Geofence, bool, error)

	RecordStatus(ctx context.Context, status domain.DeviceStatus) error
	Statuses(ctx context.Context, familyID string) ([]domain.DeviceStatus, error)

	CreateSOS(ctx context.Context, event domain.SOSEvent) error
	CancelSOS(ctx context.Context, familyID string, eventID string) (domain.SOSEvent, bool, error)
	SOSEvents(ctx context.Context, familyID string, limit int) ([]domain.SOSEvent, error)
	ActiveSOS(ctx context.Context, familyID string) ([]domain.SOSEvent, error)
	ActiveSOSByDevice(ctx context.Context, familyID string, deviceID string) (domain.SOSEvent, bool, error)

	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed wearable repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a wearable repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a wearable repository with an injected
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

// Policy returns the stored location policy for a family.
func (r *PostgresRepository) Policy(
	ctx context.Context,
	familyID string,
) (domain.LocationPolicy, bool, error) {
	var policy domain.LocationPolicy
	err := r.pool.QueryRow(ctx, `
		SELECT family_id, enabled, permission_granted, precision,
			upload_interval_seconds, retention_days, geofence_radius_meters,
			updated_at
		FROM wearable_location_policy
		WHERE family_id = $1
	`, familyID).Scan(
		&policy.FamilyID,
		&policy.Enabled,
		&policy.PermissionGranted,
		&policy.Precision,
		&policy.UploadIntervalSeconds,
		&policy.RetentionDays,
		&policy.GeofenceRadiusMeters,
		&policy.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LocationPolicy{}, false, nil
	}
	if err != nil {
		return domain.LocationPolicy{}, false, fmt.Errorf("read wearable policy: %w", err)
	}
	return policy, true, nil
}

// SavePolicy upserts the family location policy.
func (r *PostgresRepository) SavePolicy(
	ctx context.Context,
	policy domain.LocationPolicy,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO wearable_location_policy (
			family_id, enabled, permission_granted, precision,
			upload_interval_seconds, retention_days, geofence_radius_meters,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			enabled = EXCLUDED.enabled,
			permission_granted = EXCLUDED.permission_granted,
			precision = EXCLUDED.precision,
			upload_interval_seconds = EXCLUDED.upload_interval_seconds,
			retention_days = EXCLUDED.retention_days,
			geofence_radius_meters = EXCLUDED.geofence_radius_meters,
			updated_at = NOW()
	`,
		policy.FamilyID,
		policy.Enabled,
		policy.PermissionGranted,
		policy.Precision,
		policy.UploadIntervalSeconds,
		policy.RetentionDays,
		policy.GeofenceRadiusMeters,
	)
	if err != nil {
		return fmt.Errorf("save wearable policy: %w", err)
	}
	return nil
}

// ClearFamilyLocations deletes every retained position and status for a family.
// It is the deletion half of the privacy contract: revoking the location
// permission removes the stored positions.
func (r *PostgresRepository) ClearFamilyLocations(
	ctx context.Context,
	familyID string,
) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM wearable_device_status WHERE family_id = $1
	`, familyID)
	if err != nil {
		return fmt.Errorf("clear wearable locations: %w", err)
	}
	return nil
}

// CreateContact inserts one emergency contact.
func (r *PostgresRepository) CreateContact(
	ctx context.Context,
	contact domain.EmergencyContact,
) error {
	var childID any
	if contact.ChildID != "" {
		childID = contact.ChildID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO wearable_contacts (
			id, family_id, child_id, display_name, phone, role, priority,
			enabled, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
	`,
		contact.ID,
		contact.FamilyID,
		childID,
		contact.DisplayName,
		contact.Phone,
		contact.Role,
		contact.Priority,
		contact.Enabled,
	)
	if err != nil {
		return fmt.Errorf("create wearable contact: %w", err)
	}
	return nil
}

// UpdateContact updates one emergency contact within a family.
func (r *PostgresRepository) UpdateContact(
	ctx context.Context,
	contact domain.EmergencyContact,
) error {
	var childID any
	if contact.ChildID != "" {
		childID = contact.ChildID
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE wearable_contacts
		SET child_id = $3,
			display_name = $4,
			phone = $5,
			role = $6,
			priority = $7,
			enabled = $8,
			updated_at = NOW()
		WHERE id = $1 AND family_id = $2
	`,
		contact.ID,
		contact.FamilyID,
		childID,
		contact.DisplayName,
		contact.Phone,
		contact.Role,
		contact.Priority,
		contact.Enabled,
	)
	if err != nil {
		return fmt.Errorf("update wearable contact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrContactNotFound
	}
	return nil
}

// DeleteContact removes one emergency contact within a family.
func (r *PostgresRepository) DeleteContact(
	ctx context.Context,
	familyID string,
	contactID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM wearable_contacts WHERE id = $1 AND family_id = $2
	`, contactID, familyID)
	if err != nil {
		return fmt.Errorf("delete wearable contact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrContactNotFound
	}
	return nil
}

// Contacts returns every emergency contact in a family, ordered by priority.
func (r *PostgresRepository) Contacts(
	ctx context.Context,
	familyID string,
) ([]domain.EmergencyContact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, family_id, COALESCE(child_id::text, ''), display_name, phone,
			role, priority, enabled, created_at, updated_at
		FROM wearable_contacts
		WHERE family_id = $1
		ORDER BY priority ASC, created_at DESC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list wearable contacts: %w", err)
	}
	defer rows.Close()
	contacts := make([]domain.EmergencyContact, 0, 8)
	for rows.Next() {
		var contact domain.EmergencyContact
		if err := rows.Scan(
			&contact.ID,
			&contact.FamilyID,
			&contact.ChildID,
			&contact.DisplayName,
			&contact.Phone,
			&contact.Role,
			&contact.Priority,
			&contact.Enabled,
			&contact.CreatedAt,
			&contact.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan wearable contact: %w", err)
		}
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wearable contacts: %w", err)
	}
	return contacts, nil
}

// Contact returns one emergency contact within a family.
func (r *PostgresRepository) Contact(
	ctx context.Context,
	familyID string,
	contactID string,
) (domain.EmergencyContact, bool, error) {
	var contact domain.EmergencyContact
	err := r.pool.QueryRow(ctx, `
		SELECT id, family_id, COALESCE(child_id::text, ''), display_name, phone,
			role, priority, enabled, created_at, updated_at
		FROM wearable_contacts
		WHERE family_id = $1 AND id = $2
	`, familyID, contactID).Scan(
		&contact.ID,
		&contact.FamilyID,
		&contact.ChildID,
		&contact.DisplayName,
		&contact.Phone,
		&contact.Role,
		&contact.Priority,
		&contact.Enabled,
		&contact.CreatedAt,
		&contact.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EmergencyContact{}, false, nil
	}
	if err != nil {
		return domain.EmergencyContact{}, false, fmt.Errorf("read wearable contact: %w", err)
	}
	return contact, true, nil
}

// SaveGeofence upserts the single home geofence for a family.
func (r *PostgresRepository) SaveGeofence(
	ctx context.Context,
	geofence domain.Geofence,
) error {
	var childID any
	if geofence.ChildID != "" {
		childID = geofence.ChildID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO wearable_geofences (
			id, family_id, child_id, latitude, longitude, radius_meters,
			enabled, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET
			child_id = EXCLUDED.child_id,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			radius_meters = EXCLUDED.radius_meters,
			enabled = EXCLUDED.enabled,
			updated_at = NOW()
	`,
		geofence.ID,
		geofence.FamilyID,
		childID,
		geofence.Latitude,
		geofence.Longitude,
		geofence.RadiusMeters,
		geofence.Enabled,
	)
	if err != nil {
		return fmt.Errorf("save wearable geofence: %w", err)
	}
	return nil
}

// Geofence returns the stored home geofence for a family.
func (r *PostgresRepository) Geofence(
	ctx context.Context,
	familyID string,
) (domain.Geofence, bool, error) {
	var geofence domain.Geofence
	err := r.pool.QueryRow(ctx, `
		SELECT id, family_id, COALESCE(child_id::text, ''), latitude, longitude,
			radius_meters, enabled, created_at, updated_at
		FROM wearable_geofences
		WHERE family_id = $1
	`, familyID).Scan(
		&geofence.ID,
		&geofence.FamilyID,
		&geofence.ChildID,
		&geofence.Latitude,
		&geofence.Longitude,
		&geofence.RadiusMeters,
		&geofence.Enabled,
		&geofence.CreatedAt,
		&geofence.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Geofence{}, false, nil
	}
	if err != nil {
		return domain.Geofence{}, false, fmt.Errorf("read wearable geofence: %w", err)
	}
	return geofence, true, nil
}

// RecordStatus upserts the latest device wearable snapshot.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.DeviceStatus,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO wearable_device_status (
			device_id, family_id, location_enabled, permission_granted,
			precision, sos_state, latitude, longitude, accuracy_meters,
			geofence_state, last_reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			location_enabled = EXCLUDED.location_enabled,
			permission_granted = EXCLUDED.permission_granted,
			precision = EXCLUDED.precision,
			sos_state = EXCLUDED.sos_state,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			accuracy_meters = EXCLUDED.accuracy_meters,
			geofence_state = EXCLUDED.geofence_state,
			last_reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		status.LocationEnabled,
		status.PermissionGranted,
		status.Precision,
		status.SOSState,
		status.Latitude,
		status.Longitude,
		status.AccuracyMeters,
		status.GeofenceState,
	)
	if err != nil {
		return fmt.Errorf("record wearable device status: %w", err)
	}
	return nil
}

// Statuses returns every device status in a family.
func (r *PostgresRepository) Statuses(
	ctx context.Context,
	familyID string,
) ([]domain.DeviceStatus, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT device_id, family_id, location_enabled, permission_granted,
			precision, sos_state, latitude, longitude, accuracy_meters,
			geofence_state, last_reported_at
		FROM wearable_device_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list wearable device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.DeviceStatus, 0, 8)
	for rows.Next() {
		var status domain.DeviceStatus
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&status.LocationEnabled,
			&status.PermissionGranted,
			&status.Precision,
			&status.SOSState,
			&status.Latitude,
			&status.Longitude,
			&status.AccuracyMeters,
			&status.GeofenceState,
			&status.LastReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan wearable device status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wearable device status: %w", err)
	}
	return statuses, nil
}

// CreateSOS inserts one SOS event.
func (r *PostgresRepository) CreateSOS(
	ctx context.Context,
	event domain.SOSEvent,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO wearable_sos_events (
			id, family_id, device_id, latitude, longitude, state, triggered_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`,
		event.ID,
		event.FamilyID,
		event.DeviceID,
		event.Latitude,
		event.Longitude,
		event.State,
	)
	if err != nil {
		return fmt.Errorf("create wearable sos: %w", err)
	}
	return nil
}

// CancelSOS transitions one SOS event to cancelled within a family.
func (r *PostgresRepository) CancelSOS(
	ctx context.Context,
	familyID string,
	eventID string,
) (domain.SOSEvent, bool, error) {
	var event domain.SOSEvent
	err := r.pool.QueryRow(ctx, `
		UPDATE wearable_sos_events
		SET state = 'cancelled', cancelled_at = NOW()
		WHERE id = $1 AND family_id = $2 AND state = 'triggered'
		RETURNING id, family_id, device_id, latitude, longitude, state,
			triggered_at, cancelled_at
	`, eventID, familyID).Scan(
		&event.ID,
		&event.FamilyID,
		&event.DeviceID,
		&event.Latitude,
		&event.Longitude,
		&event.State,
		&event.TriggeredAt,
		&event.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SOSEvent{}, false, nil
	}
	if err != nil {
		return domain.SOSEvent{}, false, fmt.Errorf("cancel wearable sos: %w", err)
	}
	return event, true, nil
}

// SOSEvents returns the most recent SOS events in a family.
func (r *PostgresRepository) SOSEvents(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.SOSEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, family_id, device_id, latitude, longitude, state,
			triggered_at, cancelled_at
		FROM wearable_sos_events
		WHERE family_id = $1
		ORDER BY triggered_at DESC
		LIMIT $2
	`, familyID, limit)
	if err != nil {
		return nil, fmt.Errorf("list wearable sos: %w", err)
	}
	defer rows.Close()
	return scanSOSEvents(rows)
}

// ActiveSOS returns every active (triggered, not cancelled) SOS in a family.
func (r *PostgresRepository) ActiveSOS(
	ctx context.Context,
	familyID string,
) ([]domain.SOSEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, family_id, device_id, latitude, longitude, state,
			triggered_at, cancelled_at
		FROM wearable_sos_events
		WHERE family_id = $1 AND state = 'triggered'
		ORDER BY triggered_at DESC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list active wearable sos: %w", err)
	}
	defer rows.Close()
	return scanSOSEvents(rows)
}

// ActiveSOSByDevice returns the active SOS for one device, if any.
func (r *PostgresRepository) ActiveSOSByDevice(
	ctx context.Context,
	familyID string,
	deviceID string,
) (domain.SOSEvent, bool, error) {
	var event domain.SOSEvent
	err := r.pool.QueryRow(ctx, `
		SELECT id, family_id, device_id, latitude, longitude, state,
			triggered_at, cancelled_at
		FROM wearable_sos_events
		WHERE family_id = $1 AND device_id = $2 AND state = 'triggered'
		ORDER BY triggered_at DESC
		LIMIT 1
	`, familyID, deviceID).Scan(
		&event.ID,
		&event.FamilyID,
		&event.DeviceID,
		&event.Latitude,
		&event.Longitude,
		&event.State,
		&event.TriggeredAt,
		&event.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SOSEvent{}, false, nil
	}
	if err != nil {
		return domain.SOSEvent{}, false, fmt.Errorf("read active wearable sos: %w", err)
	}
	return event, true, nil
}

func scanSOSEvents(rows pgx.Rows) ([]domain.SOSEvent, error) {
	events := make([]domain.SOSEvent, 0, 8)
	for rows.Next() {
		var event domain.SOSEvent
		if err := rows.Scan(
			&event.ID,
			&event.FamilyID,
			&event.DeviceID,
			&event.Latitude,
			&event.Longitude,
			&event.State,
			&event.TriggeredAt,
			&event.CancelledAt,
		); err != nil {
			return nil, fmt.Errorf("scan wearable sos: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wearable sos: %w", err)
	}
	return events, nil
}

// AdminStats returns a bounded fleet summary over the requested window.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE location_enabled),
			COUNT(*) FILTER (WHERE permission_granted),
			COUNT(*) FILTER (WHERE sos_state = 'triggered'),
			COUNT(*) FILTER (WHERE geofence_state = 'outside')
		FROM wearable_device_status
		WHERE last_reported_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.DeviceCount,
		&stats.LocationEnabledCount,
		&stats.PermissionCount,
		&stats.SOSTriggeredCount,
		&stats.GeofenceOutsideCount,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read wearable stats: %w", err)
	}

	sosCancelled, err := r.countSOSState(ctx, windowDays, domain.SOSStateCancelled)
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.SOSCancelledCount = sosCancelled

	precisionBuckets, err := r.buckets(ctx, windowDays, "precision")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.PrecisionBuckets = precisionBuckets

	stateBuckets, err := r.buckets(ctx, windowDays, "geofence_state")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.StateBuckets = stateBuckets
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

// countSOSState counts the SOS events in one state over the window.
func (r *PostgresRepository) countSOSState(
	ctx context.Context,
	windowDays int,
	state string,
) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM wearable_sos_events
		WHERE state = $2
			AND triggered_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays, state).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("read wearable sos count: %w", err)
	}
	return count, nil
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
	case "precision":
		query = `
			SELECT precision AS bucket, COUNT(*)
			FROM wearable_device_status
			WHERE last_reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "geofence_state":
		query = `
			SELECT geofence_state AS bucket, COUNT(*)
			FROM wearable_device_status
			WHERE last_reported_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	default:
		return nil, fmt.Errorf("unsupported wearable bucket column %q", column)
	}
	rows, err := r.pool.Query(ctx, query, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read wearable %s buckets: %w", column, err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 6)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan wearable %s bucket: %w", column, err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wearable %s buckets: %w", column, err)
	}
	return buckets, nil
}
