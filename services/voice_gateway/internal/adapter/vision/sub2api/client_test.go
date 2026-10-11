package sub2api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/vision"
)

func TestAnalyzeReturnsBoundedResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected authorization header")
		}
		if request.Header.Get("X-Sprout-Project") != "vision" {
			t.Errorf("unexpected project header: %q", request.Header.Get("X-Sprout-Project"))
		}
		response.Header().Set("Content-Type", "application/json")
		content := "```json\n" +
			`{"labels":[{"label":"apple","confidence":9100}],"summary":"这是一颗苹果"}` +
			"\n```"
		payload, marshalErr := json.Marshal(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"content": content},
			}},
		})
		if marshalErr != nil {
			t.Fatalf("encode fixture: %v", marshalErr)
		}
		_, _ = response.Write(payload)
	}))
	defer server.Close()

	client := New(Config{
		BaseURL: server.URL,
		APIKey:  "secret",
		Model:   "vision-model",
	})
	result, err := client.Analyze(context.Background(), vision.ModelRequest{
		TaskID:      "task-123456",
		DeviceID:    "device-1234567",
		TaskType:    vision.TaskTypeObjectRecognition,
		Prompt:      "这是什么",
		FrameWidth:  640,
		FrameHeight: 480,
		FrameBytes:  131072,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if result.Model != "vision-model" {
		t.Fatalf("unexpected model: %q", result.Model)
	}
	if len(result.Labels) != 1 || result.Labels[0].Label != "apple" ||
		result.Labels[0].Confidence != 9100 {
		t.Fatalf("unexpected labels: %+v", result.Labels)
	}
	if result.Summary != "这是一颗苹果" {
		t.Fatalf("unexpected summary: %q", result.Summary)
	}
}

func TestAnalyzeRejectsMissingCredentials(t *testing.T) {
	client := New(Config{BaseURL: "http://127.0.0.1:1", Model: "vision-model"})
	_, err := client.Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelUnavailable) {
		t.Fatalf("expected ErrModelUnavailable, got %v", err)
	}
}

func TestAnalyzeReportsProviderFailureWithoutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.WriteHeader(http.StatusBadGateway)
		_, _ = response.Write([]byte(`{"error":"sk-secret-leak-token-value"}`))
	}))
	defer server.Close()

	client := New(Config{
		BaseURL: server.URL,
		APIKey:  "secret",
		Model:   "vision-model",
	})
	_, err := client.Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelUnavailable) {
		t.Fatalf("expected ErrModelUnavailable, got %v", err)
	}
	if strings.Contains(err.Error(), "sk-secret-leak-token-value") {
		t.Fatalf("provider error body leaked into error: %v", err)
	}
}
