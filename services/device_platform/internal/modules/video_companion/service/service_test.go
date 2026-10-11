package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_companion/domain"
)

// fixedClock returns a stable instant so the answered/ended stamps are
// deterministic across runs.
type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)
}

type fakeRepository struct {
	contacts map[string]domain.Contact
	calls    map[string]domain.Call
	statuses map[string]domain.DeviceStatus
	stats    domain.AdminStats
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		contacts: map[string]domain.Contact{},
		calls:    map[string]domain.Call{},
		statuses: map[string]domain.DeviceStatus{},
	}
}

func (r *fakeRepository) CreateContact(_ context.Context, contact domain.Contact) error {
	r.contacts[contact.ID] = contact
	return nil
}

func (r *fakeRepository) UpdateContact(_ context.Context, contact domain.Contact) error {
	if _, ok := r.contacts[contact.ID]; !ok {
		return domain.ErrContactNotFound
	}
	r.contacts[contact.ID] = contact
	return nil
}

func (r *fakeRepository) DeleteContact(_ context.Context, _ string, contactID string) error {
	if _, ok := r.contacts[contactID]; !ok {
		return domain.ErrContactNotFound
	}
	delete(r.contacts, contactID)
	return nil
}

func (r *fakeRepository) Contacts(_ context.Context, _ string) ([]domain.Contact, error) {
	contacts := make([]domain.Contact, 0, len(r.contacts))
	for _, contact := range r.contacts {
		contacts = append(contacts, contact)
	}
	return contacts, nil
}

func (r *fakeRepository) Contact(
	_ context.Context,
	_ string,
	contactID string,
) (domain.Contact, bool, error) {
	contact, ok := r.contacts[contactID]
	return contact, ok, nil
}

func (r *fakeRepository) CreateCall(_ context.Context, call domain.Call) error {
	r.calls[call.ID] = call
	return nil
}

func (r *fakeRepository) Call(
	_ context.Context,
	_ string,
	callID string,
) (domain.Call, bool, error) {
	call, ok := r.calls[callID]
	return call, ok, nil
}

func (r *fakeRepository) UpdateCall(_ context.Context, call domain.Call) error {
	if _, ok := r.calls[call.ID]; !ok {
		return domain.ErrCallNotFound
	}
	r.calls[call.ID] = call
	return nil
}

func (r *fakeRepository) Calls(_ context.Context, _ string, _ int) ([]domain.Call, error) {
	calls := make([]domain.Call, 0, len(r.calls))
	for _, call := range r.calls {
		calls = append(calls, call)
	}
	return calls, nil
}

func (r *fakeRepository) RecordStatus(_ context.Context, status domain.DeviceStatus) error {
	r.statuses[status.DeviceID] = status
	return nil
}

func (r *fakeRepository) Statuses(_ context.Context, _ string) ([]domain.DeviceStatus, error) {
	statuses := make([]domain.DeviceStatus, 0, len(r.statuses))
	for _, status := range r.statuses {
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (r *fakeRepository) AdminStats(_ context.Context, _ int) (domain.AdminStats, error) {
	return r.stats, nil
}

func newService(t *testing.T, repository *fakeRepository) *Service {
	t.Helper()
	service, err := New(Options{
		Repository: repository,
		Clock:      fixedClock{},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func TestCreateContactValidatesAndStores(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	contact, err := service.CreateContact(context.Background(), "family-1", domain.ContactInput{
		DisplayName: "妈妈",
		Role:        domain.RoleGuardian,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create contact: %v", err)
	}
	if contact.ID == "" || contact.DisplayName != "妈妈" {
		t.Fatalf("unexpected contact: %+v", contact)
	}

	if _, err := service.CreateContact(context.Background(), "family-1", domain.ContactInput{
		DisplayName: "",
		Role:        domain.RoleGuardian,
	}); !errors.Is(err, domain.ErrInvalidContact) {
		t.Fatalf("expected ErrInvalidContact, got %v", err)
	}
	if _, err := service.CreateContact(context.Background(), "family-1", domain.ContactInput{
		DisplayName: "爸爸",
		Role:        "stranger",
	}); !errors.Is(err, domain.ErrInvalidContact) {
		t.Fatalf("expected ErrInvalidContact, got %v", err)
	}
}

func TestStartCallRequiresKnownContact(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	contact, err := service.CreateContact(context.Background(), "family-1", domain.ContactInput{
		DisplayName: "奶奶",
		Role:        domain.RoleFamily,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create contact: %v", err)
	}

	call, err := service.StartCall(context.Background(), "family-1", domain.CallInput{
		DeviceID:  "device-1",
		ContactID: contact.ID,
		Initiator: domain.InitiatorGuardian,
	})
	if err != nil {
		t.Fatalf("start call: %v", err)
	}
	if call.State != domain.StateRinging || call.ID == "" {
		t.Fatalf("unexpected call: %+v", call)
	}

	if _, err := service.StartCall(context.Background(), "family-1", domain.CallInput{
		DeviceID:  "device-1",
		ContactID: "missing",
		Initiator: domain.InitiatorGuardian,
	}); !errors.Is(err, domain.ErrContactNotFound) {
		t.Fatalf("expected ErrContactNotFound, got %v", err)
	}
}

func TestUpdateCallStateStampsTimesAndReason(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	call, err := service.StartCall(context.Background(), "family-1", domain.CallInput{
		DeviceID:  "device-1",
		Initiator: domain.InitiatorGuardian,
	})
	if err != nil {
		t.Fatalf("start call: %v", err)
	}

	active, err := service.UpdateCallState(
		context.Background(), "family-1", call.ID,
		domain.StateActive, domain.QualityGood, "",
	)
	if err != nil {
		t.Fatalf("activate call: %v", err)
	}
	if active.AnsweredAt == nil || active.EndedAt != nil {
		t.Fatalf("unexpected active call: %+v", active)
	}

	ended, err := service.UpdateCallState(
		context.Background(), "family-1", call.ID,
		domain.StateEnded, domain.QualityGood, domain.EndHangup,
	)
	if err != nil {
		t.Fatalf("end call: %v", err)
	}
	if ended.EndedAt == nil || ended.EndReason != domain.EndHangup {
		t.Fatalf("unexpected ended call: %+v", ended)
	}

	if _, err := service.UpdateCallState(
		context.Background(), "family-1", call.ID,
		domain.StateEnded, domain.QualityGood, "because",
	); !errors.Is(err, domain.ErrInvalidCall) {
		t.Fatalf("expected ErrInvalidCall, got %v", err)
	}

	if _, err := service.UpdateCallState(
		context.Background(), "family-1", "missing",
		domain.StateEnded, domain.QualityGood, domain.EndHangup,
	); !errors.Is(err, domain.ErrCallNotFound) {
		t.Fatalf("expected ErrCallNotFound, got %v", err)
	}
}

func TestReportStatusAndCapability(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	status, err := service.ReportStatus(
		context.Background(), "family-1", "device-1",
		domain.CallStatusInput{
			CallState:         domain.StateActive,
			Quality:           domain.QualityFair,
			CameraAllowed:     true,
			MicrophoneAllowed: true,
			LastCallID:        "call-1",
		},
	)
	if err != nil {
		t.Fatalf("report status: %v", err)
	}
	if status.CallState != domain.StateActive {
		t.Fatalf("unexpected status: %+v", status)
	}

	capability, err := service.Capability(context.Background(), "family-1", "device-1")
	if err != nil {
		t.Fatalf("capability: %v", err)
	}
	if !capability.Supported || !capability.CameraAllowed {
		t.Fatalf("unexpected capability: %+v", capability)
	}

	missing, err := service.Capability(context.Background(), "family-1", "device-2")
	if err != nil {
		t.Fatalf("capability: %v", err)
	}
	if missing.Supported {
		t.Fatal("an unreported device must not be reported as supported")
	}

	if _, err := service.ReportStatus(
		context.Background(), "family-1", "device-1",
		domain.CallStatusInput{
			CallState: "unknown-state",
			Quality:   domain.QualityGood,
		},
	); !errors.Is(err, domain.ErrInvalidCall) {
		t.Fatalf("expected ErrInvalidCall, got %v", err)
	}
}

func TestAdminStatsDefaultsWindow(t *testing.T) {
	repository := newFakeRepository()
	repository.stats = domain.AdminStats{CallCount: 9}
	service := newService(t, repository)
	stats, err := service.AdminStats(context.Background(), 0)
	if err != nil {
		t.Fatalf("admin stats: %v", err)
	}
	if stats.CallCount != 9 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}
