package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/domain"
)

type readinessRepository struct {
	*fakeRepository
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
			fakeRepository: &fakeRepository{},
			readiness:      readiness,
			err:            err,
		},
	})
}

func TestReadinessHealthyWithDefaultPolicyAndNoDevices(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{
		OfflineEnabled: true,
	}, nil)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy || !readiness.OfflineEnabled {
		t.Fatalf("unexpected readiness: %+v", readiness)
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
	service := New(Options{Repository: &fakeRepository{}})
	if _, err := service.Readiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("err = %v, want ErrReadinessUnavailable", err)
	}
}
