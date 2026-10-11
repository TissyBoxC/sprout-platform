package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/display/domain"
)

type fakeRepository struct {
	policy   domain.Policy
	statuses []domain.Status
	saved    domain.Input
	recorded domain.Status
	stats    domain.AdminStats
}

func (r *fakeRepository) Current(context.Context, string) (domain.Policy, error) {
	return r.policy, nil
}

func (r *fakeRepository) Save(
	_ context.Context,
	_ string,
	input domain.Input,
) (domain.Policy, error) {
	r.saved = input
	policy := domain.Default()
	policy.Revision = 1
	policy.BrightnessPercent = input.BrightnessPercent
	return policy, nil
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

func TestPublishRejectsOutOfRangePolicy(t *testing.T) {
	service, err := New(Options{Repository: &fakeRepository{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	input := domain.Input{
		BrightnessPercent:      0,
		ColorTemperatureKelvin: 4500,
		MinViewDistanceCM:      40,
		DailyLimitMinutes:      60,
		BreakIntervalMinutes:   20,
		DimAfterSeconds:        60,
	}
	if _, err := service.Publish(context.Background(), "family-1", input); !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("expected ErrInvalidPolicy, got %v", err)
	}
}

func TestPublishClampsNothingAndStoresValidInput(t *testing.T) {
	repository := &fakeRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	input := domain.Input{
		BrightnessPercent:      80,
		ColorTemperatureKelvin: 3000,
		MinViewDistanceCM:      30,
		DailyLimitMinutes:      90,
		BreakIntervalMinutes:   15,
		DimAfterSeconds:        30,
		AutoBrightness:         true,
		NightLight:             true,
		DistanceReminder:       true,
	}
	policy, err := service.Publish(context.Background(), "family-1", input)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if repository.saved.BrightnessPercent != 80 || policy.Revision != 1 {
		t.Fatalf("unexpected saved policy: %+v", repository.saved)
	}
}

func TestCapabilityReflectsReportedDevice(t *testing.T) {
	repository := &fakeRepository{
		policy: domain.Policy{Revision: 3},
		statuses: []domain.Status{
			{
				DeviceID:       "device-1",
				FamilyID:       "family-1",
				ScreenWidth:    480,
				ScreenHeight:   320,
				TouchSupported: true,
				PolicyRevision: 3,
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
	if !capability.Supported || !capability.TouchSupported || !capability.UpToDate {
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
			BrightnessPercent: 55,
			ScreenWidth:       480,
			ScreenHeight:      320,
			TouchSupported:    true,
			PolicyRevision:    2,
		},
	)
	if err != nil {
		t.Fatalf("report status: %v", err)
	}
	if repository.recorded.BrightnessPercent != 55 || status.PolicyRevision != 2 {
		t.Fatalf("unexpected recorded status: %+v", repository.recorded)
	}
	if _, err := service.ReportStatus(
		context.Background(),
		"family-1",
		"device-1",
		domain.StatusInput{ScreenWidth: 99999},
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
