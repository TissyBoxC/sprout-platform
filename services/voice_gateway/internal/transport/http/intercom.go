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
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/intercom"
	"github.com/gorilla/websocket"
)

const (
	// intercomKeyPattern bounds the identifiers accepted from device_platform.
	// It rejects path separators, whitespace, and control characters before
	// any identifier reaches a device lookup or a log line.
	intercomKeyPatternText = `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`
)

var intercomKeyPattern = regexp.MustCompile(intercomKeyPatternText)

// Stable intercom error codes. They are part of the service contract with
// device_platform, so a caller can distinguish "not answered yet" from
// "unknown call" without parsing a message.
const (
	intercomCodeInvalidRequest     = "intercom_invalid_request"
	intercomCodeSessionNotFound    = "intercom_session_not_found"
	intercomCodeSessionNotActive   = "intercom_session_not_active"
	intercomCodeSessionExpired     = "intercom_session_expired"
	intercomCodeSessionAlreadyOpen = "intercom_session_already_open"
	intercomCodeLimitReached       = "intercom_limit_reached"
	intercomCodeUnavailable        = "intercom_unavailable"
	intercomCodeDisabled           = "intercom_disabled"
	intercomCodeRelayClosed        = "intercom_relay_closed"
	intercomCodeRelayInvalid       = "intercom_relay_invalid"
)

// IntercomSessionResolver reads the platform call state for one call.
type IntercomSessionResolver interface {
	ResolveSession(ctx context.Context, sessionID string, deviceID string) (intercom.Session, error)
}

// intercomHandler serves the internal authorize endpoint and the guardian
// relay upgrade.
type intercomHandler struct {
	resolver  IntercomSessionResolver
	relays    *intercom.RelayManager
	connector intercom.DeviceConnector
	logger    *slog.Logger
	config    IntercomTransportConfig
}

// IntercomTransportConfig bounds the guardian relay endpoint.
type IntercomTransportConfig struct {
	// Path is the guardian upgrade route advertised to device_platform.
	Path string
	// MaxCallDuration is the gateway relay cap.
	MaxCallDuration time.Duration
	// MaxFrameBytes bounds one inbound guardian frame.
	MaxFrameBytes int64
	// MaxFramesPerSecond and MaxBytesPerSecond throttle guardian audio.
	MaxFramesPerSecond int
	MaxBytesPerSecond  int
	// WriteQueueDepth bounds the guardian outbound queue.
	WriteQueueDepth int
	// WriteTimeout bounds one guardian websocket write.
	WriteTimeout time.Duration
	// PongWait and PingInterval drive guardian keepalives.
	PongWait     time.Duration
	PingInterval time.Duration
	// RelayWriteQueueDepth bounds the device-side relay queue.
	RelayWriteQueueDepth int
	// AllowedOrigins is the exact browser Origin allowlist for the guardian
	// relay upgrade. Native guardian apps without Origin remain supported.
	AllowedOrigins []string
}

type intercomAuthorizeRequest struct {
	SessionID   string `json:"session_id"`
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

type intercomAuthorizeResponse struct {
	SessionID string `json:"session_id"`
	DeviceID  string `json:"device_id"`
	State     string `json:"state"`
	// RelayPath is where the guardian should connect. It carries no token:
	// the guardian authenticates with its own credential on that upgrade.
	RelayPath  string `json:"relay_path"`
	ExpiresAt  string `json:"expires_at"`
	MaxSeconds int    `json:"max_duration_seconds"`
}

// authorize is the POST /internal/v1/intercom/sessions/{session_id}/relay
// handler.
//
// device_platform calls it after the child accepted and before it sends audio.
// The handler re-reads the call state from the platform, so the accept step
// cannot be bypassed by a caller that simply claims the call is active.
func (handler intercomHandler) authorize(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.resolver == nil || handler.relays == nil || handler.connector == nil {
		writeIntercomError(response, request, http.StatusServiceUnavailable, intercomCodeDisabled, "对讲暂未开启", false)
		return
	}
	sessionID := strings.TrimSpace(request.PathValue("session_id"))
	var payload intercomAuthorizeRequest
	decoder := json.NewDecoder(io.LimitReader(request.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeIntercomError(response, request, http.StatusBadRequest, intercomCodeInvalidRequest, "请检查对讲请求", false)
		return
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.DeviceID = strings.TrimSpace(payload.DeviceID)
	if !validIntercomKey(sessionID) || payload.DeviceID != "" && !validIntercomKey(payload.DeviceID) {
		writeIntercomError(response, request, http.StatusUnprocessableEntity, intercomCodeInvalidRequest, "请检查对讲请求", false)
		return
	}
	// The path identifier is authoritative; a body that disagrees is a bug or
	// an attempt to authorize one call while naming another.
	if payload.SessionID != "" && payload.SessionID != sessionID {
		writeIntercomError(response, request, http.StatusUnprocessableEntity, intercomCodeInvalidRequest, "请检查对讲请求", false)
		return
	}
	if payload.DeviceID == "" || strings.TrimSpace(payload.DeviceToken) == "" {
		writeIntercomError(response, request, http.StatusUnprocessableEntity, intercomCodeInvalidRequest, "请检查对讲请求", false)
		return
	}

	session, err := handler.resolver.ResolveSession(request.Context(), sessionID, payload.DeviceID)
	if err != nil {
		handler.writeResolveError(response, request, sessionID, err)
		return
	}
	// The accept gate is enforced on the resolved value, not on the resolver
	// alone: an implementation that returns a ringing call must still be
	// refused here rather than trusted.
	if !session.State.AllowsAudio() {
		handler.writeResolveError(response, request, sessionID, intercom.ErrSessionNotActive)
		return
	}
	if session.ID != sessionID || session.DeviceID != payload.DeviceID {
		handler.writeResolveError(response, request, sessionID, intercom.ErrSessionNotFound)
		return
	}
	// The relay cap is the smaller of the platform deadline and the local
	// limit, so the gateway never keeps a channel open past the call record.
	maxDuration := handler.config.MaxCallDuration
	if maxDuration <= 0 {
		maxDuration = intercom.MaxCallDuration
	}
	if remaining := time.Until(session.ExpiresAt); remaining > 0 && remaining < maxDuration {
		maxDuration = remaining
	}
	if session.ExpiresAt.Before(time.Now().Add(time.Second)) {
		writeIntercomError(response, request, http.StatusConflict, intercomCodeSessionExpired, "通话已结束", false)
		return
	}

	// A relay that is already open for this call is reused rather than
	// duplicated, so a retry from device_platform cannot fan audio to the
	// device twice.
	if handler.relays.ActiveSession(session.ID) {
		writeIntercomSuccess(response, request, session, handler.config.Path, maxDuration)
		return
	}

	// The relay enforces its cap from Session.ExpiresAt, so the cap has to be
	// baked into the admitted session. Leaving it only in the response body
	// would let the configured limit be advisory rather than enforced.
	session.ExpiresAt = time.Now().Add(maxDuration)
	outbound, err := handler.connector.OpenDeviceRelay(session, payload.DeviceToken)
	if err != nil {
		// A device that is not connected is a retryable condition: the call
		// stays active on the platform and the guardian can reconnect.
		handler.logger.Warn("intercom relay refused", "session_id", sessionID, "reason", "device_unavailable")
		writeIntercomError(response, request, http.StatusServiceUnavailable, intercomCodeUnavailable, "设备暂时无法连接，请稍后重试", true)
		return
	}
	if _, err := handler.relays.Admit(
		context.Background(),
		session,
		outbound,
		payload.DeviceToken,
		handler.config.RelayWriteQueueDepth,
		handler.logger,
	); err != nil {
		outbound.Close()
		switch {
		case errors.Is(err, intercom.ErrSessionAlreadyRelayed):
			writeIntercomSuccess(response, request, session, handler.config.Path, maxDuration)
		case errors.Is(err, intercom.ErrLimitReached):
			writeIntercomError(response, request, http.StatusTooManyRequests, intercomCodeLimitReached, "对讲繁忙，请稍后重试", true)
		default:
			writeIntercomError(response, request, http.StatusServiceUnavailable, intercomCodeUnavailable, "对讲暂不可用，请稍后重试", true)
		}
		return
	}
	handler.logger.Info("intercom relay opened", "session_id", session.ID, "device_id", session.DeviceID)
	writeIntercomSuccess(response, request, session, handler.config.Path, maxDuration)
}

func (handler intercomHandler) writeResolveError(
	response http.ResponseWriter,
	request *http.Request,
	sessionID string,
	err error,
) {
	switch {
	case errors.Is(err, intercom.ErrSessionNotFound):
		// Log the lifecycle only. The identifier is bounded by
		// validIntercomKey and never contains audio or a token.
		handler.logger.Warn("intercom authorization refused", "session_id", sessionID, "reason", "session_not_found")
		writeIntercomError(response, request, http.StatusNotFound, intercomCodeSessionNotFound, "通话不存在或已结束", false)
	case errors.Is(err, intercom.ErrSessionNotActive):
		handler.logger.Warn("intercom authorization refused", "session_id", sessionID, "reason", "session_not_active")
		writeIntercomError(response, request, http.StatusConflict, intercomCodeSessionNotActive, "孩子尚未接听，暂不能建立对讲", false)
	case errors.Is(err, intercom.ErrSessionExpired):
		handler.logger.Warn("intercom authorization refused", "session_id", sessionID, "reason", "session_expired")
		writeIntercomError(response, request, http.StatusGone, intercomCodeSessionExpired, "通话已结束", false)
	case errors.Is(err, intercom.ErrInvalidRequest):
		writeIntercomError(response, request, http.StatusUnprocessableEntity, intercomCodeInvalidRequest, "请检查对讲请求", false)
	default:
		handler.logger.Warn("intercom authorization unavailable", "session_id", sessionID, "reason", "platform_unavailable")
		writeIntercomError(response, request, http.StatusServiceUnavailable, intercomCodeUnavailable, "对讲服务暂时不可用，请稍后重试", true)
	}
}

func writeIntercomSuccess(
	response http.ResponseWriter,
	request *http.Request,
	session intercom.Session,
	relayPath string,
	maxDuration time.Duration,
) {
	// Round up: reporting 0 seconds for a live call would tell the guardian
	// the channel is already over.
	maxSeconds := int((maxDuration + time.Second - 1) / time.Second)
	if maxSeconds < 1 {
		maxSeconds = 1
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, intercomAuthorizeResponse{
		SessionID:  session.ID,
		DeviceID:   session.DeviceID,
		State:      string(session.State),
		RelayPath:  relayPath,
		ExpiresAt:  session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		MaxSeconds: maxSeconds,
	})
}

func writeIntercomError(
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
func (handler intercomHandler) normalizedConfig() IntercomTransportConfig {
	config := handler.config
	if config.MaxCallDuration <= 0 {
		config.MaxCallDuration = intercom.MaxCallDuration
	}
	if config.MaxFrameBytes <= 0 {
		config.MaxFrameBytes = 4096
	}
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
	if config.RelayWriteQueueDepth <= 0 {
		config.RelayWriteQueueDepth = 64
	}
	return config
}

// upgrader returns the guardian upgrade policy.
//
// Native guardian clients send no Origin and authenticate with their own
// credential; a browser origin must appear in the explicit allowlist.
func (handler intercomHandler) upgrader() websocket.Upgrader {
	return websocket.Upgrader{
		HandshakeTimeout: intercomHandshakeTimeout,
		CheckOrigin:      guardianOriginChecker(handler.config.AllowedOrigins),
	}
}

func guardianOriginChecker(allowedOrigins []string) func(*http.Request) bool {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSuffix(strings.TrimSpace(origin), "/")
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	return func(request *http.Request) bool {
		origin := strings.TrimSuffix(strings.TrimSpace(request.Header.Get("Origin")), "/")
		if origin == "" {
			return true
		}
		if len(allowed) == 0 {
			return false
		}
		_, ok := allowed[origin]
		return ok
	}
}

func validIntercomKey(value string) bool {
	return intercomKeyPattern.MatchString(value)
}
