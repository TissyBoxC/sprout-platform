// Package repository persists the platform-wide content offline policy.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for the offline policy.
type Repository interface {
	// Current returns the published policy, or the package default when no
	// operator has ever published one.
	Current(ctx context.Context) (domain.Policy, error)
	// Save validates nothing itself (the service does) and bumps the revision
	// inside one transaction so a device never reads a half-written policy.
	Save(ctx context.Context, input domain.Input) (domain.Policy, error)
}

// PostgresRepository is the PostgreSQL-backed offline policy repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates an offline policy repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Current returns the singleton policy row or the default when it is absent.
func (r *PostgresRepository) Current(ctx context.Context) (domain.Policy, error) {
	policy := domain.Default()
	var revision int
	err := r.pool.QueryRow(ctx, `
		SELECT offline_enabled,
			max_cached_packages,
			max_cache_bytes,
			cache_ttl_hours,
			download_over_wifi_only,
			revision
		FROM content_offline_policy
		WHERE singleton = TRUE
	`).Scan(
		&policy.OfflineEnabled,
		&policy.MaxCachedPackages,
		&policy.MaxCacheBytes,
		&policy.CacheTTLHours,
		&policy.DownloadOverWifiOnly,
		&revision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(), nil
	}
	if err != nil {
		return domain.Policy{}, fmt.Errorf("read content offline policy: %w", err)
	}
	policy.Revision = revision
	return policy, nil
}

// Save upserts the singleton policy and bumps its revision.
func (r *PostgresRepository) Save(
	ctx context.Context,
	input domain.Input,
) (domain.Policy, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("begin offline policy save: %w", err)
	}
	defer tx.Rollback(ctx)

	var revision int
	err = tx.QueryRow(ctx, `
		INSERT INTO content_offline_policy (
			singleton,
			offline_enabled,
			max_cached_packages,
			max_cache_bytes,
			cache_ttl_hours,
			download_over_wifi_only,
			revision,
			updated_at
		) VALUES (TRUE, $1, $2, $3, $4, $5, 1, NOW())
		ON CONFLICT (singleton)
		DO UPDATE SET
			offline_enabled = EXCLUDED.offline_enabled,
			max_cached_packages = EXCLUDED.max_cached_packages,
			max_cache_bytes = EXCLUDED.max_cache_bytes,
			cache_ttl_hours = EXCLUDED.cache_ttl_hours,
			download_over_wifi_only = EXCLUDED.download_over_wifi_only,
			revision = content_offline_policy.revision + 1,
			updated_at = NOW()
		RETURNING revision
	`,
		input.OfflineEnabled,
		input.MaxCachedPackages,
		input.MaxCacheBytes,
		input.CacheTTLHours,
		input.DownloadOverWifiOnly,
	).Scan(&revision)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("save content offline policy: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Policy{}, fmt.Errorf("commit content offline policy: %w", err)
	}
	return domain.Policy{
		OfflineEnabled:       input.OfflineEnabled,
		MaxCachedPackages:    input.MaxCachedPackages,
		MaxCacheBytes:        input.MaxCacheBytes,
		CacheTTLHours:        input.CacheTTLHours,
		DownloadOverWifiOnly: input.DownloadOverWifiOnly,
		Revision:             revision,
	}, nil
}
