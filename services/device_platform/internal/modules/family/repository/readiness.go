package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

// Readiness validates the one-owner-per-family invariant with aggregate reads.
// Emails, account identifiers, and invitation addresses are never selected.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("family database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		WITH families AS (
			SELECT DISTINCT family_id FROM family_members
		),
		counts AS (
			SELECT
				family_id,
				COUNT(*) AS member_count,
				COUNT(*) FILTER (WHERE role = 'owner') AS owner_count
			FROM family_members
			GROUP BY family_id
		)
		SELECT
			(SELECT COUNT(*) FROM families),
			COALESCE(SUM(counts.member_count), 0),
			COALESCE(SUM(counts.owner_count), 0),
			COUNT(*) FILTER (WHERE counts.owner_count = 0),
			COUNT(*) FILTER (WHERE counts.owner_count > 1)
		FROM counts
	`).Scan(
		&readiness.FamilyCount,
		&readiness.MemberCount,
		&readiness.OwnerCount,
		&readiness.OwnerlessFamilyCount,
		&readiness.MultiOwnerFamilyCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read family membership readiness: %w", err)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM family_invitations
		WHERE status = 'pending'
		  AND expires_at > NOW()
	`).Scan(&readiness.PendingInvitationCount); err != nil {
		return domain.Readiness{}, fmt.Errorf("read family invitation readiness: %w", err)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM parent_accounts
		WHERE role = 'parent'
		  AND status = 'active'
	`).Scan(&readiness.ParentAccountCount); err != nil {
		return domain.Readiness{}, fmt.Errorf("read family parent account readiness: %w", err)
	}
	return readiness, nil
}
