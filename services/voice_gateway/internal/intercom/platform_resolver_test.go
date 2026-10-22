package intercom

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

const testResolverToken = "unit-test-service-token-0123456789abcdef"

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

func writePlatformCall(writer http.ResponseWriter, status int, call platformCall) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"schema_version": "1.0.0",
		"request_id":     "request-123456",
		"data":           call,
		"error":          nil,
	})
}

// The resolver is the gateway's accept gate. Every non-active platform state
// must be refused, otherwise a ringing call could start carrying audio.
func TestResolveSessionStateGating(t *testing.T) {
	deadline := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	for _, testCase := range []struct {
		name          string
		state         string
		status        int
		wantErr       error
		wantErrPrefix string
	}{
		{name: "ringing is refused", state: "ringing", status: http.StatusOK, wantErr: ErrSessionNotActive},
		{name: "missed is refused", state: "missed", status: http.StatusOK, wantErr: ErrSessionNotActive},
		{name: "declined is refused", state: "declined", status: http.StatusOK, wantErr: ErrSessionNotActive},
		{name: "cancelled is refused", state: "cancelled", status: http.StatusOK, wantErr: ErrSessionNotActive},
		{name: "ended is refused", state: "ended", status: http.StatusOK, wantErr: ErrSessionNotActive},
		{name: "empty state is refused", state: "", status: http.StatusOK, wantErr: ErrSessionNotActive},
		{name: "unknown state is refused", state: "talking", status: http.StatusOK, wantErr: ErrSessionNotActive},
		{name: "active is accepted", state: "active", status: http.StatusOK},
		{name: "unknown call is not found", status: http.StatusNotFound, wantErr: ErrSessionNotFound},
		{name: "gone call is not found", status: http.StatusGone, wantErr: ErrSessionNotFound},
		{name: "platform failure is unavailable", status: http.StatusInternalServerError, wantErr: ErrPlatformUnavailable},
		{name: "platform outage is unavailable", status: http.StatusServiceUnavailable, wantErr: ErrPlatformUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resolver := newTestResolver(t, func(writer http.ResponseWriter, request *http.Request) {
				if testCase.status != http.StatusOK {
					writer.WriteHeader(testCase.status)
					return
				}
				writePlatformCall(writer, http.StatusOK, platformCall{
					CallID:    "call_alpha",
					DeviceID:  "device_alpha",
					State:     testCase.state,
					ExpiresAt: deadline,
				})
			})

			session, err := resolver.ResolveSession(context.Background(), "call_alpha", "device_alpha")
			if testCase.wantErr != nil {
				if !errors.Is(err, testCase.wantErr) {
					t.Fatalf("ResolveSession() error = %v, want %v", err, testCase.wantErr)
				}
				if session.ID != "" {
					t.Fatalf("refused call returned a session: %+v", session)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveSession() error = %v, want success", err)
			}
			if session.ID != "call_alpha" || session.DeviceID != "device_alpha" {
				t.Fatalf("unexpected session: %+v", session)
			}
			if !session.State.AllowsAudio() {
				t.Fatalf("accepted session state %q does not allow audio", session.State)
			}
		})
	}
}

func TestResolveSessionRejectsExpiredCall(t *testing.T) {
	expired := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	resolver := newTestResolver(t, func(writer http.ResponseWriter, _ *http.Request) {
		writePlatformCall(writer, http.StatusOK, platformCall{
			CallID:    "call_alpha",
			DeviceID:  "device_alpha",
			State:     "active",
			ExpiresAt: expired,
		})
	})
	_, err := resolver.ResolveSession(context.Background(), "call_alpha", "device_alpha")
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("ResolveSession() error = %v, want ErrSessionExpired", err)
	}
}

func TestResolveSessionRejectsMismatchedIdentity(t *testing.T) {
	deadline := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	for _, testCase := range []struct {
		name string
		call platformCall
	}{
		{
			name: "different call id",
			call: platformCall{CallID: "call_other", DeviceID: "device_alpha", State: "active", ExpiresAt: deadline},
		},
		{
			name: "different device",
			call: platformCall{CallID: "call_alpha", DeviceID: "device_other", State: "active", ExpiresAt: deadline},
		},
		{
			name: "missing call id",
			call: platformCall{DeviceID: "device_alpha", State: "active", ExpiresAt: deadline},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			call := testCase.call
			resolver := newTestResolver(t, func(writer http.ResponseWriter, _ *http.Request) {
				writePlatformCall(writer, http.StatusOK, call)
			})
			if _, err := resolver.ResolveSession(context.Background(), "call_alpha", "device_alpha"); !errors.Is(err, ErrSessionNotFound) {
				t.Fatalf("ResolveSession() error = %v, want ErrSessionNotFound", err)
			}
		})
	}
}

// The resolver must send the service token and read the call-scoped path.
func TestResolveSessionUsesServiceTokenAndCallPath(t *testing.T) {
	deadline := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	var seenPath string
	var seenAuthorization string
	resolver := newTestResolver(t, func(writer http.ResponseWriter, request *http.Request) {
		seenPath = request.URL.Path
		seenAuthorization = request.Header.Get("Authorization")
		writePlatformCall(writer, http.StatusOK, platformCall{
			CallID:    "call_alpha",
			DeviceID:  "device_alpha",
			State:     "active",
			ExpiresAt: deadline,
		})
	})
	if _, err := resolver.ResolveSession(context.Background(), "call_alpha", "device_alpha"); err != nil {
		t.Fatalf("ResolveSession() error = %v", err)
	}
	if seenPath != "/internal/v1/devices/device_alpha/intercom-calls/call_alpha" {
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
	_, err = resolver.ResolveSession(context.Background(), "call_alpha", "device_alpha")
	if !errors.Is(err, ErrPlatformUnavailable) {
		t.Fatalf("ResolveSession() error = %v, want ErrPlatformUnavailable", err)
	}
	if strings.Contains(err.Error(), testResolverToken) {
		t.Fatalf("transport error leaked the service token: %v", err)
	}
}
