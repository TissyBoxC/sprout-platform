package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
)

// Readiness returns aggregate usage-report counters without loading device
// names, category usage, blocked counts or any child-facing metrics.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("usage report database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(DISTINCT device_id),
			COUNT(DISTINCT parent_account_id)
		FROM device_usage_daily
	`).Scan(
		&readiness.ReportedDayCount,
		&readiness.ReportingDeviceCount,
		&readiness.ReportingFamilyCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read usage report readiness: %w", err)
	}
	return readiness, nil
}
