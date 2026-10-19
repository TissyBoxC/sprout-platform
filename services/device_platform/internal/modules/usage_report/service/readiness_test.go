package service

import (
	"context"
	"errors"
	"testing"

	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
)

type readinessRepository struct {
	*memoryUsageRepository
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
	policyFixed bool,
) *Service {
	t.Helper()
	options := Options{
		Repository: readinessRepository{
			memoryUsageRepository: newMemoryUsageRepository(),
			readiness:             readiness,
			err:                   readErr,
		},
		PolicyService: fixedPolicyReader{
			effective: &policydomain.EffectivePolicy{},
		},
		Clock: usageFixedClock{},
	}
	if !policyFixed {
		options.PolicyService = nil
	}
	service, err := New(options)
	if err != nil {
		t.Fatalf("create usage report service: %v", err)
	}
	return service
}

func TestReadinessHealthyWithEmptyLedger(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{}, nil, true)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy ||
		readiness.ReportedDayCount != 0 ||
		!readiness.PolicyReaderReady {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestReadinessMapsAggregateReadFailure(t *testing.T) {
	service := newReadinessService(
		t,
		domain.Readiness{},
		errors.New("database unavailable"),
		true,
	)
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected aggregate read failure to surface")
	}
}

func TestReadinessRejectsRepositoryWithoutReadinessReader(t *testing.T) {
	service, err := New(Options{
		Repository:    newMemoryUsageRepository(),
		PolicyService: fixedPolicyReader{},
		Clock:         usageFixedClock{},
	})
	if err != nil {
		t.Fatalf("create usage report service: %v", err)
	}
	if _, err := service.Readiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("err = %v, want ErrReadinessUnavailable", err)
	}
}
