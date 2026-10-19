package service

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
)

// CheckReadiness is the Feature Center entry point for English learning health.
func (s *Service) CheckReadiness(ctx context.Context) (domain.Readiness, error) {
	return s.Readiness(ctx)
}
