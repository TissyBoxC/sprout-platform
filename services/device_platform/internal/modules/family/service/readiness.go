package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

// ReadinessReader provides aggregate family membership and invitation state.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that membership storage is readable and no family has
// lost its unique owner. No families and no pending invitations are healthy
// first-deployment states; an ownerless or multiply-owned family is degraded.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read family readiness: %w", err)
	}
	if readiness.OwnerlessFamilyCount > 0 ||
		readiness.MultiOwnerFamilyCount > 0 ||
		readiness.ParentAccountCount != readiness.FamilyCount {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}
