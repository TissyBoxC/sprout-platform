package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/domain"
	"github.com/jackc/pgx/v5"
)

// Readiness returns the effective policy and an aggregate acknowledgement
// count without loading family or device identifiers.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("offline content database is unavailable")
	}
	readiness := domain.Readiness{}
	err := r.pool.QueryRow(ctx, `
		SELECT offline_enabled, revision
		FROM content_offline_policy
		WHERE singleton = TRUE
	`).Scan(&readiness.OfflineEnabled, &readiness.PolicyRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		defaults := domain.Default()
		readiness.OfflineEnabled = defaults.OfflineEnabled
		readiness.PolicyRevision = defaults.Revision
	} else if err != nil {
		return domain.Readiness{}, fmt.Errorf("read offline content policy readiness: %w", err)
	}
	if readiness.PolicyRevision < 0 {
		readiness.PolicyRevision = 0
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT device_id)
		FROM content_offline_policy_acks
	`).Scan(&readiness.AcknowledgedDeviceCount); err != nil {
		return domain.Readiness{}, fmt.Errorf("read offline content ack readiness: %w", err)
	}
	return readiness, nil
}
