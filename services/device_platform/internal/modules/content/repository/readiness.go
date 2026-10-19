package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
)

// Readiness returns aggregate content lifecycle counters without loading
// package titles, asset keys, review reasons, actors or child-facing data.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("content database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(
				(SELECT revision FROM content_catalog_revision WHERE singleton),
				0
			),
			COUNT(DISTINCT package_id) FILTER (WHERE status = 'published'),
			COUNT(DISTINCT package_id) FILTER (
				WHERE submitted_at IS NOT NULL
				   OR approved_at IS NOT NULL
				   OR status IN ('in_review', 'published', 'withdrawn', 'archived')
			)
		FROM content_package_versions
	`).Scan(
		&readiness.CatalogRevision,
		&readiness.PublishedPackageCount,
		&readiness.ReviewedPackageCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read content library readiness: %w", err)
	}
	return readiness, nil
}
