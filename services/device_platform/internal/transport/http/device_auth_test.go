package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
)

type stubDeviceSessionVerifier struct {
	deviceID string
	err      error
}

func (verifier stubDeviceSessionVerifier) VerifyDeviceSession(
	context.Context,
	string,
) (string, error) {
	return verifier.deviceID, verifier.err
}

// deviceRouteRequest builds a request whose path value matches the {device_id}
// segment the router would have populated.
func deviceRouteRequest(t *testing.T, pathDeviceID string, token string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(
		"GET",
		"/api/v1/devices/"+pathDeviceID+"/display/policy",
		nil,
	)
	request.SetPathValue("device_id", pathDeviceID)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}

func TestRequireDeviceSessionRejectsMissingToken(t *testing.T) {
	reached := false
	handler := requireDeviceSession(
		stubDeviceSessionVerifier{deviceID: "device_a"},
		func(http.ResponseWriter, *http.Request) { reached = true },
	)

	recorder := httptest.NewRecorder()
	handler(recorder, deviceRouteRequest(t, "device_a", ""))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if reached {
		t.Fatal("handler ran without a device session")
	}
}

func TestRequireDeviceSessionRejectsUnknownSession(t *testing.T) {
	reached := false
	handler := requireDeviceSession(
		stubDeviceSessionVerifier{err: bindingdomain.ErrDeviceSessionExpired},
		func(http.ResponseWriter, *http.Request) { reached = true },
	)

	recorder := httptest.NewRecorder()
	handler(recorder, deviceRouteRequest(t, "device_a", "token"))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if reached {
		t.Fatal("handler ran with an expired session")
	}
}

func TestRequireDeviceSessionRejectsCrossDeviceAccess(t *testing.T) {
	reached := false
	// The session belongs to device_a but the path names device_b, which is the
	// privilege escalation this wrapper exists to stop.
	handler := requireDeviceSession(
		stubDeviceSessionVerifier{deviceID: "device_a"},
		func(http.ResponseWriter, *http.Request) { reached = true },
	)

	recorder := httptest.NewRecorder()
	handler(recorder, deviceRouteRequest(t, "device_b", "token"))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if reached {
		t.Fatal("handler ran for another device")
	}
}

func TestRequireDeviceSessionAllowsMatchingDevice(t *testing.T) {
	var seenDeviceID string
	handler := requireDeviceSession(
		stubDeviceSessionVerifier{deviceID: "device_a"},
		func(_ http.ResponseWriter, request *http.Request) {
			seenDeviceID = deviceSessionFromContext(request.Context())
		},
	)

	recorder := httptest.NewRecorder()
	handler(recorder, deviceRouteRequest(t, "device_a", "token"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if seenDeviceID != "device_a" {
		t.Fatalf("session device = %q, want device_a", seenDeviceID)
	}
}

func TestRequireDeviceSessionFailsClosedWithoutVerifier(t *testing.T) {
	reached := false
	handler := requireDeviceSession(
		nil,
		func(http.ResponseWriter, *http.Request) { reached = true },
	)

	recorder := httptest.NewRecorder()
	handler(recorder, deviceRouteRequest(t, "device_a", "token"))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if reached {
		t.Fatal("handler ran without a verifier")
	}
}

func TestRequireDeviceSessionMapsVerificationError(t *testing.T) {
	handler := requireDeviceSession(
		stubDeviceSessionVerifier{err: errors.New("storage unavailable")},
		func(http.ResponseWriter, *http.Request) {
			t.Fatal("handler ran after a verification error")
		},
	)

	recorder := httptest.NewRecorder()
	handler(recorder, deviceRouteRequest(t, "device_a", "token"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}
