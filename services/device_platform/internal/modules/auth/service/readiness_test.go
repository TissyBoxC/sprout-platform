package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
)

func TestReadinessHealthyOnEmptyPlatformWithWorkingDependencies(t *testing.T) {
	repository := &memoryRepository{
		readiness: domain.Readiness{
			ActiveParentCount: 0,
			ActiveAdminCount:  0,
		},
	}
	service := newAdminTestService(t, repository)
	service.mfaCipher = plaintextMFACipher{}
	service.mfaAuditor = &recordingMFAAuditor{}

	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
	if readiness.ActiveParentCount != 0 || readiness.ActiveSessionCount != 0 {
		t.Fatalf("unexpected counters: %+v", readiness)
	}
}

func TestReadinessDegradesWithoutMFADependencies(t *testing.T) {
	repository := &memoryRepository{
		readiness: domain.Readiness{
			ActiveParentCount: 1,
			ActiveAdminCount:  1,
		},
	}
	service := newAdminTestService(t, repository)

	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
	if readiness.MFACipherReady || readiness.MFAAuditorReady {
		t.Fatalf("missing MFA dependencies were reported ready: %+v", readiness)
	}
}

func TestReadinessHealthyWhenAccountsAndMFAReady(t *testing.T) {
	repository := &memoryRepository{
		readiness: domain.Readiness{
			ActiveParentCount:    3,
			ActiveAdminCount:     1,
			MFAEnabledAdminCount: 1,
		},
	}
	service := newAdminTestService(t, repository)
	service.mfaCipher = plaintextMFACipher{}
	service.mfaAuditor = &recordingMFAAuditor{}

	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
	if !readiness.MFACipherReady || !readiness.MFAAuditorReady {
		t.Fatalf("MFA dependencies not reported: %+v", readiness)
	}
}

func TestReadinessDegradesWhenAdministratorHasNoMFAEnrolment(t *testing.T) {
	repository := &memoryRepository{
		readiness: domain.Readiness{
			ActiveParentCount:    1,
			ActiveAdminCount:     1,
			MFAEnabledAdminCount: 0,
		},
	}
	service := newAdminTestService(t, repository)
	service.mfaCipher = plaintextMFACipher{}
	service.mfaAuditor = &recordingMFAAuditor{}

	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
}

func TestReadinessMapsRepositoryFailure(t *testing.T) {
	repository := &memoryRepository{readinessErr: errors.New("database unavailable")}
	service := newAdminTestService(t, repository)

	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected repository failure to surface")
	}
}
