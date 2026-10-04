package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/codec"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
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

	control := NewControlFrame(controlTypeCancel, "session_delta", "device_alpha")
	control.Reason = "guardian_cancelled"
	writeControl(t, connection, control)

	closed := readControl(t, connection)
	if closed.Type != controlTypeSessionClosed {
		t.Fatalf("expected session_closed, got %+v", closed)
	}
	waitFor(t, time.Second, func() bool { return manager.Count() == 0 })
}

func TestDisconnectCleansUpSession(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	writeControl(t, connection, sessionStartFrame("session_epsilon", "device_alpha"))
	_ = readControl(t, connection)
	if manager.Count() != 1 {
		t.Fatalf("expected active session before disconnect, got %d", manager.Count())
	}
	_ = connection.Close()
	waitFor(t, 2*time.Second, func() bool { return manager.Count() == 0 })
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
			return &scriptedWebSocketDetector{values: []bool{true, true, true, false}}
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
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		t.Fatalf("read control frame: %v", err)
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
