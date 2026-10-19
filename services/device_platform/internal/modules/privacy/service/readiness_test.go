package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/domain"
)

func TestReadinessAcceptsEmptyLifecycleStore(t *testing.T) {
	service := newTestService(
		t,
		&memoryRepository{
			readiness: domain.Readiness{RecentAuditEventCount: 1},
		},
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
}

func TestReadinessDegradesForStaleDeletion(t *testing.T) {
	repository := &memoryRepository{
		readiness: domain.Readiness{
			StaleDeletionCount:    1,
			RecentAuditEventCount: 1,
		},
	}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
}

func TestReadinessDegradesWithoutRecentAuditProof(t *testing.T) {
	service := newTestService(
		t,
		&memoryRepository{},
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
	if readiness.AuditCoverageProof {
		t.Fatal("empty audit store must not claim audit coverage")
	}
}

func TestReadinessMapsRepositoryFailure(t *testing.T) {
	repository := &memoryRepository{readinessErr: errors.New("database unavailable")}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected repository failure to surface")
	}
}
