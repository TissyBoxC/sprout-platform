package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
)

// Readiness returns aggregate publication counters without loading entry
// values, signatures, actors or any user-visible text.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("ui text database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(
				(SELECT revision FROM ui_text_catalog_clock WHERE singleton),
				0
			),
			COUNT(*) FILTER (WHERE status = 'published'),
			COUNT(*) FILTER (WHERE status = 'draft'),
			COUNT(*) FILTER (WHERE status = 'in_review')
		FROM ui_text_package_versions
	`).Scan(
		&readiness.CatalogRevision,
		&readiness.PublishedPackageCount,
		&readiness.DraftPackageCount,
		&readiness.InReviewPackageCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read ui text readiness: %w", err)
	}
	return readiness, nil
}
