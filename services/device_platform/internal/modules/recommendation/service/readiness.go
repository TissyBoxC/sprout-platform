package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/domain"
)

// Readiness verifies that the published catalog is readable without requiring
// a family scope. An empty catalog is healthy; a missing catalog reader or an
// unavailable signal reader degrades recommendations.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.catalog == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	candidates, err := s.catalog.ListCandidates(ctx, "", "")
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read recommendation catalog readiness: %w", err)
	}
	readiness := domain.Readiness{
		CandidateCount: len(candidates),
	}
	if s.signals == nil {
		readiness.SignalsReady = false
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.SignalsReady = true
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}
