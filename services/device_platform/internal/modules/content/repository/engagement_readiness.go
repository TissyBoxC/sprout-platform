package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
)

// EngagementReadiness returns aggregate favourite and playback-history counts
// without loading account or device identifiers, package titles, outcomes or
// per-family rows.
func (r *PostgresRepository) EngagementReadiness(ctx context.Context) (domain.EngagementReadiness, error) {
	if r == nil || r.pool == nil {
		return domain.EngagementReadiness{}, fmt.Errorf("content engagement database is unavailable")
	}
	var readiness domain.EngagementReadiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM content_favorites),
			(SELECT COUNT(*) FROM content_play_history)
	`).Scan(
		&readiness.FavoriteCount,
		&readiness.PlayHistoryCount,
	); err != nil {
		return domain.EngagementReadiness{}, fmt.Errorf("read content engagement readiness: %w", err)
	}
	return readiness, nil
}

// ThemePackReadiness returns aggregate theme-pack lifecycle counts without
// loading theme titles, summaries, member package ids or age tiers.
func (r *PostgresRepository) ThemePackReadiness(ctx context.Context) (domain.ThemePackReadiness, error) {
	if r == nil || r.pool == nil {
		return domain.ThemePackReadiness{}, fmt.Errorf("content theme database is unavailable")
	}
	var readiness domain.ThemePackReadiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'published'),
			COUNT(*) FILTER (WHERE status = 'draft'),
			COUNT(*) FILTER (WHERE status = 'withdrawn')
		FROM content_theme_packs
	`).Scan(
		&readiness.ThemePackCount,
		&readiness.PublishedCount,
		&readiness.DraftCount,
		&readiness.WithdrawnCount,
	); err != nil {
		return domain.ThemePackReadiness{}, fmt.Errorf("read content theme readiness: %w", err)
	}
	return readiness, nil
}
