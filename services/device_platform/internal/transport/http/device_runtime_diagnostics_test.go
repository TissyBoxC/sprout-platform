package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	auditdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
)

func TestDiagnosticValidationRunsBeforeRuntimeWrite(t *testing.T) {
	handler := deviceRuntimeHandler{
		diagnosticService: stubRuntimeDiagnosticService{
			validateErr: auditdomain.ErrInvalidDiagnostics,
		},
	}
	request := newDiagnosticHeartbeatRequest(t)
	recorder := httptest.NewRecorder()

	// The runtime service is intentionally nil. A malformed diagnostic payload
	// must be rejected before transport attempts any runtime persistence.
	handler.recordHeartbeat(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d", http.StatusUnprocessableEntity, recorder.Code)
	}
}

func TestDiagnosticServiceUnavailableIsReportedBeforeRuntimeWrite(t *testing.T) {
	handler := deviceRuntimeHandler{}
	request := newDiagnosticHeartbeatRequest(t)
	recorder := httptest.NewRecorder()

	handler.recordHeartbeat(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

type stubRuntimeDiagnosticService struct {
	validateErr error
	recordErr   error
}

func (stub stubRuntimeDiagnosticService) Validate(
	_ string,
	_ *auditdomain.Diagnostics,
) (*auditdomain.Diagnostics, error) {
	return nil, stub.validateErr
}

func (stub stubRuntimeDiagnosticService) Record(
	_ context.Context,
	_ string,
	_ time.Time,
	_ *auditdomain.Diagnostics,
) error {
	return stub.recordErr
}

func newDiagnosticHeartbeatRequest(t *testing.T) *http.Request {
	t.Helper()
	body := `{
		"heartbeat_id":"heartbeat_demo_001",
		"reported_at":"2026-10-04T10:00:00Z",
		"firmware_version":"0.4.0",
		"connection":{"state":"online","transport":"wifi"},
		"network_quality":{"level":"good","rssi_dbm":-58,"latency_ms":42,"packet_loss_percent":1},
		"time_sync":{"state":"synchronized","source":"sntp","last_synced_at":"2026-10-04T10:00:00Z","offset_ms":12},
		"offline":{"state":"online","reason":"none","fallback_active":false,"pending_telemetry":0},
		"diagnostics":{
			"schema_version":"2.0.0",
			"newest_sequence":12
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
