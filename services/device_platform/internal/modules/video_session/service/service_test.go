package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_session/domain"
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
		CameraAllowed:       input.CameraAllowed,
		MicrophoneAllowed:   input.MicrophoneAllowed,
		RequireChildConsent: input.RequireChildConsent,
		QualityFloor:        input.QualityFloor,
		RingTimeoutSeconds:  input.RingTimeoutSeconds,
		MaxSessionSeconds:   input.MaxSessionSeconds,
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
		CameraAllowed:       true,
		MicrophoneAllowed:   true,
		RequireChildConsent: false,
		QualityFloor:        domain.QualityGood,
		RingTimeoutSeconds:  45,
		MaxSessionSeconds:   900,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if policy.Revision != 1 {
		t.Fatalf("revision = %d, want 1", policy.Revision)
	}
	if !policy.CameraAllowed || !policy.MicrophoneAllowed {
		t.Fatalf("published policy lost media permissions")
	}

	if _, err := service.Publish(context.Background(), "family-1", domain.Input{
		QualityFloor:       domain.QualityGood,
		RingTimeoutSeconds: 1,
		MaxSessionSeconds:  900,
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
			SessionSupported:  true,
			CameraAllowed:     true,
			MicrophoneAllowed: true,
			CallState:         domain.StateActive,
			Quality:           domain.QualityFair,
			LastEndReason:     domain.EndNone,
			PolicyRevision:    5,
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
	if !capability.Supported || !capability.UpToDate {
		t.Fatalf("capability = %+v, want supported and up to date", capability)
	}
	if capability.PolicyRevision != 5 || capability.AppliedRevision != 5 {
		t.Fatalf("revisions = %d/%d", capability.PolicyRevision, capability.AppliedRevision)
	}
}

func TestCapabilityStaleDeviceIsOutdated(t *testing.T) {
	service, repository := newService(t)
	repository.policy.Revision = 4
	repository.statuses["device-1"] = domain.Status{
		DeviceID:         "device-1",
		FamilyID:         "family-1",
		SessionSupported: true,
		CallState:        domain.StateIdle,
		Quality:          domain.QualityUnknown,
		LastEndReason:    domain.EndNone,
		PolicyRevision:   2,
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
	if capability.CallState != domain.StateIdle ||
		capability.Quality != domain.QualityUnknown {
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
			SessionSupported: true,
			CallState:        "dialing",
			Quality:          domain.QualityGood,
			LastEndReason:    domain.EndNone,
		},
	); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
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
