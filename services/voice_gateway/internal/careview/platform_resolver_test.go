package careview

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testResolverToken = "unit-test-care-view-service-token-0123456789"

func newTestResolver(t *testing.T, handler http.HandlerFunc) *PlatformSessionResolver {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	resolver, err := NewPlatformSessionResolver(PlatformResolverConfig{
		BaseURL:      server.URL,
		ServiceToken: testResolverToken,
		Timeout:      2 * time.Second,
	})
	if err != nil {
		t.Fatalf("create platform resolver: %v", err)
	}
	return resolver
}

func writePlatformSession(writer http.ResponseWriter, status int, session platformSession) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"schema_version": "1.0.0",
		"request_id":     "request-123456",
		"data":           session,
		"error":          nil,
	})
}

// The resolver is the gateway's parse gate. Every non-streaming platform state
// must be refused, otherwise a requested session could start carrying frames.
func TestResolveSessionStateGating(t *testing.T) {
	deadline := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	for _, testCase := range []struct {
		name    string
		state   string
		status  int
		wantErr error
	}{
		{name: "requested is refused", state: "requested", status: http.StatusOK, wantErr: ErrNotStreaming},
		{name: "ended is refused", state: "ended", status: http.StatusOK, wantErr: ErrNotStreaming},
		{name: "declined is refused", state: "declined", status: http.StatusOK, wantErr: ErrNotStreaming},
		{name: "failed is refused", state: "failed", status: http.StatusOK, wantErr: ErrNotStreaming},
		{name: "empty state is refused", state: "", status: http.StatusOK, wantErr: ErrNotStreaming},
		{name: "unknown state is refused", state: "paused", status: http.StatusOK, wantErr: ErrNotStreaming},
		{name: "streaming is accepted", state: "streaming", status: http.StatusOK},
		{name: "unknown session is not found", status: http.StatusNotFound, wantErr: ErrSessionNotFound},
		{name: "forbidden session is not found", status: http.StatusForbidden, wantErr: ErrSessionNotFound},
		{name: "platform failure is unavailable", status: http.StatusInternalServerError, wantErr: ErrPlatformUnavailable},
		{name: "platform outage is unavailable", status: http.StatusServiceUnavailable, wantErr: ErrPlatformUnavailable},
		{name: "too many requests is unavailable", status: http.StatusTooManyRequests, wantErr: ErrPlatformUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resolver := newTestResolver(t, func(writer http.ResponseWriter, _ *http.Request) {
				if testCase.status != http.StatusOK {
					writer.WriteHeader(testCase.status)
					return
				}
				writePlatformSession(writer, http.StatusOK, platformSession{
					SessionID: "session_alpha",
					DeviceID:  "device_alpha",
					State:     testCase.state,
					ExpiresAt: deadline,
				})
			})

			session, err := resolver.ResolveSession(context.Background(), "session_alpha", "device_alpha")
			if testCase.wantErr != nil {
				if !errors.Is(err, testCase.wantErr) {
					t.Fatalf("ResolveSession() error = %v, want %v", err, testCase.wantErr)
				}
				if session.ID != "" {
					t.Fatalf("refused session returned a value: %+v", session)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveSession() error = %v, want success", err)
			}
			if session.ID != "session_alpha" || session.DeviceID != "device_alpha" {
				t.Fatalf("unexpected session: %+v", session)
			}
			if !session.State.AllowsFrames() {
				t.Fatalf("accepted session state %q does not allow frames", session.State)
			}
			if session.PlatformDeadline.IsZero() || session.ExpiresAt.IsZero() {
				t.Fatalf("accepted session lost its deadline: %+v", session)
			}
		})
	}
}

func TestResolveSessionRejectsExpiredSession(t *testing.T) {
	expired := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	resolver := newTestResolver(t, func(writer http.ResponseWriter, _ *http.Request) {
		writePlatformSession(writer, http.StatusOK, platformSession{
			SessionID: "session_alpha",
			DeviceID:  "device_alpha",
			State:     "streaming",
			ExpiresAt: expired,
		})
	})
	_, err := resolver.ResolveSession(context.Background(), "session_alpha", "device_alpha")
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("ResolveSession() error = %v, want ErrSessionExpired", err)
	}
}

func TestResolveSessionRejectsMismatchedIdentity(t *testing.T) {
	deadline := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	for _, testCase := range []struct {
		name    string
		session platformSession
	}{
		{
			name:    "different session id",
			session: platformSession{SessionID: "session_other", DeviceID: "device_alpha", State: "streaming", ExpiresAt: deadline},
		},
		{
			name:    "different device",
			session: platformSession{SessionID: "session_alpha", DeviceID: "device_other", State: "streaming", ExpiresAt: deadline},
		},
		{
			name:    "missing session id",
			session: platformSession{DeviceID: "device_alpha", State: "streaming", ExpiresAt: deadline},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			platformValue := testCase.session
			resolver := newTestResolver(t, func(writer http.ResponseWriter, _ *http.Request) {
				writePlatformSession(writer, http.StatusOK, platformValue)
			})
			_, err := resolver.ResolveSession(context.Background(), "session_alpha", "device_alpha")
			if !errors.Is(err, ErrSessionNotFound) {
				t.Fatalf("ResolveSession() error = %v, want ErrSessionNotFound", err)
			}
		})
	}
}

func TestResolveSessionRejectsInvalidDeadline(t *testing.T) {
	resolver := newTestResolver(t, func(writer http.ResponseWriter, _ *http.Request) {
		writePlatformSession(writer, http.StatusOK, platformSession{
			SessionID: "session_alpha",
			DeviceID:  "device_alpha",
			State:     "streaming",
			ExpiresAt: "not-a-timestamp",
		})
	})
	if _, err := resolver.ResolveSession(context.Background(), "session_alpha", "device_alpha"); !errors.Is(err, ErrPlatformUnavailable) {
		t.Fatalf("ResolveSession() error = %v, want ErrPlatformUnavailable", err)
	}
}

// The resolver must send the service token and read the same path
// device_platform serves for a device-scoped session read.
func TestResolveSessionUsesServiceTokenAndSessionPath(t *testing.T) {
	deadline := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	var seenPath string
	var seenAuthorization string
	resolver := newTestResolver(t, func(writer http.ResponseWriter, request *http.Request) {
		seenPath = request.URL.Path
		seenAuthorization = request.Header.Get("Authorization")
		writePlatformSession(writer, http.StatusOK, platformSession{
			SessionID: "session_alpha",
			DeviceID:  "device_alpha",
			State:     "streaming",
			ExpiresAt: deadline,
		})
	})
	if _, err := resolver.ResolveSession(context.Background(), "session_alpha", "device_alpha"); err != nil {
		t.Fatalf("ResolveSession() error = %v", err)
	}
	if seenPath != "/internal/v1/devices/device_alpha/care-view-sessions/session_alpha" {
		t.Fatalf("unexpected status path: %q", seenPath)
	}
	if seenAuthorization != "Bearer "+testResolverToken {
		t.Fatalf("unexpected authorization header: %q", seenAuthorization)
	}
}

func TestNewPlatformSessionResolverValidatesConfig(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		config PlatformResolverConfig
	}{
		{name: "missing base url", config: PlatformResolverConfig{ServiceToken: testResolverToken}},
		{name: "short token", config: PlatformResolverConfig{BaseURL: "https://platform.example", ServiceToken: "short"}},
		{name: "credentials in url", config: PlatformResolverConfig{BaseURL: "https://user:pass@platform.example", ServiceToken: testResolverToken}},
		{name: "query in url", config: PlatformResolverConfig{BaseURL: "https://platform.example?x=1", ServiceToken: testResolverToken}},
		{name: "fragment in url", config: PlatformResolverConfig{BaseURL: "https://platform.example#x", ServiceToken: testResolverToken}},
		{name: "unsupported scheme", config: PlatformResolverConfig{BaseURL: "ftp://platform.example", ServiceToken: testResolverToken}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewPlatformSessionResolver(testCase.config); err == nil {
				t.Fatal("expected configuration to be rejected")
			}
		})
	}
}

func TestResolveSessionNeverLeaksTokenInError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	resolver, err := NewPlatformSessionResolver(PlatformResolverConfig{
		BaseURL:      server.URL,
		ServiceToken: testResolverToken,
		Timeout:      time.Second,
	})
	if err != nil {
		t.Fatalf("create platform resolver: %v", err)
	}
	_, err = resolver.ResolveSession(context.Background(), "session_alpha", "device_alpha")
	if !errors.Is(err, ErrPlatformUnavailable) {
		t.Fatalf("ResolveSession() error = %v, want ErrPlatformUnavailable", err)
	}
	if strings.Contains(err.Error(), testResolverToken) {
		t.Fatalf("transport error leaked the service token: %v", err)
	}
}

func TestAllowsFramesOnlyForStreaming(t *testing.T) {
	for _, testCase := range []struct {
		state State
		want  bool
	}{
		{state: StateRequested, want: false},
		{state: StateStreaming, want: true},
		{state: StateEnded, want: false},
		{state: StateDeclined, want: false},
		{state: StateFailed, want: false},
		{state: State(""), want: false},
		{state: State("bogus"), want: false},
	} {
		if got := testCase.state.AllowsFrames(); got != testCase.want {
			t.Fatalf("AllowsFrames(%q) = %v, want %v", testCase.state, got, testCase.want)
		}
	}
}
