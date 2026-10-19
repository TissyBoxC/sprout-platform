package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
)

// ReadinessReader provides aggregate child profile counters.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that profile storage is readable and the safety policy
// provisioner is wired. An empty profile set is healthy because a newly
// deployed platform may not have completed its first guardian registration.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read child profile readiness: %w", err)
	}
	readiness.PolicyProvisionerReady = s.policyProvisioner != nil
	// A profile row without its required policy is a broken provisioning
	// outcome: the child can be listed but cannot be used safely. The
	// repository reports the invariant counter and the service applies it.
	if readiness.ProfileCount < readiness.FamiliesWithProfile ||
		readiness.OrphanedProfileCount > 0 ||
		readiness.ProfileCount != readiness.PolicyCount {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	if !readiness.PolicyProvisionerReady {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}
