package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/image_privacy/domain"
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
		SchemaVersion:       domain.SchemaVersion,
		Enabled:             input.Enabled,
		UploadAllowed:       input.UploadAllowed,
		LocalProcessingOnly: input.LocalProcessingOnly,
		ProcessingMode:      input.ProcessingMode,
		UploadScope:         input.UploadScope,
		RetentionDays:       input.RetentionDays,
		RedactionLevel:      input.RedactionLevel,
		RequireChildConsent: input.RequireChildConsent,
		Revision:            r.policy.Revision + 1,
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
		Enabled:             true,
		UploadAllowed:       true,
		LocalProcessingOnly: false,
		ProcessingMode:      domain.ModeLocalFull,
		UploadScope:         domain.ScopeThumbnails,
		RetentionDays:       7,
		RedactionLevel:      domain.RedactionStandard,
		RequireChildConsent: false,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if policy.Revision != 1 {
		t.Fatalf("revision = %d, want 1", policy.Revision)
	}
	if !policy.Enabled || !policy.UploadAllowed {
		t.Fatalf("published policy lost field values: %+v", policy)
	}
	if policy.UploadScope != domain.ScopeThumbnails {
		t.Fatalf("scope = %q", policy.UploadScope)
	}

	if _, err := service.Publish(context.Background(), "family-1", domain.Input{
		Enabled:             true,
		UploadAllowed:       true,
		LocalProcessingOnly: false,
		ProcessingMode:      "cloud",
		UploadScope:         domain.ScopeThumbnails,
		RetentionDays:       7,
		RedactionLevel:      domain.RedactionStandard,
	}); !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("err = %v, want ErrInvalidPolicy", err)
	}
}

func TestPublishNormalisesDerivedFields(t *testing.T) {
	service, _ := newService(t)
	policy, err := service.Publish(context.Background(), "family-1", domain.Input{
		Enabled:             false,
		UploadAllowed:       true,
		LocalProcessingOnly: false,
		ProcessingMode:      domain.ModeLocalFull,
		UploadScope:         domain.ScopeAnalysis,
		RetentionDays:       7,
		RedactionLevel:      domain.RedactionNone,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if policy.UploadAllowed ||
		policy.UploadScope != domain.ScopeNone ||
		policy.ProcessingMode != domain.ModeOff {
		t.Fatalf("published policy not normalised: %+v", policy)
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
			CameraSupported: true,
			LocalProcessing: true,
			UploadAllowed:   true,
			ProcessingMode:  domain.ModeLocalFull,
			UploadScope:     domain.ScopeThumbnails,
			RedactionLevel:  domain.RedactionStandard,
			ProcessedCount:  10,
			UploadedCount:   3,
			BlockedCount:    1,
			LastBlockReason: domain.BlockScope,
			PolicyRevision:  5,
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
	if !capability.Supported || !capability.UpToDate || !capability.UploadAllowed {
		t.Fatalf("capability = %+v, want supported, up to date, upload allowed", capability)
	}
	if capability.PolicyRevision != 5 || capability.AppliedRevision != 5 {
		t.Fatalf("revisions = %d/%d", capability.PolicyRevision, capability.AppliedRevision)
	}
}

func TestCapabilityStaleDeviceIsOutdated(t *testing.T) {
	service, repository := newService(t)
	repository.policy.Revision = 4
	repository.statuses["device-1"] = domain.Status{
		DeviceID:        "device-1",
		FamilyID:        "family-1",
		CameraSupported: true,
		LocalProcessing: true,
		ProcessingMode:  domain.ModeMetadata,
		UploadScope:     domain.ScopeNone,
		RedactionLevel:  domain.RedactionStrict,
		LastBlockReason: domain.BlockNone,
		PolicyRevision:  2,
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
	if capability.ProcessingMode != domain.ModeOff ||
		capability.UploadScope != domain.ScopeNone ||
		capability.RedactionLevel != domain.RedactionStrict {
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
			CameraSupported: true,
			LocalProcessing: true,
			ProcessingMode:  "cloud",
			UploadScope:     domain.ScopeNone,
			RedactionLevel:  domain.RedactionStrict,
			LastBlockReason: domain.BlockNone,
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
			CameraSupported: false,
			LocalProcessing: true,
			UploadAllowed:   true,
			ProcessingMode:  domain.ModeLocalFull,
			UploadScope:     domain.ScopeAnalysis,
			RedactionLevel:  domain.RedactionNone,
			ProcessedCount:  20,
			UploadedCount:   5,
			BlockedCount:    2,
			LastBlockReason: domain.BlockQuota,
			PolicyRevision:  3,
		},
	)
	if err != nil {
		t.Fatalf("ReportStatus: %v", err)
	}
	if status.LocalProcessing ||
		status.UploadAllowed ||
		status.ProcessingMode != domain.ModeOff ||
		status.UploadScope != domain.ScopeNone ||
		status.RedactionLevel != domain.RedactionStrict ||
		status.ProcessedCount != 0 ||
		status.UploadedCount != 0 ||
		status.BlockedCount != 0 ||
		status.LastBlockReason != domain.BlockNone {
		t.Fatalf("unsupported device not coerced: %+v", status)
	}
}

func TestDeviceStatusesFlagsUpToDate(t *testing.T) {
	service, repository := newService(t)
	repository.policy.Revision = 3
	repository.statuses["device-1"] = domain.Status{
		DeviceID:        "device-1",
		FamilyID:        "family-1",
		CameraSupported: true,
		LocalProcessing: true,
		ProcessingMode:  domain.ModeMetadata,
		UploadScope:     domain.ScopeNone,
		RedactionLevel:  domain.RedactionStrict,
		LastBlockReason: domain.BlockNone,
		PolicyRevision:  3,
	}
	repository.statuses["device-2"] = domain.Status{
		DeviceID:        "device-2",
		FamilyID:        "family-1",
		CameraSupported: true,
		LocalProcessing: true,
		ProcessingMode:  domain.ModeMetadata,
		UploadScope:     domain.ScopeNone,
		RedactionLevel:  domain.RedactionStrict,
		LastBlockReason: domain.BlockNone,
		PolicyRevision:  1,
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
