package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
)

type readinessRepository struct {
	*memoryRepository
	readiness domain.Readiness
	err       error
}

func (r readinessRepository) Readiness(context.Context) (domain.Readiness, error) {
	return r.readiness, r.err
}

func newReadinessService(
	t *testing.T,
	readiness domain.Readiness,
	readErr error,
) *Service {
	t.Helper()
	service, err := New(Options{
		Repository: readinessRepository{
			memoryRepository: newMemoryRepository(),
			readiness:        readiness,
			err:              readErr,
		},
	})
	if err != nil {
		t.Fatalf("create english service: %v", err)
	}
	return service
}

func completeAgeCatalog() map[string]bool {
	return map[string]bool{
		domain.AgeTier3To4: true,
		domain.AgeTier5To6: true,
		domain.AgeTier7To8: true,
	}
}

func TestReadinessHealthyWithCompleteCatalogueAndNoAttempts(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{
		PublishedLessonCount: 3,
		PracticeAttemptCount: 0,
		CatalogByAgeTier:     completeAgeCatalog(),
	}, nil)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
}

func TestReadinessDegradesWhenAnAgeTierHasNoPublishedLesson(t *testing.T) {
	catalog := completeAgeCatalog()
	catalog[domain.AgeTier7To8] = false
	service := newReadinessService(t, domain.Readiness{
		PublishedLessonCount: 2,
		CatalogByAgeTier:     catalog,
	}, nil)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
}

func TestReadinessMapsAggregateReadFailure(t *testing.T) {
	service := newReadinessService(
		t,
		domain.Readiness{},
		errors.New("database unavailable"),
	)
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected aggregate read failure to surface")
	}
}

func TestReadinessRejectsRepositoryWithoutReadinessReader(t *testing.T) {
	service, err := New(Options{Repository: newMemoryRepository()})
	if err != nil {
		t.Fatalf("create english service: %v", err)
	}
	if _, err := service.Readiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("err = %v, want ErrReadinessUnavailable", err)
	}
}
