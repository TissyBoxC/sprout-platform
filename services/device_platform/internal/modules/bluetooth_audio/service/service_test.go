package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/bluetooth_audio/domain"
)

// fakeRepository is an in-memory double so the service tests exercise the real
// validation and lifecycle without a database.
type fakeRepository struct {
	policy   domain.Policy
	statuses map[string]domain.Status
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		policy:   domain.Default(),
		statuses: map[string]domain.Status{},
	}
}

func (r *fakeRepository) Current(
	_ context.Context,
	_ string,
) (domain.Policy, error) {
	return r.policy, nil
}

func (r *fakeRepository) Save(
	_ context.Context,
	_ string,
	input domain.Input,
) (domain.Policy, error) {
	r.policy = domain.Policy{
		SchemaVersion:      domain.SchemaVersion,
		Enabled:            input.Enabled,
		SpeakerModeAllowed: input.SpeakerModeAllowed,
		PairingAllowed:     input.PairingAllowed,
		AutoReconnect:      input.AutoReconnect,
		PreferredCodec:     input.PreferredCodec,
		MaxVolumePercent:   input.MaxVolumePercent,
		DisplayName:        input.DisplayName,
		Revision:           r.policy.Revision + 1,
	}
	return r.policy, nil
}

func (r *fakeRepository) RecordStatus(
	_ context.Context,
	status domain.Status,
) error {
	r.statuses[status.DeviceID] = status
	return nil
}

func (r *fakeRepository) Statuses(
	_ context.Context,
	_ string,
) ([]domain.Status, error) {
	statuses := make([]domain.Status, 0, len(r.statuses))
	for _, status := range r.statuses {
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (r *fakeRepository) AdminStats(
	_ context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	return domain.AdminStats{
		WindowDays:  windowDays,
		DeviceCount: uint64(len(r.statuses)),
	}, nil
}

func newService(t *testing.T) (*Service, *fakeRepository) {
	t.Helper()
	repository := newFakeRepository()
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return service, repository
}

func TestNewRequiresRepository(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatalf("New without repository should fail")
	}
}

func TestCurrentRejectsEmptyFamily(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.Current(context.Background(), "  "); !errors.Is(
		err,
		domain.ErrInvalidPolicy,
	) {
		t.Fatalf("err = %v, want ErrInvalidPolicy", err)
	}
}

func TestPublishValidatesAndBumpsRevision(t *testing.T) {
	service, _ := newService(t)
	policy, err := service.Publish(context.Background(), "family-1", domain.Input{
		Enabled:            true,
		SpeakerModeAllowed: true,
		PairingAllowed:     true,
		AutoReconnect:      false,
		PreferredCodec:     domain.CodecAAC,
		MaxVolumePercent:   60,
		DisplayName:        "客厅音箱",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if policy.Revision != 1 {
		t.Fatalf("revision = %d, want 1", policy.Revision)
	}
	if !policy.PairingAllowed || policy.AutoReconnect {
		t.Fatalf("published policy lost field values: %+v", policy)
	}

	if _, err := service.Publish(context.Background(), "family-1", domain.Input{
		PreferredCodec:   "aptx",
		MaxVolumePercent: 60,
	}); !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("err = %v, want ErrInvalidPolicy", err)
	}
}

func TestCapabilityReportsUpToDate(t *testing.T) {
	service, repository := newService(t)
	repository.policy.Revision = 5

	if _, err := service.ReportStatus(
		context.Background(),
		"family-1",
		"device-1",
		domain.StatusInput{
			BluetoothSupported:   true,
			SpeakerMode:          true,
			AdapterState:         domain.AdapterConnected,
			PairedDeviceCount:    2,
			ConnectedDeviceCount: 1,
			ActiveCodec:          domain.ActiveCodecAAC,
			VolumePercent:        40,
			LastDisconnectReason: domain.DisconnectNone,
			PolicyRevision:       5,
		},
	); err != nil {
		t.Fatalf("ReportStatus: %v", err)
	}

	capability, err := service.Capability(
		context.Background(),
		"family-1",
		"device-1",
	)
	if err != nil {
		t.Fatalf("Capability: %v", err)
	}
	if !capability.Supported || !capability.UpToDate || !capability.SpeakerMode {
		t.Fatalf("capability = %+v, want supported, up to date, speaker on", capability)
	}
	if capability.PolicyRevision != 5 || capability.AppliedRevision != 5 {
		t.Fatalf("revisions = %d/%d", capability.PolicyRevision, capability.AppliedRevision)
	}
}

func TestCapabilityStaleDeviceIsOutdated(t *testing.T) {
	service, repository := newService(t)
	repository.policy.Revision = 4
	repository.statuses["device-1"] = domain.Status{
		DeviceID:             "device-1",
		FamilyID:             "family-1",
		BluetoothSupported:   true,
		AdapterState:         domain.AdapterIdle,
		ActiveCodec:          domain.ActiveCodecUnknown,
		LastDisconnectReason: domain.DisconnectNone,
		PolicyRevision:       2,
	}

	capability, err := service.Capability(
		context.Background(),
		"family-1",
		"device-1",
	)
	if err != nil {
		t.Fatalf("Capability: %v", err)
	}
	if capability.UpToDate {
		t.Fatalf("capability = %+v, want outdated", capability)
	}
}

func TestCapabilityUnknownDevice(t *testing.T) {
	service, _ := newService(t)
	capability, err := service.Capability(
		context.Background(),
		"family-1",
		"device-missing",
	)
	if err != nil {
		t.Fatalf("Capability: %v", err)
	}
	if capability.Supported {
		t.Fatalf("unknown device should not be supported")
	}
	if capability.AdapterState != domain.AdapterOff ||
		capability.ActiveCodec != domain.ActiveCodecUnknown {
		t.Fatalf("unknown device bounds = %+v", capability)
	}
}

func TestReportStatusRejectsUnknownValues(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.ReportStatus(
		context.Background(),
		"family-1",
		"device-1",
		domain.StatusInput{
			BluetoothSupported:   true,
			AdapterState:         "sleeping",
			ActiveCodec:          domain.ActiveCodecUnknown,
			LastDisconnectReason: domain.DisconnectNone,
		},
	); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestReportStatusCoercesUnsupportedDevice(t *testing.T) {
	service, _ := newService(t)
	status, err := service.ReportStatus(
		context.Background(),
		"family-1",
		"device-1",
		domain.StatusInput{
			BluetoothSupported:   false,
			SpeakerMode:          true,
			AdapterState:         domain.AdapterConnected,
			PairedDeviceCount:    1,
			ConnectedDeviceCount: 1,
			ActiveCodec:          domain.ActiveCodecSBC,
			VolumePercent:        80,
			LastDisconnectReason: domain.DisconnectUser,
		},
	)
	if err != nil {
		t.Fatalf("ReportStatus: %v", err)
	}
	if status.SpeakerMode ||
		status.AdapterState != domain.AdapterOff ||
		status.PairedDeviceCount != 0 ||
		status.VolumePercent != 0 {
		t.Fatalf("unsupported device not coerced: %+v", status)
	}
}

func TestDeviceStatusesFlagsUpToDate(t *testing.T) {
	service, repository := newService(t)
	repository.policy.Revision = 3
	repository.statuses["device-1"] = domain.Status{
		DeviceID:             "device-1",
		FamilyID:             "family-1",
		BluetoothSupported:   true,
		AdapterState:         domain.AdapterIdle,
		ActiveCodec:          domain.ActiveCodecUnknown,
		LastDisconnectReason: domain.DisconnectNone,
		PolicyRevision:       3,
	}
	repository.statuses["device-2"] = domain.Status{
		DeviceID:             "device-2",
		FamilyID:             "family-1",
		BluetoothSupported:   true,
		AdapterState:         domain.AdapterIdle,
		ActiveCodec:          domain.ActiveCodecUnknown,
		LastDisconnectReason: domain.DisconnectNone,
		PolicyRevision:       1,
	}

	devices, err := service.DeviceStatuses(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("DeviceStatuses: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(devices))
	}
	upToDate := map[string]bool{}
	for _, device := range devices {
		upToDate[device.DeviceID] = device.UpToDate
	}
	if !upToDate["device-1"] || upToDate["device-2"] {
		t.Fatalf("up-to-date flags = %+v", upToDate)
	}
}

func TestAdminStatsClampsWindow(t *testing.T) {
	service, _ := newService(t)
	stats, err := service.AdminStats(context.Background(), 1000)
	if err != nil {
		t.Fatalf("AdminStats: %v", err)
	}
	if stats.WindowDays != 7 {
		t.Fatalf("window = %d, want 7", stats.WindowDays)
	}
}
