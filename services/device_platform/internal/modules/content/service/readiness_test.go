package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/repository"
)

type readinessRepository struct {
	repository.Repository
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
			Repository: newMemoryRepository(),
			readiness:  readiness,
			err:        err,
		},
		AssetReader:  validAssetReader(),
		PolicyReader: policyReaderStub{},
	})
	if newErr != nil {
		t.Fatalf("create content service: %v", newErr)
	}
	return service
}

func TestReadinessHealthyWithEmptyCatalogAndDependencies(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{
		CatalogRevision:       7,
		PublishedPackageCount: 0,
		ReviewedPackageCount:  0,
	}, nil)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy ||
		readiness.CatalogRevision != 7 ||
		!readiness.AssetReaderReady ||
		!readiness.PolicyReaderReady {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestReadinessDegradesWithoutAssetOrPolicyReader(t *testing.T) {
	for _, options := range []Options{
		{
			Repository: readinessRepository{
				Repository: newMemoryRepository(),
				readiness:  domain.Readiness{},
			},
			PolicyReader: policyReaderStub{},
		},
		{
			Repository: readinessRepository{
				Repository: newMemoryRepository(),
				readiness:  domain.Readiness{},
			},
			AssetReader: validAssetReader(),
		},
	} {
		service, err := New(options)
		if err != nil {
			t.Fatalf("create content service: %v", err)
		}
		readiness, err := service.Readiness(context.Background())
		if err != nil {
			t.Fatalf("readiness: %v", err)
		}
		if readiness.State != domain.ReadinessDegraded {
			t.Fatalf("state = %q, want degraded", readiness.State)
		}
	}
}

func TestReadinessMapsAggregateReadFailure(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{}, errors.New("database unavailable"))
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected aggregate read failure to surface")
	}
}

func TestReadinessRejectsRepositoryWithoutReadinessReader(t *testing.T) {
	service, err := New(Options{
		Repository:   newMemoryRepository(),
		AssetReader:  validAssetReader(),
		PolicyReader: policyReaderStub{},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	if _, err := service.Readiness(context.Background()); !errors.Is(err, domain.ErrReadinessUnavailable) {
		t.Fatalf("err = %v, want ErrReadinessUnavailable", err)
	}
}
