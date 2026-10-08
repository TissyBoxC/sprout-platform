package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	auditdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
)

type stubBindingRevoker struct {
	deviceID      string
	disableDevice bool
	calls         int
	err           error
}

func (stub *stubBindingRevoker) RevokeSessions(
	_ context.Context,
	deviceID string,
	disableDevice bool,
) error {
	stub.calls++
	stub.deviceID = deviceID
	stub.disableDevice = disableDevice
	return stub.err
}

func TestRevokeSessionsRevokesWithoutDisablingByDefault(t *testing.T) {
	revoker := &stubBindingRevoker{}
	handler := deviceRuntimeHandler{bindingRevoker: revoker}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/devices/sprout_device_001/sessions/revoke",
		strings.NewReader(`{}`),
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.revokeSessions(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if revoker.calls != 1 || revoker.deviceID != "sprout_device_001" ||
		revoker.disableDevice {
		t.Fatalf("unexpected revoke invocation: %+v", revoker)
	}
	var envelope struct {
		Data struct {
			SessionsRevoked bool `json:"sessions_revoked"`
			DeviceDisabled  bool `json:"device_disabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.SessionsRevoked || envelope.Data.DeviceDisabled {
		t.Fatalf("unexpected revoke response: %s", recorder.Body.String())
	}
}

func TestRevokeSessionsCanDisableDevice(t *testing.T) {
	revoker := &stubBindingRevoker{}
	handler := deviceRuntimeHandler{bindingRevoker: revoker}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/devices/sprout_device_001/sessions/revoke",
		strings.NewReader(`{"disable_device":true}`),
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.revokeSessions(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if !revoker.disableDevice {
		t.Fatal("expected the device to be disabled")
	}
}

func TestRevokeSessionsRejectsUnknownDevice(t *testing.T) {
	handler := deviceRuntimeHandler{
		bindingRevoker: &stubBindingRevoker{err: bindingdomain.ErrDeviceNotFound},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/devices/sprout_device_001/sessions/revoke",
		strings.NewReader(`{}`),
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.revokeSessions(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestRevokeSessionsReportsUnavailableRevoker(t *testing.T) {
	handler := deviceRuntimeHandler{}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/devices/sprout_device_001/sessions/revoke",
		strings.NewReader(`{}`),
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.revokeSessions(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func TestRecordHeartbeatRejectsInvalidProvisioningBeforeRuntimeWrite(t *testing.T) {
	handler := deviceRuntimeHandler{
		diagnosticService: stubRuntimeDiagnosticService{
			validateErr: auditdomain.ErrInvalidDiagnostics,
		},
	}
	request := newProvisioningHeartbeatRequest(t)
	recorder := httptest.NewRecorder()

	// The runtime service is intentionally nil. A malformed provisioning
	// payload must be rejected before transport attempts persistence.
	handler.recordHeartbeat(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d", http.StatusUnprocessableEntity, recorder.Code)
	}
}

func TestRecordHeartbeatReportsUnavailableProvisioningService(t *testing.T) {
	handler := deviceRuntimeHandler{}
	request := newProvisioningHeartbeatRequest(t)
	recorder := httptest.NewRecorder()

	handler.recordHeartbeat(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func newProvisioningHeartbeatRequest(t *testing.T) *http.Request {
	t.Helper()
	body := `{
		"heartbeat_id":"heartbeat_demo_001",
		"reported_at":"2026-10-04T10:00:00Z",
		"firmware_version":"0.7.0",
		"connection":{"state":"online","transport":"wifi"},
		"network_quality":{"level":"good","rssi_dbm":-58,"latency_ms":42,"packet_loss_percent":1},
		"time_sync":{"state":"synchronized","source":"sntp","last_synced_at":"2026-10-04T10:00:00Z","offset_ms":12},
		"offline":{"state":"online","reason":"none","fallback_active":false,"pending_telemetry":0},
		"provisioning":{
			"state":"provisioned",
			"wifi_configured":true,
			"session_state":"ready",
			"events":[]
		}
	}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/devices/sprout_device_001/runtime/heartbeat",
		strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer device_session_token")
	request.SetPathValue("device_id", "sprout_device_001")
	return request
}
