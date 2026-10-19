package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
)

// ReadinessReader provides aggregate policy and revision state.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that policy storage and device revision state are both
// readable. An empty policy set is healthy because no child needs a policy yet.
// A family with policy rows but no revision row degrades change propagation.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read parent policy readiness: %w", err)
	}
	readiness.VoiceDefaultsReady = s.voiceDefaults != nil
	if readiness.RevisionRowCount < readiness.FamilyCount ||
		!readiness.VoiceDefaultsReady {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}
