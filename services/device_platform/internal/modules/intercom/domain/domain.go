// Package domain defines voice intercom call rules between a guardian and a
// device.
//
// A call is a guardian-initiated, child-accepted, time-bounded audio session.
// The state machine here is the single place that decides whether audio may
// flow: audio may only be relayed while the call is active, and a call only
// becomes active after the device reports that the child pressed accept.
//
// 与语音对话不同，对讲是家长单向发起、孩子必须在设备上主动接受才能建立的声音
// 通道。任何「未接受就传音频」的路径都会变成对孩子的静默监听，因此接受这一步
// 在领域层被建模为独立的、不可跳过的事件。
package domain

import (
	"errors"
	"time"
)

// CallState is the lifecycle position of one intercom call.
type CallState string

const (
	// StateRinging means the device is prompting the child. No audio flows.
	StateRinging CallState = "ringing"
	// StateActive means the child accepted and audio may be relayed.
	StateActive CallState = "active"
	// StateEnded means the call finished normally after being active.
	StateEnded CallState = "ended"
	// StateMissed means nobody accepted before the ring timeout.
	StateMissed CallState = "missed"
	// StateDeclined means the child actively refused the call.
	StateDeclined CallState = "declined"
	// StateCancelled means the guardian gave up before the device answered.
	StateCancelled CallState = "cancelled"
)

// IsTerminal reports whether the state can never transition again.
func (state CallState) IsTerminal() bool {
	switch state {
	case StateEnded, StateMissed, StateDeclined, StateCancelled:
		return true
	default:
		return false
	}
}

// AllowsAudio reports whether audio may be relayed in this state.
//
// Only an accepted call carries audio. This is the enforcement point for the
// "never capture without consent" baseline, so callers must not re-derive it.
func (state CallState) AllowsAudio() bool {
	return state == StateActive
}

// Call limits applied to a single call and to a guardian's calling rate.
const (
	// RingTimeout is how long the device may prompt before the call is missed.
	RingTimeout = 45 * time.Second
	// MaxCallDuration bounds one call so the channel cannot stay open indefinitely.
	MaxCallDuration = 10 * time.Minute
	// MaxCallsPerWindow bounds how many calls one guardian may start.
	MaxCallsPerWindow = 10
	// CallRateWindow is the rolling window for MaxCallsPerWindow.
	CallRateWindow = time.Hour
)

var (
	// ErrNotRinging is returned when accepting a call that is not being offered.
	ErrNotRinging = errors.New("call is not ringing")
	// ErrNotActive is returned when audio is requested for a non-active call.
	ErrNotActive = errors.New("call is not active")
	// ErrCallFinished is returned when a terminal call is mutated.
	ErrCallFinished = errors.New("call already finished")
	// ErrRateLimited is returned when a guardian starts calls too often.
	ErrRateLimited = errors.New("too many calls in the window")
	// ErrIntercomDisabled is returned when the guardian turned intercom off.
	ErrIntercomDisabled = errors.New("intercom is disabled for this family")
	// ErrCrossFamily is returned when the device and guardian are unrelated.
	//
	// It is deliberately indistinguishable from "not found" at the transport
	// layer so a caller cannot enumerate other families' devices.
	ErrCrossFamily = errors.New("device does not belong to this family")
)

// Call is one intercom call.
type Call struct {
	ID          string
	FamilyID    string
	DeviceID    string
	ChildID     string
	State       CallState
	StartedAt   time.Time
	AcceptedAt  *time.Time
	FinishedAt  *time.Time
	// DeclineReason is set only for terminated calls, and never contains audio
	// or personal data.
	DeclineReason string
}

// Duration is the measured talk time of a call, zero until it was accepted.
func (call Call) Duration(now time.Time) time.Duration {
	if call.AcceptedAt == nil {
		return 0
	}
	end := now
	if call.FinishedAt != nil {
		end = *call.FinishedAt
	}
	if end.Before(*call.AcceptedAt) {
		return 0
	}
	return end.Sub(*call.AcceptedAt)
}

// Deadline is the instant at which the call must be closed by itself.
//
// A ringing call is bounded by the ring timeout and an active call by the
// maximum talk time, so a forgotten or abandoned call cannot hold the channel
// open.
func (call Call) Deadline() time.Time {
	if call.State == StateActive && call.AcceptedAt != nil {
		return call.AcceptedAt.Add(MaxCallDuration)
	}
	return call.StartedAt.Add(RingTimeout)
}

// Accept moves a ringing call to active.
func (call *Call) Accept(now time.Time) error {
	if call.State.IsTerminal() {
		return ErrCallFinished
	}
	if call.State != StateRinging {
		return ErrNotRinging
	}
	acceptedAt := now
	call.State = StateActive
	call.AcceptedAt = &acceptedAt
	return nil
}

// Decline moves a ringing call to declined.
func (call *Call) Decline(now time.Time, reason string) error {
	if call.State.IsTerminal() {
		return ErrCallFinished
	}
	if call.State != StateRinging {
		// An accepted call is hung up rather than declined; conflating the two
		// would let a decline silently report success after audio already flowed.
		return ErrNotRinging
	}
	return call.finish(now, StateDeclined, reason)
}

// HangUp closes an active call.
func (call *Call) HangUp(now time.Time, reason string) error {
	if call.State.IsTerminal() {
		return ErrCallFinished
	}
	if call.State != StateActive {
		return ErrNotActive
	}
	return call.finish(now, StateEnded, reason)
}

// Cancel closes a ringing call the child never answered.
func (call *Call) Cancel(now time.Time) error {
	if call.State.IsTerminal() {
		return ErrCallFinished
	}
	if call.State != StateRinging {
		return ErrNotRinging
	}
	return call.finish(now, StateCancelled, "guardian_cancelled")
}

// Expire closes a call that ran past its deadline.
//
// A ringing call becomes missed and an active call becomes ended, which keeps
// "nobody answered" distinguishable from "we talked and the limit hit".
func (call *Call) Expire(now time.Time) error {
	if call.State.IsTerminal() {
		return ErrCallFinished
	}
	if now.Before(call.Deadline()) {
		return ErrNotActive
	}
	switch call.State {
	case StateRinging:
		return call.finish(now, StateMissed, "ring_timeout")
	case StateActive:
		return call.finish(now, StateEnded, "max_duration")
	default:
		return ErrNotActive
	}
}

func (call *Call) finish(now time.Time, state CallState, reason string) error {
	finishedAt := now
	call.State = state
	call.FinishedAt = &finishedAt
	call.DeclineReason = reason
	return nil
}

// Request is a guardian's request to call a device.
type Request struct {
	FamilyID string
	DeviceID string
	ChildID  string
}

// Validate reports whether the request carries the identifiers a call needs.
func (request Request) Validate() error {
	if request.FamilyID == "" || request.DeviceID == "" {
		return ErrCrossFamily
	}
	return nil
}
