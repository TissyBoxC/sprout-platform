// Package repository persists device usage report days.
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines usage report persistence operations.
type Repository interface {
	UpsertDaily(ctx context.Context, usage *domain.DailyUsage) error
	ListByFamily(
		ctx context.Context,
		parentAccountID string,
		fromDate time.Time,
		days int,
	) ([]domain.DailyUsage, error)
}

// PostgresRepository is the PostgreSQL-backed usage report repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a usage report repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// UpsertDaily replaces one device-local usage day idempotently.
func (r *PostgresRepository) UpsertDaily(
	ctx context.Context,
	usage *domain.DailyUsage,
) error {
	categories, err := json.Marshal(usage.Categories)
	if err != nil {
		return fmt.Errorf("encode usage categories: %w", err)
	}
	blocked, err := json.Marshal(usage.Blocked)
	if err != nil {
		return fmt.Errorf("encode usage blocked counts: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO device_usage_daily (
			report_date,
			parent_account_id,
			device_id,
			timezone_offset_minutes,
			active_seconds,
			conversation_count,
			conversation_seconds,
			content_play_count,
			content_seconds,
			category_usage,
			blocked_counts,
			created_at,
			updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11::jsonb, $12, $13
		)
		ON CONFLICT (device_id, report_date) DO UPDATE
		SET parent_account_id = EXCLUDED.parent_account_id,
		    timezone_offset_minutes = EXCLUDED.timezone_offset_minutes,
		    active_seconds = EXCLUDED.active_seconds,
		    conversation_count = EXCLUDED.conversation_count,
		    conversation_seconds = EXCLUDED.conversation_seconds,
		    content_play_count = EXCLUDED.content_play_count,
		    content_seconds = EXCLUDED.content_seconds,
		    category_usage = EXCLUDED.category_usage,
		    blocked_counts = EXCLUDED.blocked_counts,
		    updated_at = EXCLUDED.updated_at
	`,
		usage.ReportDate,
		usage.ParentAccountID,
		usage.DeviceID,
		usage.TimezoneOffsetMinutes,
		usage.ActiveSeconds,
		usage.ConversationCount,
		usage.ConversationSeconds,
		usage.ContentPlayCount,
		usage.ContentSeconds,
		categories,
		blocked,
		usage.CreatedAt,
		usage.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert usage report day: %w", err)
	}
	return nil
}

// ListByFamily returns recent device usage days for one guardian.
func (r *PostgresRepository) ListByFamily(
	ctx context.Context,
	parentAccountID string,
	fromDate time.Time,
	days int,
) ([]domain.DailyUsage, error) {
	rows, err := r.pool.Query(ctx, usageSelect+`
		WHERE usage.parent_account_id = $1
		  AND usage.report_date >= $2
		ORDER BY usage.report_date DESC, usage.device_id ASC
		LIMIT $3
	`, parentAccountID, fromDate, days*32)
	if err != nil {
		return nil, fmt.Errorf("list usage report days: %w", err)
	}
	defer rows.Close()
	return scanUsageRows(rows)
}

const usageSelect = `
	SELECT
		usage.report_date,
		usage.parent_account_id,
		usage.device_id,
		COALESCE(bindings.device_name, ''),
		usage.timezone_offset_minutes,
		usage.active_seconds,
		usage.conversation_count,
		usage.conversation_seconds,
		usage.content_play_count,
		usage.content_seconds,
		usage.category_usage,
		usage.blocked_counts,
		usage.created_at,
		usage.updated_at
	FROM device_usage_daily AS usage
	LEFT JOIN device_bindings AS bindings
	  ON bindings.device_id = usage.device_id
`

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanUsageRows(rows interface {
	Next() bool
	Scan(destinations ...any) error
	Err() error
}) ([]domain.DailyUsage, error) {
	usages := make([]domain.DailyUsage, 0)
	for rows.Next() {
		usage, err := scanUsage(rows)
		if err != nil {
			return nil, err
		}
		usages = append(usages, *usage)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage report days: %w", err)
	}
	return usages, nil
}

func scanUsage(row rowScanner) (*domain.DailyUsage, error) {
	var usage domain.DailyUsage
	var reportDate time.Time
	var categories []byte
	var blocked []byte
	if err := row.Scan(
		&reportDate,
		&usage.ParentAccountID,
		&usage.DeviceID,
		&usage.DeviceName,
		&usage.TimezoneOffsetMinutes,
		&usage.ActiveSeconds,
		&usage.ConversationCount,
		&usage.ConversationSeconds,
		&usage.ContentPlayCount,
		&usage.ContentSeconds,
		&categories,
		&blocked,
		&usage.CreatedAt,
		&usage.UpdatedAt,
	); err != nil {
		return nil, err
	}
	usage.ReportDate = reportDate.Format("2006-01-02")
	if err := json.Unmarshal(categories, &usage.Categories); err != nil {
		return nil, fmt.Errorf("decode usage categories: %w", err)
	}
	if err := json.Unmarshal(blocked, &usage.Blocked); err != nil {
		return nil, fmt.Errorf("decode usage blocked counts: %w", err)
	}
	if usage.Categories == nil {
		usage.Categories = []domain.CategoryUsage{}
	}
	return &usage, nil
}

var _ Repository = (*PostgresRepository)(nil)
