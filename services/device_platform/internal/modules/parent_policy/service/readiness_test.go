package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
)

func TestReadinessAcceptsEmptyPolicyStore(t *testing.T) {
	service, err := New(Options{
		Repository:    newMemoryPolicyRepository(),
		VoiceDefaults: fixedVoiceDefaults{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
}

func TestReadinessDegradesWhenRevisionMissingOrVoiceDefaultsUnwired(t *testing.T) {
	repository := newMemoryPolicyRepository()
	repository.policies["child-1"] = &domain.Policy{
		ID:            "policy-1",
		FamilyID:      "family-1",
		ChildID:       "child-1",
		PolicyVersion: 1,
		CreatedAt:     fixedClock{}.Now(),
		UpdatedAt:     fixedClock{}.Now(),
	}
	repository.readiness.RevisionRowCount = 1
	service, err := New(Options{
		Repository:    repository,
		VoiceDefaults: fixedVoiceDefaults{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
}

func TestReadinessDegradesWhenRevisionMissing(t *testing.T) {
	repository := newMemoryPolicyRepository()
	repository.policies["child-1"] = &domain.Policy{
		ID:            "policy-1",
		FamilyID:      "family-1",
		ChildID:       "child-1",
		PolicyVersion: 1,
		CreatedAt:     fixedClock{}.Now(),
		UpdatedAt:     fixedClock{}.Now(),
	}
	repository.readiness.RevisionRowCount = 0
	service, err := New(Options{
		Repository:    repository,
		VoiceDefaults: fixedVoiceDefaults{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
}

func TestReadinessDegradesWhenVoiceDefaultsUnwired(t *testing.T) {
	repository := newMemoryPolicyRepository()
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded ||
		readiness.VoiceDefaultsReady {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestReadinessMapsRepositoryFailure(t *testing.T) {
	repository := newMemoryPolicyRepository()
	repository.readinessErr = errors.New("database unavailable")
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected repository failure to surface")
	}
}
