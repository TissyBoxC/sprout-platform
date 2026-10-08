package app

import (
	"context"
	"errors"

	featureCenterDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	featureCenterService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// nonzeroRowProbe treats an unprotected table read as the health signal for a
// feature. It returns degraded rather than unavailable when the table is empty,
// because "no rows yet" is a valid early-lifecycle state and never a failure.
// A missing table or a failed connection reports unavailable so the console
// never shows a healthy capability backed by a broken schema.
func nonzeroRowProbe(
	pool *pgxpool.Pool,
	query string,
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if pool == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			var marker int
			err := pool.QueryRow(ctx, query).Scan(&marker)
			if errors.Is(err, pgx.ErrNoRows) {
				// The table exists but has no rows yet: the capability is
				// deployed and reachable, just not populated.
				return featureCenterDomain.HealthDegraded, nil
			}
			if err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			return featureCenterDomain.HealthHealthy, nil
		},
	)
}
