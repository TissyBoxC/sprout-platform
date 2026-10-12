package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	telemetrydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/telemetry/domain"
)

type stubTelemetryService struct {
	windowDays int
	summary    telemetrydomain.Summary
	err        error
}

func (stub *stubTelemetryService) Summary(
	_ context.Context,
	windowDays int,
) (telemetrydomain.Summary, error) {
	stub.windowDays = windowDays
	if stub.err != nil {
		return telemetrydomain.Summary{}, stub.err
	}
	return stub.summary, nil
}

func TestTelemetrySummaryReturnsProjection(t *testing.T) {
	service := &stubTelemetryService{
		summary: telemetrydomain.Summary{
			WindowDays:  7,
			DeviceCount: 3,
			OnlineCount: 2,
		},
	}
	handler := telemetryHandler{service: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/telemetry/summary?window_days=14",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.summary(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if service.windowDays != 14 {
		t.Fatalf("window days = %d, want 14", service.windowDays)
	}
	var body struct {
		Data telemetrydomain.Summary `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.DeviceCount != 3 || body.Data.OnlineCount != 2 {
		t.Fatalf("unexpected summary: %+v", body.Data)
	}
}

func TestTelemetrySummaryDefaultsWindow(t *testing.T) {
	service := &stubTelemetryService{}
	handler := telemetryHandler{service: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/telemetry/summary",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.summary(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if service.windowDays != 0 {
		t.Fatalf("window days = %d, want 0 for service default", service.windowDays)
	}
}

func TestTelemetrySummaryRejectsInvalidWindow(t *testing.T) {
	service := &stubTelemetryService{}
	handler := telemetryHandler{service: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/telemetry/summary?window_days=999",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.summary(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
	if service.windowDays != 0 {
		t.Fatalf("service was called with %d, want no call", service.windowDays)
	}
}

func TestTelemetrySummaryMapsInvalidWindowError(t *testing.T) {
	service := &stubTelemetryService{err: telemetrydomain.ErrInvalidWindow}
	handler := telemetryHandler{service: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/telemetry/summary?window_days=30",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.summary(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
}

func TestTelemetrySummaryMapsUnexpectedError(t *testing.T) {
	service := &stubTelemetryService{err: errors.New("database unavailable")}
	handler := telemetryHandler{service: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/telemetry/summary",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.summary(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}
