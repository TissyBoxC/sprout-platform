package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
)

// Readiness returns non-sensitive account and session counters from the
// authoritative persistence. The privacy service owns the MFA challenge state,
// so this reader only reports TOTP enrollment and account classes.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("authentication database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE role = 'parent' AND status = 'active'),
			COUNT(*) FILTER (WHERE role = 'admin' AND status = 'active'),
			COUNT(*) FILTER (
				WHERE role = 'admin'
				  AND status = 'active'
				  AND EXISTS (
					SELECT 1
					FROM admin_totp_credentials AS credentials
					WHERE credentials.parent_account_id = parent_accounts.id
					  AND credentials.enabled_at IS NOT NULL
				  )
			)
		FROM parent_accounts
	`).Scan(
		&readiness.ActiveParentCount,
		&readiness.ActiveAdminCount,
		&readiness.MFAEnabledAdminCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read authentication account readiness: %w", err)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM auth_sessions
		WHERE revoked_at IS NULL
		  AND expires_at > NOW()
	`).Scan(&readiness.ActiveSessionCount); err != nil {
		return domain.Readiness{}, fmt.Errorf("read authentication session readiness: %w", err)
	}
	return readiness, nil
}
