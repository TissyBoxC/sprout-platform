package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/domain"
)

func newSession(t *testing.T) domain.Session {
	t.Helper()
	started := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	return domain.Session{
		ID:         "session-1",
		FamilyID:   "family-1",
		DeviceID:   "device-1",
		GuardianID: "guardian-1",
		State:      domain.StateRequested,
		StartedAt:  started,
		ExpiresAt:  started.Add(domain.MaxViewDuration),
	}
}

func TestOnlyStreamingAllowsFrames(t *testing.T) {
	// This is the single gate that keeps a ringing-but-unconfirmed camera from
	// sending pixels, so every state is checked directly.
	cases := map[domain.ViewState]bool{
		domain.StateRequested: false,
		domain.StateStreaming: true,
		domain.StateEnded:     false,
		domain.StateDeclined:  false,
		domain.StateFailed:    false,
	}
	for state, want := range cases {
		if got := state.AllowsFrames(); got != want {
			t.Errorf("AllowsFrames(%s) = %v, want %v", state, got, want)
		}
	}
}

func TestRequestedSessionCannotStreamUntilAccepted(t *testing.T) {
	session := newSession(t)

	// A guardian asking is not a camera turning on. The device must confirm.
	if session.State.AllowsFrames() {
		t.Fatal("a requested session must not allow frames")
	}
}

func TestAcceptMovesRequestedToStreaming(t *testing.T) {
	session := newSession(t)
	if err := session.Accept(session.StartedAt.Add(time.Second)); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if session.State != domain.StateStreaming {
		t.Fatalf("state = %s, want streaming", session.State)
	}
	if !session.State.AllowsFrames() {
		t.Fatal("streaming must allow frames")
	}
}

func TestAcceptRejectsAnExpiredRequest(t *testing.T) {
	session := newSession(t)
	// A device that confirms after the deadline would otherwise start a camera
	// nobody is waiting for.
	err := session.Accept(session.ExpiresAt.Add(time.Second))
	if !errors.Is(err, domain.ErrNotStreaming) {
		t.Fatalf("accept after deadline err = %v, want ErrNotStreaming", err)
	}
	if session.State != domain.StateRequested {
		t.Fatalf("state = %s, want requested", session.State)
	}
}

func TestAcceptRequiresRequestedState(t *testing.T) {
	session := newSession(t)
	if err := session.Accept(session.StartedAt); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	// A duplicate acknowledgement must not disturb a running session.
	if err := session.Accept(session.StartedAt); !errors.Is(err, domain.ErrNotRequested) {
		t.Fatalf("second accept err = %v, want ErrNotRequested", err)
	}
}

func TestEndKeepsDuration(t *testing.T) {
	session := newSession(t)
	_ = session.Accept(session.StartedAt)
	finished := session.StartedAt.Add(90 * time.Second)

	if err := session.End(finished, "guardian_closed"); err != nil {
		t.Fatalf("end: %v", err)
	}
	if session.State != domain.StateEnded {
		t.Fatalf("state = %s, want ended", session.State)
	}
	if got := session.Duration(finished); got != 90*time.Second {
		t.Fatalf("duration = %v, want 90s", got)
	}
	// A finished view must never carry frames again.
	if session.State.AllowsFrames() {
		t.Fatal("an ended session must not allow frames")
	}
}

func TestEndIsRejectedOnTerminalSession(t *testing.T) {
	session := newSession(t)
	_ = session.Accept(session.StartedAt)
	if err := session.End(session.StartedAt, "guardian_closed"); err != nil {
		t.Fatalf("end: %v", err)
	}
	if err := session.End(session.StartedAt, "guardian_closed"); !errors.Is(err, domain.ErrFinished) {
		t.Fatalf("second end err = %v, want ErrFinished", err)
	}
}

func TestDeclineAndFailAreTerminal(t *testing.T) {
	declined := newSession(t)
	if err := declined.Decline(declined.StartedAt, "child_closed"); err != nil {
		t.Fatalf("decline: %v", err)
	}
	if declined.State != domain.StateDeclined || !declined.State.IsTerminal() {
		t.Fatalf("state = %s, want terminal declined", declined.State)
	}

	failed := newSession(t)
	if err := failed.Fail(failed.StartedAt, "camera_unavailable"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if failed.State != domain.StateFailed || !failed.State.IsTerminal() {
		t.Fatalf("state = %s, want terminal failed", failed.State)
	}
}

func TestExpireDistinguishesStartedFromNeverStarted(t *testing.T) {
	// A session nobody ever saw should not be reported as a completed look.
	running := newSession(t)
	_ = running.Accept(running.StartedAt)
	if err := running.Expire(running.ExpiresAt); err != nil {
		t.Fatalf("expire streaming: %v", err)
	}
	if running.State != domain.StateEnded || running.FinishReason != "expired" {
		t.Fatalf("streaming expire = %s/%s, want ended/expired", running.State, running.FinishReason)
	}

	neverStarted := newSession(t)
	if err := neverStarted.Expire(neverStarted.ExpiresAt); err != nil {
		t.Fatalf("expire requested: %v", err)
	}
	if neverStarted.State != domain.StateFailed || neverStarted.FinishReason != "request_timeout" {
		t.Fatalf("requested expire = %s/%s, want failed/request_timeout",
			neverStarted.State, neverStarted.FinishReason)
	}
}

func TestExpireRejectsTerminalSession(t *testing.T) {
	session := newSession(t)
	_ = session.Accept(session.StartedAt)
	_ = session.End(session.StartedAt, "guardian_closed")
	if err := session.Expire(session.ExpiresAt); !errors.Is(err, domain.ErrFinished) {
		t.Fatalf("expire terminal err = %v, want ErrFinished", err)
	}
}

func TestFinishReasonIsBounded(t *testing.T) {
	session := newSession(t)
	_ = session.Accept(session.StartedAt)
	long := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := session.End(session.StartedAt, long); err != nil {
		t.Fatalf("end: %v", err)
	}
	// Reasons come from device input, so they must not become free-text storage.
	if len(session.FinishReason) > 32 {
		t.Fatalf("reason length = %d, want <= 32", len(session.FinishReason))
	}
}

func TestValidateRequiresConsent(t *testing.T) {
	base := domain.Request{
		FamilyID:   "family-1",
		DeviceID:   "device-1",
		GuardianID: "guardian-1",
	}

	if err := base.Validate(); !errors.Is(err, domain.ErrNoConsent) {
		t.Fatalf("validate without consent err = %v, want ErrNoConsent", err)
	}

	base.ConsentGranted = true
	if err := base.Validate(); err != nil {
		t.Fatalf("validate with consent: %v", err)
	}
}

func TestValidateRejectsIncompleteRequest(t *testing.T) {
	cases := []domain.Request{
		{DeviceID: "device-1", GuardianID: "g", ConsentGranted: true},
		{FamilyID: "family-1", GuardianID: "g", ConsentGranted: true},
		{FamilyID: "family-1", DeviceID: "device-1", ConsentGranted: true},
	}
	for _, request := range cases {
		if err := request.Validate(); !errors.Is(err, domain.ErrInvalidRequest) {
			t.Errorf("validate(%+v) err = %v, want ErrInvalidRequest", request, err)
		}
	}
}

func TestDurationIsClampedBeforeStart(t *testing.T) {
	session := newSession(t)
	// A skewed clock must not produce a negative duration in the UI.
	if got := session.Duration(session.StartedAt.Add(-time.Hour)); got != 0 {
		t.Fatalf("duration before start = %v, want 0", got)
	}
}
