package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TissyBoxC/sprout-platform/packages/go/observability"
)

func TestWriteSuccessIncludesCorrelation(t *testing.T) {
	handler := observability.WithRequestMetadata(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		WriteSuccess(response, request, http.StatusOK, map[string]string{
			"status": "ready",
		})
	}))

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	var envelope Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.SchemaVersion != SchemaVersion {
		t.Fatalf("unexpected schema version: %q", envelope.SchemaVersion)
	}
	if envelope.RequestID == "" {
		t.Fatal("expected correlation request ID")
	}
	if envelope.Error != nil {
		t.Fatalf("unexpected error: %+v", envelope.Error)
	}
}

func TestWriteErrorUsesStableCode(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/runtime", nil)
	recorder := httptest.NewRecorder()

	WriteError(
		recorder,
		request,
		http.StatusUnauthorized,
		"unauthenticated",
		"服务认证失败",
		false,
	)

	var envelope Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != "unauthenticated" {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
	if envelope.Error.Retryable {
		t.Fatal("authentication failure must not be retryable")
	}
}

func TestWriteSuccessWithNoticePreservesDataAndAddsNotice(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/example", nil)
	recorder := httptest.NewRecorder()

	WriteSuccessWithNotice(
		recorder,
		request,
		http.StatusAccepted,
		map[string]any{"saved": true},
		NoticeBody{
			Code:    "audit_write_failed",
			Message: "操作已保存，但安全记录没有完成。",
		},
	)

	var envelope Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want accepted", recorder.Code)
	}
	if envelope.Error != nil {
		t.Fatalf("success notice must not be modeled as an error: %+v", envelope.Error)
	}
	if envelope.Notice == nil || envelope.Notice.Code != "audit_write_failed" {
		t.Fatalf("unexpected notice: %+v", envelope.Notice)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok || data["saved"] != true {
		t.Fatalf("data shape changed: %#v", envelope.Data)
	}
}
