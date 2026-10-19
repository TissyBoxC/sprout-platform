package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
)

// Readiness returns aggregate profile counts without loading child names,
// birthdays, interests, or any other personal field.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("child profile database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(profile.id),
			COUNT(DISTINCT profile.family_id),
			COUNT(*) FILTER (WHERE policy.id IS NULL),
			COUNT(policy.id)
		FROM child_profiles AS profile
		LEFT JOIN parent_policies AS policy
		  ON policy.child_id = profile.id
		 AND policy.family_id = profile.family_id
	`).Scan(
		&readiness.ProfileCount,
		&readiness.FamiliesWithProfile,
		&readiness.OrphanedProfileCount,
		&readiness.PolicyCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read child profile readiness: %w", err)
	}
	return readiness, nil
}
