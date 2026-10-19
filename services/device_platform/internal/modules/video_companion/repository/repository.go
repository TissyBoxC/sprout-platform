// Package repository persists video-companionship contacts, calls and device
// status.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_companion/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for video companionship.
type Repository interface {
	CreateContact(ctx context.Context, contact domain.Contact) error
	UpdateContact(ctx context.Context, contact domain.Contact) error
	DeleteContact(ctx context.Context, familyID string, contactID string) error
	Contacts(ctx context.Context, familyID string) ([]domain.Contact, error)
	Contact(ctx context.Context, familyID string, contactID string) (domain.Contact, bool, error)

	CreateCall(ctx context.Context, call domain.Call) error
	Call(ctx context.Context, familyID string, callID string) (domain.Call, bool, error)
	UpdateCall(ctx context.Context, call domain.Call) error
	Calls(ctx context.Context, familyID string, limit int) ([]domain.Call, error)

	RecordStatus(ctx context.Context, status domain.DeviceStatus) error
	Statuses(ctx context.Context, familyID string) ([]domain.DeviceStatus, error)

	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed video-companion repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a video-companion repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a video-companion repository with an
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

// CreateContact inserts one contact.
func (r *PostgresRepository) CreateContact(
	ctx context.Context,
	contact domain.Contact,
) error {
	var childID any
	if contact.ChildID != "" {
		childID = contact.ChildID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO video_contacts (
			id, family_id, child_id, display_name, role, enabled, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`,
		contact.ID,
		contact.FamilyID,
		childID,
		contact.DisplayName,
		contact.Role,
		contact.Enabled,
	)
	if err != nil {
		return fmt.Errorf("create video contact: %w", err)
	}
	return nil
}

// UpdateContact updates one contact within a family.
func (r *PostgresRepository) UpdateContact(
	ctx context.Context,
	contact domain.Contact,
) error {
	var childID any
	if contact.ChildID != "" {
		childID = contact.ChildID
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE video_contacts
		SET child_id = $3,
			display_name = $4,
			role = $5,
			enabled = $6,
			updated_at = NOW()
		WHERE id = $1 AND family_id = $2
	`,
		contact.ID,
		contact.FamilyID,
		childID,
		contact.DisplayName,
		contact.Role,
		contact.Enabled,
	)
	if err != nil {
		return fmt.Errorf("update video contact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrContactNotFound
	}
	return nil
}

// DeleteContact removes one contact within a family.
func (r *PostgresRepository) DeleteContact(
	ctx context.Context,
	familyID string,
	contactID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM video_contacts WHERE id = $1 AND family_id = $2
	`, contactID, familyID)
	if err != nil {
		return fmt.Errorf("delete video contact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrContactNotFound
	}
	return nil
}

// Contacts returns every contact in a family.
func (r *PostgresRepository) Contacts(
	ctx context.Context,
	familyID string,
) ([]domain.Contact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, family_id, COALESCE(child_id::text, ''), display_name, role,
			enabled, created_at, updated_at
		FROM video_contacts
		WHERE family_id = $1
		ORDER BY created_at DESC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list video contacts: %w", err)
	}
	defer rows.Close()
	contacts := make([]domain.Contact, 0, 8)
	for rows.Next() {
		var contact domain.Contact
		if err := rows.Scan(
			&contact.ID,
			&contact.FamilyID,
			&contact.ChildID,
			&contact.DisplayName,
			&contact.Role,
			&contact.Enabled,
			&contact.CreatedAt,
			&contact.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan video contact: %w", err)
		}
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate video contacts: %w", err)
	}
	return contacts, nil
}

// Contact returns one contact within a family.
func (r *PostgresRepository) Contact(
	ctx context.Context,
	familyID string,
	contactID string,
) (domain.Contact, bool, error) {
	var contact domain.Contact
	err := r.pool.QueryRow(ctx, `
		SELECT id, family_id, COALESCE(child_id::text, ''), display_name, role,
			enabled, created_at, updated_at
		FROM video_contacts
		WHERE family_id = $1 AND id = $2
	`, familyID, contactID).Scan(
		&contact.ID,
		&contact.FamilyID,
		&contact.ChildID,
		&contact.DisplayName,
		&contact.Role,
		&contact.Enabled,
		&contact.CreatedAt,
		&contact.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Contact{}, false, nil
	}
	if err != nil {
		return domain.Contact{}, false, fmt.Errorf("read video contact: %w", err)
	}
	return contact, true, nil
}

// CreateCall inserts one call.
func (r *PostgresRepository) CreateCall(
	ctx context.Context,
	call domain.Call,
) error {
	var deviceID any
	if call.DeviceID != "" {
		deviceID = call.DeviceID
	}
	var contactID any
	if call.ContactID != "" {
		contactID = call.ContactID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO video_calls (
			id, family_id, device_id, contact_id, initiator, state, quality,
			started_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`,
		call.ID,
		call.FamilyID,
		deviceID,
		contactID,
		call.Initiator,
		call.State,
		call.Quality,
	)
	if err != nil {
		return fmt.Errorf("create video call: %w", err)
	}
	return nil
}

// Call returns one call within a family.
func (r *PostgresRepository) Call(
	ctx context.Context,
	familyID string,
	callID string,
) (domain.Call, bool, error) {
	var call domain.Call
	var endReason *string
	err := r.pool.QueryRow(ctx, `
		SELECT id, family_id, COALESCE(device_id, ''), COALESCE(contact_id::text, ''),
			initiator, state, quality, end_reason, started_at, answered_at, ended_at
		FROM video_calls
		WHERE family_id = $1 AND id = $2
	`, familyID, callID).Scan(
		&call.ID,
		&call.FamilyID,
		&call.DeviceID,
		&call.ContactID,
		&call.Initiator,
		&call.State,
		&call.Quality,
		&endReason,
		&call.StartedAt,
		&call.AnsweredAt,
		&call.EndedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Call{}, false, nil
	}
	if err != nil {
		return domain.Call{}, false, fmt.Errorf("read video call: %w", err)
	}
	if endReason != nil {
		call.EndReason = *endReason
	}
	return call, true, nil
}

// UpdateCall updates one call within a family.
func (r *PostgresRepository) UpdateCall(
	ctx context.Context,
	call domain.Call,
) error {
	var endReason any
	if call.EndReason != "" {
		endReason = call.EndReason
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE video_calls
		SET state = $3,
			quality = $4,
			end_reason = $5,
			answered_at = $6,
			ended_at = $7
		WHERE id = $1 AND family_id = $2
	`,
		call.ID,
		call.FamilyID,
		call.State,
		call.Quality,
		endReason,
		call.AnsweredAt,
		call.EndedAt,
	)
	if err != nil {
		return fmt.Errorf("update video call: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCallNotFound
	}
	return nil
}

// Calls returns the most recent calls in a family.
func (r *PostgresRepository) Calls(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.Call, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, family_id, COALESCE(device_id, ''), COALESCE(contact_id::text, ''),
			initiator, state, quality, end_reason, started_at, answered_at, ended_at
		FROM video_calls
		WHERE family_id = $1
		ORDER BY started_at DESC
		LIMIT $2
	`, familyID, limit)
	if err != nil {
		return nil, fmt.Errorf("list video calls: %w", err)
	}
	defer rows.Close()
	calls := make([]domain.Call, 0, limit)
	for rows.Next() {
		var call domain.Call
		var endReason *string
		if err := rows.Scan(
			&call.ID,
			&call.FamilyID,
			&call.DeviceID,
			&call.ContactID,
			&call.Initiator,
			&call.State,
			&call.Quality,
			&endReason,
			&call.StartedAt,
			&call.AnsweredAt,
			&call.EndedAt,
		); err != nil {
			return nil, fmt.Errorf("scan video call: %w", err)
		}
		if endReason != nil {
			call.EndReason = *endReason
		}
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate video calls: %w", err)
	}
	return calls, nil
}

// RecordStatus upserts the latest device video status.
func (r *PostgresRepository) RecordStatus(
	ctx context.Context,
	status domain.DeviceStatus,
) error {
	var lastCallID any
	if status.LastCallID != "" {
		lastCallID = status.LastCallID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_video_status (
			device_id, family_id, call_state, quality, camera_allowed,
			microphone_allowed, last_call_id, reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (device_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			call_state = EXCLUDED.call_state,
			quality = EXCLUDED.quality,
			camera_allowed = EXCLUDED.camera_allowed,
			microphone_allowed = EXCLUDED.microphone_allowed,
			last_call_id = EXCLUDED.last_call_id,
			reported_at = NOW()
	`,
		status.DeviceID,
		status.FamilyID,
		status.CallState,
		status.Quality,
		status.CameraAllowed,
		status.MicrophoneAllowed,
		lastCallID,
	)
	if err != nil {
		return fmt.Errorf("record video device status: %w", err)
	}
	return nil
}

// Statuses returns every device status in a family.
func (r *PostgresRepository) Statuses(
	ctx context.Context,
	familyID string,
) ([]domain.DeviceStatus, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT device_id, family_id, call_state, quality, camera_allowed,
			microphone_allowed, COALESCE(last_call_id::text, ''), reported_at
		FROM device_video_status
		WHERE family_id = $1
		ORDER BY device_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list video device status: %w", err)
	}
	defer rows.Close()
	statuses := make([]domain.DeviceStatus, 0, 8)
	for rows.Next() {
		var status domain.DeviceStatus
		if err := rows.Scan(
			&status.DeviceID,
			&status.FamilyID,
			&status.CallState,
			&status.Quality,
			&status.CameraAllowed,
			&status.MicrophoneAllowed,
			&status.LastCallID,
			&status.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan video device status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate video device status: %w", err)
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
			COUNT(*) FILTER (WHERE state = 'active'),
			COUNT(*) FILTER (WHERE state = 'ended'),
			COUNT(*) FILTER (WHERE state = 'missed'),
			COUNT(*) FILTER (WHERE state = 'rejected'),
			COUNT(*) FILTER (WHERE state = 'failed'),
			COUNT(*) FILTER (WHERE quality = 'poor')
		FROM video_calls
		WHERE started_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays).Scan(
		&stats.CallCount,
		&stats.ActiveCount,
		&stats.EndedCount,
		&stats.MissedCount,
		&stats.RejectedCount,
		&stats.FailedCount,
		&stats.PoorQualityCount,
	)
	if err != nil {
		return domain.AdminStats{}, fmt.Errorf("read video call stats: %w", err)
	}

	stateBuckets, err := r.buckets(ctx, windowDays, "state")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.StateBuckets = stateBuckets

	reasonBuckets, err := r.buckets(ctx, windowDays, "end_reason")
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.ReasonBuckets = reasonBuckets
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

// buckets returns one column's histogram over the window. The column is chosen
// from a fixed allow-list so it can never be attacker-controlled; a null end
// reason is reported as "none".
func (r *PostgresRepository) buckets(
	ctx context.Context,
	windowDays int,
	column string,
) ([]domain.Count, error) {
	query := ""
	switch column {
	case "state":
		query = `
			SELECT state AS bucket, COUNT(*)
			FROM video_calls
			WHERE started_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "end_reason":
		query = `
			SELECT COALESCE(end_reason, 'none') AS bucket, COUNT(*)
			FROM video_calls
			WHERE started_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	default:
		return nil, fmt.Errorf("unsupported video bucket column %q", column)
	}
	rows, err := r.pool.Query(ctx, query, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read video %s buckets: %w", column, err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 6)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan video %s bucket: %w", column, err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate video %s buckets: %w", column, err)
	}
	return buckets, nil
}

// Readiness aggregates active devices that advertise video calls. A failed call
// is a runtime fault, while missing or stale device state degrades readiness.
func (r *PostgresRepository) Readiness(
	ctx context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	return featurehealth.ReadDeviceStateReadiness(ctx, featurehealth.DeviceStateQuery{
		Pool:            r.pool,
		Capability:      "video_call",
		StateTable:      "device_video_status",
		TimestampColumn: "reported_at",
		FaultCondition:  "status.call_state = 'failed'",
	}, freshness)
}
