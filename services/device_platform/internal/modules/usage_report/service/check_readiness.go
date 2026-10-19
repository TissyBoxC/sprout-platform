package service

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
)

// CheckReadiness is the Feature Center entry point for usage-report health.
func (s *Service) CheckReadiness(ctx context.Context) (domain.Readiness, error) {
	return s.Readiness(ctx)
}
