package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/domain"
)

var baseTime = time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)

func newCall(state domain.CallState) domain.Call {
	return domain.Call{
		ID:        "call-1",
		FamilyID:  "family-1",
		DeviceID:  "device-1",
		ChildID:   "child-1",
		State:     state,
		StartedAt: baseTime,
	}
}

func TestOnlyActiveCallsCarryAudio(t *testing.T) {
	// The whole privacy model rests on this: ringing means the child has not
	// consented yet, so audio must be refused at the state level.
	cases := map[domain.CallState]bool{
		domain.StateRinging:   false,
		domain.StateActive:    true,
		domain.StateEnded:     false,
		domain.StateMissed:    false,
		domain.StateDeclined:  false,
		domain.StateCancelled: false,
	}
	for state, wantAudio := range cases {
		if got := state.AllowsAudio(); got != wantAudio {
			t.Errorf("state %q AllowsAudio() = %v, want %v", state, got, wantAudio)
		}
	}
}

func TestAcceptMovesRingingToActive(t *testing.T) {
	call := newCall(domain.StateRinging)
	acceptedAt := baseTime.Add(5 * time.Second)

	if err := call.Accept(acceptedAt); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if call.State != domain.StateActive {
		t.Fatalf("state = %q, want active", call.State)
	}
	if call.AcceptedAt == nil || !call.AcceptedAt.Equal(acceptedAt) {
		t.Fatalf("acceptedAt = %v, want %v", call.AcceptedAt, acceptedAt)
	}
	if !call.State.AllowsAudio() {
		t.Fatal("accepted call must allow audio")
	}
}

func TestAcceptRequiresRinging(t *testing.T) {
	for _, state := range []domain.CallState{domain.StateActive, domain.StateEnded} {
		call := newCall(state)
		err := call.Accept(baseTime.Add(time.Second))
		if state == domain.StateActive {
			if !errors.Is(err, domain.ErrNotRinging) {
				t.Fatalf("state %q: err = %v, want ErrNotRinging", state, err)
			}
			continue
		}
		if !errors.Is(err, domain.ErrCallFinished) {
			t.Fatalf("state %q: err = %v, want ErrCallFinished", state, err)
		}
	}
}

func TestDeclineOnlyAppliesToRinging(t *testing.T) {
	call := newCall(domain.StateRinging)
	if err := call.Decline(baseTime.Add(2*time.Second), "child_declined"); err != nil {
		t.Fatalf("decline: %v", err)
	}
	if call.State != domain.StateDeclined {
		t.Fatalf("state = %q, want declined", call.State)
	}
	if call.State.AllowsAudio() {
		t.Fatal("declined call must not allow audio")
	}

	// Declining after accept must fail: the two are different endings.
	active := newCall(domain.StateActive)
	if err := active.Decline(baseTime.Add(time.Second), "child_declined"); !errors.Is(err, domain.ErrNotRinging) {
		t.Fatalf("decline active err = %v, want ErrNotRinging", err)
	}
}

func TestHangUpRequiresActive(t *testing.T) {
	active := newCall(domain.StateActive)
	acceptedAt := baseTime.Add(3 * time.Second)
	active.AcceptedAt = &acceptedAt

	if err := active.HangUp(baseTime.Add(63*time.Second), "guardian_hangup"); err != nil {
		t.Fatalf("hang up: %v", err)
	}
	if active.State != domain.StateEnded {
		t.Fatalf("state = %q, want ended", active.State)
	}
	if got := active.Duration(baseTime.Add(5 * time.Minute)); got != time.Minute {
		t.Fatalf("duration = %v, want 1m", got)
	}

	// Hanging up a call that was never accepted is a cancel, not a hang up.
	ringing := newCall(domain.StateRinging)
	if err := ringing.HangUp(baseTime.Add(time.Second), "guardian_hangup"); !errors.Is(err, domain.ErrNotActive) {
		t.Fatalf("hang up ringing err = %v, want ErrNotActive", err)
	}
}

func TestCancelOnlyAppliesToRinging(t *testing.T) {
	call := newCall(domain.StateRinging)
	if err := call.Cancel(baseTime.Add(4 * time.Second)); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if call.State != domain.StateCancelled {
		t.Fatalf("state = %q, want cancelled", call.State)
	}

	active := newCall(domain.StateActive)
	if err := active.Cancel(baseTime.Add(time.Second)); !errors.Is(err, domain.ErrNotRinging) {
		t.Fatalf("cancel active err = %v, want ErrNotRinging", err)
	}
}

func TestRingTimeoutMakesCallMissed(t *testing.T) {
	call := newCall(domain.StateRinging)

	// Still inside the window: nothing expires yet.
	if err := call.Expire(baseTime.Add(domain.RingTimeout - time.Second)); !errors.Is(err, domain.ErrNotActive) {
		t.Fatalf("early expire err = %v, want ErrNotActive", err)
	}
	if err := call.Expire(baseTime.Add(domain.RingTimeout)); err != nil {
		t.Fatalf("expire at deadline: %v", err)
	}
	if call.State != domain.StateMissed {
		t.Fatalf("state = %q, want missed", call.State)
	}
	if call.Duration(baseTime.Add(time.Hour)) != 0 {
		t.Fatal("a missed call has no talk time")
	}
}

func TestActiveCallExpiresAtMaxDuration(t *testing.T) {
	call := newCall(domain.StateActive)
	acceptedAt := baseTime.Add(time.Second)
	call.AcceptedAt = &acceptedAt

	if err := call.Expire(acceptedAt.Add(domain.MaxCallDuration - time.Second)); !errors.Is(err, domain.ErrNotActive) {
		t.Fatalf("early expire err = %v, want ErrNotActive", err)
	}
	if err := call.Expire(acceptedAt.Add(domain.MaxCallDuration)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if call.State != domain.StateEnded {
		t.Fatalf("state = %q, want ended", call.State)
	}
	// A long call that hit the cap is still a real conversation, so it keeps
	// its talk time instead of being reported as zero.
	if got := call.Duration(baseTime.Add(time.Hour)); got != domain.MaxCallDuration {
		t.Fatalf("duration = %v, want %v", got, domain.MaxCallDuration)
	}
}

func TestTerminalStatesCannotChange(t *testing.T) {
	terminal := []domain.CallState{
		domain.StateEnded,
		domain.StateMissed,
		domain.StateDeclined,
		domain.StateCancelled,
	}
	for _, state := range terminal {
		if !state.IsTerminal() {
			t.Fatalf("state %q should be terminal", state)
		}
		call := newCall(state)
		now := baseTime.Add(time.Hour)
		if err := call.Accept(now); !errors.Is(err, domain.ErrCallFinished) {
			t.Errorf("state %q accept err = %v, want ErrCallFinished", state, err)
		}
		if err := call.HangUp(now, "x"); !errors.Is(err, domain.ErrCallFinished) {
			t.Errorf("state %q hangup err = %v, want ErrCallFinished", state, err)
		}
		if err := call.Cancel(now); !errors.Is(err, domain.ErrCallFinished) {
			t.Errorf("state %q cancel err = %v, want ErrCallFinished", state, err)
		}
		if err := call.Expire(now); !errors.Is(err, domain.ErrCallFinished) {
			t.Errorf("state %q expire err = %v, want ErrCallFinished", state, err)
		}
	}
}

func TestDeadlineDependsOnState(t *testing.T) {
	ringing := newCall(domain.StateRinging)
	if got := ringing.Deadline(); !got.Equal(baseTime.Add(domain.RingTimeout)) {
		t.Fatalf("ringing deadline = %v", got)
	}

	active := newCall(domain.StateActive)
	acceptedAt := baseTime.Add(10 * time.Second)
	active.AcceptedAt = &acceptedAt
	if got := active.Deadline(); !got.Equal(acceptedAt.Add(domain.MaxCallDuration)) {
		t.Fatalf("active deadline = %v", got)
	}
}

func TestRequestRequiresIdentifiers(t *testing.T) {
	if err := (domain.Request{FamilyID: "f", DeviceID: "d"}).Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := (domain.Request{DeviceID: "d"}).Validate(); !errors.Is(err, domain.ErrCrossFamily) {
		t.Fatalf("missing family err = %v, want ErrCrossFamily", err)
	}
	if err := (domain.Request{FamilyID: "f"}).Validate(); !errors.Is(err, domain.ErrCrossFamily) {
		t.Fatalf("missing device err = %v, want ErrCrossFamily", err)
	}
}
