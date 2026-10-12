package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAIUsageReconciliationWindowDefaultsToLastDay(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families/parent-001/ai-usage",
		nil,
	)
	start, end, ok := aiUsageReconciliationWindow(request)
	if !ok {
		t.Fatal("expected the default window to be accepted")
	}
	if !end.After(start) {
		t.Fatalf("expected end after start, got %v..%v", start, end)
	}
	if window := end.Sub(start); window != 24*time.Hour {
		t.Fatalf("default window = %v, want 24h", window)
	}
}

func TestAIUsageReconciliationWindowRejectsInvalidRange(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families/parent-001/ai-usage"+
			"?start_time=2026-10-02T00:00:00Z&end_time=2026-10-01T00:00:00Z",
		nil,
	)
	if _, _, ok := aiUsageReconciliationWindow(request); ok {
		t.Fatal("expected an inverted range to be rejected")
	}
}

func TestAIUsageReconciliationWindowRejectsOversizedRange(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families/parent-001/ai-usage"+
			"?start_time=2026-01-01T00:00:00Z&end_time=2026-10-01T00:00:00Z",
		nil,
	)
	if _, _, ok := aiUsageReconciliationWindow(request); ok {
		t.Fatal("expected a window beyond the cap to be rejected")
	}
}

func TestAIUsageReconciliationWindowParsesExplicitRange(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families/parent-001/ai-usage"+
			"?start_time=2026-10-01T00:00:00Z&end_time=2026-10-03T00:00:00Z",
		nil,
	)
	start, end, ok := aiUsageReconciliationWindow(request)
	if !ok {
		t.Fatal("expected an explicit valid window to be accepted")
	}
	wantStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("window = %v..%v, want %v..%v", start, end, wantStart, wantEnd)
	}
}
