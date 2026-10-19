package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
)

func TestReadinessAcceptsEmptyProfileStore(t *testing.T) {
	service, err := New(Options{
		Repository:        newMemoryChildRepository(),
		PolicyProvisioner: &memoryPolicyProvisioner{},
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
	if readiness.ProfileCount != 0 || readiness.FamiliesWithProfile != 0 {
		t.Fatalf("unexpected counters: %+v", readiness)
	}
}

func TestReadinessDegradesWithoutPolicyProvisioner(t *testing.T) {
	service, err := New(Options{Repository: newMemoryChildRepository()})
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

func TestReadinessDegradesWhenProfileLacksMandatoryPolicy(t *testing.T) {
	repository := newMemoryChildRepository()
	repository.children["child_1"] = &domain.Child{
		ID:       "child_1",
		FamilyID: "family_1",
	}
	repository.readiness.OrphanedProfileCount = 1
	service, err := New(Options{
		Repository:        repository,
		PolicyProvisioner: &memoryPolicyProvisioner{},
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
	if readiness.OrphanedProfileCount != 1 {
		t.Fatalf("orphaned profile count = %d, want 1", readiness.OrphanedProfileCount)
	}
}

func TestReadinessMapsRepositoryFailure(t *testing.T) {
	repository := newMemoryChildRepository()
	repository.readinessErr = errors.New("database unavailable")
	service, err := New(Options{
		Repository:        repository,
		PolicyProvisioner: &memoryPolicyProvisioner{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected repository failure to surface")
	}
}
