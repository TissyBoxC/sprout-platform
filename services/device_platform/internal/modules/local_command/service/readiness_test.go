package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/domain"
)

type readinessRepository struct {
	*stubRepository
	readiness domain.Readiness
	err       error
}

func (r readinessRepository) Readiness(context.Context) (domain.Readiness, error) {
	return r.readiness, r.err
}

func newReadinessService(t *testing.T, readiness domain.Readiness, err error) *Service {
	t.Helper()
	return New(Options{
		Repository: readinessRepository{
			stubRepository: &stubRepository{},
			readiness:      readiness,
			err:            err,
		},
	})
}

func TestReadinessHealthyWithNoCommands(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{}, nil)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
}

func TestReadinessDegradesForCommandsWithoutRevisionClock(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{
		CommandCount:       1,
		FamilyCount:        0,
		CommandFamilyCount: 1,
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
	service := New(Options{Repository: &stubRepository{}})
	if _, err := service.Readiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("err = %v, want ErrReadinessUnavailable", err)
	}
}
