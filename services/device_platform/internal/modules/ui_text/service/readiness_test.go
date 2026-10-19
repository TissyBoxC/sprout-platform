package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
)

type readinessRepository struct {
	*fakeRepository
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
	verifier SignatureVerifier,
) *Service {
	t.Helper()
	service, err := New(Options{
		Repository: readinessRepository{
			fakeRepository: newFakeRepository(),
			readiness:      readiness,
			err:            readErr,
		},
		Verifier: verifier,
	})
	if err != nil {
		t.Fatalf("create ui text service: %v", err)
	}
	return service
}

func TestReadinessHealthyWithEmptyCatalogAndVerifier(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{
		CatalogRevision: 3,
	}, nil, &recordingVerifier{})
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy ||
		readiness.CatalogRevision != 3 ||
		!readiness.SignatureVerifierReady {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestReadinessDegradesWithoutSignatureVerifier(t *testing.T) {
	service := newReadinessService(t, domain.Readiness{}, nil, nil)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded ||
		readiness.SignatureVerifierReady {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestReadinessMapsAggregateReadFailure(t *testing.T) {
	service := newReadinessService(
		t,
		domain.Readiness{},
		errors.New("database unavailable"),
		&recordingVerifier{},
	)
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected aggregate read failure to surface")
	}
}

func TestReadinessRejectsRepositoryWithoutReadinessReader(t *testing.T) {
	service, err := New(Options{
		Repository: newFakeRepository(),
		Verifier:   &recordingVerifier{},
	})
	if err != nil {
		t.Fatalf("create ui text service: %v", err)
	}
	if _, err := service.Readiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("err = %v, want ErrReadinessUnavailable", err)
	}
}
