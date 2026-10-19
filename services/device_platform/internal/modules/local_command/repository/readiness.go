package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/domain"
)

// Readiness returns aggregate command counters without loading family ids,
// trigger phrases, action arguments or device-scoped payloads.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("local command database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM local_commands),
			(SELECT COUNT(*) FROM local_commands WHERE enabled = TRUE),
			(SELECT COUNT(*) FROM local_command_revisions),
			(SELECT COUNT(DISTINCT family_id) FROM local_commands)
	`).Scan(
		&readiness.CommandCount,
		&readiness.EnabledCommandCount,
		&readiness.FamilyCount,
		&readiness.CommandFamilyCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read local command readiness: %w", err)
	}
	return readiness, nil
}
