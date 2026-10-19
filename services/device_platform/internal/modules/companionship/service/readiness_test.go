package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/domain"
)

type readinessRepository struct {
	*memoryRepository
	readiness domain.Readiness
	err       error
}

func (r readinessRepository) Readiness(context.Context) (domain.Readiness, error) {
	return r.readiness, r.err
}

func newReadinessService(t *testing.T, readiness domain.Readiness, err error) *Service {
	t.Helper()
	service, newErr := New(Options{
		Repository: readinessRepository{
			memoryRepository: newMemoryRepository(),
			readiness:        readiness,
			err:              err,
		},
	})
	if newErr != nil {
		t.Fatalf("create companionship service: %v", newErr)
	}
	return service
}

func TestReadinessHealthyWithEmptyGrowthAndReminderStores(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{}, nil)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
}

func TestReadinessDegradesForInconsistentEnabledReminderCount(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{
		HabitReminderCount:        1,
		EnabledHabitReminderCount: 2,
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
		t.Fatalf("create companionship service: %v", err)
	}
	if _, err := service.Readiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("err = %v, want ErrReadinessUnavailable", err)
	}
}
