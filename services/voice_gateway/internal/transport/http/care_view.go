package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/careview"
	"github.com/gorilla/websocket"
)

// Stable care-view error codes. They are part of the service contract with
// device_platform and the parent application, so a caller can distinguish
// "not accepted yet" from "unknown view" without parsing a message.
const (
	careViewCodeInvalidRequest  = "care_view_invalid_request"
	careViewCodeSessionNotFound = "care_view_session_not_found"
	careViewCodeNotActive       = "care_view_not_active"
	careViewCodeExpired         = "care_view_expired"
	careViewCodeLimitReached    = "care_view_limit_reached"
	careViewCodeUnavailable     = "care_view_unavailable"
	careViewCodeSessionOccupied = "care_view_session_occupied"
	careViewCodeRelayInvalid    = "care_view_relay_invalid"
	careViewCodeFrameRejected   = "care_view_frame_rejected"
)

// careViewKeyPattern bounds the identifiers accepted from device_platform.
// It rejects path separators, whitespace, and control characters before any
// identifier reaches a device lookup or a log line.
var careViewKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// CareViewSessionResolver reads the platform view state for one view.
type CareViewSessionResolver interface {
	ResolveSession(ctx context.Context, sessionID string, deviceID string) (careview.Session, error)
}

// CareViewTransportConfig bounds the guardian live-view endpoint.
type CareViewTransportConfig struct {
	// Path is the guardian upgrade route advertised to device_platform.
	Path string
	// MaxViewDuration is the gateway relay cap.
	MaxViewDuration time.Duration
	// MaxFrameBytes bounds one frame in either direction.
	MaxFrameBytes int64
	// MaxFramesPerSecond throttles guardian frames.
	MaxFramesPerSecond int
	// WriteQueueDepth bounds the guardian and device-bound queues.
	WriteQueueDepth int
	// WriteTimeout bounds one websocket write.
	WriteTimeout time.Duration
	// PongWait and PingInterval drive guardian keepalives.
	PongWait     time.Duration
	PingInterval time.Duration
	// StateRecheckInterval re-reads the platform state while frames flow.
	StateRecheckInterval time.Duration
	// AllowedOrigins is the exact browser Origin allowlist for the guardian
	// upgrade. Native parent applications without Origin remain supported.
	AllowedOrigins []string
}

// careViewHandler serves the internal authorize endpoint and the guardian
// live-view upgrade.
type careViewHandler struct {
	resolver  careview.SessionResolver
	relays    *careview.RelayManager
	connector careview.DeviceConnector
	logger    *slog.Logger
	config    CareViewTransportConfig
}

type careViewAuthorizeRequest struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

// careViewStream is the response body of the authorize endpoint.
//
// It carries no token and no frame: StreamPath is where the parent application
// connects, and it authenticates with its own credential there.
type careViewStream struct {
	SessionID  string `json:"session_id"`
	DeviceID   string `json:"device_id"`
	State      string `json:"state"`
	StreamPath string `json:"stream_path"`
	ExpiresAt  string `json:"expires_at"`
	MaxFPS     int    `json:"max_fps"`
}

// authorize is the POST /internal/v1/care-view/sessions/{session_id}/stream
// handler.
//
// device_platform calls it once the device has confirmed its indication and
// before the parent application opens the stream. The handler re-reads the
// view state from the platform, so the acknowledgement gate cannot be bypassed
// by a caller that simply claims the view is streaming.
func (handler careViewHandler) authorize(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.resolver == nil || handler.relays == nil || handler.connector == nil {
		writeCareViewError(response, request, http.StatusServiceUnavailable, careViewCodeUnavailable, "实时查看暂时不可用，请稍后重试", true)
		return
	}
	sessionID := strings.TrimSpace(request.PathValue("session_id"))
	var payload careViewAuthorizeRequest
	decoder := json.NewDecoder(io.LimitReader(request.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeCareViewError(response, request, http.StatusBadRequest, careViewCodeInvalidRequest, "请检查查看请求", false)
		return
	}
	payload.DeviceID = strings.TrimSpace(payload.DeviceID)
	if !validCareViewKey(sessionID) || !validCareViewKey(payload.DeviceID) {
		writeCareViewError(response, request, http.StatusBadRequest, careViewCodeInvalidRequest, "请检查查看请求", false)
		return
	}
	// The device token is accepted but not required: the relay reaches the device
	// through the connection the device already opened, keyed by device_id, so
	// demanding a credential the platform does not hold would fail calls for a
	// reason unrelated to what the check appears to protect.
	payload.DeviceToken = strings.TrimSpace(payload.DeviceToken)

	session, err := handler.resolver.ResolveSession(request.Context(), sessionID, payload.DeviceID)
	if err != nil {
		handler.writeResolveError(response, request, sessionID, err)
		return
	}
	// The streaming gate is enforced on the resolved value, not on the resolver
	// alone: an implementation that returns a requested session must still be
	// refused here rather than trusted.
	if !session.State.AllowsFrames() {
		handler.writeResolveError(response, request, sessionID, careview.ErrNotStreaming)
		return
	}
	if session.ID != sessionID || session.DeviceID != payload.DeviceID {
		handler.writeResolveError(response, request, sessionID, careview.ErrSessionNotFound)
		return
	}
	// The relay cap is the smaller of the platform deadline and the local
	// limit, so the gateway never keeps a camera channel open past the record
	// it relies on.
	config := handler.normalizedConfig()
	now := time.Now()
	expiresAt := now.Add(config.MaxViewDuration)
	if session.ExpiresAt.Before(expiresAt) {
		expiresAt = session.ExpiresAt
	}
	// Only a session that is still ahead needs a stream; a platform deadline in
	// the past has nothing left to show.
	if !now.Before(expiresAt) {
		writeCareViewError(response, request, http.StatusGone, careViewCodeExpired, "这次查看已结束", false)
		return
	}

	// The relay enforces its cap from Session.ExpiresAt, so the narrowed value
	// has to be baked into the admitted session. Leaving it only in the
	// response body would let the platform deadline be advisory.
	session.ExpiresAt = expiresAt

	if handler.relays.ActiveSession(session.ID) {
		// A retry from device_platform must reuse the open relay rather than
		// duplicate the device side.
		writeCareViewStream(response, request, session, config)
		return
	}

	outbound, err := handler.connector.OpenDeviceRelay(session, payload.DeviceToken)
	if err != nil {
		// A device that is not connected is a retryable condition: the view
		// stays streaming on the platform and the parent can reconnect.
		handler.logger.Warn("care view relay refused", "session_id", sessionID, "reason", "device_unavailable")
		writeCareViewError(response, request, http.StatusServiceUnavailable, careViewCodeUnavailable, "设备暂时无法连接，请稍后重试", true)
		return
	}
	if _, err := handler.relays.Admit(context.Background(), session, careview.RelayAdmission{
		Outbound:        outbound,
		MaxFrameBytes:   int(config.MaxFrameBytes),
		WriteQueueDepth: config.WriteQueueDepth,
		Logger:          handler.logger,
	}); err != nil {
		outbound.Close()
		switch {
		case errors.Is(err, careview.ErrSessionAlreadyRelayed):
			writeCareViewStream(response, request, session, config)
		case errors.Is(err, careview.ErrLimitReached):
			writeCareViewError(response, request, http.StatusTooManyRequests, careViewCodeLimitReached, "实时查看繁忙，请稍后重试", true)
		default:
			writeCareViewError(response, request, http.StatusServiceUnavailable, careViewCodeUnavailable, "实时查看暂不可用，请稍后重试", true)
		}
		return
	}
	handler.logger.Info("care view relay opened", "session_id", session.ID, "device_id", session.DeviceID)
	writeCareViewStream(response, request, session, config)
}

func (handler careViewHandler) writeResolveError(
	response http.ResponseWriter,
	request *http.Request,
	sessionID string,
	err error,
) {
	switch {
	case errors.Is(err, careview.ErrSessionNotFound):
		// Log the lifecycle only. The identifier is bounded by
		// validCareViewKey and never contains frame bytes or a token.
		handler.logger.Warn("care view authorization refused", "session_id", sessionID, "reason", "session_not_found")
		writeCareViewError(response, request, http.StatusNotFound, careViewCodeSessionNotFound, "这次查看不存在或已结束", false)
	case errors.Is(err, careview.ErrNotStreaming):
		handler.logger.Warn("care view authorization refused", "session_id", sessionID, "reason", "not_streaming")
		writeCareViewError(response, request, http.StatusConflict, careViewCodeNotActive, "设备还没有开始推流，暂时看不到画面", false)
	case errors.Is(err, careview.ErrSessionExpired):
		handler.logger.Warn("care view authorization refused", "session_id", sessionID, "reason", "session_expired")
		writeCareViewError(response, request, http.StatusGone, careViewCodeExpired, "这次查看已结束", false)
	case errors.Is(err, careview.ErrInvalidRequest):
		writeCareViewError(response, request, http.StatusBadRequest, careViewCodeInvalidRequest, "请检查查看请求", false)
	default:
		handler.logger.Warn("care view authorization unavailable", "session_id", sessionID, "reason", "platform_unavailable")
		writeCareViewError(response, request, http.StatusServiceUnavailable, careViewCodeUnavailable, "实时查看服务暂时不可用，请稍后重试", true)
	}
}

func writeCareViewStream(
	response http.ResponseWriter,
	request *http.Request,
	session careview.Session,
	config CareViewTransportConfig,
) {
	httpapi.WriteSuccess(response, request, http.StatusOK, careViewStream{
		SessionID:  session.ID,
		DeviceID:   session.DeviceID,
		State:      string(session.State),
		StreamPath: config.Path,
		ExpiresAt:  session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		MaxFPS:     config.MaxFramesPerSecond,
	})
}

func writeCareViewError(
	response http.ResponseWriter,
	request *http.Request,
	status int,
	code string,
	message string,
	retryable bool,
) {
	httpapi.WriteError(response, request, status, code, message, retryable)
}

// normalizedConfig applies bounded defaults to a partial configuration.
func (handler careViewHandler) normalizedConfig() CareViewTransportConfig {
	config := handler.config
	if config.MaxViewDuration <= 0 || config.MaxViewDuration > careview.MaxViewDuration {
		config.MaxViewDuration = careview.MaxViewDuration
	}
	if config.MaxFrameBytes <= 0 {
		config.MaxFrameBytes = int64(careview.NormalizeMaxFrameBytes(0))
	}
	if config.MaxFrameBytes > careview.MaxFrameBytesLimit {
		config.MaxFrameBytes = careview.MaxFrameBytesLimit
	}
	config.MaxFramesPerSecond = careview.NormalizeMaxFramesPerSecond(config.MaxFramesPerSecond)
	if config.WriteQueueDepth <= 0 {
		config.WriteQueueDepth = 64
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = 10 * time.Second
	}
	if config.PongWait <= 0 {
		config.PongWait = 60 * time.Second
	}
	if config.PingInterval <= 0 || config.PingInterval >= config.PongWait {
		config.PingInterval = 25 * time.Second
	}
	if config.StateRecheckInterval <= 0 {
		config.StateRecheckInterval = 15 * time.Second
	}
	return config
}

// upgrader returns the guardian upgrade policy.
//
// Native parent clients send no Origin and authenticate with their own
// credential; a browser origin must appear in the explicit allowlist. The
// guardian origin checker is shared with the intercom relay because both routes
// serve the same parent applications.
func (handler careViewHandler) upgrader() websocket.Upgrader {
	return websocket.Upgrader{
		HandshakeTimeout: careViewHandshakeTimeout,
		CheckOrigin:      guardianOriginChecker(handler.config.AllowedOrigins),
	}
}

func validCareViewKey(value string) bool {
	return careViewKeyPattern.MatchString(value)
}
