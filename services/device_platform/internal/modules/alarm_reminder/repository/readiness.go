package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/domain"
)

// Readiness returns aggregate alarm and firing counters without loading
// labels, prompts, child identifiers or device identifiers.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("alarm reminder database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM alarm_reminders),
			(SELECT COUNT(*) FROM alarm_reminders WHERE enabled = TRUE),
			(SELECT COUNT(*) FROM alarm_reminder_events)
	`).Scan(
		&readiness.AlarmCount,
		&readiness.EnabledCount,
		&readiness.EventCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read alarm reminder readiness: %w", err)
	}
	return readiness, nil
}
