package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

func TestReadinessAcceptsNoFamilies(t *testing.T) {
	service := newService(t, newFakeRepository(), time.Now())
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
}

func TestReadinessDegradesForOwnerlessFamily(t *testing.T) {
	repository := newFakeRepository()
	repository.members[memberKey("family-1", "guardian-1")] = domain.Member{
		ID:       "member-1",
		FamilyID: "family-1",
		UserID:   "guardian-1",
		Role:     domain.RoleGuardian,
	}
	service := newService(t, repository, time.Now())
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded ||
		readiness.OwnerlessFamilyCount != 1 {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestReadinessDegradesWhenActiveParentHasNoFamily(t *testing.T) {
	repository := newFakeRepository()
	repository.readiness = domain.Readiness{
		ParentAccountCount: 1,
	}
	service := newService(t, repository, time.Now())
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
	if readiness.ParentAccountCount != 1 || readiness.FamilyCount != 0 {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestReadinessMapsRepositoryFailure(t *testing.T) {
	repository := newFakeRepository()
	repository.readinessErr = errors.New("database unavailable")
	service := newService(t, repository, time.Now())
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected repository failure to surface")
	}
}
