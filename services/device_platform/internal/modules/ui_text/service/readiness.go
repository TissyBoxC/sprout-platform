package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
)

// ReadinessReader provides aggregate UI text publication counters for
// capability health. It is narrower than Repository so tests use a fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that UI text persistence and publication signing are
// usable. An empty catalog is healthy for a first deployment, but missing
// signature verification degrades publishing while delivery can remain up.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read ui text readiness: %w", err)
	}
	if readiness.CatalogRevision < 0 {
		readiness.CatalogRevision = 0
	}
	readiness.SignatureVerifierReady = s.verifier != nil
	if !readiness.SignatureVerifierReady {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}
