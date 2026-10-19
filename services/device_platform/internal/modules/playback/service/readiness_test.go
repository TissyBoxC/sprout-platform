package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/domain"
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
	withReaders bool,
) *Service {
	t.Helper()
	options := Options{
		Repository: readinessRepository{
			memoryRepository: &memoryRepository{},
			readiness:        readiness,
			err:              readErr,
		},
	}
	if withReaders {
		options.DeviceReader = stubDeviceReader{deviceIDs: []string{"device-1"}}
		options.PackageReader = stubPackageReader{
			published: map[string]bool{"lesson_story": true},
		}
	}
	service, err := New(options)
	if err != nil {
		t.Fatalf("create playback service: %v", err)
	}
	return service
}

func TestReadinessHealthyWithNoRequests(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{}, nil, true)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy ||
		!readiness.DeviceReaderReady ||
		!readiness.PackageReaderReady {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestReadinessDegradesWithoutValidationReaders(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{}, nil, false)
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
		true,
	)
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected aggregate read failure to surface")
	}
}

func TestReadinessRejectsRepositoryWithoutReadinessReader(t *testing.T) {
	service, err := New(Options{
		Repository:    &memoryRepository{},
		DeviceReader:  stubDeviceReader{deviceIDs: []string{"device-1"}},
		PackageReader: stubPackageReader{},
	})
	if err != nil {
		t.Fatalf("create playback service: %v", err)
	}
	if _, err := service.Readiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("err = %v, want ErrReadinessUnavailable", err)
	}
}
