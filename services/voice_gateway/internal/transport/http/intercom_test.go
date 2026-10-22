package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/intercom"
	"github.com/gorilla/websocket"
)

const (
	testInternalToken = "unit-test-internal-service-token-123456"
	testIntercomPath  = "/v1/intercom"
)

// lockedBuffer is a concurrency-safe log sink. httptest serves requests on
// several goroutines, so a bare bytes.Buffer would race with the test's reads.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (sink *lockedBuffer) Write(payload []byte) (int, error) {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return sink.buffer.Write(payload)
}

func (sink *lockedBuffer) String() string {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return sink.buffer.String()
}

// intercomTestRig wires the real router, the real relay manager, and a device
// sink that records what the device side received.
type intercomTestRig struct {
	server    *httptest.Server
	relays    *intercom.RelayManager
	resolver  *stubIntercomResolver
	device    *recordingDeviceSink
	router    http.Handler
	logBuffer *lockedBuffer
}

type stubIntercomResolver struct {
	mutex   sync.Mutex
	session intercom.Session
	err     error
	calls   int
}

func (resolver *stubIntercomResolver) ResolveSession(
	_ context.Context,
	sessionID string,
	deviceID string,
) (intercom.Session, error) {
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	resolver.calls++
	if resolver.err != nil {
		return intercom.Session{}, resolver.err
	}
	if resolver.session.ID != sessionID || resolver.session.DeviceID != deviceID {
		return intercom.Session{}, intercom.ErrSessionNotFound
	}
	return resolver.session, nil
}

func (resolver *stubIntercomResolver) callCount() int {
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	return resolver.calls
}

type recordingDeviceSink struct {
	mutex   sync.Mutex
	frames  [][]byte
	closed  bool
	onFrame func(payload []byte)
}

// SendDeviceFrame is the realtime transport surface the relay writes through.
func (sink *recordingDeviceSink) SendDeviceFrame(payload []byte) error {
	return sink.SendRelayFrame(payload)
}

// SendRelayFrame records one device-bound relay frame.
func (sink *recordingDeviceSink) SendRelayFrame(payload []byte) error {
	sink.mutex.Lock()
	sink.frames = append(sink.frames, append([]byte(nil), payload...))
	callback := sink.onFrame
	sink.mutex.Unlock()
	if callback != nil {
		callback(payload)
	}
	return nil
}

// Close records that the relay released the device side.
func (sink *recordingDeviceSink) Close() {
	sink.mutex.Lock()
	sink.closed = true
	sink.mutex.Unlock()
}

func (sink *recordingDeviceSink) isClosed() bool {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return sink.closed
}

func (sink *recordingDeviceSink) frameCount() int {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return len(sink.frames)
}

func (sink *recordingDeviceSink) lastFrame() []byte {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	if len(sink.frames) == 0 {
		return nil
	}
	return sink.frames[len(sink.frames)-1]
}

// stubDeviceConnector returns the rig's device sink for any device.
type stubDeviceConnector struct {
	sink *recordingDeviceSink
	err  error
}

func (connector *stubDeviceConnector) OpenDeviceRelay(
	_ intercom.Session,
	_ string,
) (intercom.OutboundSink, error) {
	if connector.err != nil {
		return nil, connector.err
	}
	return connector.sink, nil
}

func newIntercomTestRig(
	t *testing.T,
	session intercom.Session,
	resolveErr error,
) *intercomTestRig {
	t.Helper()
	rig := &intercomTestRig{
		relays:    intercom.NewRelayManager(8),
		resolver:  &stubIntercomResolver{session: session, err: resolveErr},
		device:    &recordingDeviceSink{},
		logBuffer: &lockedBuffer{},
	}
	t.Cleanup(rig.relays.CloseAll)

	logger := slog.New(slog.NewJSONHandler(rig.logBuffer, nil))
	rig.router = NewRouter(RouterOptions{
		Logger: logger,
		InternalAPIConfig: config.InternalAPIConfig{
			Enabled:   true,
			AuthToken: testInternalToken,
		},
		IntercomResolver:  rig.resolver,
		IntercomRelays:    rig.relays,
		IntercomConnector: &stubDeviceConnector{sink: rig.device},
		IntercomEnabled:   true,
		IntercomTransport: IntercomTransportConfig{
			Path:                 testIntercomPath,
			MaxCallDuration:      200 * time.Millisecond,
			MaxFrameBytes:        4096,
			WriteQueueDepth:      8,
			WriteTimeout:         2 * time.Second,
			PongWait:             5 * time.Second,
			PingInterval:         2 * time.Second,
			RelayWriteQueueDepth: 8,
		},
	})
	rig.server = httptest.NewServer(rig.router)
	t.Cleanup(rig.server.Close)
	return rig
}

func activeIntercomSession(ttl time.Duration) intercom.Session {
	return intercom.Session{
		ID:        "call_alpha",
		DeviceID:  "device_alpha",
		State:     intercom.StateActive,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// authorizeRelay calls the internal endpoint the way device_platform does.
func (rig *intercomTestRig) authorizeRelay(
	t *testing.T,
	sessionID string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/intercom/sessions/"+sessionID+"/relay",
		strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+testInternalToken)
	recorder := httptest.NewRecorder()
	rig.router.ServeHTTP(recorder, request)
	return recorder
}

type intercomEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Data          struct {
		SessionID  string `json:"session_id"`
		DeviceID   string `json:"device_id"`
		State      string `json:"state"`
		RelayPath  string `json:"relay_path"`
		MaxSeconds int    `json:"max_duration_seconds"`
	} `json:"data"`
	Error *struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

func decodeIntercomEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) intercomEnvelope {
	t.Helper()
	var envelope intercomEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode intercom envelope: %v (%s)", err, recorder.Body.String())
	}
	return envelope
}

func (rig *intercomTestRig) dialGuardian(
	t *testing.T,
	sessionID string,
	deviceID string,
) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(rig.server.URL, "http") +
		testIntercomPath + "/" + sessionID + "?device_id=" + deviceID
	return websocket.DefaultDialer.Dial(url, nil)
}

// Table-driven proof that a non-active call never opens a relay.
func TestIntercomAuthorizeRefusesNonActiveSession(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		state      intercom.State
		wantStatus int
		wantCode   string
	}{
		{
			name:       "ringing is refused",
			state:      intercom.StateRinging,
			wantStatus: http.StatusConflict,
			wantCode:   intercomCodeSessionNotActive,
		},
		{
			name:       "declined is refused",
			state:      intercom.StateDeclined,
			wantStatus: http.StatusConflict,
			wantCode:   intercomCodeSessionNotActive,
		},
		{
			name:       "missed is refused",
			state:      intercom.StateMissed,
			wantStatus: http.StatusConflict,
			wantCode:   intercomCodeSessionNotActive,
		},
		{
			name:       "ended is refused",
			state:      intercom.StateEnded,
			wantStatus: http.StatusConflict,
			wantCode:   intercomCodeSessionNotActive,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			session := activeIntercomSession(time.Minute)
			session.State = testCase.state
			rig := newIntercomTestRig(t, session, nil)

			recorder := rig.authorizeRelay(
				t,
				"call_alpha",
				`{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
			)
			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
			envelope := decodeIntercomEnvelope(t, recorder)
			if envelope.Error == nil || envelope.Error.Code != testCase.wantCode {
				t.Fatalf("unexpected error envelope: %+v", envelope)
			}
			if rig.relays.Count() != 0 {
				t.Fatalf("a %s call opened %d relays", testCase.state, rig.relays.Count())
			}
			if rig.device.frameCount() != 0 {
				t.Fatal("a non-active call delivered audio to the device")
			}

			// A guardian that ignores the refusal must still not get a socket.
			connection, response, err := rig.dialGuardian(t, "call_alpha", "device_alpha")
			if connection != nil {
				_ = connection.Close()
			}
			if err == nil {
				t.Fatal("guardian relay accepted a non-active call")
			}
			if response == nil || response.StatusCode != http.StatusNotFound {
				t.Fatalf("expected 404 for an unauthorized relay, got %#v", response)
			}
		})
	}
}

func TestIntercomAuthorizeRejectsUnknownSession(t *testing.T) {
	rig := newIntercomTestRig(t, activeIntercomSession(time.Minute), intercom.ErrSessionNotFound)

	recorder := rig.authorizeRelay(
		t,
		"call_missing",
		`{"session_id":"call_missing","device_id":"device_alpha","device_token":"device-token"}`,
	)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
	envelope := decodeIntercomEnvelope(t, recorder)
	if envelope.Error == nil {
		t.Fatal("expected an error envelope for an unknown session")
	}
	if envelope.Error.Code != intercomCodeSessionNotFound {
		t.Fatalf("error code = %q, want %q", envelope.Error.Code, intercomCodeSessionNotFound)
	}
	if envelope.Error.Retryable {
		t.Fatal("an unknown session must not be reported as retryable")
	}
	if rig.relays.Count() != 0 {
		t.Fatal("an unknown session opened a relay")
	}
}

// Every stable refusal path must return a specific code, never a generic 500.
func TestIntercomAuthorizeReturnsStableErrorCodes(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		resolveErr error
		connErr    error
		sessionID  string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "platform unavailable",
			resolveErr: intercom.ErrPlatformUnavailable,
			sessionID:  "call_alpha",
			body:       `{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   intercomCodeUnavailable,
		},
		{
			name:       "expired session",
			resolveErr: intercom.ErrSessionExpired,
			sessionID:  "call_alpha",
			body:       `{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
			wantStatus: http.StatusGone,
			wantCode:   intercomCodeSessionExpired,
		},
		{
			name:      "device not connected",
			connErr:   intercom.ErrDeviceNotConnected,
			sessionID: "call_alpha",
			body:      `{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
			// The device side will come back while the call is still active.
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   intercomCodeUnavailable,
		},
		{
			name:       "missing device token",
			sessionID:  "call_alpha",
			body:       `{"session_id":"call_alpha","device_id":"device_alpha"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   intercomCodeInvalidRequest,
		},
		{
			name:       "body names another session",
			sessionID:  "call_alpha",
			body:       `{"session_id":"call_other","device_id":"device_alpha","device_token":"device-token"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   intercomCodeInvalidRequest,
		},
		{
			name:       "unknown field",
			sessionID:  "call_alpha",
			body:       `{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token","audio":"AAAA"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   intercomCodeInvalidRequest,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rig := newIntercomTestRig(t, activeIntercomSession(time.Minute), testCase.resolveErr)
			connector := &stubDeviceConnector{sink: rig.device, err: testCase.connErr}
			rig.router = NewRouter(RouterOptions{
				Logger:            slog.New(slog.NewJSONHandler(rig.logBuffer, nil)),
				InternalAPIConfig: config.InternalAPIConfig{Enabled: true, AuthToken: testInternalToken},
				IntercomResolver:  rig.resolver,
				IntercomRelays:    rig.relays,
				IntercomConnector: connector,
				IntercomTransport: IntercomTransportConfig{Path: testIntercomPath},
			})

			recorder := rig.authorizeRelay(t, testCase.sessionID, testCase.body)
			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
			envelope := decodeIntercomEnvelope(t, recorder)
			if envelope.Error == nil || envelope.Error.Code != testCase.wantCode {
				t.Fatalf("unexpected error envelope: %+v", envelope)
			}
			if recorder.Code == http.StatusInternalServerError {
				t.Fatal("intercom refusal must not be a generic internal error")
			}
		})
	}
}

// A malformed path identifier must be refused by the handler itself. It is
// exercised directly because ServeMux sanitizes the path before routing.
func TestIntercomAuthorizeRejectsInvalidPathIdentifier(t *testing.T) {
	handler := intercomHandler{
		resolver:  &stubIntercomResolver{session: activeIntercomSession(time.Minute)},
		relays:    intercom.NewRelayManager(4),
		connector: &stubDeviceConnector{sink: &recordingDeviceSink{}},
		logger:    slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
		config:    IntercomTransportConfig{Path: testIntercomPath},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/intercom/sessions/bad/session/relay",
		strings.NewReader(`{"device_id":"device_alpha","device_token":"device-token"}`),
	)
	request.SetPathValue("session_id", "bad/session")
	recorder := httptest.NewRecorder()
	handler.authorize(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
	envelope := decodeIntercomEnvelope(t, recorder)
	if envelope.Error == nil || envelope.Error.Code != intercomCodeInvalidRequest {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
}

func TestIntercomAuthorizeRequiresServiceToken(t *testing.T) {
	rig := newIntercomTestRig(t, activeIntercomSession(time.Minute), nil)
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/intercom/sessions/call_alpha/relay",
		strings.NewReader(`{"device_id":"device_alpha","device_token":"device-token"}`),
	)
	recorder := httptest.NewRecorder()
	rig.router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if rig.relays.Count() != 0 {
		t.Fatal("an unauthenticated caller opened a relay")
	}
}

// An unauthenticated internal API must not even expose the route.
func TestIntercomRoutesHiddenWhenInternalAPIDisabled(t *testing.T) {
	router := NewRouter(RouterOptions{
		Logger:            slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
		InternalAPIConfig: config.InternalAPIConfig{Enabled: false},
		IntercomResolver:  &stubIntercomResolver{session: activeIntercomSession(time.Minute)},
		IntercomRelays:    intercom.NewRelayManager(4),
		IntercomConnector: &stubDeviceConnector{sink: &recordingDeviceSink{}},
		IntercomTransport: IntercomTransportConfig{Path: testIntercomPath},
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/intercom/sessions/call_alpha/relay",
		strings.NewReader(`{"device_id":"device_alpha","device_token":"device-token"}`),
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

// The accepted path relays guardian audio to the device and device audio back
// to the guardian.
func TestIntercomRelayForwardsBothDirections(t *testing.T) {
	rig := newIntercomTestRig(t, activeIntercomSession(5*time.Second), nil)

	recorder := rig.authorizeRelay(
		t,
		"call_alpha",
		`{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	envelope := decodeIntercomEnvelope(t, recorder)
	if envelope.Data.SessionID != "call_alpha" || envelope.Data.State != string(intercom.StateActive) {
		t.Fatalf("unexpected authorize data: %+v", envelope.Data)
	}
	if envelope.Data.RelayPath != testIntercomPath {
		t.Fatalf("relay path = %q, want %q", envelope.Data.RelayPath, testIntercomPath)
	}

	connection, _, err := rig.dialGuardian(t, "call_alpha", "device_alpha")
	if err != nil {
		t.Fatalf("dial guardian relay: %v", err)
	}
	defer connection.Close()

	if err := connection.WriteMessage(websocket.BinaryMessage, []byte("guardian-pcm")); err != nil {
		t.Fatalf("write guardian audio: %v", err)
	}
	waitForTestCondition(t, 2*time.Second, func() bool { return rig.device.frameCount() == 1 })
	if string(rig.device.lastFrame()) != "guardian-pcm" {
		t.Fatalf("device frame = %q, want guardian-pcm", rig.device.lastFrame())
	}

	// The device side answers through the same relay.
	relay, ok := rig.relays.Relay("call_alpha")
	if !ok {
		t.Fatal("relay is not registered")
	}
	if err := relay.ForwardToGuardian([]byte("device-pcm")); err != nil {
		t.Fatalf("forward to guardian: %v", err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		t.Fatalf("read guardian frame: %v", err)
	}
	if messageType != websocket.BinaryMessage || string(payload) != "device-pcm" {
		t.Fatalf("guardian received type=%d payload=%q", messageType, payload)
	}
}

// A guardian hangup closes the device side immediately.
func TestIntercomGuardianHangupClosesRelay(t *testing.T) {
	rig := newIntercomTestRig(t, activeIntercomSession(5*time.Second), nil)
	rig.authorizeRelay(
		t,
		"call_alpha",
		`{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
	)
	relay, ok := rig.relays.Relay("call_alpha")
	if !ok {
		t.Fatal("relay is not registered")
	}

	connection, _, err := rig.dialGuardian(t, "call_alpha", "device_alpha")
	if err != nil {
		t.Fatalf("dial guardian relay: %v", err)
	}
	defer connection.Close()

	if err := connection.WriteMessage(websocket.TextMessage, []byte(`{"type":"hangup"}`)); err != nil {
		t.Fatalf("write hangup: %v", err)
	}
	waitForTestCondition(t, 2*time.Second, func() bool {
		select {
		case <-relay.Done():
			return true
		default:
			return false
		}
	})
	if relay.CloseReason() != intercom.CloseReasonGuardianHangup {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), intercom.CloseReasonGuardianHangup)
	}
	if rig.relays.Count() != 0 {
		t.Fatalf("relay registry still holds %d relays after hangup", rig.relays.Count())
	}
}

// A device hangup closes the guardian socket immediately.
func TestIntercomDeviceHangupClosesGuardian(t *testing.T) {
	rig := newIntercomTestRig(t, activeIntercomSession(5*time.Second), nil)
	rig.authorizeRelay(
		t,
		"call_alpha",
		`{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
	)
	relay, ok := rig.relays.Relay("call_alpha")
	if !ok {
		t.Fatal("relay is not registered")
	}

	connection, _, err := rig.dialGuardian(t, "call_alpha", "device_alpha")
	if err != nil {
		t.Fatalf("dial guardian relay: %v", err)
	}
	defer connection.Close()

	relay.Close(intercom.CloseReasonDeviceHangup)

	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, readErr := connection.ReadMessage(); readErr != nil {
			var closeErr *websocket.CloseError
			if errors.As(readErr, &closeErr) {
				if closeErr.Text != string(intercom.CloseReasonDeviceHangup) {
					t.Fatalf("guardian close reason = %q, want %q", closeErr.Text, intercom.CloseReasonDeviceHangup)
				}
				return
			}
			t.Fatalf("guardian socket closed without a close frame: %v", readErr)
		}
	}
}

// Reaching the relay duration cap closes both sides cleanly.
func TestIntercomDurationCapClosesBothSides(t *testing.T) {
	// The rig caps at 200ms; the platform deadline is far away, so the cap is
	// what ends the call.
	rig := newIntercomTestRig(t, activeIntercomSession(30*time.Second), nil)
	recorder := rig.authorizeRelay(
		t,
		"call_alpha",
		`{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	relay, ok := rig.relays.Relay("call_alpha")
	if !ok {
		t.Fatal("relay is not registered")
	}

	connection, _, err := rig.dialGuardian(t, "call_alpha", "device_alpha")
	if err != nil {
		t.Fatalf("dial guardian relay: %v", err)
	}
	defer connection.Close()

	started := time.Now()
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, readErr := connection.ReadMessage(); readErr != nil {
			break
		}
	}
	elapsed := time.Since(started)

	if elapsed < 150*time.Millisecond {
		t.Fatalf("relay closed after %v, before the 200ms cap", elapsed)
	}
	select {
	case <-relay.Done():
	default:
		t.Fatal("relay is still open after the duration cap")
	}
	if relay.CloseReason() != intercom.CloseReasonMaxDuration {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), intercom.CloseReasonMaxDuration)
	}
	if rig.relays.Count() != 0 {
		t.Fatalf("relay registry still holds %d relays after the cap", rig.relays.Count())
	}
}

// The duration cap is the smaller of the configured cap and the platform
// deadline, so a call near its platform deadline cannot be extended.
func TestIntercomAuthorizeNarrowsToPlatformDeadline(t *testing.T) {
	rig := newIntercomTestRig(t, activeIntercomSession(1500*time.Millisecond), nil)
	recorder := rig.authorizeRelay(
		t,
		"call_alpha",
		`{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
	)
	envelope := decodeIntercomEnvelope(t, recorder)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if envelope.Data.MaxSeconds != 1 {
		t.Fatalf("max seconds = %d, want 1 (platform deadline is 1.5s)", envelope.Data.MaxSeconds)
	}
}

// A guardian socket without an authorized relay must be refused, which is the
// only way a ringing call could otherwise reach audio.
func TestIntercomRelayRefusesUnattachedSession(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		sessionID  string
		deviceID   string
		wantStatus int
	}{
		{name: "unknown call", sessionID: "call_unknown", deviceID: "device_alpha", wantStatus: http.StatusNotFound},
		{name: "wrong device", sessionID: "call_alpha", deviceID: "device_other", wantStatus: http.StatusNotFound},
		{name: "invalid identifier", sessionID: "bad id", deviceID: "device_alpha", wantStatus: http.StatusUnprocessableEntity},
		{name: "missing device", sessionID: "call_alpha", deviceID: "", wantStatus: http.StatusUnprocessableEntity},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rig := newIntercomTestRig(t, activeIntercomSession(time.Minute), nil)
			connection, response, err := rig.dialGuardian(t, testCase.sessionID, testCase.deviceID)
			if connection != nil {
				_ = connection.Close()
			}
			if err == nil {
				t.Fatal("guardian relay accepted an unattached session")
			}
			if response == nil || response.StatusCode != testCase.wantStatus {
				t.Fatalf("expected status %d, got %#v", testCase.wantStatus, response)
			}
		})
	}
}

// A retried authorize call must reuse the open relay rather than fan audio to
// the device twice.
func TestIntercomAuthorizeIsIdempotentForOpenRelay(t *testing.T) {
	rig := newIntercomTestRig(t, activeIntercomSession(time.Minute), nil)
	body := `{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`
	first := rig.authorizeRelay(t, "call_alpha", body)
	second := rig.authorizeRelay(t, "call_alpha", body)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("authorize statuses = %d, %d, want 200, 200", first.Code, second.Code)
	}
	if rig.relays.Count() != 1 {
		t.Fatalf("open relays = %d, want 1", rig.relays.Count())
	}
}

func TestIntercomRelayAcceptsSingleGuardianOnly(t *testing.T) {
	rig := newIntercomTestRig(t, activeIntercomSession(5*time.Second), nil)
	rig.authorizeRelay(
		t,
		"call_alpha",
		`{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token"}`,
	)
	first, _, err := rig.dialGuardian(t, "call_alpha", "device_alpha")
	if err != nil {
		t.Fatalf("dial first guardian: %v", err)
	}
	defer first.Close()

	second, response, err := rig.dialGuardian(t, "call_alpha", "device_alpha")
	if second != nil {
		_ = second.Close()
	}
	if err == nil {
		t.Fatal("a second guardian socket attached to the same call")
	}
	// The first guardian still holds the relay, so the second attach is a
	// conflict rather than a missing call.
	if response == nil || response.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for a second guardian, got %#v", response)
	}
}

// The close log records lifecycle only: identifier, duration, and reason.
func TestIntercomRelayLogOmitsAudioAndTokens(t *testing.T) {
	rig := newIntercomTestRig(t, activeIntercomSession(5*time.Second), nil)
	rig.authorizeRelay(
		t,
		"call_alpha",
		`{"session_id":"call_alpha","device_id":"device_alpha","device_token":"device-token-secret"}`,
	)
	connection, _, err := rig.dialGuardian(t, "call_alpha", "device_alpha")
	if err != nil {
		t.Fatalf("dial guardian relay: %v", err)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, []byte("AUDIO-CONTENT-MARKER")); err != nil {
		t.Fatalf("write guardian audio: %v", err)
	}
	waitForTestCondition(t, 2*time.Second, func() bool { return rig.device.frameCount() == 1 })
	if err := connection.WriteMessage(websocket.TextMessage, []byte(`{"type":"hangup"}`)); err != nil {
		t.Fatalf("write hangup: %v", err)
	}
	_ = connection.Close()
	waitForTestCondition(t, 2*time.Second, func() bool { return rig.relays.Count() == 0 })

	output := rig.logBuffer.String()
	if !strings.Contains(output, `"session_id":"call_alpha"`) {
		t.Fatalf("close log is missing the session id: %s", output)
	}
	if !strings.Contains(output, `"close_reason"`) {
		t.Fatalf("close log is missing the close reason: %s", output)
	}
	if strings.Contains(output, "AUDIO-CONTENT-MARKER") {
		t.Fatal("access log or close log leaked audio content")
	}
	if strings.Contains(output, "device-token-secret") {
		t.Fatal("logs leaked the device token")
	}
	if strings.Contains(output, testInternalToken) {
		t.Fatal("logs leaked the internal service token")
	}
}

func TestReadyEndpointReportsIntercomCapability(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		enabled bool
		want    string
	}{
		{name: "disabled", enabled: false, want: `"intercom_enabled":false`},
		{name: "enabled", enabled: true, want: `"intercom_enabled":true`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			options := newTestRouterOptions()
			options.IntercomEnabled = testCase.enabled
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			recorder := httptest.NewRecorder()
			NewRouter(options).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			if !strings.Contains(recorder.Body.String(), testCase.want) {
				t.Fatalf("ready response %s does not contain %s", recorder.Body.String(), testCase.want)
			}
		})
	}
}

// The device conversation endpoint is mounted on its own path and must keep
// upgrading through the same middleware chain the intercom relay uses.
func TestRealtimeDevicePathStillUpgrades(t *testing.T) {
	upgraded := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	realtimeHandler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		select {
		case upgraded <- struct{}{}:
		default:
		}
	})

	router := NewRouter(RouterOptions{
		Logger:          slog.New(slog.NewJSONHandler(&lockedBuffer{}, nil)),
		RealtimeHandler: realtimeHandler,
		RealtimePath:    "/v1/voice",
	})
	server := httptest.NewServer(router)
	defer server.Close()

	connection, response, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/v1/voice",
		nil,
	)
	if connection != nil {
		_ = connection.Close()
	}
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("device realtime upgrade failed: %v (status %d)", err, status)
	}
	select {
	case <-upgraded:
	case <-time.After(2 * time.Second):
		t.Fatal("device realtime handler was not reached")
	}
}

func waitForTestCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}
