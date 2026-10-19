package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
)

// ReadinessReader provides aggregate usage-report counters for capability
// health. It is narrower than Repository so tests use a small fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that the usage ledger and policy dependency are usable.
// No reports yet is healthy; missing policy resolution degrades remaining-time
// summaries while raw ingestion and totals remain available.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read usage report readiness: %w", err)
	}
	readiness.PolicyReaderReady = s.policyService != nil
	if !readiness.PolicyReaderReady {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}
