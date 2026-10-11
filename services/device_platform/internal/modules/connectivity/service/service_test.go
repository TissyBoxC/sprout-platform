package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/connectivity/domain"
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
	policy.MonthlyQuotaMB = input.MonthlyQuotaMB
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

func validInput() domain.Input {
	defaults := domain.Default()
	return domain.Input{
		Enabled:             true,
		RoamingAllowed:      false,
		RemoteWakeEnabled:   true,
		MeteredUploadLimit:  true,
		Preference:          domain.PreferenceWiFiFirst,
		MonthlyQuotaMB:      defaults.MonthlyQuotaMB,
		LowQuotaWarnPercent: defaults.LowQuotaWarnPercent,
		KeepaliveSeconds:    defaults.KeepaliveSeconds,
	}
}

func TestPublishRejectsOutOfRangePolicy(t *testing.T) {
	service, err := New(Options{Repository: &fakeRepository{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	input := validInput()
	input.MonthlyQuotaMB = 9999999
	if _, err := service.Publish(context.Background(), "family-1", input); !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("expected ErrInvalidPolicy, got %v", err)
	}
}

func TestPublishRejectsUnsupportedPreference(t *testing.T) {
	service, err := New(Options{Repository: &fakeRepository{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	input := validInput()
	input.Preference = "bluetooth"
	if _, err := service.Publish(context.Background(), "family-1", input); !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("expected ErrInvalidPolicy, got %v", err)
	}
}

func TestPublishStoresValidInput(t *testing.T) {
	repository := &fakeRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	input := validInput()
	input.MonthlyQuotaMB = 4096
	input.Preference = domain.PreferenceCellularFirst
	policy, err := service.Publish(context.Background(), "family-1", input)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if repository.saved.MonthlyQuotaMB != 4096 || policy.Revision != 1 {
		t.Fatalf("unexpected saved policy: %+v", repository.saved)
	}
}

func TestCapabilityReflectsReportedDevice(t *testing.T) {
	repository := &fakeRepository{
		policy: domain.Policy{Revision: 3, MonthlyQuotaMB: 2048},
		statuses: []domain.Status{
			{
				DeviceID:       "device-1",
				FamilyID:       "family-1",
				Bearer:         domain.BearerCellular,
				ESIMState:      domain.ESIMActive,
				SignalPercent:  70,
				UsedMB:         2500,
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
	if !capability.Supported || !capability.UpToDate || !capability.QuotaExhausted {
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
			Bearer:         domain.BearerCellular,
			ESIMState:      domain.ESIMActive,
			SignalPercent:  80,
			UsedMB:         1024,
			PolicyRevision: 2,
		},
	)
	if err != nil {
		t.Fatalf("report status: %v", err)
	}
	if repository.recorded.UsedMB != 1024 || status.PolicyRevision != 2 {
		t.Fatalf("unexpected recorded status: %+v", repository.recorded)
	}
	if _, err := service.ReportStatus(
		context.Background(),
		"family-1",
		"device-1",
		domain.StatusInput{Bearer: domain.BearerCellular, ESIMState: domain.ESIMActive, SignalPercent: 200},
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
