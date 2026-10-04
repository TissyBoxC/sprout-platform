package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
)

// Diagnostics are one administrator-facing read model. Interaction events
// must survive the handler projection without losing their detail fields.
func TestGetDiagnosticsReturnsInteractionHistory(t *testing.T) {
	reportedAt := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	handler := New(&stubAuditService{
		snapshot: &domain.Snapshot{
			DeviceID:             "sprout_device_001",
			HealthState:          domain.HealthHealthy,
			UpdatedAt:            reportedAt,
			RetentionBoot:        domain.RetentionBootEvents,
			RetentionFailures:    domain.RetentionFailures,
			RetentionRecovery:    domain.RetentionRecovery,
			RetentionInteraction: domain.RetentionInteraction,
			InteractionEvents: []domain.InteractionEvent{
				{
					EventID:         "interaction_00000013",
					EventType:       domain.InteractionEventFactoryResetCompleted,
					Sequence:        13,
					DetailCode:      "guardian_request",
					DurationMS:      250,
					FirmwareVersion: "0.4.0",
					ReportedAt:      reportedAt,
				},
			},
		},
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/devices/sprout_device_001/diagnostics",
		nil,
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.GetDiagnostics(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	var envelope struct {
		Data struct {
			InteractionEvents []struct {
				EventType  string `json:"event_type"`
				DetailCode string `json:"detail_code"`
			} `json:"interaction_events"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.InteractionEvents) != 1 ||
		envelope.Data.InteractionEvents[0].EventType != domain.InteractionEventFactoryResetCompleted ||
		envelope.Data.InteractionEvents[0].DetailCode != "guardian_request" {
		t.Fatalf("unexpected interaction response: %s", recorder.Body.String())
	}
}
