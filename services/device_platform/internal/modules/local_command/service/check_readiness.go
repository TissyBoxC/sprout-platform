package service

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/domain"
)

// CheckReadiness is the Feature Center entry point for local command health.
func (s *Service) CheckReadiness(ctx context.Context) (domain.Readiness, error) {
	return s.Readiness(ctx)
}
