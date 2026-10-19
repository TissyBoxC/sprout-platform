package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
)

// ReadinessReader provides aggregate English-learning counters for capability
// health. It is narrower than Repository so tests use a small fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that the published lesson catalogue covers every approved
// age tier and that practice history is queryable. No attempts yet is healthy;
// an empty catalogue or missing age tier degrades learning.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read english learning readiness: %w", err)
	}
	if readiness.CatalogByAgeTier == nil {
		readiness.CatalogByAgeTier = make(map[string]bool, len(domain.AgeTiers))
	}
	complete := readiness.PublishedLessonCount > 0
	for _, ageTier := range domain.AgeTiers {
		if !readiness.CatalogByAgeTier[ageTier] {
			complete = false
		}
	}
	if !complete {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}
