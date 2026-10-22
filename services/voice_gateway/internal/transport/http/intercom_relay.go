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

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/intercom"
	"github.com/gorilla/websocket"
)

const (
	// intercomHandshakeTimeout bounds one guardian upgrade.
	intercomHandshakeTimeout = 10 * time.Second
	// intercomCloseTimeout bounds the close handshake with the guardian.
	intercomCloseTimeout = 2 * time.Second
)

// intercomControlType identifies one guardian control message.
type intercomControlType string

const (
	// intercomControlHangup ends the call from the guardian side.
	intercomControlHangup intercomControlType = "hangup"
	// intercomControlPing checks liveness without carrying payload.
	intercomControlPing intercomControlType = "ping"
	// intercomControlPong answers a gateway ping.
	intercomControlPong intercomControlType = "pong"
)

// intercomFrame is one guardian control message.
//
// It deliberately carries no free-form fields: a control message can only ask
// to hang up or check liveness, so it cannot smuggle audio or text through the
// text channel.
type intercomFrame struct {
	Type intercomControlType `json:"type"`
}

// relayGuardian handles the guardian side of an authorized intercom call.
//
// The handler refuses to attach unless a relay already exists for the call.
// The device-side channel is opened only by the internal authorize endpoint,
// which requires an active platform call, so this path can never carry audio
// before the child accepted.
func (handler intercomHandler) relayGuardian(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.relays == nil || handler.logger == nil {
		writeIntercomError(response, request, http.StatusServiceUnavailable, intercomCodeDisabled, "对讲暂未开启", false)
		return
	}
	sessionID := strings.TrimSpace(request.PathValue("session_id"))
	deviceID := strings.TrimSpace(request.URL.Query().Get("device_id"))
	if !validIntercomKey(sessionID) || !validIntercomKey(deviceID) {
		writeIntercomError(response, request, http.StatusUnprocessableEntity, intercomCodeRelayInvalid, "通话信息不正确", false)
		return
	}
	relay, ok := handler.relays.Relay(sessionID)
	if !ok || relay.Session().DeviceID != deviceID {
		// An unknown call, a relay on another replica, and a rejected call are
		// indistinguishable here, so a caller cannot probe which calls exist.
		writeIntercomError(response, request, http.StatusNotFound, intercomCodeSessionNotFound, "通话不存在或未接通", false)
		return
	}
	// AttachGuardian is the single-owner gate: a second guardian socket for the
	// same call would duplicate audio to the device.
	if err := relay.AttachGuardian(); err != nil {
		writeIntercomError(response, request, http.StatusConflict, intercomCodeSessionAlreadyOpen, "通话已在进行中", false)
		return
	}

	upgrader := handler.upgrader()
	connection, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		handler.logger.Warn("intercom guardian upgrade failed", "session_id", sessionID, "error", err.Error())
		relay.Close(intercom.CloseReasonTransportClosed)
		return
	}
	defer connection.Close()
	handler.runGuardian(connection, relay)
}

// guardianWriter owns the only goroutine that writes to the guardian socket.
//
// The gateway replies with gateway-originated control frames and device audio,
// so a single writer keeps write deadlines and ordering unambiguous.
type guardianWriter struct {
	connection   *websocket.Conn
	queue        chan []byte
	writeTimeout time.Duration
	closeOnce    sync.Once
	closed       chan struct{}
	closeReason  intercom.CloseReason
}

func newGuardianWriter(
	connection *websocket.Conn,
	queueDepth int,
	writeTimeout time.Duration,
) *guardianWriter {
	if queueDepth <= 0 {
		queueDepth = 64
	}
	if writeTimeout <= 0 {
		writeTimeout = 10 * time.Second
	}
	return &guardianWriter{
		connection:   connection,
		queue:        make(chan []byte, queueDepth),
		writeTimeout: writeTimeout,
		closed:       make(chan struct{}),
	}
}

// enqueueBinary queues one device audio frame for the guardian.
func (writer *guardianWriter) enqueueBinary(payload []byte) error {
	if len(payload) == 0 {
		return errors.New("guardian frame is empty")
	}
	frame := append([]byte(nil), payload...)
	select {
	case <-writer.closed:
		return errors.New("guardian writer is closed")
	case writer.queue <- frame:
		return nil
	default:
		// Dropping audio silently would deliver chopped sound to a guardian, so
		// a full queue ends the relay instead.
		return errors.New("guardian send queue is full")
	}
}

func (writer *guardianWriter) enqueueText(value string) error {
	frame := []byte(value)
	select {
	case <-writer.closed:
		return errors.New("guardian writer is closed")
	case writer.queue <- frame:
		return nil
	default:
		return errors.New("guardian send queue is full")
	}
}

func (writer *guardianWriter) start() {
	go func() {
		for {
			select {
			case <-writer.closed:
				return
			case frame := <-writer.queue:
				if err := writer.connection.SetWriteDeadline(
					time.Now().Add(writer.writeTimeout),
				); err != nil {
					writer.closeWith(intercom.CloseReasonTransportClosed)
					return
				}
				messageType := websocket.BinaryMessage
				if json.Valid(frame) {
					messageType = websocket.TextMessage
				}
				if err := writer.connection.WriteMessage(messageType, frame); err != nil {
					writer.closeWith(intercom.CloseReasonTransportClosed)
					return
				}
			}
		}
	}()
}

// closeWith sends a safe, content-free close control and stops the writer.
//
// WriteControl is the only concurrency-safe write on a gorilla connection, so
// the close reason is flushed even if the writer goroutine is mid-write.
func (writer *guardianWriter) closeWith(reason intercom.CloseReason) {
	writer.closeOnce.Do(func() {
		writer.closeReason = reason
		_ = writer.connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, string(reason)),
			time.Now().Add(intercomCloseTimeout),
		)
		close(writer.closed)
		_ = writer.connection.Close()
	})
}

func (writer *guardianWriter) isClosed() bool {
	select {
	case <-writer.closed:
		return true
	default:
		return false
	}
}

// runGuardian drives the guardian socket until either side hangs up.
func (handler intercomHandler) runGuardian(connection *websocket.Conn, relay *intercom.Relay) {
	config := handler.normalizedConfig()
	connection.SetReadLimit(config.MaxFrameBytes)
	_ = connection.SetReadDeadline(time.Now().Add(config.PongWait))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(config.PongWait))
	})

	writer := newGuardianWriter(connection, config.WriteQueueDepth, config.WriteTimeout)
	defer writer.closeWith(relay.CloseReason())
	writer.start()
	relay.AttachGuardianWriter(writer.enqueueBinary)

	relayContext := relay.Context()
	// The duration cap is derived from the platform call deadline, so it
	// cannot be extended by a caller and cannot drift from the call record.
	duration := time.Until(relay.Session().ExpiresAt)
	if duration <= 0 {
		relay.Close(intercom.CloseReasonMaxDuration)
		return
	}
	durationTimer := time.NewTimer(duration)
	defer durationTimer.Stop()
	// The cap must close both sides even while the read loop is blocked in
	// ReadMessage, so it runs on its own goroutine and records max_duration
	// before the socket teardown unblocks the reader.
	capDone := make(chan struct{})
	defer close(capDone)
	go func() {
		select {
		case <-durationTimer.C:
			relay.Close(intercom.CloseReasonMaxDuration)
		case <-capDone:
		case <-relay.Done():
		}
	}()

	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go runIntercomHeartbeat(relayContext, writer, config.PingInterval, heartbeatDone)

	// A device hangup or the duration cap must unblock the guardian read
	// loop: closing the socket is the only way out of a blocking ReadMessage.
	go func() {
		select {
		case <-relay.Done():
			writer.closeWith(relay.CloseReason())
		case <-heartbeatDone:
		}
	}()

	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			switch {
			case writer.isClosed():
				// The relay already closed; keep the recorded reason.
			case isGuardianClose(err):
				relay.Close(intercom.CloseReasonGuardianHangup)
			default:
				relay.Close(intercom.CloseReasonTransportClosed)
			}
			return
		}
		if relay.Session().State != intercom.StateActive {
			// Defense in depth: a relay is only created for an active call, and
			// any disagreeing value refuses the frame instead of forwarding it.
			relay.Close(intercom.CloseReasonCallEnded)
			return
		}

		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) == 0 || int64(len(payload)) > config.MaxFrameBytes {
				relay.Close(intercom.CloseReasonTransportClosed)
				return
			}
			if err := relay.ForwardToDevice(payload); err != nil {
				relay.Close(intercom.CloseReasonTransportClosed)
				return
			}
		case websocket.TextMessage:
			control, decodeErr := decodeIntercomFrame(payload)
			if decodeErr != nil {
				_ = writer.enqueueText(`{"type":"error","code":"intercom_relay_invalid"}`)
				continue
			}
			switch control.Type {
			case intercomControlHangup:
				relay.Close(intercom.CloseReasonGuardianHangup)
				return
			case intercomControlPing:
				_ = writer.enqueueText(`{"type":"pong"}`)
			case intercomControlPong:
				continue
			}
		default:
			relay.Close(intercom.CloseReasonTransportClosed)
			return
		}
	}
}

func decodeIntercomFrame(payload []byte) (intercomFrame, error) {
	if len(payload) == 0 || len(payload) > 512 {
		return intercomFrame{}, errors.New("intercom control frame is invalid")
	}
	var frameValue intercomFrame
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&frameValue); err != nil {
		return intercomFrame{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return intercomFrame{}, errors.New("intercom control frame has trailing data")
	}
	switch frameValue.Type {
	case intercomControlPing, intercomControlHangup, intercomControlPong:
		return frameValue, nil
	default:
		return intercomFrame{}, errors.New("intercom control type is invalid")
	}
}

func isGuardianClose(err error) bool {
	return websocket.IsCloseError(
		err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	)
}

func runIntercomHeartbeat(
	ctx context.Context,
	writer *guardianWriter,
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
