package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/input/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
)

type fakeRepository struct {
	config   domain.Config
	statuses []domain.Status
	saved    domain.Input
	recorded domain.Status
	stats    domain.AdminStats
}

type readinessRepository struct {
	*fakeRepository
	freshness time.Duration
	readiness featurehealth.DeviceCapabilityReadiness
}

func (r *readinessRepository) Readiness(
	_ context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	r.freshness = freshness
	return r.readiness, nil
}

func (r *fakeRepository) Current(context.Context, string) (domain.Config, error) {
	return r.config, nil
}

func (r *fakeRepository) Save(
	_ context.Context,
	_ string,
	input domain.Input,
) (domain.Config, error) {
	r.saved = input
	config := domain.Default()
	config.Revision = 1
	config.Sensitivity = input.Sensitivity
	return config, nil
}

func (r *fakeRepository) RecordStatus(_ context.Context, status domain.Status) error {
	r.recorded = status
	return nil
}

func (r *fakeRepository) Statuses(context.Context, string) ([]domain.Status, error) {
	return r.statuses, nil
}

func (r *fakeRepository) AdminStats(context.Context, int) (domain.AdminStats, error) {
	return r.stats, nil
}

func TestPublishRejectsOutOfRangeConfig(t *testing.T) {
	service, err := New(Options{Repository: &fakeRepository{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	input := domain.Default()
	_, err = service.Publish(context.Background(), "family-1", domain.Input{
		Sensitivity:     0,
		RotationDegrees: 0,
		LongPressMS:     input.LongPressMS,
		DoubleTapMS:     input.DoubleTapMS,
		SwipeDistancePX: input.SwipeDistancePX,
	})
	if !errors.Is(err, domain.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestPublishRejectsUnsupportedRotation(t *testing.T) {
	service, err := New(Options{Repository: &fakeRepository{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	defaults := domain.Default()
	_, err = service.Publish(context.Background(), "family-1", domain.Input{
		Sensitivity:     defaults.Sensitivity,
		RotationDegrees: 45,
		LongPressMS:     defaults.LongPressMS,
		DoubleTapMS:     defaults.DoubleTapMS,
		SwipeDistancePX: defaults.SwipeDistancePX,
	})
	if !errors.Is(err, domain.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestPublishStoresValidInput(t *testing.T) {
	repository := &fakeRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	input := domain.Input{
		Enabled:          true,
		TapEnabled:       true,
		SwipeEnabled:     true,
		LongPressEnabled: true,
		HapticEnabled:    true,
		Sensitivity:      80,
		RotationDegrees:  90,
		LongPressMS:      600,
		DoubleTapMS:      250,
		SwipeDistancePX:  80,
	}
	config, err := service.Publish(context.Background(), "family-1", input)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if repository.saved.Sensitivity != 80 || config.Revision != 1 {
		t.Fatalf("unexpected saved config: %+v", repository.saved)
	}
}

func TestCapabilityReflectsReportedDevice(t *testing.T) {
	repository := &fakeRepository{
		config: domain.Config{Revision: 3},
		statuses: []domain.Status{
			{
				DeviceID:        "device-1",
				FamilyID:        "family-1",
				TouchSupported:  true,
				HapticSupported: true,
				GestureCount:    42,
				PolicyRevision:  3,
			},
		},
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	capability, err := service.Capability(context.Background(), "family-1", "device-1")
	if err != nil {
		t.Fatalf("capability: %v", err)
	}
	if !capability.Supported || !capability.HapticSupported || !capability.UpToDate {
		t.Fatalf("unexpected capability: %+v", capability)
	}

	missing, err := service.Capability(context.Background(), "family-1", "device-2")
	if err != nil {
		t.Fatalf("capability: %v", err)
	}
	if missing.Supported {
		t.Fatal("an unreported device must not be reported as supported")
	}
}

func TestCheckReadinessReturnsRepositoryHealth(t *testing.T) {
	repository := &readinessRepository{
		fakeRepository: &fakeRepository{},
		readiness: featurehealth.DeviceCapabilityReadiness{
			Status: featurehealth.StatusDegraded,
			Facts: featurehealth.DeviceCapabilityFacts{
				ExpectedDevices: 2,
				ReportedDevices: 1,
			},
		},
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	readiness, err := service.CheckReadiness(context.Background())
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if readiness.Status != featurehealth.StatusDegraded ||
		repository.freshness != capabilityFreshness {
		t.Fatalf("readiness = %+v, freshness = %s", readiness, repository.freshness)
	}
}

func TestCheckReadinessFailsClosedWithoutCapabilityReader(t *testing.T) {
	service, err := New(Options{Repository: &fakeRepository{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	readiness, err := service.CheckReadiness(context.Background())
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if readiness.Status != featurehealth.StatusUnavailable {
		t.Fatalf("status = %q, want unavailable", readiness.Status)
	}
}

func TestReportStatusValidatesBounds(t *testing.T) {
	repository := &fakeRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	status, err := service.ReportStatus(
		context.Background(),
		"family-1",
		"device-1",
		domain.StatusInput{
			TouchSupported:  true,
			HapticSupported: true,
			GestureCount:    120,
			PolicyRevision:  2,
		},
	)
	if err != nil {
		t.Fatalf("report status: %v", err)
	}
	if repository.recorded.GestureCount != 120 || status.PolicyRevision != 2 {
		t.Fatalf("unexpected recorded status: %+v", repository.recorded)
	}
	if _, err := service.ReportStatus(
		context.Background(),
		"family-1",
		"device-1",
		domain.StatusInput{GestureCount: -1},
	); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestAdminStatsDefaultsWindow(t *testing.T) {
	repository := &fakeRepository{stats: domain.AdminStats{DeviceCount: 4}}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	stats, err := service.AdminStats(context.Background(), 0)
	if err != nil {
		t.Fatalf("admin stats: %v", err)
	}
	if stats.DeviceCount != 4 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}
