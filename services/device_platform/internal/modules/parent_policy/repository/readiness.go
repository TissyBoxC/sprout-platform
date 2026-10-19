package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
)

// Readiness verifies that both policy content and the revision table used by
// devices are readable. A policy family without a revision row is a degraded
// synchronization state because a device cannot detect policy changes.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("parent policy database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(DISTINCT family_id)
		FROM parent_policies
	`).Scan(&readiness.PolicyCount, &readiness.FamilyCount); err != nil {
		return domain.Readiness{}, fmt.Errorf("read parent policy readiness: %w", err)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM parent_policy_revisions AS revisions
		WHERE EXISTS (
			SELECT 1
			FROM parent_policies AS policies
			WHERE policies.family_id = revisions.family_id
		)
	`).Scan(&readiness.RevisionRowCount); err != nil {
		return domain.Readiness{}, fmt.Errorf("read parent policy revision readiness: %w", err)
	}
	return readiness, nil
}
