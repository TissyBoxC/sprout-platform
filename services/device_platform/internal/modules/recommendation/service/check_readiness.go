package service

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/domain"
)

// CheckReadiness preserves the existing Feature Center probe contract while
// mapping the structured result back to an error. A degraded recommendation
// capability returns ErrReadinessUnavailable; the assembly owner can replace
// this adapter with Readiness when it projects degraded explicitly.
func (s *Service) CheckReadiness(ctx context.Context) error {
	readiness, err := s.Readiness(ctx)
	if err != nil {
		return err
	}
	if readiness.State != domain.ReadinessHealthy {
		return domain.ErrReadinessUnavailable
	}
	return nil
}
