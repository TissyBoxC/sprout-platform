package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/domain"
)

// Readiness returns aggregate companionship counters without loading family or
// child identifiers, growth titles, notes, reminder labels or prompts.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("companionship database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM companionship_growth_records),
			(SELECT COUNT(*) FROM companionship_habit_reminders),
			(
				SELECT COUNT(*)
				FROM companionship_habit_reminders
				WHERE enabled = TRUE
			)
	`).Scan(
		&readiness.GrowthRecordCount,
		&readiness.HabitReminderCount,
		&readiness.EnabledHabitReminderCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read companionship readiness: %w", err)
	}
	return readiness, nil
}
