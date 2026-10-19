package service

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
)

// CheckReadiness is the Feature Center entry point for content-library health.
func (s *Service) CheckReadiness(ctx context.Context) (domain.Readiness, error) {
	return s.Readiness(ctx)
}

// CheckEngagementReadiness is the Feature Center entry point for the
// favourite-and-history feature, which shares the content repository.
func (s *Service) CheckEngagementReadiness(
	ctx context.Context,
) (domain.EngagementReadiness, error) {
	return s.EngagementReadiness(ctx)
}

// CheckThemePackReadiness is the Feature Center entry point for the theme-pack
// feature, which shares the content repository.
func (s *Service) CheckThemePackReadiness(
	ctx context.Context,
) (domain.ThemePackReadiness, error) {
	return s.ThemePackReadiness(ctx)
}
