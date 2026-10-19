package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/domain"
)

// Readiness returns aggregate playback request counters without loading family
// or device identifiers, package titles or guardian notes.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("playback database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status IN ('pending', 'acknowledged')),
			COUNT(*) FILTER (WHERE status = 'failed')
		FROM playback_requests
	`).Scan(
		&readiness.RequestCount,
		&readiness.PendingCount,
		&readiness.FailedCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read playback readiness: %w", err)
	}
	return readiness, nil
}
