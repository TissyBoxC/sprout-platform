// Package domain defines the live care-view session a guardian opens to look at
// a camera the family owns.
//
// A care view is not a recording. Only the envelope of a session is persisted:
// who looked, at which device, for how long, and why it ended. Frames are relayed
// live and never stored, so there is no archive of a child's room to leak, replay,
// or hand over later. Anything that would make a frame durable belongs outside
// this feature.
//
// Two rules shape every transition here. A guardian may only watch a device the
// family owns, and the device must be showing its own visible and audible
// indication while a session streams. The platform cannot verify that indication
// itself; it enforces the device contract by refusing to authorize a stream the
// device never acknowledged.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidRequest means the request could not address a real device.
	ErrInvalidRequest = errors.New("invalid care view request")
	// ErrDisabled means the family has not turned care view on.
	ErrDisabled = errors.New("care view is not enabled for this family")
	// ErrRateLimited means the family opened views too often.
	ErrRateLimited = errors.New("care view rate limit reached")
	// ErrCrossFamily means the device belongs to another family.
	ErrCrossFamily = errors.New("care view device belongs to another family")
	// ErrNotRequested means the device has not accepted the session yet, so no
	// frame may flow.
	ErrNotRequested = errors.New("care view session has not been accepted")
	// ErrNotStreaming means the session is no longer carrying frames.
	ErrNotStreaming = errors.New("care view session is not streaming")
	// ErrFinished means the session already reached a terminal state.
	ErrFinished = errors.New("care view session already finished")
	// ErrNoConsent means the guardian has not granted image consent.
	ErrNoConsent = errors.New("guardian image consent is required for care view")
)

// ViewState is the lifecycle position of one care view.
type ViewState string

const (
	// StateRequested is the moment after a guardian asks but before the device
	// confirms it is showing its indication. No frame may flow in this state.
	StateRequested ViewState = "requested"
	// StateStreaming is the only state that carries frames.
	StateStreaming ViewState = "streaming"
	// StateEnded means the view ran and was closed, whether by the guardian, the
	// child, or the deadline.
	StateEnded ViewState = "ended"
	// StateDeclined means the device refused, typically because the child closed
	// it or the camera was unavailable.
	StateDeclined ViewState = "declined"
	// StateFailed means the stream broke before it could start.
	StateFailed ViewState = "failed"
)

const (
	// MaxViewDuration bounds a single look. A camera left running in a child's
	// room is the failure mode this limit exists to prevent.
	MaxViewDuration = 5 * time.Minute
	// RequestTimeout bounds how long a device may take to confirm its
	// indication before the request is abandoned.
	RequestTimeout = 20 * time.Second
	// MaxSessionsPerWindow caps how often one family may open a view.
	MaxSessionsPerWindow = 20
	// SessionRateWindow is the window for MaxSessionsPerWindow.
	SessionRateWindow = time.Hour
	// DefaultHistoryLimit is how many past sessions a family sees by default.
	DefaultHistoryLimit = 20
	// MaxHistoryLimit is the largest history page accepted.
	MaxHistoryLimit = 100
)

// IsTerminal reports whether the session can no longer change state.
func (state ViewState) IsTerminal() bool {
	return state == StateEnded || state == StateDeclined || state == StateFailed
}

// AllowsFrames reports whether frames may be relayed in this state.
//
// This is the single place that answers the question. A requested session is
// deliberately excluded: the device has not yet proven it is showing the child
// an indication, and streaming before that would be a covert camera.
func (state ViewState) AllowsFrames() bool {
	return state == StateStreaming
}

// Session is one guardian's look at one device.
type Session struct {
	ID         string
	FamilyID   string
	DeviceID   string
	ChildID    string
	GuardianID string
	State      ViewState
	StartedAt  time.Time
	// ExpiresAt is the hard deadline the device and the relay both enforce
	// independently of the platform.
	ExpiresAt  time.Time
	FinishedAt *time.Time
	// FinishReason is a short stable token, never free text from a user.
	FinishReason string
	// FramesDelivered counts relayed frames for operations. It never describes
	// what the frames contained.
	FramesDelivered int64
}

// Duration returns how long the session ran, or how long it has been open.
func (session Session) Duration(now time.Time) time.Duration {
	if session.FinishedAt != nil {
		return session.FinishedAt.Sub(session.StartedAt)
	}
	if now.Before(session.StartedAt) {
		return 0
	}
	return now.Sub(session.StartedAt)
}

// Deadline returns the instant the session must stop.
func (session Session) Deadline() time.Time {
	return session.ExpiresAt
}

// expireDue reports whether the session has run past its deadline.
func (session Session) expireDue(now time.Time) bool {
	return !now.Before(session.ExpiresAt)
}

// Accept moves a requested session into streaming.
//
// Only the device may call this, and only after it has started showing its
// indication. The platform trusts the device's acknowledgement here because the
// device is the only party that can see whether the screen and speaker are on.
func (session *Session) Accept(now time.Time) error {
	if session.State != StateRequested {
		return ErrNotRequested
	}
	if session.expireDue(now) {
		return ErrNotStreaming
	}
	session.State = StateStreaming
	return nil
}

// End closes a live session. Talk time is kept so a guardian can see how long a
// look lasted.
func (session *Session) End(now time.Time, reason string) error {
	if session.State.IsTerminal() {
		return ErrFinished
	}
	finishedAt := now.UTC()
	session.State = StateEnded
	session.FinishedAt = &finishedAt
	session.FinishReason = normaliseReason(reason, "ended")
	return nil
}

// Decline records that the device or child refused the view.
func (session *Session) Decline(now time.Time, reason string) error {
	if session.State.IsTerminal() {
		return ErrFinished
	}
	finishedAt := now.UTC()
	session.State = StateDeclined
	session.FinishedAt = &finishedAt
	session.FinishReason = normaliseReason(reason, "declined")
	return nil
}

// Fail records that the stream could not start or broke.
func (session *Session) Fail(now time.Time, reason string) error {
	if session.State.IsTerminal() {
		return ErrFinished
	}
	finishedAt := now.UTC()
	session.State = StateFailed
	session.FinishedAt = &finishedAt
	session.FinishReason = normaliseReason(reason, "failed")
	return nil
}

// Expire closes a session that ran out of time.
//
// A session that ran is reported as ended so it reads as a completed look, while
// one that never started is reported as failed: nobody actually saw anything, and
// calling that "ended" would overstate what happened.
func (session *Session) Expire(now time.Time) error {
	if session.State.IsTerminal() {
		return ErrFinished
	}
	if session.State == StateStreaming {
		return session.End(now, "expired")
	}
	return session.Fail(now, "request_timeout")
}

// normaliseReason keeps every reason a short stable token.
func normaliseReason(reason, fallback string) string {
	if reason == "" {
		return fallback
	}
	if len(reason) > 32 {
		return reason[:32]
	}
	return reason
}

// Request is a guardian's validated intent to look.
type Request struct {
	FamilyID   string
	DeviceID   string
	ChildID    string
	GuardianID string
	// ConsentGranted reports the guardian's image consent decision. A view
	// without consent must not start.
	ConsentGranted bool
}

// Validate checks the request can address a real device.
func (request Request) Validate() error {
	if request.FamilyID == "" || request.DeviceID == "" || request.GuardianID == "" {
		return ErrInvalidRequest
	}
	if !request.ConsentGranted {
		return ErrNoConsent
	}
	return nil
}
