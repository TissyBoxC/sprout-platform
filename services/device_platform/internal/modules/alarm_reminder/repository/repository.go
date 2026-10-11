// Package repository persists guardian alarm reminders and the bounded firing
// acknowledgements devices report.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for alarm reminders.
type Repository interface {
	CreateAlarm(ctx context.Context, alarm *domain.Alarm) error
	UpdateAlarm(ctx context.Context, alarm *domain.Alarm) error
	GetAlarm(ctx context.Context, familyID string, alarmID string) (*domain.Alarm, error)
	ListAlarms(ctx context.Context, familyID string) ([]domain.Alarm, error)
	ListEnabledAlarms(ctx context.Context, familyID string) ([]domain.Alarm, error)
	DeleteAlarm(ctx context.Context, familyID string, alarmID string) error
	AlarmCount(ctx context.Context, familyID string) (int, error)

	RecordEvent(ctx context.Context, event *domain.AlarmEvent) error
	ListEvents(ctx context.Context, familyID string, limit int) ([]domain.AlarmEvent, error)
}

// PostgresRepository is the PostgreSQL-backed alarm reminder repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates an alarm reminder repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// CreateAlarm inserts one alarm.
func (r *PostgresRepository) CreateAlarm(
	ctx context.Context,
	alarm *domain.Alarm,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO alarm_reminders (
			id, family_id, child_id, label, prompt, alarm_time, weekdays,
			enabled, snooze_minutes, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6::time, $7, $8, $9, $10, $11)
	`,
		alarm.ID,
		alarm.FamilyID,
		nullableUUID(alarm.ChildID),
		alarm.Label,
		alarm.Prompt,
		alarm.AlarmTime,
		alarm.Weekdays,
		alarm.Enabled,
		alarm.SnoozeMin,
		alarm.CreatedAt,
		alarm.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert alarm reminder: %w", err)
	}
	return nil
}

// UpdateAlarm replaces the editable fields of one alarm.
func (r *PostgresRepository) UpdateAlarm(
	ctx context.Context,
	alarm *domain.Alarm,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE alarm_reminders
		SET
			child_id = $3,
			label = $4,
			prompt = $5,
			alarm_time = $6::time,
			weekdays = $7,
			enabled = $8,
			snooze_minutes = $9,
			updated_at = $10
		WHERE family_id = $1 AND id = $2
	`,
		alarm.FamilyID,
		alarm.ID,
		nullableUUID(alarm.ChildID),
		alarm.Label,
		alarm.Prompt,
		alarm.AlarmTime,
		alarm.Weekdays,
		alarm.Enabled,
		alarm.SnoozeMin,
		alarm.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update alarm reminder: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAlarmNotFound
	}
	return nil
}

// GetAlarm returns one family-owned alarm.
func (r *PostgresRepository) GetAlarm(
	ctx context.Context,
	familyID string,
	alarmID string,
) (*domain.Alarm, error) {
	var alarm domain.Alarm
	err := r.pool.QueryRow(ctx, `
		SELECT
			id, family_id, COALESCE(child_id::text, ''), label, prompt,
			to_char(alarm_time, 'HH24:MI'), weekdays, enabled,
			snooze_minutes, created_at, updated_at
		FROM alarm_reminders
		WHERE family_id = $1 AND id = $2
	`, familyID, alarmID).Scan(
		&alarm.ID,
		&alarm.FamilyID,
		&alarm.ChildID,
		&alarm.Label,
		&alarm.Prompt,
		&alarm.AlarmTime,
		&alarm.Weekdays,
		&alarm.Enabled,
		&alarm.SnoozeMin,
		&alarm.CreatedAt,
		&alarm.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAlarmNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get alarm reminder: %w", err)
	}
	return &alarm, nil
}

// ListAlarms returns every alarm owned by one family, newest first.
func (r *PostgresRepository) ListAlarms(
	ctx context.Context,
	familyID string,
) ([]domain.Alarm, error) {
	return r.listAlarms(ctx, familyID, false)
}

// ListEnabledAlarms returns only the enabled alarms of one family, ordered by
// their local time so a device can walk them in ring order.
func (r *PostgresRepository) ListEnabledAlarms(
	ctx context.Context,
	familyID string,
) ([]domain.Alarm, error) {
	return r.listAlarms(ctx, familyID, true)
}

func (r *PostgresRepository) listAlarms(
	ctx context.Context,
	familyID string,
	enabledOnly bool,
) ([]domain.Alarm, error) {
	query := `
		SELECT
			id, family_id, COALESCE(child_id::text, ''), label, prompt,
			to_char(alarm_time, 'HH24:MI'), weekdays, enabled,
			snooze_minutes, created_at, updated_at
		FROM alarm_reminders
		WHERE family_id = $1
	`
	if enabledOnly {
		query += " AND enabled = TRUE ORDER BY alarm_time ASC, id ASC"
	} else {
		query += " ORDER BY created_at DESC, id DESC"
	}
	rows, err := r.pool.Query(ctx, query, familyID)
	if err != nil {
		return nil, fmt.Errorf("list alarm reminders: %w", err)
	}
	defer rows.Close()
	alarms := make([]domain.Alarm, 0, 16)
	for rows.Next() {
		var alarm domain.Alarm
		if err := rows.Scan(
			&alarm.ID,
			&alarm.FamilyID,
			&alarm.ChildID,
			&alarm.Label,
			&alarm.Prompt,
			&alarm.AlarmTime,
			&alarm.Weekdays,
			&alarm.Enabled,
			&alarm.SnoozeMin,
			&alarm.CreatedAt,
			&alarm.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan alarm reminder: %w", err)
		}
		alarms = append(alarms, alarm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alarm reminders: %w", err)
	}
	return alarms, nil
}

// DeleteAlarm removes one family-owned alarm. Its firing events cascade.
func (r *PostgresRepository) DeleteAlarm(
	ctx context.Context,
	familyID string,
	alarmID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM alarm_reminders
		WHERE family_id = $1 AND id = $2
	`, familyID, alarmID)
	if err != nil {
		return fmt.Errorf("delete alarm reminder: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAlarmNotFound
	}
	return nil
}

// AlarmCount returns how many alarms one family currently stores.
func (r *PostgresRepository) AlarmCount(
	ctx context.Context,
	familyID string,
) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM alarm_reminders WHERE family_id = $1
	`, familyID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count alarm reminders: %w", err)
	}
	return count, nil
}

// RecordEvent upserts the latest firing state for one alarm and local date so a
// delivered-then-retried report is idempotent.
func (r *PostgresRepository) RecordEvent(
	ctx context.Context,
	event *domain.AlarmEvent,
) error {
	var deviceID any
	if strings.TrimSpace(event.DeviceID) != "" {
		deviceID = event.DeviceID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO alarm_reminder_events (
			id, alarm_id, family_id, device_id, alarm_date, state, reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (alarm_id, alarm_date)
		DO UPDATE SET
			state = EXCLUDED.state,
			device_id = EXCLUDED.device_id,
			family_id = EXCLUDED.family_id,
			reported_at = NOW()
	`,
		event.ID,
		event.AlarmID,
		event.FamilyID,
		deviceID,
		event.AlarmDate,
		event.State,
	)
	if err != nil {
		return fmt.Errorf("record alarm reminder event: %w", err)
	}
	return nil
}

// ListEvents returns the most recent firing events of one family.
func (r *PostgresRepository) ListEvents(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.AlarmEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id, alarm_id, family_id, COALESCE(device_id, ''), alarm_date,
			state, reported_at
		FROM alarm_reminder_events
		WHERE family_id = $1
		ORDER BY reported_at DESC, id DESC
		LIMIT $2
	`, familyID, limit)
	if err != nil {
		return nil, fmt.Errorf("list alarm reminder events: %w", err)
	}
	defer rows.Close()
	events := make([]domain.AlarmEvent, 0, limit)
	for rows.Next() {
		var event domain.AlarmEvent
		if err := rows.Scan(
			&event.ID,
			&event.AlarmID,
			&event.FamilyID,
			&event.DeviceID,
			&event.AlarmDate,
			&event.State,
			&event.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan alarm reminder event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alarm reminder events: %w", err)
	}
	return events, nil
}

// nullableUUID returns NULL for an empty id so the soft child reference stays
// optional without storing an empty string in a UUID column.
func nullableUUID(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
