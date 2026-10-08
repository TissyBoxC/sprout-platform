package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
)

type stubAuditService struct {
	snapshot         *domain.Snapshot
	provisioning     *domain.ProvisioningSnapshot
	err              error
	lastLimit        int
	lastDevice       string
	lastProvisioning string
}

func (stub *stubAuditService) Get(
	_ context.Context,
	deviceID string,
	limit int,
) (*domain.Snapshot, error) {
	stub.lastDevice = deviceID
	stub.lastLimit = limit
	return stub.snapshot, stub.err
}

func (stub *stubAuditService) GetProvisioning(
	_ context.Context,
	deviceID string,
	limit int,
) (*domain.ProvisioningSnapshot, error) {
	stub.lastProvisioning = deviceID
	stub.lastLimit = limit
	return stub.provisioning, stub.err
}

func TestGetDiagnosticsReturnsEmptyStateForDeviceWithoutHistory(t *testing.T) {
	handler := New(&stubAuditService{err: domain.ErrEventNotFound})
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
			DeviceID                  string `json:"device_id"`
			HealthState               string `json:"health_state"`
			ErrorCount                int    `json:"error_count"`
			InteractionEvents         []any  `json:"interaction_events"`
			RetentionInteractionEvent int    `json:"retention_interaction_events"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.DeviceID != "sprout_device_001" ||
		envelope.Data.HealthState != domain.HealthUnknown ||
		envelope.Data.ErrorCount != 0 ||
		envelope.Data.InteractionEvents == nil ||
		envelope.Data.RetentionInteractionEvent != domain.RetentionInteraction {
		t.Fatalf("unexpected empty diagnostic response: %s", recorder.Body.String())
	}
}

func TestGetDiagnosticsCapsRequestedLimit(t *testing.T) {
	service := &stubAuditService{
		snapshot: &domain.Snapshot{
			DeviceID:    "sprout_device_001",
			BootEvents:  []domain.BootEvent{},
			Failures:    []domain.ModuleFailure{},
			HealthState: domain.HealthHealthy,
			UpdatedAt:   time.Now().UTC(),
		},
	}
	handler := New(service)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/devices/sprout_device_001/diagnostics?limit=999",
		nil,
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.GetDiagnostics(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if service.lastLimit != 100 {
		t.Fatalf("expected limit to be capped at 100, got %d", service.lastLimit)
	}
}

func TestGetDiagnosticsDoesNotExposeRepositoryErrors(t *testing.T) {
	handler := New(&stubAuditService{err: errors.New("database offline")})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/devices/sprout_device_001/diagnostics",
		nil,
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.GetDiagnostics(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "暂时无法读取设备诊断") {
		t.Fatalf("expected a safe user-facing error, got %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "database offline") {
		t.Fatalf("expected repository details to stay hidden: %s", recorder.Body.String())
	}
}

func TestGetProvisioningReturnsEmptyStateForDeviceWithoutHistory(t *testing.T) {
	handler := New(&stubAuditService{err: domain.ErrEventNotFound})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/devices/sprout_device_001/provisioning",
		nil,
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.GetProvisioning(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	var envelope struct {
		Data struct {
			DeviceID                   string `json:"device_id"`
			State                      string `json:"state"`
			SessionState               string `json:"session_state"`
			Events                     []any  `json:"events"`
			RetentionProvisioningEvent int    `json:"retention_provisioning_events"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.DeviceID != "sprout_device_001" ||
		envelope.Data.State != domain.ProvisioningStateUnprovisioned ||
		envelope.Data.SessionState != domain.SessionStateReady ||
		envelope.Data.Events == nil ||
		envelope.Data.RetentionProvisioningEvent != domain.RetentionProvisioning {
		t.Fatalf("unexpected empty provisioning response: %s", recorder.Body.String())
	}
}

func TestGetProvisioningReturnsEventHistory(t *testing.T) {
	reportedAt := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	handler := New(&stubAuditService{
		provisioning: &domain.ProvisioningSnapshot{
			DeviceID:        "sprout_device_001",
			State:           domain.ProvisioningStateProvisioned,
			WiFiConfigured:  true,
			SessionState:    domain.SessionStateReady,
			NewestSequence:  5,
			RetentionEvents: domain.RetentionProvisioning,
			UpdatedAt:       reportedAt,
			Events: []domain.ProvisioningEvent{
				{
					EventID:         "provisioning_00000005",
					EventType:       domain.ProvisioningEventBindingConfirmed,
					Sequence:        5,
					DetailCode:      "guardian_binding",
					FirmwareVersion: "0.7.0",
					ReportedAt:      reportedAt,
				},
			},
		},
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/devices/sprout_device_001/provisioning",
		nil,
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.GetProvisioning(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "binding_confirmed") {
		t.Fatalf("expected provisioning event in response: %s", recorder.Body.String())
	}
}

func TestGetProvisioningDoesNotExposeRepositoryErrors(t *testing.T) {
	handler := New(&stubAuditService{err: errors.New("database offline")})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/devices/sprout_device_001/provisioning",
		nil,
	)
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.GetProvisioning(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "database offline") {
		t.Fatalf("expected repository details to stay hidden: %s", recorder.Body.String())
	}
}
