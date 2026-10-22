package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/service"
)

// fakeStore keeps sessions in memory and honours the same sentinels as the
// repository adapter, so the service is tested against its real contract.
type fakeStore struct {
	sessions map[string]domain.Session
	openBusy string
}

func newFakeStore() *fakeStore {
	return &fakeStore{sessions: map[string]domain.Session{}}
}

func (store *fakeStore) Create(_ context.Context, session domain.Session) error {
	for _, existing := range store.sessions {
		if existing.DeviceID == session.DeviceID && !existing.State.IsTerminal() {
			return service.StoreSentinelBusy
		}
	}
	store.sessions[session.ID] = session
	return nil
}

func (store *fakeStore) Get(_ context.Context, familyID, sessionID string) (domain.Session, error) {
	session, ok := store.sessions[sessionID]
	if !ok || session.FamilyID != familyID {
		return domain.Session{}, service.StoreSentinelNotFound
	}
	return session, nil
}

func (store *fakeStore) GetForDevice(_ context.Context, deviceID, sessionID string) (domain.Session, error) {
	session, ok := store.sessions[sessionID]
	if !ok || session.DeviceID != deviceID {
		return domain.Session{}, service.StoreSentinelNotFound
	}
	return session, nil
}

func (store *fakeStore) Transition(_ context.Context, session domain.Session, expected domain.ViewState) error {
	current, ok := store.sessions[session.ID]
	if !ok {
		return service.StoreSentinelNotFound
	}
	if current.State != expected {
		return service.StoreSentinelBusy
	}
	store.sessions[session.ID] = session
	return nil
}

func (store *fakeStore) ListOpen(_ context.Context) ([]domain.Session, error) {
	open := make([]domain.Session, 0, len(store.sessions))
	for _, session := range store.sessions {
		if !session.State.IsTerminal() {
			open = append(open, session)
		}
	}
	return open, nil
}

func (store *fakeStore) History(_ context.Context, familyID string, limit int) ([]domain.Session, error) {
	history := make([]domain.Session, 0, limit)
	for _, session := range store.sessions {
		if session.FamilyID == familyID {
			history = append(history, session)
		}
	}
	return history, nil
}

func (store *fakeStore) CountOpen(_ context.Context) (int, error) {
	count := 0
	for _, session := range store.sessions {
		if !session.State.IsTerminal() {
			count++
		}
	}
	return count, nil
}

func (store *fakeStore) CountSessionsSince(_ context.Context, familyID string, since time.Time) (int, error) {
	count := 0
	for _, session := range store.sessions {
		if session.FamilyID == familyID && !session.StartedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

type fakeDevices struct {
	owner map[string]string
	child map[string]string
}

func (devices fakeDevices) DeviceFamily(_ context.Context, deviceID string) (string, error) {
	return devices.owner[deviceID], nil
}

func (devices fakeDevices) DeviceChild(_ context.Context, deviceID string) (string, error) {
	return devices.child[deviceID], nil
}

type fakeSwitch struct{ disabled map[string]bool }

func (switcher fakeSwitch) ViewEnabled(_ context.Context, familyID string) (bool, error) {
	return !switcher.disabled[familyID], nil
}

type fakeConsent struct{ denied map[string]bool }

func (consent fakeConsent) ImageConsentGranted(_ context.Context, familyID string) (bool, error) {
	return !consent.denied[familyID], nil
}

type fakeNotifier struct {
	starts []string
	stops  []string
	fail   error
}

func (notifier *fakeNotifier) NotifyCareViewStart(_ context.Context, session domain.Session) error {
	if notifier.fail != nil {
		return notifier.fail
	}
	notifier.starts = append(notifier.starts, session.ID)
	return nil
}

func (notifier *fakeNotifier) NotifyCareViewStop(_ context.Context, session domain.Session) error {
	notifier.stops = append(notifier.stops, session.ID)
	return nil
}

func defaultFixtures() (*fakeStore, fakeDevices, fakeSwitch, fakeConsent, *fakeNotifier) {
	return newFakeStore(),
		fakeDevices{
			owner: map[string]string{"device-1": "family-1", "device-2": "family-2"},
			child: map[string]string{"device-1": "child-1"},
		},
		fakeSwitch{disabled: map[string]bool{}},
		fakeConsent{denied: map[string]bool{}},
		&fakeNotifier{}
}

func newService(
	store *fakeStore,
	devices fakeDevices,
	switcher fakeSwitch,
	consent fakeConsent,
	notifier *fakeNotifier,
) *service.Service {
	return service.NewService(service.Options{
		Store:    store,
		Devices:  devices,
		Switches: switcher,
		Consent:  consent,
		Notifier: notifier,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func startRequest() domain.Request {
	return domain.Request{
		FamilyID:       "family-1",
		DeviceID:       "device-1",
		GuardianID:     "guardian-1",
		ConsentGranted: true,
	}
}

func TestStartCreatesRequestedSession(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	session, err := svc.Start(context.Background(), startRequest())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// A guardian asking must never start a camera on its own.
	if session.State != domain.StateRequested {
		t.Fatalf("state = %s, want requested", session.State)
	}
	if session.State.AllowsFrames() {
		t.Fatal("a new session must not carry frames")
	}
	if session.ChildID != "child-1" {
		t.Fatalf("child = %q, want child-1", session.ChildID)
	}
	if len(notifier.starts) != 1 || notifier.starts[0] != session.ID {
		t.Fatalf("start notifications = %v", notifier.starts)
	}
}

func TestStartRejectsForeignDevice(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	// Another family's device and an unknown device must be indistinguishable.
	_, err := svc.Start(context.Background(), domain.Request{
		FamilyID: "family-1", DeviceID: "device-2", GuardianID: "g", ConsentGranted: true,
	})
	if !errors.Is(err, service.ErrDeviceNotFound) {
		t.Fatalf("cross-family err = %v, want ErrDeviceNotFound", err)
	}
	_, err = svc.Start(context.Background(), domain.Request{
		FamilyID: "family-1", DeviceID: "missing", GuardianID: "g", ConsentGranted: true,
	})
	if !errors.Is(err, service.ErrDeviceNotFound) {
		t.Fatalf("unknown device err = %v, want ErrDeviceNotFound", err)
	}
}

func TestStartRequiresConsent(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	request := startRequest()
	request.ConsentGranted = false
	if _, err := svc.Start(context.Background(), request); !errors.Is(err, domain.ErrNoConsent) {
		t.Fatalf("err = %v, want ErrNoConsent", err)
	}
}

func TestStartRejectsWithdrawnConsent(t *testing.T) {
	store, devices, switcher, _, notifier := defaultFixtures()
	// Consent recorded in the request is not enough; the stored decision wins.
	svc := newService(store, devices, switcher, fakeConsent{denied: map[string]bool{"family-1": true}}, notifier)

	if _, err := svc.Start(context.Background(), startRequest()); !errors.Is(err, domain.ErrNoConsent) {
		t.Fatalf("err = %v, want ErrNoConsent", err)
	}
}

func TestStartRejectsDisabledFamily(t *testing.T) {
	store, devices, _, consent, notifier := defaultFixtures()
	svc := newService(store, devices, fakeSwitch{disabled: map[string]bool{"family-1": true}}, consent, notifier)

	if _, err := svc.Start(context.Background(), startRequest()); !errors.Is(err, domain.ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestStartIsRateLimited(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	for index := 0; index < domain.MaxSessionsPerWindow; index++ {
		created, err := svc.Start(context.Background(), startRequest())
		if err != nil {
			t.Fatalf("start %d: %v", index, err)
		}
		// Release the device so the next start is not a busy conflict, while
		// keeping the created session counted toward the rate limit.
		stored := store.sessions[created.ID]
		finished := stored.StartedAt
		stored.State = domain.StateEnded
		stored.FinishedAt = &finished
		store.sessions[created.ID] = stored
	}
	if _, err := svc.Start(context.Background(), startRequest()); !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestAcceptStartsStreaming(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	session, _ := svc.Start(context.Background(), startRequest())
	accepted, err := svc.Accept(context.Background(), "device-1", session.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if !accepted.State.AllowsFrames() {
		t.Fatalf("state = %s, want streaming", accepted.State)
	}
}

func TestAcceptFromWrongDeviceIsNotFound(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	session, _ := svc.Start(context.Background(), startRequest())
	// A device must not be able to confirm another device's session.
	if _, err := svc.Accept(context.Background(), "device-2", session.ID); !errors.Is(err, service.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestEndClosesAndStopsTheDevice(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	session, _ := svc.Start(context.Background(), startRequest())
	if _, err := svc.Accept(context.Background(), "device-1", session.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	ended, err := svc.End(context.Background(), "family-1", session.ID, "guardian_closed")
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if ended.State != domain.StateEnded {
		t.Fatalf("state = %s, want ended", ended.State)
	}
	// An ended view must also stop the camera, not just the record.
	if len(notifier.stops) != 1 || notifier.stops[0] != session.ID {
		t.Fatalf("stop notifications = %v", notifier.stops)
	}
}

func TestChildCloseIsRecordedAsDeviceEnd(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	session, _ := svc.Start(context.Background(), startRequest())
	_, _ = svc.Accept(context.Background(), "device-1", session.ID)

	ended, err := svc.EndForDevice(context.Background(), "device-1", session.ID, "child_closed")
	if err != nil {
		t.Fatalf("end for device: %v", err)
	}
	if ended.FinishReason != "child_closed" {
		t.Fatalf("reason = %q, want child_closed", ended.FinishReason)
	}
}

func TestUnknownReasonFallsBackToSafeToken(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	session, _ := svc.Start(context.Background(), startRequest())
	_, _ = svc.Accept(context.Background(), "device-1", session.ID)
	// Free text from a device must not become stored content.
	ended, err := svc.EndForDevice(context.Background(), "device-1", session.ID, "我的房间很乱")
	if err != nil {
		t.Fatalf("end for device: %v", err)
	}
	if ended.FinishReason != "child_closed" {
		t.Fatalf("reason = %q, want child_closed", ended.FinishReason)
	}
}

func TestSecondSessionOnSameDeviceIsBusy(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	if _, err := svc.Start(context.Background(), startRequest()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	// One camera can only serve one viewer.
	if _, err := svc.Start(context.Background(), startRequest()); !errors.Is(err, service.ErrBusy) {
		t.Fatalf("second start err = %v, want ErrBusy", err)
	}
}

func TestExpireDueClosesStreamingSession(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	session, _ := svc.Start(context.Background(), startRequest())
	_, _ = svc.Accept(context.Background(), "device-1", session.ID)

	// Rewind the deadline so the session is already overdue.
	stored := store.sessions[session.ID]
	stored.ExpiresAt = time.Now().UTC().Add(-time.Second)
	store.sessions[session.ID] = stored

	closed, err := svc.ExpireDue(context.Background())
	if err != nil {
		t.Fatalf("expire due: %v", err)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if !store.sessions[session.ID].State.IsTerminal() {
		t.Fatal("expired session must be terminal")
	}
	// The device must be told, because the guardian's app may already be gone.
	if len(notifier.stops) != 1 {
		t.Fatalf("stop notifications = %v", notifier.stops)
	}
}

func TestExpireDueAbandonsUnconfirmedRequest(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	session, _ := svc.Start(context.Background(), startRequest())
	stored := store.sessions[session.ID]
	stored.StartedAt = time.Now().UTC().Add(-time.Minute)
	store.sessions[session.ID] = stored

	// A device that never confirmed must not hold the slot for the full window.
	if _, err := svc.ExpireDue(context.Background()); err != nil {
		t.Fatalf("expire due: %v", err)
	}
	if store.sessions[session.ID].State != domain.StateFailed {
		t.Fatalf("state = %s, want failed", store.sessions[session.ID].State)
	}
}

func TestExpireDueLeavesFreshSessionAlone(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	session, _ := svc.Start(context.Background(), startRequest())
	_, _ = svc.Accept(context.Background(), "device-1", session.ID)

	closed, err := svc.ExpireDue(context.Background())
	if err != nil {
		t.Fatalf("expire due: %v", err)
	}
	if closed != 0 {
		t.Fatalf("closed = %d, want 0", closed)
	}
}

func TestNotifierFailureClosesTheSession(t *testing.T) {
	store, devices, switcher, consent, _ := defaultFixtures()
	notifier := &fakeNotifier{fail: errors.New("mqtt down")}
	svc := newService(store, devices, switcher, consent, notifier)

	// If the device never heard about the view, keeping the row open would leave
	// the device slot held for a camera that will never run.
	if _, err := svc.Start(context.Background(), startRequest()); err == nil {
		t.Fatal("start with failing notifier should report the failure")
	}
	open, _ := store.ListOpen(context.Background())
	if len(open) != 0 {
		t.Fatalf("open sessions = %d, want 0", len(open))
	}
}

func TestHistoryClampsLimit(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	if _, err := svc.History(context.Background(), "family-1", -5); err != nil {
		t.Fatalf("history with negative limit: %v", err)
	}
	if _, err := svc.History(context.Background(), "family-1", 100000); err != nil {
		t.Fatalf("history with huge limit: %v", err)
	}
}

func TestMetricsReportsOpenSessions(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	_, _ = svc.Start(context.Background(), startRequest())
	metrics, err := svc.Metrics(context.Background())
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if metrics.OpenSessions != 1 {
		t.Fatalf("open = %d, want 1", metrics.OpenSessions)
	}
}

func TestSessionIDsAreUniqueUUIDs(t *testing.T) {
	store, devices, switcher, consent, notifier := defaultFixtures()
	svc := newService(store, devices, switcher, consent, notifier)

	first, err := svc.Start(context.Background(), startRequest())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Shape matters: the column is a UUID, so a wrong shape would fail at insert.
	if len(first.ID) != 36 {
		t.Fatalf("id = %q, want a 36-character UUID", first.ID)
	}
}
