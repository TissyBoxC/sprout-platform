// Package repository persists companionship growth records and habit reminders.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for companionship data.
type Repository interface {
	CreateGrowthRecord(ctx context.Context, record *domain.GrowthRecord) error
	ListGrowthRecords(
		ctx context.Context,
		familyID string,
		query domain.GrowthRecordQuery,
	) ([]domain.GrowthRecord, error)
	DeleteGrowthRecord(ctx context.Context, familyID string, recordID string) error
	GetGrowthRecord(
		ctx context.Context,
		familyID string,
		recordID string,
	) (*domain.GrowthRecord, error)

	CreateHabitReminder(ctx context.Context, reminder *domain.HabitReminder) error
	UpdateHabitReminder(ctx context.Context, reminder *domain.HabitReminder) error
	ListHabitReminders(ctx context.Context, familyID string) ([]domain.HabitReminder, error)
	GetHabitReminder(
		ctx context.Context,
		familyID string,
		reminderID string,
	) (*domain.HabitReminder, error)
	DeleteHabitReminder(ctx context.Context, familyID string, reminderID string) error
}

// PostgresRepository is the PostgreSQL-backed companionship repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a companionship repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// CreateGrowthRecord inserts one timeline entry.
func (r *PostgresRepository) CreateGrowthRecord(
	ctx context.Context,
	record *domain.GrowthRecord,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO companionship_growth_records (
			id,
			family_id,
			child_id,
			category,
			title,
			note,
			recorded_at,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		record.ID,
		record.FamilyID,
		nullableUUID(record.ChildID),
		record.Category,
		record.Title,
		record.Note,
		record.RecordedAt,
		record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert growth record: %w", err)
	}
	return nil
}

// ListGrowthRecords returns one family's timeline, newest first, bounded by the
// query limit so a long-lived family cannot exhaust the response.
func (r *PostgresRepository) ListGrowthRecords(
	ctx context.Context,
	familyID string,
	query domain.GrowthRecordQuery,
) ([]domain.GrowthRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			family_id,
			COALESCE(child_id::text, ''),
			category,
			title,
			note,
			recorded_at,
			created_at
		FROM companionship_growth_records
		WHERE family_id = $1
			AND ($2 = '' OR child_id::text = $2)
			AND ($3 = '' OR category = $3)
			AND ($4::timestamptz IS NULL OR recorded_at < $4)
		ORDER BY recorded_at DESC, id DESC
		LIMIT $5
	`,
		familyID,
		query.ChildID,
		query.Category,
		query.BeforeTime,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list growth records: %w", err)
	}
	defer rows.Close()

	records := make([]domain.GrowthRecord, 0, query.Limit)
	for rows.Next() {
		var record domain.GrowthRecord
		if err := rows.Scan(
			&record.ID,
			&record.FamilyID,
			&record.ChildID,
			&record.Category,
			&record.Title,
			&record.Note,
			&record.RecordedAt,
			&record.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan growth record: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate growth records: %w", err)
	}
	return records, nil
}

// GetGrowthRecord returns one family-owned entry.
func (r *PostgresRepository) GetGrowthRecord(
	ctx context.Context,
	familyID string,
	recordID string,
) (*domain.GrowthRecord, error) {
	var record domain.GrowthRecord
	err := r.pool.QueryRow(ctx, `
		SELECT
			id,
			family_id,
			COALESCE(child_id::text, ''),
			category,
			title,
			note,
			recorded_at,
			created_at
		FROM companionship_growth_records
		WHERE family_id = $1 AND id = $2
	`, familyID, recordID).Scan(
		&record.ID,
		&record.FamilyID,
		&record.ChildID,
		&record.Category,
		&record.Title,
		&record.Note,
		&record.RecordedAt,
		&record.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrGrowthRecordNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get growth record: %w", err)
	}
	return &record, nil
}

// DeleteGrowthRecord removes one family-owned entry.
func (r *PostgresRepository) DeleteGrowthRecord(
	ctx context.Context,
	familyID string,
	recordID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM companionship_growth_records
		WHERE family_id = $1 AND id = $2
	`, familyID, recordID)
	if err != nil {
		return fmt.Errorf("delete growth record: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrGrowthRecordNotFound
	}
	return nil
}

// CreateHabitReminder inserts one reminder.
func (r *PostgresRepository) CreateHabitReminder(
	ctx context.Context,
	reminder *domain.HabitReminder,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO companionship_habit_reminders (
			id,
			family_id,
			child_id,
			label,
			prompt,
			reminder_time,
			weekdays,
			enabled,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6::time, $7, $8, $9, $10)
	`,
		reminder.ID,
		reminder.FamilyID,
		nullableUUID(reminder.ChildID),
		reminder.Label,
		reminder.Prompt,
		reminder.ReminderTime,
		reminder.Weekdays,
		reminder.Enabled,
		reminder.CreatedAt,
		reminder.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert habit reminder: %w", err)
	}
	return nil
}

// UpdateHabitReminder replaces the editable fields of one reminder.
func (r *PostgresRepository) UpdateHabitReminder(
	ctx context.Context,
	reminder *domain.HabitReminder,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE companionship_habit_reminders
		SET
			child_id = $3,
			label = $4,
			prompt = $5,
			reminder_time = $6::time,
			weekdays = $7,
			enabled = $8,
			updated_at = $9
		WHERE family_id = $1 AND id = $2
	`,
		reminder.FamilyID,
		reminder.ID,
		nullableUUID(reminder.ChildID),
		reminder.Label,
		reminder.Prompt,
		reminder.ReminderTime,
		reminder.Weekdays,
		reminder.Enabled,
		reminder.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update habit reminder: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrHabitReminderNotFound
	}
	return nil
}

// ListHabitReminders returns every reminder owned by one family, newest first.
func (r *PostgresRepository) ListHabitReminders(
	ctx context.Context,
	familyID string,
) ([]domain.HabitReminder, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			family_id,
			COALESCE(child_id::text, ''),
			label,
			prompt,
			to_char(reminder_time, 'HH24:MI'),
			weekdays,
			enabled,
			created_at,
			updated_at
		FROM companionship_habit_reminders
		WHERE family_id = $1
		ORDER BY created_at DESC, id DESC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list habit reminders: %w", err)
	}
	defer rows.Close()

	reminders := make([]domain.HabitReminder, 0, 16)
	for rows.Next() {
		var reminder domain.HabitReminder
		if err := rows.Scan(
			&reminder.ID,
			&reminder.FamilyID,
			&reminder.ChildID,
			&reminder.Label,
			&reminder.Prompt,
			&reminder.ReminderTime,
			&reminder.Weekdays,
			&reminder.Enabled,
			&reminder.CreatedAt,
			&reminder.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan habit reminder: %w", err)
		}
		reminders = append(reminders, reminder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate habit reminders: %w", err)
	}
	return reminders, nil
}

// GetHabitReminder returns one family-owned reminder.
func (r *PostgresRepository) GetHabitReminder(
	ctx context.Context,
	familyID string,
	reminderID string,
) (*domain.HabitReminder, error) {
	var reminder domain.HabitReminder
	err := r.pool.QueryRow(ctx, `
		SELECT
			id,
			family_id,
			COALESCE(child_id::text, ''),
			label,
			prompt,
			to_char(reminder_time, 'HH24:MI'),
			weekdays,
			enabled,
			created_at,
			updated_at
		FROM companionship_habit_reminders
		WHERE family_id = $1 AND id = $2
	`, familyID, reminderID).Scan(
		&reminder.ID,
		&reminder.FamilyID,
		&reminder.ChildID,
		&reminder.Label,
		&reminder.Prompt,
		&reminder.ReminderTime,
		&reminder.Weekdays,
		&reminder.Enabled,
		&reminder.CreatedAt,
		&reminder.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrHabitReminderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get habit reminder: %w", err)
	}
	return &reminder, nil
}

// DeleteHabitReminder removes one family-owned reminder.
func (r *PostgresRepository) DeleteHabitReminder(
	ctx context.Context,
	familyID string,
	reminderID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM companionship_habit_reminders
		WHERE family_id = $1 AND id = $2
	`, familyID, reminderID)
	if err != nil {
		return fmt.Errorf("delete habit reminder: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrHabitReminderNotFound
	}
	return nil
}

// nullableUUID returns NULL for an empty id so the soft child reference stays
// optional without storing an empty string in a UUID column.
func nullableUUID(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
