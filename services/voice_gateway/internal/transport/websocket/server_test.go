package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/codec"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/playback"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/vad"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/session"
	"github.com/gorilla/websocket"
)

func TestHandlerRejectsMissingTokenBeforeUpgrade(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection, response, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/voice",
		nil,
	)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatal("expected missing token to be rejected")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %#v", response)
	}
	if server.Stats().RejectedConnections == 0 {
		t.Fatal("rejected connection counter was not incremented")
	}
}

func TestHandlerRejectsDeviceWhenGuardianConsentIsWithdrawn(t *testing.T) {
	manager := session.NewManager(session.ManagerConfig{
		MaxSessions:          8,
		MaxSessionsPerDevice: 2,
	})
	defer manager.CloseAll()
	server, err := NewServer(ServerConfig{
		Manager:    manager,
		Verifier:   testTokenVerifier{},
		Authorizer: rejectingAuthorizer{},
	}, nil)
	if err != nil {
		t.Fatalf("create websocket server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer valid-token")
	connection, response, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/voice",
		header,
	)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatal("expected withdrawn consent to be rejected")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %#v", response)
	}
	if server.Stats().RejectedConnections == 0 {
		t.Fatal("rejected connection counter was not incremented")
	}
}

func TestSessionStartAudioFrameAndClose(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_alpha", "device_alpha"))
	started := readControl(t, connection)
	if started.Type != controlTypeSessionStarted || started.SessionID != "session_alpha" {
		t.Fatalf("unexpected session_started frame: %+v", started)
	}
	listening := readControl(t, connection)
	if listening.Type != controlTypeSessionState || listening.State != string(session.StateListening) {
		t.Fatalf("unexpected session state frame: %+v", listening)
	}
	if manager.Count() != 1 {
		t.Fatalf("expected one active session, got %d", manager.Count())
	}

	envelope := makeAudioEnvelope(t, "session_alpha", 7)
	if err := connection.WriteMessage(websocket.BinaryMessage, envelope); err != nil {
		t.Fatalf("write audio frame: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		active, ok := manager.Get("session_alpha")
		return ok && active.Stats().AcceptedFrames == 1
	})

	writeControl(t, connection, endFrame("session_alpha", "device_alpha", "user_finished"))
	idle := readControl(t, connection)
	if idle.Type != controlTypeSessionState || idle.State != string(session.StateIdle) {
		t.Fatalf("unexpected idle state frame: %+v", idle)
	}
	closed := readControl(t, connection)
	if closed.Type != controlTypeSessionClosed || closed.Reason != "user_finished" {
		t.Fatalf("unexpected session_closed frame: %+v", closed)
	}
	waitFor(t, time.Second, func() bool { return manager.Count() == 0 })
}

func TestMalformedControlFrameReturnsStableError(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	if err := connection.WriteMessage(websocket.TextMessage, []byte(`{"schema_version":"1.0.0","type":"not_a_type"}`)); err != nil {
		t.Fatalf("write malformed control frame: %v", err)
	}
	control := readControl(t, connection)
	if control.Type != controlTypeError || control.Code != errorCodeInvalidControl {
		t.Fatalf("unexpected error frame: %+v", control)
	}
}

func TestMalformedAudioFrameReturnsStableError(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_beta", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	if err := connection.WriteMessage(websocket.BinaryMessage, []byte("not-a-sraw-frame")); err != nil {
		t.Fatalf("write malformed audio frame: %v", err)
	}
	control := readControl(t, connection)
	if control.Type != controlTypeError || control.Code != errorCodeInvalidAudio {
		t.Fatalf("unexpected error frame: %+v", control)
	}
}

func TestRateLimitReturnsRateLimited(t *testing.T) {
	server, manager := newTestServer(t)
	server.config.MaxFramesPerSecond = 2
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_gamma", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	writeControl(t, connection, pingFrame("session_gamma", "device_alpha"))
	_ = readControl(t, connection)
	writeControl(t, connection, pingFrame("session_gamma", "device_alpha"))
	errorFrame := readControl(t, connection)
	if errorFrame.Code != errorCodeRateLimited {
		t.Fatalf("expected rate_limited, got %+v", errorFrame)
	}
}

func TestCancelClosesSession(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_delta", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	control := NewControlFrame(controlTypeCancel, "session_delta", "device_alpha")
	control.Reason = "guardian_cancelled"
	writeControl(t, connection, control)

	idle := readControl(t, connection)
	if idle.Type != controlTypeSessionState || idle.State != string(session.StateIdle) {
		t.Fatalf("expected idle session state, got %+v", idle)
	}
	closed := readControl(t, connection)
	if closed.Type != controlTypeSessionClosed {
		t.Fatalf("expected session_closed, got %+v", closed)
	}
	waitFor(t, time.Second, func() bool { return manager.Count() == 0 })
}

func TestWakeDetectedStartsListening(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_wake", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	confidence := 875
	wake := NewControlFrame(controlTypeWakeDetected, "session_wake", "device_alpha")
	wake.StreamID = "stream_alpha"
	wake.WakeWord = "nihaoxiaozhi"
	wake.WakeConfidence = &confidence
	writeControl(t, connection, wake)

	waitFor(t, time.Second, func() bool {
		voiceSession, ok := manager.Get("session_wake")
		return ok && voiceSession.State() == session.StateListening
	})
}

func TestWakeDetectedCreatesSession(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()

	confidence := 900
	wake := NewControlFrame(controlTypeWakeDetected, "session_wake_only", "device_alpha")
	wake.StreamID = "stream_wake_only"
	wake.WakeWord = "nihaoxiaozhi"
	wake.WakeConfidence = &confidence
	writeControl(t, connection, wake)

	started := readControl(t, connection)
	if started.Type != controlTypeSessionStarted || started.SessionID != "session_wake_only" {
		t.Fatalf("unexpected session_started frame: %+v", started)
	}
	listening := readControl(t, connection)
	if listening.Type != controlTypeSessionState || listening.State != string(session.StateListening) {
		t.Fatalf("unexpected session state frame: %+v", listening)
	}
	voiceSession, ok := manager.Get("session_wake_only")
	if !ok || voiceSession.State() != session.StateListening {
		t.Fatalf("wake-created session is not listening: ok=%v state=%v", ok, voiceSession.State())
	}
}

func TestWakeCreatedSessionRunsTurn(t *testing.T) {
	manager := session.NewManager(session.ManagerConfig{MaxSessions: 8, MaxSessionsPerDevice: 2})
	defer manager.CloseAll()
	turnSeen := make(chan session.AudioSegment, 1)
	server, err := NewServer(ServerConfig{
		Manager:  manager,
		Verifier: testTokenVerifier{},
		DetectorFactory: func() vad.Detector {
			return &scriptedWebSocketDetector{values: []bool{true, true, false}}
		},
		SegmentHandler: func(segment session.AudioSegment, sink AudioSink) {
			turnSeen <- segment
			pcm := make([]int16, frame.SamplesPerFrame)
			_ = sink.EnqueueAudio("wake_turn_reply", playback.PriorityConversation, pcm, true, nil)
		},
	}, nil)
	if err != nil {
		t.Fatalf("create websocket server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()

	confidence := 900
	wake := NewControlFrame(controlTypeWakeDetected, "session_wake_turn", "device_alpha")
	wake.StreamID = "stream_wake_turn"
	wake.WakeWord = "nihaoxiaozhi"
	wake.WakeConfidence = &confidence
	writeControl(t, connection, wake)

	started := readControl(t, connection)
	if started.Type != controlTypeSessionStarted {
		t.Fatalf("expected session_started, got %+v", started)
	}
	listening := readControl(t, connection)
	if listening.Type != controlTypeSessionState || listening.State != string(session.StateListening) {
		t.Fatalf("expected listening after wake, got %+v", listening)
	}

	// The wake-created session must accept audio and drive a full turn without a
	// preceding session_start, proving the closed loop from wake to reply.
	writeCompletedUtterance(t, connection, "session_wake_turn", 1)
	stateTypes := make([]string, 0, 3)
	for index := 0; index < 3; index++ {
		stateFrame := readControl(t, connection)
		if stateFrame.Type != controlTypeSessionState {
			t.Fatalf("expected session_state, got %+v", stateFrame)
		}
		stateTypes = append(stateTypes, stateFrame.State)
	}
	expected := []string{
		string(session.StateThinking),
		string(session.StateSpeaking),
		string(session.StateListening),
	}
	for index, state := range expected {
		if stateTypes[index] != state {
			t.Fatalf("expected state sequence %v, got %v", expected, stateTypes)
		}
	}
	select {
	case segment := <-turnSeen:
		if segment.SessionID != "session_wake_turn" {
			t.Fatalf("unexpected segment: %+v", segment)
		}
	case <-time.After(time.Second):
		t.Fatal("wake-created session did not reach the conversation handler")
	}
}

func TestSessionStateFromClientIsRejected(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_state_direction", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	stateFrame := NewControlFrame(controlTypeSessionState, "session_state_direction", "device_alpha")
	stateFrame.State = string(session.StateListening)
	writeControl(t, connection, stateFrame)

	errorFrame := readControl(t, connection)
	if errorFrame.Type != controlTypeError || errorFrame.Code != errorCodeInvalidControl {
		t.Fatalf("expected invalid_control, got %+v", errorFrame)
	}
}

func TestSessionStartCarriesContentCategoryIntoSession(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	start := sessionStartFrame("session_category", "device_alpha")
	start.ContentCategory = "story"
	writeControl(t, connection, start)
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	voiceSession, ok := manager.Get("session_category")
	if !ok {
		t.Fatal("session was not created")
	}
	if voiceSession.ContentCategory() != "story" {
		t.Fatalf(
			"session content category = %q, want story",
			voiceSession.ContentCategory(),
		)
	}
}

func TestSessionStartRejectsInvalidContentCategory(t *testing.T) {
	control := sessionStartFrame("session_bad_category", "device_alpha")
	control.ContentCategory = "Story; DROP TABLE"
	if err := control.Validate(); err == nil {
		t.Fatal("invalid content category was accepted")
	}
}

func TestMalformedSessionStateIsRejected(t *testing.T) {
	_, err := DecodeControlFrame([]byte(
		`{"schema_version":"1.0.0","type":"session_state","session_id":"session_state",` +
			`"device_id":"device_alpha","sent_at":"2026-10-04T03:21:00Z","state":"paused"}`,
	))
	if err == nil {
		t.Fatal("expected malformed session_state to be rejected")
	}
}

func TestWakeDetectedRejectsWrongStream(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_wake_mismatch", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	confidence := 875
	wake := NewControlFrame(
		controlTypeWakeDetected,
		"session_wake_mismatch",
		"device_alpha",
	)
	wake.StreamID = "stream_other"
	wake.WakeWord = "nihaoxiaozhi"
	wake.WakeConfidence = &confidence
	writeControl(t, connection, wake)

	errorFrame := readControl(t, connection)
	if errorFrame.Code != errorCodeInvalidControl {
		t.Fatalf("expected invalid_control, got %+v", errorFrame)
	}
}

func TestDisconnectCleansUpSession(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	writeControl(t, connection, sessionStartFrame("session_epsilon", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)
	if manager.Count() != 1 {
		t.Fatalf("expected active session before disconnect, got %d", manager.Count())
	}
	_ = connection.Close()
	waitFor(t, 2*time.Second, func() bool { return manager.Count() == 0 })
}

func TestDisconnectRevokesAuthenticatedToken(t *testing.T) {
	manager := session.NewManager(session.ManagerConfig{MaxSessions: 8, MaxSessionsPerDevice: 2})
	defer manager.CloseAll()

	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_alpha", "token_revoke_on_close", 5*time.Minute)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	server, err := NewServer(ServerConfig{
		Manager:      manager,
		Verifier:     verifier,
		TokenRevoker: verifier.RevokeIdentity,
		DetectorFactory: func() vad.Detector {
			return &scriptedWebSocketDetector{values: []bool{true, true, false}}
		},
	}, nil)
	if err != nil {
		t.Fatalf("create websocket server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, token)
	if _, err := verifier.VerifyDeviceToken(token); err != nil {
		t.Fatalf("token should be valid while connected: %v", err)
	}
	_ = connection.Close()

	waitFor(t, 2*time.Second, func() bool {
		_, verifyErr := verifier.VerifyDeviceToken(token)
		return verifyErr != nil
	})
}

func TestHeartbeatPingPong(t *testing.T) {
	server, manager := newTestServer(t)
	server.config.PingInterval = 20 * time.Millisecond
	server.config.PongWait = time.Second
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	connection.SetPingHandler(func(data string) error {
		return connection.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})
	writeControl(t, connection, sessionStartFrame("session_zeta", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	receivedPing := make(chan struct{}, 1)
	connection.SetPingHandler(func(data string) error {
		select {
		case receivedPing <- struct{}{}:
		default:
		}
		return connection.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})
	go func() {
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-receivedPing:
	case <-time.After(time.Second):
		t.Fatal("server did not send a heartbeat ping")
	}
}

func newTestServer(t *testing.T) (*Server, *session.Manager) {
	t.Helper()
	manager := session.NewManager(session.ManagerConfig{MaxSessions: 8, MaxSessionsPerDevice: 2})
	server, err := NewServer(ServerConfig{
		Manager:  manager,
		Verifier: testTokenVerifier{},
		DetectorFactory: func() vad.Detector {
			return &scriptedWebSocketDetector{values: []bool{true, true, false}}
		},
	}, nil)
	if err != nil {
		t.Fatalf("create websocket server: %v", err)
	}
	return server, manager
}

func dialAuthenticated(t *testing.T, serverURL string, token string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	connection, response, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(serverURL, "http")+"/voice",
		header,
	)
	if err != nil {
		if response != nil {
			t.Fatalf("dial authenticated websocket: %v (status %d)", err, response.StatusCode)
		}
		t.Fatalf("dial authenticated websocket: %v", err)
	}
	return connection
}

// audioCapturePeer is a minimal client/server websocket pair that lets a test
// drive connectionAudioSink.SendAudio directly and observe what reached the
// device. The writer runs its normal background loop so start/close semantics
// match production.
type audioCapturePeer struct {
	writer     *connectionWriter
	connection *websocket.Conn
}

func newAudioCapturePeer(t *testing.T) (*audioCapturePeer, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverConn := make(chan *websocket.Conn, 1)
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		serverConn <- connection
	}))

	client, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(httpServer.URL, "http"),
		nil,
	)
	if err != nil {
		httpServer.Close()
		t.Fatalf("dial capture websocket: %v", err)
	}
	var connection *websocket.Conn
	select {
	case connection = <-serverConn:
	case <-time.After(2 * time.Second):
		_ = client.Close()
		httpServer.Close()
		t.Fatal("capture websocket upgrade timed out")
	}

	writer := newConnectionWriter(connection, defaultWriteQueueDepth, time.Second)
	writer.start()
	peer := &audioCapturePeer{writer: writer, connection: client}
	cleanup := func() {
		_ = writer.Close()
		_ = connection.Close()
		_ = client.Close()
		httpServer.Close()
	}
	return peer, cleanup
}

func (peer *audioCapturePeer) readBinary(t *testing.T) []byte {
	t.Helper()
	_ = peer.connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		messageType, payload, err := peer.connection.ReadMessage()
		if err != nil {
			t.Fatalf("read capture frame: %v", err)
		}
		if messageType == websocket.BinaryMessage {
			return payload
		}
	}
}

func (peer *audioCapturePeer) hasPendingBinary(timeout time.Duration) bool {
	_ = peer.connection.SetReadDeadline(time.Now().Add(timeout))
	messageType, _, err := peer.connection.ReadMessage()
	return err == nil && messageType == websocket.BinaryMessage
}

func writeControl(t *testing.T, connection *websocket.Conn, control ControlFrame) {
	t.Helper()
	encoded, err := control.Encode()
	if err != nil {
		t.Fatalf("encode control frame: %v", err)
	}
	if err := connection.WriteMessage(websocket.TextMessage, encoded); err != nil {
		t.Fatalf("write control frame: %v", err)
	}
}

func readControl(t *testing.T, connection *websocket.Conn) ControlFrame {
	t.Helper()
	for {
		_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("read control frame: %v", err)
		}
		if messageType == websocket.BinaryMessage {
			continue
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("expected text control frame, got message type %d", messageType)
		}
		control, err := DecodeControlFrame(payload)
		if err != nil {
			t.Fatalf("decode control frame: %v", err)
		}
		return control
	}
}

func sessionStartFrame(sessionID string, deviceID string) ControlFrame {
	control := NewControlFrame(controlTypeSessionStart, sessionID, deviceID)
	control.StreamID = "stream_alpha"
	control.SampleRateHz = frame.SampleRateHz
	control.ChannelCount = frame.ChannelCount
	control.DurationMS = frame.DurationMS
	control.Encoding = frame.EncodingName
	control.FirmwareVersion = "0.4.0"
	control.Capabilities = []string{"audio_input", "wifi"}
	return control
}

func endFrame(sessionID string, deviceID string, reason string) ControlFrame {
	control := NewControlFrame(controlTypeSessionEnd, sessionID, deviceID)
	control.Reason = reason
	return control
}

func pingFrame(sessionID string, deviceID string) ControlFrame {
	return NewControlFrame(controlTypePing, sessionID, deviceID)
}

func makeAudioEnvelope(t *testing.T, sessionID string, sequence uint32) []byte {
	t.Helper()
	opusCodec, err := codec.NewOpusCodec()
	if err != nil {
		t.Fatalf("create opus codec: %v", err)
	}
	pcm := make([]int16, frame.SamplesPerFrame)
	for index := range pcm {
		pcm[index] = int16((index % 24) * 500)
	}
	payload := make([]byte, frame.MaxPayloadBytes)
	encoded, err := opusCodec.EncodePCM(pcm, payload)
	if err != nil {
		t.Fatalf("encode opus frame: %v", err)
	}
	envelope, err := EncodeAudioEnvelope(AudioEnvelope{
		SessionID:  sessionID,
		Sequence:   sequence,
		CapturedAt: time.Now().UTC(),
		Payload:    payload[:encoded],
	})
	if err != nil {
		t.Fatalf("encode audio envelope: %v", err)
	}
	return envelope
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}

// waitForControl reads control frames until one matches or the deadline
// expires. Binary reply audio is skipped so callers can assert state ordering
// without knowing how many audio frames precede the next control frame.
func waitForControl(
	t *testing.T,
	connection *websocket.Conn,
	matches func(ControlFrame) bool,
) ControlFrame {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := connection.SetReadDeadline(deadline); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("read control frame: %v", err)
		}
		if messageType != websocket.TextMessage {
			continue
		}
		control, decodeErr := DecodeControlFrame(payload)
		if decodeErr != nil {
			t.Fatalf("decode control frame: %v", decodeErr)
		}
		if matches(control) {
			return control
		}
		if time.Now().After(deadline) {
			t.Fatalf("matching control frame did not arrive: %+v", control)
		}
	}
}

func writeCompletedUtterance(
	t *testing.T,
	connection *websocket.Conn,
	sessionID string,
	start uint32,
) {
	t.Helper()
	for sequence := start; sequence < start+3; sequence++ {
		if err := connection.WriteMessage(
			websocket.BinaryMessage,
			makeAudioEnvelope(t, sessionID, sequence),
		); err != nil {
			t.Fatalf("write utterance frame %d: %v", sequence, err)
		}
	}
}

func sprintfItem(segmentID string, index int) string {
	return segmentID + "_reply_" + strconv.Itoa(index)
}

type testTokenVerifier struct{}

func (testTokenVerifier) VerifyDeviceToken(token string) (DeviceIdentity, error) {
	switch token {
	case "valid-token":
		return DeviceIdentity{DeviceID: "device_alpha", TokenID: "token_alpha"}, nil
	case "other-token":
		return DeviceIdentity{DeviceID: "device_beta", TokenID: "token_beta"}, nil
	default:
		return DeviceIdentity{}, errors.New("invalid token")
	}
}

type rejectingAuthorizer struct{}

func (rejectingAuthorizer) AuthorizeDevice(
	context.Context,
	string,
) error {
	return ErrDeviceNotAuthorized
}

type scriptedWebSocketDetector struct {
	mutex  sync.Mutex
	index  int
	values []bool
}

func (detector *scriptedWebSocketDetector) IsSpeech(_ []byte) bool {
	detector.mutex.Lock()
	defer detector.mutex.Unlock()
	if len(detector.values) == 0 {
		return true
	}
	value := detector.values[detector.index%len(detector.values)]
	detector.index++
	return value
}

func TestControlFrameRoundTrip(t *testing.T) {
	control := sessionStartFrame("session_roundtrip", "device_alpha")
	encoded, err := control.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeControlFrame(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Type != control.Type || decoded.StreamID != control.StreamID {
		t.Fatalf("round trip changed frame: %+v", decoded)
	}
}

func TestWakeDetectedFrameRoundTrip(t *testing.T) {
	confidence := 875
	control := NewControlFrame(
		controlTypeWakeDetected,
		"session_wake",
		"device_alpha",
	)
	control.StreamID = "stream_alpha"
	control.WakeWord = "nihaoxiaozhi"
	control.WakeConfidence = &confidence

	encoded, err := control.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeControlFrame(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Type != controlTypeWakeDetected ||
		decoded.StreamID != control.StreamID ||
		decoded.WakeWord != control.WakeWord ||
		decoded.WakeConfidence == nil ||
		*decoded.WakeConfidence != confidence {
		t.Fatalf("wake frame changed during round trip: %+v", decoded)
	}
}

func TestWakeDetectedFrameRejectsInvalidConfidence(t *testing.T) {
	confidence := 1001
	control := NewControlFrame(
		controlTypeWakeDetected,
		"session_wake",
		"device_alpha",
	)
	control.StreamID = "stream_alpha"
	control.WakeWord = "nihaoxiaozhi"
	control.WakeConfidence = &confidence

	if _, err := control.Encode(); err == nil {
		t.Fatal("expected invalid confidence to be rejected")
	}
}

func TestAudioEnvelopeRoundTrip(t *testing.T) {
	envelope := AudioEnvelope{
		SessionID:  "session_roundtrip",
		Sequence:   ^uint32(0),
		CapturedAt: time.Now().UTC().Truncate(time.Millisecond),
		Payload:    []byte{0x01, 0x02, 0x03},
	}
	encoded, err := EncodeAudioEnvelope(envelope)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeAudioEnvelope(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.SessionID != envelope.SessionID ||
		decoded.Sequence != envelope.Sequence ||
		!decoded.CapturedAt.Equal(envelope.CapturedAt) ||
		string(decoded.Payload) != string(envelope.Payload) {
		t.Fatalf("round trip changed envelope: %+v", decoded)
	}
}

func TestAudioEnvelopeRejectsInvalidLength(t *testing.T) {
	envelope := AudioEnvelope{
		SessionID:  "session_roundtrip",
		Sequence:   1,
		CapturedAt: time.Now().UTC(),
		Payload:    []byte{0x01},
	}
	encoded, err := EncodeAudioEnvelope(envelope)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := DecodeAudioEnvelope(encoded[:len(encoded)-1]); err == nil {
		t.Fatal("expected truncated envelope to be rejected")
	}
}

func TestControlFrameRejectsUnknownField(t *testing.T) {
	document := `{"schema_version":"1.0.0","type":"ping","session_id":"session_alpha","device_id":"device_alpha","sent_at":"2026-10-04T03:21:00Z","unexpected":true}`
	if _, err := DecodeControlFrame([]byte(document)); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}

func TestServerRejectsWrongIdentity(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_wrong", "device_beta"))
	control := readControl(t, connection)
	if control.Code != errorCodeUnauthenticated {
		t.Fatalf("expected unauthenticated, got %+v", control)
	}
}

func TestBackpressureAndStats(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		for index := 0; index < 50; index++ {
			control := NewControlFrame(controlTypePing, "session_backpressure", "device_alpha")
			encoded, _ := control.Encode()
			_ = connection.WriteMessage(websocket.TextMessage, encoded)
		}
	}()
	waitGroup.Wait()
	_ = connection.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	for {
		if _, _, err := connection.ReadMessage(); err != nil {
			break
		}
	}
	if server.Stats().AcceptedConnections != 1 {
		t.Fatalf("expected one accepted connection, got %d", server.Stats().AcceptedConnections)
	}
}

func TestConcurrentServerStats(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	var waitGroup sync.WaitGroup
	for index := 0; index < 32; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_ = server.Stats()
		}()
	}
	waitGroup.Wait()
}

func TestForwardSegmentsCallback(t *testing.T) {
	manager := session.NewManager(session.ManagerConfig{MaxSessions: 8, MaxSessionsPerDevice: 2})
	defer manager.CloseAll()
	received := make(chan session.AudioSegment, 1)
	server, err := NewServer(ServerConfig{
		Manager:  manager,
		Verifier: testTokenVerifier{},
		DetectorFactory: func() vad.Detector {
			return &scriptedWebSocketDetector{values: []bool{true, false}}
		},
		OnSegmentReady: func(segment session.AudioSegment) {
			select {
			case received <- segment:
			default:
			}
		},
	}, nil)
	if err != nil {
		t.Fatalf("create websocket server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_callback", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)
	if err := connection.WriteMessage(websocket.BinaryMessage, makeAudioEnvelope(t, "session_callback", 1)); err != nil {
		t.Fatalf("write audio frame: %v", err)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, makeAudioEnvelope(t, "session_callback", 2)); err != nil {
		t.Fatalf("write second audio frame: %v", err)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, makeAudioEnvelope(t, "session_callback", 3)); err != nil {
		t.Fatalf("write third audio frame: %v", err)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, makeAudioEnvelope(t, "session_callback", 4)); err != nil {
		t.Fatalf("write ending audio frame: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		active, ok := manager.Get("session_callback")
		return ok && active.Stats().AcceptedFrames == 4
	})
	active, _ := manager.Get("session_callback")
	t.Logf("session stats before callback wait: %+v", active.Stats())
	select {
	case segment := <-received:
		if segment.SessionID != "session_callback" {
			t.Fatalf("unexpected segment: %+v", segment)
		}
	case <-time.After(time.Second):
		t.Fatal("segment callback was not invoked")
	}
}

func TestContinuousSecondTurnAfterFirstCompletes(t *testing.T) {
	manager := session.NewManager(session.ManagerConfig{MaxSessions: 8, MaxSessionsPerDevice: 2})
	defer manager.CloseAll()
	turnCount := make(chan struct{}, 2)
	server, err := NewServer(ServerConfig{
		Manager:  manager,
		Verifier: testTokenVerifier{},
		DetectorFactory: func() vad.Detector {
			return &scriptedWebSocketDetector{values: []bool{
				true, true, false,
				true, true, false,
				true, true, false,
				true, true, false,
			}}
		},
		SegmentHandler: func(segment session.AudioSegment, sink AudioSink) {
			turnCount <- struct{}{}
			// Two frames per reply ensure the session only re-arms after the
			// whole turn is enqueued, not after the first frame becomes audible.
			for index := 0; index < 2; index++ {
				pcm := make([]int16, frame.SamplesPerFrame)
				_ = sink.EnqueueAudio(
					sprintfItem("turn_"+segment.ID, index),
					playback.PriorityConversation,
					pcm,
					true,
					nil,
				)
			}
		},
	}, nil)
	if err != nil {
		t.Fatalf("create websocket server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_multi_turn", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	writeCompletedUtterance(t, connection, "session_multi_turn", 1)
	firstThinking := readControl(t, connection)
	if firstThinking.Type != controlTypeSessionState ||
		firstThinking.State != string(session.StateThinking) {
		t.Fatalf("expected first thinking state, got %+v", firstThinking)
	}
	firstSpeaking := readControl(t, connection)
	if firstSpeaking.Type != controlTypeSessionState ||
		firstSpeaking.State != string(session.StateSpeaking) {
		t.Fatalf("expected first speaking state, got %+v", firstSpeaking)
	}
	firstListening := readControl(t, connection)
	if firstListening.Type != controlTypeSessionState ||
		firstListening.State != string(session.StateListening) {
		t.Fatalf("expected first listening state, got %+v", firstListening)
	}

	writeCompletedUtterance(t, connection, "session_multi_turn", 5)
	secondThinking := readControl(t, connection)
	if secondThinking.Type != controlTypeSessionState ||
		secondThinking.State != string(session.StateThinking) {
		t.Fatalf("expected second thinking state, got %+v", secondThinking)
	}
	secondSpeaking := readControl(t, connection)
	if secondSpeaking.Type != controlTypeSessionState ||
		secondSpeaking.State != string(session.StateSpeaking) {
		t.Fatalf("expected second speaking state, got %+v", secondSpeaking)
	}
	secondListening := readControl(t, connection)
	if secondListening.Type != controlTypeSessionState ||
		secondListening.State != string(session.StateListening) {
		t.Fatalf("expected second listening state, got %+v", secondListening)
	}
	for completed := 0; completed < 2; completed++ {
		select {
		case <-turnCount:
		case <-time.After(time.Second):
			t.Fatalf("expected two completed turns, got %d", completed)
		}
	}
}

func TestBargeInStateTransition(t *testing.T) {
	manager := session.NewManager(session.ManagerConfig{MaxSessions: 8, MaxSessionsPerDevice: 2})
	defer manager.CloseAll()
	schedulers := make(chan *playback.Scheduler, 1)
	releaseHandler := make(chan struct{})
	server, err := NewServer(ServerConfig{
		Manager:  manager,
		Verifier: testTokenVerifier{},
		DetectorFactory: func() vad.Detector {
			return &scriptedWebSocketDetector{values: []bool{true, true, false}}
		},
		onPlaybackScheduler: func(scheduler *playback.Scheduler) {
			select {
			case schedulers <- scheduler:
			default:
			}
		},
		SegmentHandler: func(segment session.AudioSegment, sink AudioSink) {
			// Hold the turn in speaking so the test can deliver a barge-in frame
			// deterministically. The safety item proves Clear(false) preserves
			// non-interruptible announcements.
			safety := make([]int16, frame.SamplesPerFrame)
			_ = sink.EnqueueAudio("safety_notice", playback.PrioritySafety, safety, false, nil)
			for index := 0; index < 3; index++ {
				reply := make([]int16, frame.SamplesPerFrame)
				_ = sink.EnqueueAudio(
					sprintfItem(segment.ID, index),
					playback.PriorityConversation,
					reply,
					true,
					nil,
				)
			}
			<-releaseHandler
		},
	}, nil)
	if err != nil {
		t.Fatalf("create websocket server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()
	writeControl(t, connection, sessionStartFrame("session_barge", "device_alpha"))
	_ = readControl(t, connection)
	_ = readControl(t, connection)

	scheduler := <-schedulers
	// The scheduler drains in the background, so pause it to hold the reply
	// frames in the queue and make the pre-barge-in state deterministic.
	scheduler.Pause()

	writeCompletedUtterance(t, connection, "session_barge", 1)
	thinking := waitForControl(t, connection, func(control ControlFrame) bool {
		return control.Type == controlTypeSessionState && control.State == string(session.StateThinking)
	})
	if thinking.SessionID != "session_barge" {
		t.Fatalf("unexpected thinking frame: %+v", thinking)
	}
	speaking := waitForControl(t, connection, func(control ControlFrame) bool {
		return control.Type == controlTypeSessionState && control.State == string(session.StateSpeaking)
	})
	if speaking.SessionID != "session_barge" {
		t.Fatalf("unexpected speaking frame: %+v", speaking)
	}
	waitFor(t, time.Second, func() bool { return scheduler.Snapshot().Pending == 4 })

	// New device audio while speaking is barge-in: it must clear conversation
	// playback, keep the non-interruptible safety frame, and return to
	// listening without an explicit cancel.
	if err := connection.WriteMessage(websocket.BinaryMessage, makeAudioEnvelope(t, "session_barge", 4)); err != nil {
		t.Fatalf("write barge-in audio: %v", err)
	}
	listening := waitForControl(t, connection, func(control ControlFrame) bool {
		return control.Type == controlTypeSessionState && control.State == string(session.StateListening)
	})
	if listening.SessionID != "session_barge" {
		t.Fatalf("unexpected barge-in state frame: %+v", listening)
	}

	snapshot := scheduler.Snapshot()
	if snapshot.Pending != 1 {
		t.Fatalf("expected only the safety frame to remain queued, got %+v", snapshot)
	}
	voiceSession, ok := manager.Get("session_barge")
	if !ok || voiceSession.State() != session.StateListening {
		t.Fatalf("expected session listening after barge-in, ok=%v", ok)
	}
	close(releaseHandler)
}

// The scheduler dequeues a frame and calls SendAudio after releasing its own
// lock. A barge-in that lands in that window must invalidate the in-flight
// frame, otherwise the device hears the canceled reply over its new speech.
func TestSendAudioDropsFramesFromCanceledTurn(t *testing.T) {
	peer, cleanup := newAudioCapturePeer(t)
	defer cleanup()

	sink := &connectionAudioSink{
		writer:    peer.writer,
		sessionID: func() string { return "session_epoch" },
	}
	// Mirror production: reply turns start at epoch 1 so zero stays reserved for
	// untagged safety audio.
	sink.turnEpoch.Store(1)
	// A frame tagged for the current turn is written.
	if err := sink.SendAudio([]byte{0x01}, sink.turnEpoch.Load()); err != nil {
		t.Fatalf("SendAudio(current turn) error = %v", err)
	}
	if got := peer.readBinary(t); len(got) == 0 {
		t.Fatal("expected the current-turn frame to reach the device")
	}

	// Simulate the barge-in bump, then a frame that was already in flight for
	// the canceled turn must be dropped instead of written.
	sink.turnEpoch.Add(1)
	if err := sink.SendAudio([]byte{0x02}, sink.turnEpoch.Load()-1); err != nil {
		t.Fatalf("SendAudio(stale turn) error = %v", err)
	}
	if peer.hasPendingBinary(time.Millisecond * 200) {
		t.Fatal("a canceled turn's frame reached the device after barge-in")
	}
}

// Untagged audio (epoch zero) must always pass the staleness check so safety
// announcements are never dropped by a barge-in.
func TestSendAudioAlwaysDeliversUntaggedAudio(t *testing.T) {
	peer, cleanup := newAudioCapturePeer(t)
	defer cleanup()

	sink := &connectionAudioSink{
		writer:    peer.writer,
		sessionID: func() string { return "session_epoch" },
	}
	sink.turnEpoch.Store(5)
	if err := sink.SendAudio([]byte{0x03}, 0); err != nil {
		t.Fatalf("SendAudio(untagged) error = %v", err)
	}
	if got := peer.readBinary(t); len(got) == 0 {
		t.Fatal("untagged safety audio was dropped")
	}
}

func TestDecodeControlFrameAcceptsSchemaExample(t *testing.T) {
	document, err := json.Marshal(sessionStartFrame("session_001", "device_001"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := DecodeControlFrame(document); err != nil {
		t.Fatalf("schema-shaped frame was rejected: %v", err)
	}
}

func TestContextCancellationDoesNotLeak(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("context cancellation contract changed")
	}
}
