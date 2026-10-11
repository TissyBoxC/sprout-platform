package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/form_factor/domain"
)

type fakeRepository struct {
	profiles map[string]domain.Profile
	recorded domain.Profile
	stats    domain.AdminStats
}

func (r *fakeRepository) Record(_ context.Context, profile domain.Profile) error {
	r.recorded = profile
	if r.profiles == nil {
		r.profiles = map[string]domain.Profile{}
	}
	r.profiles[profile.DeviceID] = profile
	return nil
}

func (r *fakeRepository) Profile(
	_ context.Context,
	_ string,
	deviceID string,
) (domain.Profile, bool, error) {
	profile, ok := r.profiles[deviceID]
	return profile, ok, nil
}

func (r *fakeRepository) Statuses(
	_ context.Context,
	_ string,
) ([]domain.Profile, error) {
	profiles := make([]domain.Profile, 0, len(r.profiles))
	for _, profile := range r.profiles {
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (r *fakeRepository) AdminStats(
	_ context.Context,
	_ int,
) (domain.AdminStats, error) {
	return r.stats, nil
}

func TestReportComposesActionsFromCapabilities(t *testing.T) {
	repository := &fakeRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	profile, err := service.Report(context.Background(), "family-1", "device-1", domain.Input{
		TechnicalName:  "moving_body",
		CapabilityMask: domain.CapDisplay | domain.CapLocomotion | domain.CapServo,
		SchemaVersion:  domain.SchemaVersion,
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if profile.TechnicalName != "moving_body" {
		t.Fatalf("unexpected name: %q", profile.TechnicalName)
	}
	expected := domain.ComposeActions(
		domain.CapDisplay | domain.CapLocomotion | domain.CapServo,
	)
	if profile.ActionMask != expected || repository.recorded.ActionMask != expected {
		t.Fatalf("action mask = %d, want %d", profile.ActionMask, expected)
	}
	if profile.ActionMask&domain.ActionWalk == 0 ||
		profile.ActionMask&domain.ActionTail == 0 {
		t.Fatalf("expected walk and tail actions: %d", profile.ActionMask)
	}
}

func TestReportRejectsUnknownCapability(t *testing.T) {
	service, err := New(Options{Repository: &fakeRepository{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.Report(context.Background(), "family-1", "device-1", domain.Input{
		TechnicalName:  "desktop",
		CapabilityMask: 0xFFFF,
		SchemaVersion:  domain.SchemaVersion,
	}); !errors.Is(err, domain.ErrInvalidProfile) {
		t.Fatalf("expected ErrInvalidProfile, got %v", err)
	}
	if _, err := service.Report(context.Background(), "family-1", "device-1", domain.Input{
		TechnicalName: "",
		SchemaVersion: domain.SchemaVersion,
	}); !errors.Is(err, domain.ErrInvalidProfile) {
		t.Fatalf("expected ErrInvalidProfile, got %v", err)
	}
	if _, err := service.Report(context.Background(), "family-1", "device-1", domain.Input{
		TechnicalName: "desktop",
		SchemaVersion: 99,
	}); !errors.Is(err, domain.ErrInvalidProfile) {
		t.Fatalf("expected ErrInvalidProfile, got %v", err)
	}
}

func TestCapabilityReflectsReportedDevice(t *testing.T) {
	repository := &fakeRepository{
		profiles: map[string]domain.Profile{
			"device-1": {
				DeviceID:       "device-1",
				FamilyID:       "family-1",
				TechnicalName:  "wearable",
				CapabilityMask: domain.CapWearable | domain.CapMicrophone | domain.CapSpeaker,
				ActionMask: domain.ComposeActions(
					domain.CapWearable | domain.CapMicrophone | domain.CapSpeaker,
				),
				SchemaVersion: domain.SchemaVersion,
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
	if !capability.Supported || capability.TechnicalName != "wearable" {
		t.Fatalf("unexpected capability: %+v", capability)
	}
	if capability.ActionMask&domain.ActionLocation == 0 ||
		capability.ActionMask&domain.ActionVoice == 0 {
		t.Fatalf("expected location and voice actions: %d", capability.ActionMask)
	}

	missing, err := service.Capability(context.Background(), "family-1", "device-2")
	if err != nil {
		t.Fatalf("capability: %v", err)
	}
	if missing.Supported {
		t.Fatal("an unreported device must not be reported as supported")
	}
}

func TestAdminStatsDefaultsWindow(t *testing.T) {
	repository := &fakeRepository{stats: domain.AdminStats{DeviceCount: 6}}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	stats, err := service.AdminStats(context.Background(), 0)
	if err != nil {
		t.Fatalf("admin stats: %v", err)
	}
	if stats.DeviceCount != 6 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}
