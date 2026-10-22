package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/careview"
	"github.com/gorilla/websocket"
)

const (
	// careViewHandshakeTimeout bounds one guardian upgrade.
	careViewHandshakeTimeout = 10 * time.Second
	// careViewCloseTimeout bounds the close handshake with the guardian.
	careViewCloseTimeout = 2 * time.Second
)

// careViewControlType identifies one guardian control message.
type careViewControlType string

const (
	// careViewControlHangup ends the stream from the guardian side.
	careViewControlHangup careViewControlType = "hangup"
	// careViewControlPing checks liveness without carrying payload.
	careViewControlPing careViewControlType = "ping"
	// careViewControlPong answers a gateway ping.
	careViewControlPong careViewControlType = "pong"
)

// careViewControl is one guardian control message.
//
// It deliberately carries no free-form fields, so the text channel cannot
// smuggle frame bytes, a child's data, or command text through it.
type careViewControl struct {
	Type careViewControlType `json:"type"`
}

// relayGuardian handles the guardian side of an admitted care-view stream.
//
// The handler refuses to attach unless a relay already exists for the view. The
// device side is opened only by the internal authorize endpoint, which requires
// a streaming platform view, so this path can never carry a frame before the
// device confirmed its indication.
func (handler careViewHandler) relayGuardian(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.relays == nil || handler.logger == nil || handler.resolver == nil {
		writeCareViewError(response, request, http.StatusServiceUnavailable, careViewCodeUnavailable, "实时查看暂时不可用，请稍后重试", true)
		return
	}
	sessionID := strings.TrimSpace(request.PathValue("session_id"))
	deviceID := strings.TrimSpace(request.URL.Query().Get("device_id"))
	if !validCareViewKey(sessionID) || !validCareViewKey(deviceID) {
		writeCareViewError(response, request, http.StatusBadRequest, careViewCodeRelayInvalid, "查看信息不正确", false)
		return
	}
	relay, ok := handler.relays.Relay(sessionID)
	if !ok || relay.Session().DeviceID != deviceID {
		// An unknown view, a relay on another replica, and a refused view are
		// indistinguishable here, so a caller cannot probe which views exist.
		writeCareViewError(response, request, http.StatusNotFound, careViewCodeSessionNotFound, "这次查看不存在或尚未开始", false)
		return
	}
	// The processing-layer gate: the relay must still be in a state that
	// allows frames right now, not merely when it was admitted.
	if !relay.IsStreaming() {
		writeCareViewError(response, request, http.StatusConflict, careViewCodeNotActive, "设备当前没有推流，暂时看不到画面", false)
		return
	}
	if relay.IsClosed() {
		writeCareViewError(response, request, http.StatusNotFound, careViewCodeSessionNotFound, "这次查看不存在或已结束", false)
		return
	}
	// AttachGuardian is the single-owner gate: a second parent socket for the
	// same view would give another guardian the same camera feed.
	if err := relay.AttachGuardian(); err != nil {
		writeCareViewError(response, request, http.StatusConflict, careViewCodeSessionOccupied, "这次查看已有一个家长在看", false)
		return
	}

	upgrader := handler.upgrader()
	connection, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		// Upgrade has already written its own error response; only the relay
		// state is left to clean up.
		handler.logger.Warn("care view guardian upgrade failed", "session_id", sessionID)
		relay.Close(careview.CloseReasonTransportClosed)
		return
	}
	defer connection.Close()
	handler.runGuardian(connection, relay)
}

// careViewWriter owns the only goroutine that writes to the guardian socket.
//
// The gateway sends binary frames to the parent and gateway-originated control
// frames, so a single writer keeps write deadlines and ordering unambiguous.
type careViewWriter struct {
	connection   *websocket.Conn
	queue        chan []byte
	writeTimeout time.Duration
	closeOnce    sync.Once
	closed       chan struct{}
}

func newCareViewWriter(
	connection *websocket.Conn,
	queueDepth int,
	writeTimeout time.Duration,
) *careViewWriter {
	if queueDepth <= 0 {
		queueDepth = 64
	}
	if writeTimeout <= 0 {
		writeTimeout = 10 * time.Second
	}
	return &careViewWriter{
		connection:   connection,
		queue:        make(chan []byte, queueDepth),
		writeTimeout: writeTimeout,
		closed:       make(chan struct{}),
	}
}

// enqueueBinary queues one device frame for the parent application.
func (writer *careViewWriter) enqueueBinary(payload []byte) error {
	if len(payload) == 0 {
		return errors.New("care view frame is empty")
	}
	frame := append([]byte(nil), payload...)
	select {
	case <-writer.closed:
		return errors.New("care view writer is closed")
	case writer.queue <- frame:
		return nil
	default:
		// Dropping a live frame silently would freeze the parent's picture
		// without telling anyone, so a full queue ends the relay instead.
		return errors.New("care view send queue is full")
	}
}

// closeWith sends a content-free close control and stops the writer.
//
// WriteControl is the only concurrency-safe write on a gorilla connection, so
// the close reason is flushed even while the writer goroutine is mid-write.
func (writer *careViewWriter) closeWith(reason careview.CloseReason) {
	writer.closeOnce.Do(func() {
		_ = writer.connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, string(reason)),
			time.Now().Add(careViewCloseTimeout),
		)
		close(writer.closed)
		_ = writer.connection.Close()
	})
}

func (writer *careViewWriter) isClosed() bool {
	select {
	case <-writer.closed:
		return true
	default:
		return false
	}
}

func (writer *careViewWriter) start() {
	go func() {
		for {
			select {
			case <-writer.closed:
				return
			case frame := <-writer.queue:
				if err := writer.connection.SetWriteDeadline(
					time.Now().Add(writer.writeTimeout),
				); err != nil {
					writer.closeWith(careview.CloseReasonTransportClosed)
					return
				}
				if err := writer.connection.WriteMessage(websocket.BinaryMessage, frame); err != nil {
					writer.closeWith(careview.CloseReasonTransportClosed)
					return
				}
			}
		}
	}()
}

// runGuardian drives the guardian socket until either side hangs up, the
// deadline passes, or the platform stops reporting streaming.
func (handler careViewHandler) runGuardian(
	connection *websocket.Conn,
	relay *careview.Relay,
) {
	config := handler.normalizedConfig()
	connection.SetReadLimit(config.MaxFrameBytes)
	_ = connection.SetReadDeadline(time.Now().Add(config.PongWait))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(config.PongWait))
	})

	writer := newCareViewWriter(connection, config.WriteQueueDepth, config.WriteTimeout)
	defer writer.closeWith(relay.CloseReason())
	writer.start()
	relay.SetGuardianWriter(writer.enqueueBinary)

	// The deadline is the earlier of the gateway cap and the platform
	// deadline, so it cannot be extended by a caller and cannot drift from the
	// session record.
	deadlineTimer := time.NewTimer(time.Until(relay.Session().ExpiresAt))
	defer deadlineTimer.Stop()
	// The deadline must close both sides even while the read loop is blocked
	// in ReadMessage, so an independent goroutine fires it and records its
	// reason before the socket teardown unblocks the reader.
	deadlineDone := make(chan struct{})
	defer close(deadlineDone)
	go func() {
		select {
		case <-deadlineTimer.C:
			relay.Close(relay.DeadlineReason())
		case <-deadlineDone:
		case <-relay.Done():
		}
	}()

	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go runCareViewHeartbeat(relay.Context(), writer, config.PingInterval, heartbeatDone)

	stateDone := make(chan struct{})
	defer close(stateDone)
	go handler.watchPlatformState(relay, config.StateRecheckInterval, stateDone)

	// A deadline, a platform state change, or a device hangup must unblock the
	// guardian read loop: closing the socket is the only way out of a blocking
	// ReadMessage.
	go func() {
		select {
		case <-relay.Done():
			writer.closeWith(relay.CloseReason())
		case <-stateDone:
		}
	}()

	limiter := newCareViewLimiter(config.MaxFramesPerSecond, time.Now)
	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			// The transport enforces the configured frame ceiling itself, so a
			// frame above the limit must be reported as a frame-limit close
			// instead of as a broken transport.
			switch {
			case errors.Is(err, websocket.ErrReadLimit):
				relay.Close(careview.CloseReasonFrameLimit)
			case writer.isClosed():
				// The relay already closed; keep the recorded reason.
			case isCareViewGuardianClose(err):
				relay.Close(careview.CloseReasonGuardianHangup)
			default:
				relay.Close(careview.CloseReasonTransportClosed)
			}
			return
		}
		// Processing-layer gate: a relay is only admitted for a streaming view,
		// and any value that stopped being streaming refuses the frame instead
		// of forwarding it.
		if !relay.IsStreaming() {
			relay.Close(careview.CloseReasonSessionEnded)
			return
		}

		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) == 0 || int64(len(payload)) > config.MaxFrameBytes {
				relay.Close(careview.CloseReasonFrameLimit)
				return
			}
			if !limiter.allow() {
				relay.Close(careview.CloseReasonFrameLimit)
				return
			}
			if forwardErr := relay.ForwardToDevice(payload); forwardErr != nil {
				if relay.IsClosed() {
					return
				}
				continue
			}
		case websocket.TextMessage:
			control, decodeErr := decodeCareViewControl(payload)
			if decodeErr != nil {
				continue
			}
			switch control.Type {
			case careViewControlHangup:
				relay.Close(careview.CloseReasonGuardianHangup)
				return
			case careViewControlPing, careViewControlPong:
				continue
			}
		default:
			relay.Close(careview.CloseReasonTransportClosed)
			return
		}
	}
}

// watchPlatformState re-reads the platform state while frames flow.
//
// The resolver already refused a non-streaming view and the relay re-checks
// every frame, but neither observes the platform ending a view on its own
// (guardian end, child decline, or the platform expiry worker). This watcher
// closes the relay within one interval, so the camera stops producing pictures
// even when the parent application keeps its socket open. A read failure fails
// closed: an unverifiable view is not a view.
func (handler careViewHandler) watchPlatformState(
	relay *careview.Relay,
	interval time.Duration,
	done <-chan struct{},
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-relay.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			stateContext, cancel := context.WithTimeout(relay.Context(), interval)
			session, err := handler.resolver.ResolveSession(
				stateContext,
				relay.Session().ID,
				relay.Session().DeviceID,
			)
			cancel()
			if err != nil {
				// A failed re-read keeps the relay running only while it was
				// already closing; otherwise the state is unknown and frames
				// must stop.
				if relay.IsClosed() || errors.Is(err, context.Canceled) {
					return
				}
				handler.logger.Warn("care view state recheck failed",
					"session_id", relay.Session().ID,
					"reason", careViewRecheckReason(err),
				)
				relay.Close(careview.CloseReasonSessionEnded)
				return
			}
			if observeErr := relay.ObserveState(session.State); observeErr != nil {
				// ObserveState already records the close so the processing gate
				// reads false before the socket teardown races it.
				return
			}
		}
	}
}

// careViewRecheckReason maps a failed state read to a fixed log token.
func careViewRecheckReason(err error) string {
	switch {
	case errors.Is(err, careview.ErrSessionNotFound):
		return "session_not_found"
	case errors.Is(err, careview.ErrNotStreaming):
		return "not_streaming"
	case errors.Is(err, careview.ErrSessionExpired):
		return "session_expired"
	default:
		return "platform_unavailable"
	}
}

func decodeCareViewControl(payload []byte) (careViewControl, error) {
	if len(payload) == 0 || len(payload) > 512 {
		return careViewControl{}, errors.New("care view control frame is invalid")
	}
	var control careViewControl
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&control); err != nil {
		return careViewControl{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return careViewControl{}, errors.New("care view control frame has trailing data")
	}
	switch control.Type {
	case careViewControlHangup, careViewControlPing, careViewControlPong:
		return control, nil
	default:
		return careViewControl{}, errors.New("care view control type is invalid")
	}
}

func isCareViewGuardianClose(err error) bool {
	return websocket.IsCloseError(
		err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	)
}

func runCareViewHeartbeat(
	ctx context.Context,
	writer *careViewWriter,
	interval time.Duration,
	done <-chan struct{},
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if writer.isClosed() {
				return
			}
			_ = writer.connection.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(writer.writeTimeout),
			)
		}
	}
}

// careViewLimiter enforces the configured frame rate on a guardian socket.
//
// It is owned by one read loop and therefore needs no lock. A token bucket with
// a full initial budget tolerates a normal burst while still bounding sustained
// traffic.
type careViewLimiter struct {
	maxFramesPerSecond int
	now                func() time.Time
	lastRefill         time.Time
	tokens             float64
}

func newCareViewLimiter(maxFramesPerSecond int, now func() time.Time) *careViewLimiter {
	if maxFramesPerSecond <= 0 {
		maxFramesPerSecond = careview.NormalizeMaxFramesPerSecond(0)
	}
	if now == nil {
		now = time.Now
	}
	current := now()
	return &careViewLimiter{
		maxFramesPerSecond: maxFramesPerSecond,
		now:                now,
		lastRefill:         current,
		tokens:             float64(maxFramesPerSecond),
	}
}

func (limiter *careViewLimiter) allow() bool {
	if limiter == nil || limiter.maxFramesPerSecond <= 0 {
		return false
	}
	now := limiter.now()
	if now.Before(limiter.lastRefill) {
		// A wall-clock rollback must not mint an unbounded new burst.
		limiter.lastRefill = now
	}
	elapsed := now.Sub(limiter.lastRefill).Seconds()
	limiter.lastRefill = now
	limiter.tokens += elapsed * float64(limiter.maxFramesPerSecond)
	if limiter.tokens > float64(limiter.maxFramesPerSecond) {
		limiter.tokens = float64(limiter.maxFramesPerSecond)
	}
	if limiter.tokens < 1 {
		return false
	}
	limiter.tokens--
	return true
}
