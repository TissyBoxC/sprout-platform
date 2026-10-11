// Package repository persists bounded vision task metadata and results.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultTaskLimit   = 20
	maxTaskLimit       = 100
	defaultResultLimit = 20
	maxResultLimit     = 100
)

// Repository defines the vision persistence and query contract.
type Repository interface {
	CreateTask(ctx context.Context, task *domain.Task) error
	GetTask(ctx context.Context, taskID string) (*domain.Task, error)
	UpdateTaskStatus(
		ctx context.Context,
		taskID string,
		status domain.TaskStatus,
		failureCode string,
		timestamp time.Time,
	) error
	ListTasksByDevice(
		ctx context.Context,
		deviceID string,
		limit int,
	) ([]domain.Task, error)
	ExpireStaleTasks(ctx context.Context, now time.Time) (int64, error)
	SaveResult(ctx context.Context, result *domain.Result) error
	GetResult(ctx context.Context, taskID string) (*domain.Result, error)
	ListResultsByDevice(
		ctx context.Context,
		deviceID string,
		limit int,
	) ([]domain.Result, error)
	DeleteExpiredResults(ctx context.Context, now time.Time) (int64, error)
}

// PostgresRepository stores vision metadata in the platform database.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a PostgreSQL-backed vision repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func normalizeLimit(limit, fallback, maximum int) int {
	if limit <= 0 {
		return fallback
	}
	if limit > maximum {
		return maximum
	}
	return limit
}

// CreateTask inserts one bounded vision task.
func (r *PostgresRepository) CreateTask(
	ctx context.Context,
	task *domain.Task,
) error {
	if task == nil {
		return errors.New("vision task is required")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO vision_tasks (
			id, device_id, task_type, status, requested_by, prompt,
			frame_width, frame_height, frame_bytes, failure_code,
			created_at, dispatched_at, completed_at, expires_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`,
		task.ID,
		task.DeviceID,
		task.TaskType,
		task.Status,
		task.RequestedBy,
		task.Prompt,
		task.FrameWidth,
		task.FrameHeight,
		task.FrameBytes,
		task.FailureCode,
		task.CreatedAt,
		task.DispatchedAt,
		task.CompletedAt,
		task.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert vision task: %w", err)
	}
	return nil
}

// GetTask loads one task by id.
func (r *PostgresRepository) GetTask(
	ctx context.Context,
	taskID string,
) (*domain.Task, error) {
	task := &domain.Task{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, device_id, task_type, status, requested_by, prompt,
		       frame_width, frame_height, frame_bytes, failure_code,
		       created_at, dispatched_at, completed_at, expires_at
		FROM vision_tasks
		WHERE id = $1
	`, taskID).Scan(
		&task.ID,
		&task.DeviceID,
		&task.TaskType,
		&task.Status,
		&task.RequestedBy,
		&task.Prompt,
		&task.FrameWidth,
		&task.FrameHeight,
		&task.FrameBytes,
		&task.FailureCode,
		&task.CreatedAt,
		&task.DispatchedAt,
		&task.CompletedAt,
		&task.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load vision task: %w", err)
	}
	return task, nil
}

// UpdateTaskStatus applies one lifecycle transition guarded by the expected
// pending/dispatched state so a stale device cannot overwrite a terminal task.
func (r *PostgresRepository) UpdateTaskStatus(
	ctx context.Context,
	taskID string,
	status domain.TaskStatus,
	failureCode string,
	timestamp time.Time,
) error {
	var command string
	switch status {
	case domain.TaskStatusDispatched:
		command = `
			UPDATE vision_tasks
			SET status = $2, dispatched_at = $4
			WHERE id = $1 AND status = 'pending'
		`
	case domain.TaskStatusCompleted:
		command = `
			UPDATE vision_tasks
			SET status = $2, completed_at = $4
			WHERE id = $1 AND status IN ('pending', 'dispatched')
		`
	case domain.TaskStatusFailed, domain.TaskStatusCancelled, domain.TaskStatusExpired:
		command = `
			UPDATE vision_tasks
			SET status = $2, failure_code = $3, completed_at = $4
			WHERE id = $1 AND status IN ('pending', 'dispatched')
		`
	default:
		return domain.ErrInvalidTask
	}
	tag, err := r.pool.Exec(ctx, command, taskID, status, failureCode, timestamp)
	if err != nil {
		return fmt.Errorf("update vision task status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrTaskNotPending
	}
	return nil
}

// ListTasksByDevice returns recent tasks for one device, newest first.
func (r *PostgresRepository) ListTasksByDevice(
	ctx context.Context,
	deviceID string,
	limit int,
) ([]domain.Task, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, device_id, task_type, status, requested_by, prompt,
		       frame_width, frame_height, frame_bytes, failure_code,
		       created_at, dispatched_at, completed_at, expires_at
		FROM vision_tasks
		WHERE device_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, deviceID, normalizeLimit(limit, defaultTaskLimit, maxTaskLimit))
	if err != nil {
		return nil, fmt.Errorf("list vision tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]domain.Task, 0)
	for rows.Next() {
		task := domain.Task{}
		if err := rows.Scan(
			&task.ID,
			&task.DeviceID,
			&task.TaskType,
			&task.Status,
			&task.RequestedBy,
			&task.Prompt,
			&task.FrameWidth,
			&task.FrameHeight,
			&task.FrameBytes,
			&task.FailureCode,
			&task.CreatedAt,
			&task.DispatchedAt,
			&task.CompletedAt,
			&task.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan vision task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vision tasks: %w", err)
	}
	return tasks, nil
}

// ExpireStaleTasks marks pending/dispatched tasks whose TTL has elapsed.
func (r *PostgresRepository) ExpireStaleTasks(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE vision_tasks
		SET status = 'expired', failure_code = 'timeout', completed_at = $1
		WHERE status IN ('pending', 'dispatched') AND expires_at <= $1
	`, now)
	if err != nil {
		return 0, fmt.Errorf("expire vision tasks: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SaveResult upserts one bounded recognition result.
func (r *PostgresRepository) SaveResult(
	ctx context.Context,
	result *domain.Result,
) error {
	if result == nil {
		return errors.New("vision result is required")
	}
	labels, err := json.Marshal(result.Labels)
	if err != nil {
		return fmt.Errorf("encode vision labels: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO vision_results (
			task_id, device_id, task_type, labels, summary,
			created_at, expires_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (task_id) DO UPDATE
		SET labels = EXCLUDED.labels,
		    summary = EXCLUDED.summary,
		    created_at = EXCLUDED.created_at,
		    expires_at = EXCLUDED.expires_at
	`,
		result.TaskID,
		result.DeviceID,
		result.TaskType,
		labels,
		result.Summary,
		result.CreatedAt,
		result.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("save vision result: %w", err)
	}
	return nil
}

// GetResult loads one result by task id.
func (r *PostgresRepository) GetResult(
	ctx context.Context,
	taskID string,
) (*domain.Result, error) {
	result := &domain.Result{}
	var labels []byte
	err := r.pool.QueryRow(ctx, `
		SELECT task_id, device_id, task_type, labels, summary,
		       created_at, expires_at
		FROM vision_results
		WHERE task_id = $1
	`, taskID).Scan(
		&result.TaskID,
		&result.DeviceID,
		&result.TaskType,
		&labels,
		&result.Summary,
		&result.CreatedAt,
		&result.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrResultNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load vision result: %w", err)
	}
	if err := json.Unmarshal(labels, &result.Labels); err != nil {
		return nil, fmt.Errorf("decode vision labels: %w", err)
	}
	if result.Labels == nil {
		result.Labels = make([]domain.ResultLabel, 0)
	}
	return result, nil
}

// ListResultsByDevice returns recent results for one device, newest first.
func (r *PostgresRepository) ListResultsByDevice(
	ctx context.Context,
	deviceID string,
	limit int,
) ([]domain.Result, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT task_id, device_id, task_type, labels, summary,
		       created_at, expires_at
		FROM vision_results
		WHERE device_id = $1 AND expires_at > NOW()
		ORDER BY created_at DESC, task_id DESC
		LIMIT $2
	`, deviceID, normalizeLimit(limit, defaultResultLimit, maxResultLimit))
	if err != nil {
		return nil, fmt.Errorf("list vision results: %w", err)
	}
	defer rows.Close()

	results := make([]domain.Result, 0)
	for rows.Next() {
		result := domain.Result{}
		var labels []byte
		if err := rows.Scan(
			&result.TaskID,
			&result.DeviceID,
			&result.TaskType,
			&labels,
			&result.Summary,
			&result.CreatedAt,
			&result.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan vision result: %w", err)
		}
		if err := json.Unmarshal(labels, &result.Labels); err != nil {
			return nil, fmt.Errorf("decode vision labels: %w", err)
		}
		if result.Labels == nil {
			result.Labels = make([]domain.ResultLabel, 0)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vision results: %w", err)
	}
	return results, nil
}

// DeleteExpiredResults prunes results past their short retention window.
func (r *PostgresRepository) DeleteExpiredResults(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM vision_results WHERE expires_at <= $1
	`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired vision results: %w", err)
	}
	return tag.RowsAffected(), nil
}

var _ Repository = (*PostgresRepository)(nil)
