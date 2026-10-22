// Package careview owns the gateway-side frame relay of the guardian care view.
//
// The gateway never creates or ends a care view: device_platform owns the
// session record, the guardian's consent, and the device acknowledgement that
// the indication is showing. The relay only admits a stream that device_platform
// reports as streaming, forwards bounded binary frames in both directions for a
// bounded time, and closes both sides on the first hangup.
//
// 网关只做转发，不参与查看状态机，也不落盘。帧字节只存在于内存中的队列与
// socket 上：既不写入文件，也不进入日志。未处于 streaming 的会话（尤其是
// requested）一律拒绝转发，这是「设备未显示指示就不得传画面」的硬性安全闸门。
package careview

import (
	"errors"
	"time"
)

// State is the platform-reported lifecycle position of one care view.
//
// The values mirror device_platform's care_view domain so the two services
// cannot silently disagree about what "streaming" means.
type State string

const (
	// StateRequested means the guardian asked but the device has not confirmed
	// its visible and audible indication yet. No frame may flow.
	StateRequested State = "requested"
	// StateStreaming is the only state that may carry frames.
	StateStreaming State = "streaming"
	// StateEnded means the view ran and was closed.
	StateEnded State = "ended"
	// StateDeclined means the device or child refused the view.
	StateDeclined State = "declined"
	// StateFailed means the stream broke before it could start.
	StateFailed State = "failed"
)

// AllowsFrames reports whether frames may be relayed in this state.
//
// This is the single copy of the rule. Callers must not re-derive it from a
// separate boolean, because a second copy of the condition is exactly how a
// requested session starts carrying frames.
func (state State) AllowsFrames() bool {
	return state == StateStreaming
}

const (
	// MaxViewDuration is the longest view device_platform permits. The relay
	// cap is validated against this value so the gateway cannot advertise a
	// duration the platform would already have closed.
	MaxViewDuration = 5 * time.Minute
	// MaxFrameBytesLimit bounds the configurable single-frame ceiling.
	MaxFrameBytesLimit = 2 * 1024 * 1024
	// MaxFrameRateLimit bounds the configurable frame rate.
	MaxFrameRateLimit = 30
	// defaultMaxRelays is the admission cap when none is configured.
	defaultMaxRelays = 256
	// defaultWriteQueueDepth bounds one relay's device-bound queue.
	defaultWriteQueueDepth = 64
)

// CloseReason is the bounded reason recorded for one relay close.
//
// The reason is a fixed gateway value, never caller-supplied text, so a close
// reason cannot smuggle frame bytes, tokens, or identifiers into a log line.
type CloseReason string

const (
	// CloseReasonGuardianHangup is a guardian-initiated hangup.
	CloseReasonGuardianHangup CloseReason = "guardian_hangup"
	// CloseReasonDeviceHangup is a device-initiated hangup: the device socket
	// went away, or the device stopped accepting frames.
	CloseReasonDeviceHangup CloseReason = "device_hangup"
	// CloseReasonMaxDuration is the relay duration cap.
	CloseReasonMaxDuration CloseReason = "max_duration"
	// CloseReasonPlatformDeadline is the platform session deadline, which may
	// be earlier than the configured cap.
	CloseReasonPlatformDeadline CloseReason = "platform_deadline"
	// CloseReasonSessionEnded is a platform state change observed by the
	// relay's state watcher.
	CloseReasonSessionEnded CloseReason = "session_ended"
	// CloseReasonFrameLimit is a frame outside the configured size or rate.
	CloseReasonFrameLimit CloseReason = "frame_limit"
	// CloseReasonTransportClosed is a socket that went away without a hangup.
	CloseReasonTransportClosed CloseReason = "transport_closed"
	// CloseReasonGatewayShutdown is a gateway shutdown with a relay open.
	CloseReasonGatewayShutdown CloseReason = "gateway_shutdown"
)

// Relay errors are sentinels so the handler maps them to stable error codes
// without inspecting message text.
var (
	// ErrInvalidRequest means the authorize payload is unusable.
	ErrInvalidRequest = errors.New("invalid care view request")
	// ErrSessionNotFound means the platform does not know this view.
	ErrSessionNotFound = errors.New("care view session not found")
	// ErrNotStreaming means the view exists but may not carry frames. A
	// requested view is rejected here, before any frame path exists.
	ErrNotStreaming = errors.New("care view session is not streaming")
	// ErrSessionExpired means the view already reached its deadline.
	ErrSessionExpired = errors.New("care view session deadline has passed")
	// ErrSessionAlreadyRelayed means another relay already holds this view.
	ErrSessionAlreadyRelayed = errors.New("care view session already has a relay")
	// ErrLimitReached means the gateway relay capacity is exhausted.
	ErrLimitReached = errors.New("care view relay limit reached")
	// ErrPlatformUnavailable means the platform status read failed.
	ErrPlatformUnavailable = errors.New("device platform is unavailable")
	// ErrRelayUnavailable means the relay endpoint itself is not configured.
	ErrRelayUnavailable = errors.New("care view relay is disabled")
	// ErrDeviceNotConnected means the device has no live realtime connection
	// on this gateway replica.
	ErrDeviceNotConnected = errors.New("device is not connected")
	// ErrRelayClosed means the relay already ended.
	ErrRelayClosed = errors.New("care view relay is closed")
	// ErrFrameRejected means a frame exceeded the configured size or rate.
	ErrFrameRejected = errors.New("care view frame rejected")
)

// Session is one view that device_platform has authorized for relay.
//
// Only the fields the gateway enforces locally are copied. The guardian,
// family, and child identities stay in device_platform because forwarding does
// not need them.
type Session struct {
	// ID is the platform view identifier.
	ID string
	// DeviceID is the device side of the relay.
	DeviceID string
	// State is the platform state observed when the relay was admitted.
	State State
	// PlatformDeadline is the session deadline device_platform reported.
	PlatformDeadline time.Time
	// ExpiresAt is the absolute instant at which the relay must close. It is
	// the earlier of PlatformDeadline and the gateway duration cap.
	ExpiresAt time.Time
}

// PublishSink sends one already-framed message to the device side of a relay.
//
// The device transport owns the binary framing, so a sink receives the exact
// payload bytes that arrived from the guardian. A nil error means the frame was
// queued; a sink error is treated as the device side being gone.
type PublishSink interface {
	SendRelayFrame(payload []byte) error
}

// OutboundSink is the device-side half of a relay the connector hands back.
type OutboundSink interface {
	PublishSink
	// Close ends the device side. It must be idempotent.
	Close()
}

// DeviceConnector attaches the gateway to the device side of a view.
//
// Implementations authenticate with the short-lived device token before
// accepting anything. A nil connector means this replica cannot reach the
// device and the relay must be refused rather than silently dropped.
type DeviceConnector interface {
	OpenDeviceRelay(session Session, deviceToken string) (OutboundSink, error)
}

// NormalizeMaxFrameBytes applies the default and the hard ceiling to a
// configured frame size.
func NormalizeMaxFrameBytes(value int) int {
	if value <= 0 {
		return 256 * 1024
	}
	if value > MaxFrameBytesLimit {
		return MaxFrameBytesLimit
	}
	return value
}

// NormalizeMaxFramesPerSecond applies the default and the hard ceiling to a
// configured frame rate.
func NormalizeMaxFramesPerSecond(value int) int {
	if value <= 0 {
		return 8
	}
	if value > MaxFrameRateLimit {
		return MaxFrameRateLimit
	}
	return value
}
