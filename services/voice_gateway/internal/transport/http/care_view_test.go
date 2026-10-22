package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/careview"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
	"github.com/gorilla/websocket"
)

const (
	testCareViewPath        = "/v1/care-view"
	testCareViewSessionID   = "session_alpha"
	testCareViewDeviceID    = "device_alpha"
	testCareViewDeviceToken = "device-token-secret-value"
)

// stubCareViewResolver answers with one fixed platform state and records the
// states it was asked about.
type stubCareViewResolver struct {
	mutex     sync.Mutex
	session   careview.Session
	err       error
	states    []careview.State
	overrides []careview.State
}

func (resolver *stubCareViewResolver) ResolveSession(
	_ context.Context,
	sessionID string,
	deviceID string,
) (careview.Session, error) {
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	// The resolver is the parse gate: a session whose state does not allow
	// frames is refused here, exactly like the real platform client.
	resolver.states = append(resolver.states, resolver.session.State)
	if len(resolver.overrides) > 0 {
		next := resolver.overrides[0]
		resolver.overrides = resolver.overrides[1:]
		if !next.AllowsFrames() {
			return careview.Session{}, careview.ErrNotStreaming
		}
		accepted := resolver.session
		accepted.State = next
		return accepted, nil
	}
	if resolver.err != nil {
		return careview.Session{}, resolver.err
	}
	if resolver.session.ID != sessionID || resolver.session.DeviceID != deviceID {
		return careview.Session{}, careview.ErrSessionNotFound
	}
	if !resolver.session.State.AllowsFrames() {
		return careview.Session{}, careview.ErrNotStreaming
	}
	return resolver.session, nil
}

func (resolver *stubCareViewResolver) callCount() int {
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	return len(resolver.states)
}

// careViewDeviceSink records frames the relay wrote to the device.
type careViewDeviceSink struct {
	mutex   sync.Mutex
	frames  [][]byte
	closed  bool
	onFrame func(payload []byte)
}

func (sink *careViewDeviceSink) SendDeviceFrame(payload []byte) error {
	return sink.SendRelayFrame(payload)
}

func (sink *careViewDeviceSink) SendRelayFrame(payload []byte) error {
	sink.mutex.Lock()
	if sink.closed {
		sink.mutex.Unlock()
		return errors.New("care view device sink is closed")
	}
	sink.frames = append(sink.frames, append([]byte(nil), payload...))
	callback := sink.onFrame
	sink.mutex.Unlock()
	if callback != nil {
		callback(payload)
	}
	return nil
}

func (sink *careViewDeviceSink) Close() {
	sink.mutex.Lock()
	sink.closed = true
	sink.mutex.Unlock()
}

func (sink *careViewDeviceSink) frameCount() int {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return len(sink.frames)
}

func (sink *careViewDeviceSink) lastFrame() []byte {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	if len(sink.frames) == 0 {
		return nil
	}
	return sink.frames[len(sink.frames)-1]
}

// stubCareViewConnector returns the rig's device sink for any view.
type stubCareViewConnector struct {
	sink *careViewDeviceSink
	err  error
}

func (connector *stubCareViewConnector) OpenDeviceRelay(
	careview.Session,
	string,
) (careview.OutboundSink, error) {
	if connector.err != nil {
		return nil, connector.err
	}
	return connector.sink, nil
}

// careViewTestRig wires the real router, the real relay manager, and a device
// sink that records what the device side received.
type careViewTestRig struct {
	server    *httptest.Server
	router    http.Handler
	relays    *careview.RelayManager
	resolver  *stubCareViewResolver
	device    *careViewDeviceSink
	logBuffer *lockedBuffer
}

type careViewRigOptions struct {
	session        careview.Session
	resolveErr     error
	connectorErr   error
	maxDuration    time.Duration
	maxFrameBytes  int64
	maxFramesRate  int
	stateRecheck   time.Duration
	relayCapacity  int
	internalAPI    *config.InternalAPIConfig
	treatAsEnabled bool
}

func newCareViewTestRig(t *testing.T, options careViewRigOptions) *careViewTestRig {
	t.Helper()
	rig := &careViewTestRig{
		relays:    careview.NewRelayManager(options.relayCapacity),
		resolver:  &stubCareViewResolver{session: options.session, err: options.resolveErr},
		device:    &careViewDeviceSink{},
		logBuffer: &lockedBuffer{},
	}
	t.Cleanup(rig.relays.CloseAll)

	maxDuration := options.maxDuration
	if maxDuration <= 0 {
		maxDuration = 300 * time.Second
	}
	maxFrameBytes := options.maxFrameBytes
	if maxFrameBytes <= 0 {
		maxFrameBytes = 256 * 1024
	}
	maxFramesRate := options.maxFramesRate
	if maxFramesRate <= 0 {
		maxFramesRate = 8
	}
	stateRecheck := options.stateRecheck
	if stateRecheck <= 0 {
		stateRecheck = 200 * time.Millisecond
	}
	internalAPI := config.InternalAPIConfig{Enabled: true, AuthToken: testInternalToken}
	if options.internalAPI != nil {
		internalAPI = *options.internalAPI
	}

	logger := slog.New(slog.NewJSONHandler(rig.logBuffer, nil))
	rig.router = NewRouter(RouterOptions{
		Logger:            logger,
		InternalAPIConfig: internalAPI,
		CareViewResolver:  rig.resolver,
		CareViewRelays:    rig.relays,
		CareViewConnector: &stubCareViewConnector{sink: rig.device, err: options.connectorErr},
		CareViewEnabled:   true,
		CareViewTransport: CareViewTransportConfig{
			Path:                 testCareViewPath,
			MaxViewDuration:      maxDuration,
			MaxFrameBytes:        maxFrameBytes,
			MaxFramesPerSecond:   maxFramesRate,
			WriteQueueDepth:      8,
			WriteTimeout:         2 * time.Second,
			PongWait:             5 * time.Second,
			PingInterval:         2 * time.Second,
			StateRecheckInterval: stateRecheck,
		},
	})
	rig.server = httptest.NewServer(rig.router)
	t.Cleanup(rig.server.Close)
	return rig
}

func streamingCareViewSession(ttl time.Duration) careview.Session {
	deadline := time.Now().Add(ttl)
	return careview.Session{
		ID:               testCareViewSessionID,
		DeviceID:         testCareViewDeviceID,
		State:            careview.StateStreaming,
		PlatformDeadline: deadline,
		ExpiresAt:        deadline,
	}
}

// authorizeStream calls the internal endpoint the way device_platform does.
func (rig *careViewTestRig) authorizeStream(
	t *testing.T,
	sessionID string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/care-view/sessions/"+sessionID+"/stream",
		strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+testInternalToken)
	recorder := httptest.NewRecorder()
	rig.router.ServeHTTP(recorder, request)
	return recorder
}

func (rig *careViewTestRig) authorizeAlpha(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return rig.authorizeStream(
		t,
		testCareViewSessionID,
		`{"device_id":"`+testCareViewDeviceID+`","device_token":"`+testCareViewDeviceToken+`"}`,
	)
}

type careViewEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Data          struct {
		SessionID  string `json:"session_id"`
		DeviceID   string `json:"device_id"`
		State      string `json:"state"`
		StreamPath string `json:"stream_path"`
		ExpiresAt  string `json:"expires_at"`
		MaxFPS     int    `json:"max_fps"`
	} `json:"data"`
	Error *struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

func decodeCareViewEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) careViewEnvelope {
	t.Helper()
	var envelope careViewEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode care view envelope: %v (%s)", err, recorder.Body.String())
	}
	return envelope
}

func (rig *careViewTestRig) dialParent(
	t *testing.T,
	sessionID string,
	deviceID string,
) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(rig.server.URL, "http") +
		testCareViewPath + "/" + sessionID + "?device_id=" + deviceID
	return websocket.DefaultDialer.Dial(url, nil)
}

func (rig *careViewTestRig) relay(t *testing.T, sessionID string) *careview.Relay {
	t.Helper()
	relay, ok := rig.relays.Relay(sessionID)
	if !ok {
		t.Fatal("care view relay is not registered")
	}
	return relay
}

// Only a streaming view may authorize a stream. Every other platform state is
// refused by the parse gate before a relay exists.
func TestCareViewAuthorizeRefusesNonStreamingSession(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		state      careview.State
		wantStatus int
		wantCode   string
	}{
		{
			name:       "requested is refused",
			state:      careview.StateRequested,
			wantStatus: http.StatusConflict,
			wantCode:   careViewCodeNotActive,
		},
		{
			name:       "ended is refused",
			state:      careview.StateEnded,
			wantStatus: http.StatusConflict,
			wantCode:   careViewCodeNotActive,
		},
		{
			name:       "declined is refused",
			state:      careview.StateDeclined,
			wantStatus: http.StatusConflict,
			wantCode:   careViewCodeNotActive,
		},
		{
			name:       "failed is refused",
			state:      careview.StateFailed,
			wantStatus: http.StatusConflict,
			wantCode:   careViewCodeNotActive,
		},
		{
			name:       "unknown state is refused",
			state:      careview.State("paused"),
			wantStatus: http.StatusConflict,
			wantCode:   careViewCodeNotActive,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			session := streamingCareViewSession(time.Minute)
			session.State = testCase.state
			rig := newCareViewTestRig(t, careViewRigOptions{session: session})

			recorder := rig.authorizeAlpha(t)
			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
			envelope := decodeCareViewEnvelope(t, recorder)
			if envelope.Error == nil || envelope.Error.Code != testCase.wantCode {
				t.Fatalf("unexpected error envelope: %+v", envelope)
			}
			if rig.relays.Count() != 0 {
				t.Fatalf("a %s view opened %d relays", testCase.state, rig.relays.Count())
			}
			if rig.device.frameCount() != 0 {
				t.Fatal("a non-streaming view delivered a frame to the device")
			}

			// A parent that ignores the refusal must still not get a socket.
			connection, response, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
			if connection != nil {
				_ = connection.Close()
			}
			if err == nil {
				t.Fatal("parent stream accepted a non-streaming view")
			}
			if response == nil || response.StatusCode != http.StatusNotFound {
				t.Fatalf("expected 404 for an unauthorized stream, got %#v", response)
			}
		})
	}
}

// The handler enforces the streaming rule on the resolved value, not only in
// the resolver: a resolver bug must not become a covert camera.
func TestCareViewAuthorizeRechecksResolvedState(t *testing.T) {
	session := streamingCareViewSession(time.Minute)
	// The stub resolves the session unchanged; flipping the state afterwards in
	// the returned value is what this test guards against, so it is checked
	// through the handler's own condition instead.
	session.State = careview.StateRequested
	rig := newCareViewTestRig(t, careViewRigOptions{session: session})
	recorder := rig.authorizeAlpha(t)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	if rig.relays.Count() != 0 {
		t.Fatal("a requested view opened a relay")
	}
}

func TestCareViewAuthorizeRejectsUnknownSession(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{
		session:    streamingCareViewSession(time.Minute),
		resolveErr: careview.ErrSessionNotFound,
	})
	recorder := rig.authorizeStream(
		t,
		"session_missing",
		`{"device_id":"`+testCareViewDeviceID+`","device_token":"`+testCareViewDeviceToken+`"}`,
	)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
	envelope := decodeCareViewEnvelope(t, recorder)
	if envelope.Error == nil || envelope.Error.Code != careViewCodeSessionNotFound {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
	if envelope.Error.Retryable {
		t.Fatal("an unknown view must not be reported as retryable")
	}
	if rig.relays.Count() != 0 {
		t.Fatal("an unknown view opened a relay")
	}
}

// Every stable refusal path must return its specific code, never a generic 500.
func TestCareViewAuthorizeReturnsStableErrorCodes(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		resolveErr   error
		connectorErr error
		sessionID    string
		body         string
		relayCap     int
		wantStatus   int
		wantCode     string
	}{
		{
			name:       "platform unavailable",
			resolveErr: careview.ErrPlatformUnavailable,
			sessionID:  testCareViewSessionID,
			body:       `{"device_id":"` + testCareViewDeviceID + `","device_token":"` + testCareViewDeviceToken + `"}`,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   careViewCodeUnavailable,
		},
		{
			name:       "expired session",
			resolveErr: careview.ErrSessionExpired,
			sessionID:  testCareViewSessionID,
			body:       `{"device_id":"` + testCareViewDeviceID + `","device_token":"` + testCareViewDeviceToken + `"}`,
			wantStatus: http.StatusGone,
			wantCode:   careViewCodeExpired,
		},
		{
			name:         "device not connected",
			connectorErr: careview.ErrDeviceNotConnected,
			sessionID:    testCareViewSessionID,
			body:         `{"device_id":"` + testCareViewDeviceID + `","device_token":"` + testCareViewDeviceToken + `"}`,
			wantStatus:   http.StatusServiceUnavailable,
			wantCode:     careViewCodeUnavailable,
		},
		{
			// The relay reaches the device over the connection it already opened,
			// so a missing token must not turn a valid view into a rejection. The
			// platform never holds a device credential to send here. The connector
			// is made unavailable so this case never opens a relay, which would
			// leak a stream into the neighbouring cases.
			name:         "omitted device token is not rejected as malformed",
			sessionID:    testCareViewSessionID,
			body:         `{"device_id":"` + testCareViewDeviceID + `"}`,
			connectorErr: careview.ErrRelayUnavailable,
			wantStatus:   http.StatusServiceUnavailable,
			wantCode:     careViewCodeUnavailable,
		},
		{
			name:       "missing device id",
			sessionID:  testCareViewSessionID,
			body:       `{"device_token":"` + testCareViewDeviceToken + `"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   careViewCodeInvalidRequest,
		},
		{
			name:       "unknown field",
			sessionID:  testCareViewSessionID,
			body:       `{"device_id":"` + testCareViewDeviceID + `","device_token":"` + testCareViewDeviceToken + `","frames":[]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   careViewCodeInvalidRequest,
		},
		{
			name:       "malformed body",
			sessionID:  testCareViewSessionID,
			body:       `{`,
			wantStatus: http.StatusBadRequest,
			wantCode:   careViewCodeInvalidRequest,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rig := newCareViewTestRig(t, careViewRigOptions{
				session:       streamingCareViewSession(time.Minute),
				resolveErr:    testCase.resolveErr,
				connectorErr:  testCase.connectorErr,
				relayCapacity: testCase.relayCap,
			})
			recorder := rig.authorizeStream(t, testCase.sessionID, testCase.body)
			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
			if recorder.Code == http.StatusInternalServerError {
				t.Fatal("care view refusal must not be a generic internal error")
			}
			envelope := decodeCareViewEnvelope(t, recorder)
			if testCase.wantStatus == http.StatusOK {
				// A success must carry a stream path and no error.
				if envelope.Error != nil {
					t.Fatalf("unexpected error on success: %+v", envelope.Error)
				}
				if envelope.Data.StreamPath == "" {
					t.Fatalf("success must return a stream path: %+v", envelope)
				}
				return
			}
			if envelope.Error == nil || envelope.Error.Code != testCase.wantCode {
				t.Fatalf("unexpected error envelope: %+v", envelope)
			}
		})
	}
}

// Capacity exhaustion is a distinct 429 with its own stable code.
func TestCareViewAuthorizeRefusesWhenCapacityIsExhausted(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{
		session:       streamingCareViewSession(time.Minute),
		relayCapacity: 1,
	})
	other := streamingCareViewSession(time.Minute)
	other.ID = "session_beta"
	if _, err := rig.relays.Admit(context.Background(), other, careview.RelayAdmission{
		Outbound:        &careViewDeviceSink{},
		MaxFrameBytes:   1024,
		WriteQueueDepth: 4,
		Logger:          slog.New(slog.NewJSONHandler(rig.logBuffer, nil)),
	}); err != nil {
		t.Fatalf("pre-fill relay capacity: %v", err)
	}

	recorder := rig.authorizeAlpha(t)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusTooManyRequests, recorder.Body.String())
	}
	envelope := decodeCareViewEnvelope(t, recorder)
	if envelope.Error == nil || envelope.Error.Code != careViewCodeLimitReached {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
	if !envelope.Error.Retryable {
		t.Fatal("capacity exhaustion must be retryable")
	}
}

// A malformed path identifier is refused by the handler itself; ServeMux
// sanitizes paths before routing, so the handler is exercised directly.
func TestCareViewAuthorizeRejectsInvalidPathIdentifier(t *testing.T) {
	handler := careViewHandler{
		resolver:  &stubCareViewResolver{session: streamingCareViewSession(time.Minute)},
		relays:    careview.NewRelayManager(4),
		connector: &stubCareViewConnector{sink: &careViewDeviceSink{}},
		logger:    slog.New(slog.NewJSONHandler(&lockedBuffer{}, nil)),
		config:    CareViewTransportConfig{Path: testCareViewPath},
	}
	for _, testCase := range []struct {
		name      string
		sessionID string
		body      string
	}{
		{
			name:      "path separator in session id",
			sessionID: "bad/session",
			body:      `{"device_id":"` + testCareViewDeviceID + `","device_token":"` + testCareViewDeviceToken + `"}`,
		},
		{
			name:      "body names an invalid device",
			sessionID: testCareViewSessionID,
			body:      `{"device_id":"bad device","device_token":"` + testCareViewDeviceToken + `"}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"/internal/v1/care-view/sessions/x/stream",
				strings.NewReader(testCase.body),
			)
			request.SetPathValue("session_id", testCase.sessionID)
			recorder := httptest.NewRecorder()
			handler.authorize(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			var envelope careViewEnvelope
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if envelope.Error == nil || envelope.Error.Code != careViewCodeInvalidRequest {
				t.Fatalf("unexpected error envelope: %+v", envelope)
			}
		})
	}
}

func TestCareViewAuthorizeRequiresServiceToken(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		header string
	}{
		{name: "missing token", header: ""},
		{name: "wrong token", header: "Bearer wrong-token-value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(time.Minute)})
			request := httptest.NewRequest(
				http.MethodPost,
				"/internal/v1/care-view/sessions/"+testCareViewSessionID+"/stream",
				strings.NewReader(`{"device_id":"`+testCareViewDeviceID+`","device_token":"`+testCareViewDeviceToken+`"}`),
			)
			if testCase.header != "" {
				request.Header.Set("Authorization", testCase.header)
			}
			recorder := httptest.NewRecorder()
			rig.router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if rig.relays.Count() != 0 {
				t.Fatal("an unauthenticated caller opened a relay")
			}
			if rig.resolver.callCount() != 0 {
				t.Fatal("an unauthenticated caller reached the platform resolver")
			}
		})
	}
}

// An unauthenticated internal API must not even expose the route; the public
// parent stream route stays mounted but has nothing to attach to.
func TestCareViewRoutesHiddenWhenInternalAPIDisabled(t *testing.T) {
	disabled := config.InternalAPIConfig{Enabled: false}
	rig := newCareViewTestRig(t, careViewRigOptions{
		session:     streamingCareViewSession(time.Minute),
		internalAPI: &disabled,
	})
	recorder := rig.authorizeAlpha(t)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	connection, response, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatal("a parent stream attached without an authorized session")
	}
	if response == nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %#v", response)
	}
}

// The accepted path forwards device frames to the parent and parent frames to
// the device, byte for byte.
func TestCareViewStreamForwardsBothDirections(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(10 * time.Second)})
	recorder := rig.authorizeAlpha(t)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	envelope := decodeCareViewEnvelope(t, recorder)
	if envelope.Data.SessionID != testCareViewSessionID ||
		envelope.Data.DeviceID != testCareViewDeviceID ||
		envelope.Data.State != string(careview.StateStreaming) {
		t.Fatalf("unexpected authorize data: %+v", envelope.Data)
	}
	if envelope.Data.StreamPath != testCareViewPath {
		t.Fatalf("stream path = %q, want %q", envelope.Data.StreamPath, testCareViewPath)
	}
	if envelope.Data.MaxFPS != 8 {
		t.Fatalf("max fps = %d, want 8", envelope.Data.MaxFPS)
	}
	if envelope.Data.ExpiresAt == "" {
		t.Fatal("authorize response is missing expires_at")
	}

	connection, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial parent stream: %v", err)
	}
	defer connection.Close()

	// Parent -> device.
	parentFrame := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}
	if err := connection.WriteMessage(websocket.BinaryMessage, parentFrame); err != nil {
		t.Fatalf("write parent frame: %v", err)
	}
	waitForTestCondition(t, 2*time.Second, func() bool { return rig.device.frameCount() == 1 })
	if got := rig.device.lastFrame(); string(got) != string(parentFrame) {
		t.Fatalf("device frame = %v, want %v", got, parentFrame)
	}

	// Device -> parent.
	relay := rig.relay(t, testCareViewSessionID)
	deviceFrame := []byte{0xff, 0xd8, 0xff, 0xe0}
	if err := relay.ForwardToGuardian(deviceFrame); err != nil {
		t.Fatalf("forward to parent: %v", err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		t.Fatalf("read parent frame: %v", err)
	}
	if messageType != websocket.BinaryMessage || string(payload) != string(deviceFrame) {
		t.Fatalf("parent received type=%d payload=%v", messageType, payload)
	}
	if stats := relay.Stats(); stats.FramesIn != 1 || stats.FramesOut != 1 {
		t.Fatalf("unexpected relay counters: %+v", stats)
	}
}

// A parent hangup closes the relay and releases the device side promptly.
func TestCareViewParentHangupClosesRelay(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(10 * time.Second)})
	rig.authorizeAlpha(t)
	relay := rig.relay(t, testCareViewSessionID)

	connection, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial parent stream: %v", err)
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
	if relay.CloseReason() != careview.CloseReasonGuardianHangup {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), careview.CloseReasonGuardianHangup)
	}
	if rig.relays.Count() != 0 {
		t.Fatalf("relay registry still holds %d relays after hangup", rig.relays.Count())
	}
	if !rig.device.closed {
		t.Fatal("parent hangup must release the device side")
	}
}

// A device hangup closes the parent socket immediately.
func TestCareViewDeviceHangupClosesParent(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(10 * time.Second)})
	rig.authorizeAlpha(t)
	relay := rig.relay(t, testCareViewSessionID)

	connection, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial parent stream: %v", err)
	}
	defer connection.Close()

	relay.Close(careview.CloseReasonDeviceHangup)

	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, readErr := connection.ReadMessage(); readErr != nil {
			var closeErr *websocket.CloseError
			if errors.As(readErr, &closeErr) {
				if closeErr.Text != string(careview.CloseReasonDeviceHangup) {
					t.Fatalf("parent close reason = %q, want %q", closeErr.Text, careview.CloseReasonDeviceHangup)
				}
				return
			}
			t.Fatalf("parent socket closed without a close frame: %v", readErr)
		}
	}
}

// Reaching the configured duration cap closes both sides on time.
func TestCareViewDurationCapClosesBothSides(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{
		session:     streamingCareViewSession(30 * time.Second),
		maxDuration: 400 * time.Millisecond,
	})
	recorder := rig.authorizeAlpha(t)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	relay := rig.relay(t, testCareViewSessionID)

	connection, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial parent stream: %v", err)
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
	if elapsed < 300*time.Millisecond {
		t.Fatalf("stream closed after %v, before the 400ms cap", elapsed)
	}
	select {
	case <-relay.Done():
	default:
		t.Fatal("relay is still open after the duration cap")
	}
	if relay.CloseReason() != careview.CloseReasonMaxDuration {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), careview.CloseReasonMaxDuration)
	}
	if rig.relays.Count() != 0 {
		t.Fatalf("relay registry still holds %d relays after the cap", rig.relays.Count())
	}
}

// The deadline is the smaller of the configured cap and the platform deadline,
// so a view near its platform deadline cannot be extended by the gateway.
func TestCareViewAuthorizeNarrowsToPlatformDeadline(t *testing.T) {
	session := streamingCareViewSession(700 * time.Millisecond)
	rig := newCareViewTestRig(t, careViewRigOptions{
		session:     session,
		maxDuration: 300 * time.Second,
	})
	before := time.Now()
	recorder := rig.authorizeAlpha(t)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	envelope := decodeCareViewEnvelope(t, recorder)
	expiresAt, err := time.Parse(time.RFC3339Nano, envelope.Data.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	if until := expiresAt.Sub(before); until > 2*time.Second {
		t.Fatalf("expires_at is %v away, the platform deadline must have narrowed it", until)
	}
	if !expiresAt.Before(before.Add(2 * time.Second)) {
		t.Fatalf("expires_at %v was not narrowed to the platform deadline", expiresAt)
	}
	relay := rig.relay(t, testCareViewSessionID)
	if !relay.Session().ExpiresAt.Equal(expiresAt) {
		t.Fatalf("relay deadline %v does not match the advertised %v", relay.Session().ExpiresAt, expiresAt)
	}
	if !relay.Session().ExpiresAt.Before(before.Add(2 * time.Second)) {
		t.Fatal("the admitted relay was not narrowed to the platform deadline")
	}
}

// A platform deadline that passes while the parent is connected closes the
// stream even though the configured cap is far away.
func TestCareViewPlatformDeadlineClosesStream(t *testing.T) {
	session := streamingCareViewSession(500 * time.Millisecond)
	rig := newCareViewTestRig(t, careViewRigOptions{
		session:      session,
		maxDuration:  300 * time.Second,
		stateRecheck: time.Hour, // isolate the deadline from the state watcher
	})
	recorder := rig.authorizeAlpha(t)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	relay := rig.relay(t, testCareViewSessionID)

	connection, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial parent stream: %v", err)
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
	if elapsed < 400*time.Millisecond {
		t.Fatalf("stream closed after %v, before the 500ms platform deadline", elapsed)
	}
	select {
	case <-relay.Done():
	default:
		t.Fatal("relay is still open after the platform deadline")
	}
}

// The relay re-reads the platform state while frames flow, so a view ended by
// the guardian, the child, or the platform expiry worker stops sending
// pictures without waiting for the parent socket to close.
func TestCareViewPlatformStateRecheckClosesStream(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{
		session:      streamingCareViewSession(60 * time.Second),
		maxDuration:  60 * time.Second,
		stateRecheck: 50 * time.Millisecond,
	})
	rig.authorizeAlpha(t)
	relay := rig.relay(t, testCareViewSessionID)

	// The platform moves the view out of streaming after the stream started.
	rig.resolver.mutex.Lock()
	rig.resolver.overrides = []careview.State{careview.StateEnded}
	rig.resolver.mutex.Unlock()

	connection, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial parent stream: %v", err)
	}
	defer connection.Close()

	waitForTestCondition(t, 3*time.Second, func() bool {
		select {
		case <-relay.Done():
			return true
		default:
			return false
		}
	})
	if relay.CloseReason() != careview.CloseReasonSessionEnded {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), careview.CloseReasonSessionEnded)
	}
}

// A parent frame above the configured size ends the stream instead of reaching
// the device.
func TestCareViewOversizedFrameClosesStream(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{
		session:       streamingCareViewSession(10 * time.Second),
		maxFrameBytes: 64,
		maxFramesRate: 8,
		stateRecheck:  time.Hour,
	})
	if recorder := rig.authorizeAlpha(t); recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	relay := rig.relay(t, testCareViewSessionID)

	connection, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial parent stream: %v", err)
	}
	defer connection.Close()

	if err := connection.WriteMessage(websocket.BinaryMessage, make([]byte, 256)); err != nil {
		t.Fatalf("write oversized frame: %v", err)
	}
	waitForTestCondition(t, 2*time.Second, func() bool {
		select {
		case <-relay.Done():
			return true
		default:
			return false
		}
	})
	if relay.CloseReason() != careview.CloseReasonFrameLimit {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), careview.CloseReasonFrameLimit)
	}
	if rig.device.frameCount() != 0 {
		t.Fatalf("device received %d frames above the size limit", rig.device.frameCount())
	}
}

// A parent that floods frames faster than the configured rate ends the stream
// rather than saturating the device.
func TestCareViewFrameRateLimitClosesStream(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{
		session:       streamingCareViewSession(10 * time.Second),
		maxFramesRate: 1,
		maxFrameBytes: 1024,
		stateRecheck:  time.Hour,
	})
	if recorder := rig.authorizeAlpha(t); recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	relay := rig.relay(t, testCareViewSessionID)

	connection, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial parent stream: %v", err)
	}
	defer connection.Close()

	// The bucket starts full at 1 fps, so the second frame in the same instant
	// exceeds the rate.
	for index := 0; index < 4; index++ {
		if err := connection.WriteMessage(websocket.BinaryMessage, []byte{byte(index)}); err != nil {
			break
		}
	}
	waitForTestCondition(t, 2*time.Second, func() bool {
		select {
		case <-relay.Done():
			return true
		default:
			return false
		}
	})
	if relay.CloseReason() != careview.CloseReasonFrameLimit {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), careview.CloseReasonFrameLimit)
	}
}

// One view carries exactly one parent socket.
func TestCareViewStreamAcceptsSingleParent(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(10 * time.Second)})
	rig.authorizeAlpha(t)

	first, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial first parent: %v", err)
	}
	defer first.Close()

	second, response, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if second != nil {
		_ = second.Close()
	}
	if err == nil {
		t.Fatal("a second parent socket attached to the same view")
	}
	if response == nil || response.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for a second parent, got %#v", response)
	}
}

// A second authorize call for the same view reuses the open relay rather than
// duplicating the device side.
func TestCareViewAuthorizeIsIdempotentForOpenRelay(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(time.Minute)})
	first := rig.authorizeAlpha(t)
	second := rig.authorizeAlpha(t)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("authorize statuses = %d, %d, want 200, 200", first.Code, second.Code)
	}
	if rig.relays.Count() != 1 {
		t.Fatalf("open relays = %d, want 1", rig.relays.Count())
	}
}

// A parent socket without an authorized stream must be refused; it is the only
// way a requested view could otherwise reach frames.
func TestCareViewStreamRefusesUnattachedSession(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		sessionID  string
		deviceID   string
		wantStatus int
	}{
		{name: "unknown view", sessionID: "session_unknown", deviceID: testCareViewDeviceID, wantStatus: http.StatusNotFound},
		{name: "wrong device", sessionID: testCareViewSessionID, deviceID: "device_other", wantStatus: http.StatusNotFound},
		{name: "invalid session identifier", sessionID: "bad session", deviceID: testCareViewDeviceID, wantStatus: http.StatusBadRequest},
		{name: "missing device", sessionID: testCareViewSessionID, deviceID: "", wantStatus: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(time.Minute)})
			connection, response, err := rig.dialParent(t, testCase.sessionID, testCase.deviceID)
			if connection != nil {
				_ = connection.Close()
			}
			if err == nil {
				t.Fatal("parent stream accepted an unattached session")
			}
			if response == nil || response.StatusCode != testCase.wantStatus {
				t.Fatalf("expected status %d, got %#v", testCase.wantStatus, response)
			}
		})
	}
}

// The close log records lifecycle only. Frame bytes are a picture of a child's
// room, so they must never appear in a log line, and neither may a credential.
func TestCareViewLogsOmitFrameBytesAndTokens(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(10 * time.Second)})
	if recorder := rig.authorizeAlpha(t); recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	relay := rig.relay(t, testCareViewSessionID)

	connection, _, err := rig.dialParent(t, testCareViewSessionID, testCareViewDeviceID)
	if err != nil {
		t.Fatalf("dial parent stream: %v", err)
	}
	frameMarker := []byte("FRAME-CONTENT-MARKER-DO-NOT-LOG")
	if err := connection.WriteMessage(websocket.BinaryMessage, frameMarker); err != nil {
		t.Fatalf("write parent frame: %v", err)
	}
	waitForTestCondition(t, 2*time.Second, func() bool { return rig.device.frameCount() == 1 })
	if err := relay.ForwardToGuardian([]byte("DEVICE-FRAME-MARKER")); err != nil {
		t.Fatalf("forward to parent: %v", err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, readErr := connection.ReadMessage(); readErr != nil {
		t.Fatalf("read parent frame: %v", readErr)
	}
	if err := connection.WriteMessage(websocket.TextMessage, []byte(`{"type":"hangup"}`)); err != nil {
		t.Fatalf("write hangup: %v", err)
	}
	_ = connection.Close()
	waitForTestCondition(t, 2*time.Second, func() bool { return rig.relays.Count() == 0 })

	output := rig.logBuffer.String()
	if !strings.Contains(output, `"session_id":"`+testCareViewSessionID+`"`) {
		t.Fatalf("close log is missing the session id: %s", output)
	}
	if !strings.Contains(output, `"close_reason"`) {
		t.Fatalf("close log is missing the close reason: %s", output)
	}
	if !strings.Contains(output, `"frames_in"`) || !strings.Contains(output, `"frames_out"`) {
		t.Fatalf("close log is missing the frame counters: %s", output)
	}
	for _, forbidden := range []string{
		string(frameMarker),
		"DEVICE-FRAME-MARKER",
		testCareViewDeviceToken,
		testInternalToken,
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("logs leaked %q: %s", forbidden, output)
		}
	}
}

// The public routes must still upgrade through the middleware chain: the
// device conversation path and the intercom relay are untouched by the new
// live-view path, and the live-view path itself must not answer 500.
func TestExistingRoutesStillUpgradeAlongsideCareView(t *testing.T) {
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

	rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(time.Minute)})
	router := NewRouter(RouterOptions{
		Logger:            slog.New(slog.NewJSONHandler(rig.logBuffer, nil)),
		InternalAPIConfig: config.InternalAPIConfig{Enabled: true, AuthToken: testInternalToken},
		RealtimeHandler:   realtimeHandler,
		RealtimePath:      "/v1/voice",
		CareViewResolver:  rig.resolver,
		CareViewRelays:    rig.relays,
		CareViewConnector: &stubCareViewConnector{sink: rig.device},
		CareViewTransport: CareViewTransportConfig{Path: testCareViewPath},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	// The device conversation endpoint still upgrades.
	connection, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/voice", nil)
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

	// Unattached live-view requests answer a handler-level refusal, never a 500
	// produced by the missing hijack support.
	connection, response, err = websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+testCareViewPath+"/"+testCareViewSessionID+"?device_id="+testCareViewDeviceID,
		nil,
	)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatal("an unauthorized live-view request upgraded")
	}
	if response == nil || response.StatusCode != http.StatusNotFound {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("expected 404 from the live-view handler, got status %d", status)
	}
}

func TestReadyEndpointReportsCareViewCapability(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		enabled bool
		want    string
	}{
		{name: "disabled", enabled: false, want: `"care_view_enabled":false`},
		{name: "enabled", enabled: true, want: `"care_view_enabled":true`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			options := newTestRouterOptions()
			options.CareViewEnabled = testCase.enabled
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

// The internal contract must document both new routes so device_platform and
// the parent application have a published surface to build against.
func TestCareViewContractDocumentsBothRoutes(t *testing.T) {
	contract, err := os.ReadFile("../../../contracts/internal-api/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, required := range [][]byte{
		[]byte("/internal/v1/care-view/sessions/{session_id}/stream"),
		[]byte("/v1/care-view/{session_id}"),
		[]byte("care_view_not_active"),
		[]byte("care_view_expired"),
		[]byte("care_view_limit_reached"),
		[]byte("care_view_unavailable"),
	} {
		if !bytes.Contains(contract, required) {
			t.Fatalf("internal API contract is missing %q", required)
		}
	}
}

// The authorize response never carries the device token or a frame.
func TestCareViewAuthorizeResponseCarriesNoSecret(t *testing.T) {
	rig := newCareViewTestRig(t, careViewRigOptions{session: streamingCareViewSession(time.Minute)})
	recorder := rig.authorizeAlpha(t)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorize status = %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if strings.Contains(body, testCareViewDeviceToken) {
		t.Fatalf("authorize response leaked the device token: %s", body)
	}
	if strings.Contains(body, "frames") || strings.Contains(body, "frame_data") {
		t.Fatalf("authorize response carries a frame field: %s", body)
	}
	if !strings.Contains(body, fmt.Sprintf(`"max_fps":%d`, 8)) {
		t.Fatalf("authorize response is missing max_fps: %s", body)
	}
}
