package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/service"
)

// fakeStore keeps calls in memory and honours the same sentinels a real store
// must return, so the service tests exercise the real orchestration rules.
type fakeStore struct {
	calls     map[string]domain.Call
	order     []string
	startedAt map[string][]time.Time
	failNext  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		calls:     make(map[string]domain.Call),
		startedAt: make(map[string][]time.Time),
	}
}

func (store *fakeStore) CreateRinging(
	_ context.Context,
	callID, familyID, deviceID, childID string,
	startedAt time.Time,
) (domain.Call, error) {
	if store.failNext != nil {
		err := store.failNext
		store.failNext = nil
		return domain.Call{}, err
	}
	for _, existing := range store.calls {
		if existing.DeviceID == deviceID && !existing.State.IsTerminal() {
			return domain.Call{}, service.StoreSentinelBusy
		}
	}
	call := domain.Call{
		ID:        callID,
		FamilyID:  familyID,
		DeviceID:  deviceID,
		ChildID:   childID,
		State:     domain.StateRinging,
		StartedAt: startedAt,
	}
	store.calls[callID] = call
	store.order = append(store.order, callID)
	store.startedAt[familyID] = append(store.startedAt[familyID], startedAt)
	return call, nil
}

func (store *fakeStore) Get(_ context.Context, familyID, callID string) (domain.Call, error) {
	call, ok := store.calls[callID]
	if !ok || call.FamilyID != familyID {
		return domain.Call{}, service.StoreSentinelNotFound
	}
	return call, nil
}

func (store *fakeStore) GetForDevice(_ context.Context, deviceID, callID string) (domain.Call, error) {
	call, ok := store.calls[callID]
	if !ok || call.DeviceID != deviceID {
		return domain.Call{}, service.StoreSentinelNotFound
	}
	return call, nil
}

func (store *fakeStore) Transition(
	_ context.Context,
	call domain.Call,
	expected domain.CallState,
) (domain.Call, error) {
	current, ok := store.calls[call.ID]
	if !ok {
		return domain.Call{}, service.StoreSentinelNotFound
	}
	if current.State != expected {
		// Another actor moved the call first.
		return domain.Call{}, service.StoreSentinelBusy
	}
	store.calls[call.ID] = call
	return call, nil
}

func (store *fakeStore) CountStartedSince(
	_ context.Context,
	familyID string,
	since time.Time,
) (int, error) {
	count := 0
	for _, startedAt := range store.startedAt[familyID] {
		if !startedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (store *fakeStore) ListOpen(_ context.Context) ([]domain.Call, error) {
	open := make([]domain.Call, 0, len(store.order))
	for _, callID := range store.order {
		if call, ok := store.calls[callID]; ok && !call.State.IsTerminal() {
			open = append(open, call)
		}
	}
	return open, nil
}

func (store *fakeStore) History(_ context.Context, familyID string, limit int) ([]domain.Call, error) {
	result := make([]domain.Call, 0, limit)
	for index := len(store.order) - 1; index >= 0 && len(result) < limit; index-- {
		call := store.calls[store.order[index]]
		if call.FamilyID == familyID {
			result = append(result, call)
		}
	}
	return result, nil
}

func (store *fakeStore) CountOpen(_ context.Context) (int, error) {
	count := 0
	for _, call := range store.calls {
		if !call.State.IsTerminal() {
			count++
		}
	}
	return count, nil
}

func (store *fakeStore) CountCallsSince(_ context.Context, since time.Time) (int, error) {
	count := 0
	for _, call := range store.calls {
		if !call.StartedAt.Before(since) {
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

type fakeSwitch struct {
	disabled map[string]bool
	err      error
}

func (switcher fakeSwitch) IntercomEnabled(_ context.Context, familyID string) (bool, error) {
	if switcher.err != nil {
		return false, switcher.err
	}
	return !switcher.disabled[familyID], nil
}

// fakeNotifier records what the platform told the device.
type fakeNotifier struct {
	rings    []string
	ends     []string
	failWith error
}

func (notifier *fakeNotifier) NotifyIntercomCall(_ context.Context, call domain.Call) error {
	if notifier.failWith != nil {
		return notifier.failWith
	}
	notifier.rings = append(notifier.rings, call.ID)
	return nil
}

func (notifier *fakeNotifier) NotifyIntercomCallEnded(_ context.Context, call domain.Call) error {
	if notifier.failWith != nil {
		return notifier.failWith
	}
	notifier.ends = append(notifier.ends, call.ID)
	return nil
}

func newService(store *fakeStore, devices fakeDevices, switcher fakeSwitch) *service.Service {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return service.NewService(service.Options{
		Store:    store,
		Devices:  devices,
		Switches: switcher,
		Logger:   logger,
	})
}

func defaultFixtures() (*fakeStore, fakeDevices, fakeSwitch) {
	return newFakeStore(),
		fakeDevices{
			owner: map[string]string{"device-1": "family-1", "device-2": "family-2"},
			child: map[string]string{"device-1": "child-1"},
		},
		fakeSwitch{disabled: map[string]bool{}}
}

func TestStartNotifiesTheDevice(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	notifier := &fakeNotifier{}
	svc := service.NewService(service.Options{
		Store:    store,
		Devices:  devices,
		Switches: switcher,
		Notifier: notifier,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	call, err := svc.Start(context.Background(), domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// The device only rings because it was told; a silent start is a dead call.
	if len(notifier.rings) != 1 || notifier.rings[0] != call.ID {
		t.Fatalf("ring notifications = %v, want [%s]", notifier.rings, call.ID)
	}
}

func TestNotifyFailureDoesNotFailTheCall(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := service.NewService(service.Options{
		Store:    store,
		Devices:  devices,
		Switches: switcher,
		Notifier: &fakeNotifier{failWith: errors.New("broker down")},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	// A device that cannot be reached must not turn the guardian's call into an
	// error: the call is stored and still rings when the device asks.
	call, err := svc.Start(context.Background(), domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start with failing notifier: %v", err)
	}
	if call.State != domain.StateRinging {
		t.Fatalf("state = %s, want ringing", call.State)
	}
}

func TestHangUpNotifiesTheDeviceToStopRinging(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	notifier := &fakeNotifier{}
	svc := service.NewService(service.Options{
		Store:    store,
		Devices:  devices,
		Switches: switcher,
		Notifier: notifier,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	call, err := svc.Start(context.Background(), domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// A ringing call is cancelled, not hung up: the two are distinct transitions.
	if _, err := svc.Cancel(context.Background(), "family-1", call.ID); err != nil {
		t.Fatalf("hangup: %v", err)
	}
	// Without this the prompt keeps sounding on the device until its own timeout.
	if len(notifier.ends) != 1 || notifier.ends[0] != call.ID {
		t.Fatalf("end notifications = %v, want [%s]", notifier.ends, call.ID)
	}
}

func TestStartRequiresOwnDevice(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)

	// Another family's device must look exactly like a missing device.
	_, err := svc.Start(context.Background(), domain.Request{FamilyID: "family-1", DeviceID: "device-2"})
	if !errors.Is(err, service.ErrDeviceNotFound) {
		t.Fatalf("cross-family start err = %v, want ErrDeviceNotFound", err)
	}
	_, err = svc.Start(context.Background(), domain.Request{FamilyID: "family-1", DeviceID: "unknown"})
	if !errors.Is(err, service.ErrDeviceNotFound) {
		t.Fatalf("unknown device start err = %v, want ErrDeviceNotFound", err)
	}
}

func TestStartRejectsDisabledIntercom(t *testing.T) {
	store, devices, _ := defaultFixtures()
	svc := newService(store, devices, fakeSwitch{disabled: map[string]bool{"family-1": true}})

	_, err := svc.Start(context.Background(), domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if !errors.Is(err, domain.ErrIntercomDisabled) {
		t.Fatalf("disabled start err = %v, want ErrIntercomDisabled", err)
	}
	// Nothing may be left behind when the call was refused.
	if len(store.calls) != 0 {
		t.Fatalf("disabled start created %d calls, want 0", len(store.calls))
	}
}

func TestStartIsRateLimited(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	for index := 0; index < domain.MaxCallsPerWindow; index++ {
		call, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
		if err != nil {
			t.Fatalf("start %d: %v", index, err)
		}
		// Close each call so the busy check does not hide the rate limit.
		if _, err := svc.Cancel(ctx, "family-1", call.ID); err != nil {
			t.Fatalf("cancel %d: %v", index, err)
		}
	}

	_, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("start past limit err = %v, want ErrRateLimited", err)
	}
}

func TestStartFillsChildFromDevice(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)

	call, err := svc.Start(context.Background(), domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if call.ChildID != "child-1" {
		t.Fatalf("child = %q, want child-1", call.ChildID)
	}
	if call.State != domain.StateRinging {
		t.Fatalf("state = %q, want ringing", call.State)
	}
	if call.State.AllowsAudio() {
		t.Fatal("a fresh call must not carry audio before the child accepts")
	}
}

func TestStartRefusesBusyDevice(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	if _, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"}); err != nil {
		t.Fatalf("first start: %v", err)
	}
	_, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if !errors.Is(err, service.ErrBusy) {
		t.Fatalf("second start err = %v, want ErrBusy", err)
	}
}

func TestAcceptActivatesCall(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	call, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	accepted, err := svc.Accept(ctx, "device-1", call.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if accepted.State != domain.StateActive {
		t.Fatalf("state = %q, want active", accepted.State)
	}
	if !accepted.State.AllowsAudio() {
		t.Fatal("accepted call must allow audio")
	}
}

func TestAcceptRejectedByWrongDevice(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	call, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Only the called device may accept; another device must not be able to
	// answer someone else's call.
	if _, err := svc.Accept(ctx, "device-2", call.ID); !errors.Is(err, service.ErrCallNotFound) {
		t.Fatalf("foreign accept err = %v, want ErrCallNotFound", err)
	}
}

func TestDoubleAcceptIsConflict(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	call, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.Accept(ctx, "device-1", call.ID); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if _, err := svc.Accept(ctx, "device-1", call.ID); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("second accept err = %v, want ErrConflict", err)
	}
}

func TestDeclineEndsRingWithoutAudio(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	call, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	declined, err := svc.Decline(ctx, "device-1", call.ID)
	if err != nil {
		t.Fatalf("decline: %v", err)
	}
	if declined.State != domain.StateDeclined {
		t.Fatalf("state = %q, want declined", declined.State)
	}
	if declined.State.AllowsAudio() {
		t.Fatal("declined call must never carry audio")
	}
}

func TestHangUpEndsActiveCall(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	call, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.Accept(ctx, "device-1", call.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	ended, err := svc.HangUp(ctx, "family-1", call.ID, "guardian_hangup")
	if err != nil {
		t.Fatalf("hang up: %v", err)
	}
	if ended.State != domain.StateEnded {
		t.Fatalf("state = %q, want ended", ended.State)
	}
	// After hangup the device may be called again.
	if _, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"}); err != nil {
		t.Fatalf("start after hangup: %v", err)
	}
}

func TestHangUpRequiresFamilyOwnership(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	call, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.Accept(ctx, "device-1", call.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	// A guardian from another family must not be able to end this call.
	if _, err := svc.HangUp(ctx, "family-2", call.ID, "x"); !errors.Is(err, service.ErrCallNotFound) {
		t.Fatalf("cross-family hangup err = %v, want ErrCallNotFound", err)
	}
}

func TestExpireDueClosesForgottenCalls(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	call, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Backdate the ring so its deadline has passed.
	stored := store.calls[call.ID]
	stored.StartedAt = time.Now().UTC().Add(-2 * domain.RingTimeout)
	store.calls[call.ID] = stored

	closed, err := svc.ExpireDue(ctx)
	if err != nil {
		t.Fatalf("expire due: %v", err)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if got := store.calls[call.ID].State; got != domain.StateMissed {
		t.Fatalf("state = %q, want missed", got)
	}
}

func TestExpireDueLeavesFreshCallsAlone(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	if _, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	closed, err := svc.ExpireDue(ctx)
	if err != nil {
		t.Fatalf("expire due: %v", err)
	}
	if closed != 0 {
		t.Fatalf("closed = %d, want 0", closed)
	}
}

func TestExpireDueEndsOverlongActiveCall(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	call, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.Accept(ctx, "device-1", call.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	acceptedAt := time.Now().UTC().Add(-domain.MaxCallDuration - time.Minute)
	stored := store.calls[call.ID]
	stored.AcceptedAt = &acceptedAt
	store.calls[call.ID] = stored

	closed, err := svc.ExpireDue(ctx)
	if err != nil {
		t.Fatalf("expire due: %v", err)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	// A call that hit the cap is still a real conversation.
	if got := store.calls[call.ID].State; got != domain.StateEnded {
		t.Fatalf("state = %q, want ended", got)
	}
}

func TestHistoryReturnsNewestFirst(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	first, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	if _, err := svc.Cancel(ctx, "family-1", first.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	second, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("start second: %v", err)
	}

	history, err := svc.History(ctx, "family-1", 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}
	if history[0].ID != second.ID {
		t.Fatalf("newest first violated: first entry = %q", history[0].ID)
	}
}

func TestReadMetricsCountsOpenCalls(t *testing.T) {
	store, devices, switcher := defaultFixtures()
	svc := newService(store, devices, switcher)
	ctx := context.Background()

	if _, err := svc.Start(ctx, domain.Request{FamilyID: "family-1", DeviceID: "device-1"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	metrics, err := svc.ReadMetrics(ctx)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if metrics.OpenCalls != 1 {
		t.Fatalf("open = %d, want 1", metrics.OpenCalls)
	}
	if metrics.CallsLast24 != 1 {
		t.Fatalf("last24 = %d, want 1", metrics.CallsLast24)
	}
}
