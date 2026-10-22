// Package intercom owns the gateway-side guardian-to-device voice intercom
// relay.
//
// The gateway never creates or drives the call lifecycle: device_platform owns
// the call record and the accept step. The relay only admits a relay attempt
// that device_platform reports as active, relays frames in both directions for
// a bounded time, and closes both sides on the first hangup.
//
// 网关只做转发，不参与通话状态机。未处于 active 的会话（尤其是 ringing）一律拒绝
// 转发，这是「孩子未接受就不传音频」的硬性安全闸门。
package intercom

import (
	"errors"
	"time"
)

// State is the platform-reported lifecycle position of one intercom call.
//
// The values mirror device_platform/internal/modules/intercom/domain so the
// two services cannot silently disagree about what "active" means.
type State string

const (
	StateRinging   State = "ringing"
	StateActive    State = "active"
	StateEnded     State = "ended"
	StateMissed    State = "missed"
	StateDeclined  State = "declined"
	StateCancelled State = "cancelled"
)

// AllowsAudio reports whether audio may be relayed for this state.
//
// Only an accepted call carries audio. Callers must not re-derive this rule
// from a boolean flag, because a second copy of the condition is exactly how a
// ringing session starts carrying audio.
func (state State) AllowsAudio() bool {
	return state == StateActive
}

// MaxCallDuration is the longest call the platform domain model permits. The
// relay cap is validated against this value so the gateway cannot advertise a
// duration the platform would already have closed.
const MaxCallDuration = 10 * time.Minute

// CloseReason is the bounded reason recorded for one relay close.
//
// The reason is a fixed gateway value, never caller-supplied text, so a close
// reason cannot smuggle audio, transcript text, or identifiers into a log.
type CloseReason string

const (
	// CloseReasonGuardianHangup is a guardian-initiated hangup.
	CloseReasonGuardianHangup CloseReason = "guardian_hangup"
	// CloseReasonDeviceHangup is a device-initiated hangup.
	CloseReasonDeviceHangup CloseReason = "device_hangup"
	// CloseReasonMaxDuration is the relay duration cap.
	CloseReasonMaxDuration CloseReason = "max_duration"
	// CloseReasonCallEnded is a platform state change that closed the call.
	CloseReasonCallEnded CloseReason = "call_ended"
	// CloseReasonTransportClosed is a socket that went away without a hangup.
	CloseReasonTransportClosed CloseReason = "transport_closed"
	// CloseReasonGatewayShutdown is a gateway shutdown with a relay open.
	CloseReasonGatewayShutdown CloseReason = "gateway_shutdown"
)

// Relay errors are sentinels so the handler maps them to stable error codes
// without inspecting message text.
var (
	// ErrInvalidRequest means the authorize payload is unusable.
	ErrInvalidRequest = errors.New("invalid intercom request")
	// ErrSessionNotFound means the platform does not know this call.
	ErrSessionNotFound = errors.New("intercom session not found")
	// ErrSessionNotActive means the call exists but may not carry audio. A
	// ringing call is rejected here, before any audio path exists.
	ErrSessionNotActive = errors.New("intercom session is not active")
	// ErrSessionExpired means the call already reached its deadline.
	ErrSessionExpired = errors.New("intercom session deadline has passed")
	// ErrSessionAlreadyRelayed means another relay already holds this call.
	ErrSessionAlreadyRelayed = errors.New("intercom session already has a relay")
	// ErrLimitReached means the gateway relay capacity is exhausted.
	ErrLimitReached = errors.New("intercom relay limit reached")
	// ErrPlatformUnavailable means the platform status check failed.
	ErrPlatformUnavailable = errors.New("device platform is unavailable")
	// ErrRelayUnavailable means the relay endpoint itself is not configured.
	ErrRelayUnavailable = errors.New("intercom relay is disabled")
)

// Session is one call that device_platform has authorized for relay.
//
// The gateway copies only the fields it enforces locally. It never stores the
// call's guardian, family, or child identity, because forwarding does not need
// it.
type Session struct {
	// ID is the platform call identifier.
	ID string
	// DeviceID is the device side of the relay.
	DeviceID string
	// State is the platform state observed when the relay was authorized.
	State State
	// ExpiresAt is the absolute instant at which the relay must close. It is
	// the earlier of the platform call deadline and the gateway duration cap.
	ExpiresAt time.Time
}

// RelaySink sends one already-framed message to the device side of a relay.
//
// The device transport owns the binary framing, so a sink receives the exact
// payload bytes that arrived from the guardian. A sink error is treated as the
// device side being gone and closes the relay.
type RelaySink interface {
	SendRelayFrame(payload []byte) error
}

// OutboundSink is the device-side half of a relay the connector hands back.
type OutboundSink interface {
	RelaySink
	// Close ends the device side. It must be idempotent.
	Close()
}

// DeviceConnector attaches the gateway to the device side of a call.
//
// Implementations authenticate with the short-lived device token before
// sending anything. A nil connector means this gateway replica cannot reach
// the device and the relay must be refused rather than silently dropped.
type DeviceConnector interface {
	OpenDeviceRelay(session Session, deviceToken string) (OutboundSink, error)
}
