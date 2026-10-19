package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/domain"
)

// ReadinessReader provides the effective offline policy and aggregate device
// acknowledgement counters for capability health.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that the platform-wide offline policy is readable. A
// default policy with no acknowledgements is healthy: it is the state before
// the first device reports. Missing persistence is unavailable, not default.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read offline content readiness: %w", err)
	}
	if readiness.PolicyRevision < 0 {
		readiness.PolicyRevision = 0
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}
