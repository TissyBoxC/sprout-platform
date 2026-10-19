package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/repository"
)

type engagementReadinessRepository struct {
	repository.Repository
	engagement    domain.EngagementReadiness
	themes        domain.ThemePackReadiness
	engagementErr error
	themeErr      error
}

func (r engagementReadinessRepository) EngagementReadiness(
	context.Context,
) (domain.EngagementReadiness, error) {
	return r.engagement, r.engagementErr
}

func (r engagementReadinessRepository) ThemePackReadiness(
	context.Context,
) (domain.ThemePackReadiness, error) {
	return r.themes, r.themeErr
}

func newEngagementReadinessService(
	t *testing.T,
	repository engagementReadinessRepository,
) *Service {
	t.Helper()
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	return service
}

func TestEngagementReadinessHealthyWithNoHistory(t *testing.T) {
	service := newEngagementReadinessService(t, engagementReadinessRepository{
		Repository: newMemoryRepository(),
	})
	readiness, err := service.EngagementReadiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
}

func TestEngagementReadinessMapsReadFailure(t *testing.T) {
	service := newEngagementReadinessService(t, engagementReadinessRepository{
		Repository:    newMemoryRepository(),
		engagementErr: errors.New("database unavailable"),
	})
	if _, err := service.EngagementReadiness(context.Background()); err == nil {
		t.Fatal("expected read failure to surface")
	}
}

func TestThemePackReadinessHealthyWithNoThemes(t *testing.T) {
	service := newEngagementReadinessService(t, engagementReadinessRepository{
		Repository: newMemoryRepository(),
	})
	readiness, err := service.ThemePackReadiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
}

func TestThemePackReadinessDegradesForUnclassifiedPack(t *testing.T) {
	service := newEngagementReadinessService(t, engagementReadinessRepository{
		Repository: newMemoryRepository(),
		themes: domain.ThemePackReadiness{
			ThemePackCount: 1,
			DraftCount:     0,
		},
	})
	readiness, err := service.ThemePackReadiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
}

func TestEngagementAndThemeReadinessRejectLegacyRepository(t *testing.T) {
	service, err := New(Options{Repository: newMemoryRepository()})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	if _, err := service.EngagementReadiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("engagement err = %v, want ErrReadinessUnavailable", err)
	}
	if _, err := service.ThemePackReadiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("theme err = %v, want ErrReadinessUnavailable", err)
	}
}
