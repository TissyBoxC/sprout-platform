package sub2api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	platformobservability "github.com/TissyBoxC/sprout-platform/packages/go/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/vision"
)

const testServiceToken = "voice-device-platform-service-token-0123456789"

type visionExecutionFixture struct {
	server      *httptest.Server
	ticketCount atomic.Int32
	ticketQuery map[string]string
	lastBody    map[string]any
	handler     func(http.ResponseWriter, *http.Request)
}

func newVisionExecutionFixture(
	t *testing.T,
	handler func(http.ResponseWriter, *http.Request),
) *visionExecutionFixture {
	t.Helper()
	fixture := &visionExecutionFixture{handler: handler}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer "+testServiceToken {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == "/internal/v1/devices/device-1234567/ai-execution-ticket":
			fixture.ticketCount.Add(1)
			fixture.ticketQuery = map[string]string{
				"model":      request.URL.Query().Get("model"),
				"project":    request.URL.Query().Get("project"),
				"session_id": request.URL.Query().Get("session_id"),
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(
				`{"data":{"ticket":"opaque-vision-ticket","expires_in":120,"model":"vision-model"},"error":null}`,
			))
		case request.Method == http.MethodPost &&
			request.URL.Path == "/internal/v1/ai-execution":
			if request.URL.Query().Get("ticket") != "opaque-vision-ticket" {
				t.Errorf("ticket = %q", request.URL.Query().Get("ticket"))
			}
			if err := json.NewDecoder(request.Body).Decode(&fixture.lastBody); err != nil {
				t.Errorf("decode execution body: %v", err)
			}
			if request.Header.Get("X-Sprout-Project") != "" ||
				request.Header.Get("X-Sprout-Device-ID") != "" {
				t.Errorf("caller-supplied provenance headers must not be forwarded")
			}
			fixture.handler(writer, request)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.String())
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (fixture *visionExecutionFixture) client() *Client {
	return New(Config{
		BaseURL:      fixture.server.URL,
		ServiceToken: testServiceToken,
		Model:        "vision-model",
		HTTPClient:   fixture.server.Client(),
	})
}

func TestAnalyzeUsesOneTimeExecutionTicketAndReturnsBoundedResult(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("accept = %q", request.Header.Get("Accept"))
		}
		writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		writeVisionSSE(t, writer,
			streamChunk(t, "```json\n"),
			streamChunk(t, `{"labels":[{"label":"apple","confidence":9100}],`),
			streamChunk(t, `"summary":"这是一颗苹果"}`+"\n```"),
		)
	})

	result, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:        "task-123456",
		DeviceID:      "device-1234567",
		TaskType:      vision.TaskTypeObjectRecognition,
		PolicyVersion: "age_5_6",
		Prompt:        "这是什么",
		FrameWidth:    640,
		FrameHeight:   480,
		FrameBytes:    131072,
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
	if fixture.ticketCount.Load() != 1 {
		t.Fatalf("ticket count = %d, want 1", fixture.ticketCount.Load())
	}
	if fixture.ticketQuery["model"] != "vision-model" ||
		fixture.ticketQuery["project"] != "vision" ||
		fixture.ticketQuery["session_id"] != "task-123456" {
		t.Fatalf("unexpected ticket scope: %+v", fixture.ticketQuery)
	}
	if fixture.lastBody["model"] != "vision-model" {
		t.Fatalf("unexpected execution body: %+v", fixture.lastBody)
	}
	if fixture.lastBody["stream"] != true {
		t.Fatalf("stream = %#v, want true", fixture.lastBody["stream"])
	}
}

func TestAnalyzeForwardsPolicyVersionWithoutCallerProvenance(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if got := request.Header.Get(platformobservability.PolicyVersionHeader); got != "age_3_4" {
			t.Errorf("policy header = %q, want age_3_4", got)
		}
		for _, header := range []string{
			platformobservability.TenantIDHeader,
			platformobservability.DeviceIDHeader,
			platformobservability.SessionIDHeader,
			"X-Sprout-Project",
		} {
			if value := request.Header.Get(header); value != "" {
				t.Errorf("%s = %q, relay must not choose provenance", header, value)
			}
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writeVisionSSE(t, writer,
			`{"choices":[{"delta":{"content":"{\"labels\":[],\"summary\":\"安全回答\"}"}}]}`,
		)
	})

	_, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:        "task-123456",
		DeviceID:      "device-1234567",
		TaskType:      vision.TaskTypeObjectRecognition,
		PolicyVersion: "age_3_4",
		FrameWidth:    320,
		FrameHeight:   240,
		FrameBytes:    2048,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

func TestAnalyzeConsumesTopLevelDeltaAndContentSSE(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: message\n\n"))
		_, _ = writer.Write([]byte(
			"data: {\"delta\":{\"content\":\"{\\\"labels\\\":[],\\\"\"}}\n\n",
		))
		_, _ = writer.Write([]byte(
			"data: {\"content\":\"summary\\\":\\\"顶层 SSE 内容\\\"}\"}\n\n",
		))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})

	result, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if err != nil {
		t.Fatalf("analyze top-level SSE: %v", err)
	}
	if result.Summary != "顶层 SSE 内容" {
		t.Fatalf("summary = %q", result.Summary)
	}
}

func TestAnalyzeRejectsInvalidPolicyVersionBeforeExecution(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		_ http.ResponseWriter,
		_ *http.Request,
	) {
		t.Fatal("execution must not be called for an invalid policy version")
	})
	_, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:        "task-123456",
		DeviceID:      "device-1234567",
		TaskType:      vision.TaskTypeObjectRecognition,
		PolicyVersion: "age 3_4",
	})
	if !errors.Is(err, vision.ErrModelUnavailable) ||
		!strings.Contains(err.Error(), "policy version is invalid") {
		t.Fatalf("expected a stable invalid policy error, got %v", err)
	}
	if fixture.ticketCount.Load() != 0 {
		t.Fatalf("ticket count = %d, want 0", fixture.ticketCount.Load())
	}
}

func TestAnalyzeReportsSSEErrorWithoutLeakingBody(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(
			"data: {\"error\":{\"code\":\"provider-secret-code\",\"message\":\"sk-secret-value\"}}\n\n",
		))
	})
	_, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelUnavailable) {
		t.Fatalf("expected ErrModelUnavailable, got %v", err)
	}
	if strings.Contains(err.Error(), "provider-secret-code") ||
		strings.Contains(err.Error(), "sk-secret-value") {
		t.Fatalf("provider stream error leaked: %v", err)
	}
}

func TestAnalyzeReportsSSEErrorEventWithoutLeakingBody(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: error\n"))
		_, _ = writer.Write([]byte(
			"data: {\"message\":\"provider-secret-value\"}\n\n",
		))
	})
	_, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelUnavailable) {
		t.Fatalf("expected ErrModelUnavailable, got %v", err)
	}
	if strings.Contains(err.Error(), "provider-secret-value") {
		t.Fatalf("provider error event leaked: %v", err)
	}
}

func TestAnalyzeRejectsSSEEOFWithoutDone(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"labels\\\":[],\\\"summary\\\":\\\"partial\\\"}\"}}]}\n\n",
		))
	})
	_, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelUnavailable) ||
		!errors.Is(err, vision.ErrModelStreamInterrupted) {
		t.Fatalf("expected a stable interrupted-stream error, got %v", err)
	}
}

func TestAnalyzePreservesContextCancellation(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := fixture.client().Analyze(ctx, vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if errors.Is(err, vision.ErrModelStreamInterrupted) {
		t.Fatalf("cancellation was misclassified as stream interruption: %v", err)
	}
}

func TestAnalyzeRejectsNonStreamingSuccessResponse(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(
			`{"choices":[{"message":{"content":"{\"labels\":[],\"summary\":\"unsafe\"}"}}]}`,
		))
	})
	_, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelUnavailable) ||
		!strings.Contains(err.Error(), "not an event stream") {
		t.Fatalf("expected a stable non-stream response error, got %v", err)
	}
}

func TestAnalyzeRejectsNonStopFinishReason(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"labels\\\":[],\\\"summary\\\":\\\"partial\\\"}\"},\"finish_reason\":\"length\"}]}\n\n",
		))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
	_, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelStreamInterrupted) {
		t.Fatalf("expected a stable non-stop error, got %v", err)
	}
}

func TestDecodeContentRejectsNonStrictJSON(t *testing.T) {
	for _, content := range []string{
		`{"labels":[],"summary":"ok"} trailing`,
		`{"labels":[],"summary":"ok"}{"labels":[]}`,
		`{"labels":[],"summary":"ok","unexpected":true}`,
	} {
		if _, err := decodeContent(content); err == nil {
			t.Fatalf("content was accepted as strict JSON: %q", content)
		}
	}
}

func TestAnalyzeRejectsMissingServiceToken(t *testing.T) {
	client := New(Config{
		BaseURL: "http://127.0.0.1:1",
		Model:   "vision-model",
	})
	_, err := client.Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelUnavailable) ||
		!strings.Contains(err.Error(), "service token") {
		t.Fatalf("expected ErrModelUnavailable with a token error, got %v", err)
	}
}

func TestAnalyzeRejectsMissingDeviceOrTaskIdentity(t *testing.T) {
	for _, request := range []vision.ModelRequest{
		{
			TaskID:   "task-123456",
			TaskType: vision.TaskTypeObjectRecognition,
		},
		{
			DeviceID: "device-1234567",
			TaskType: vision.TaskTypeObjectRecognition,
		},
	} {
		client := New(Config{
			BaseURL:      "http://127.0.0.1:1",
			ServiceToken: testServiceToken,
			Model:        "vision-model",
		})
		_, err := client.Analyze(context.Background(), request)
		if !errors.Is(err, vision.ErrModelUnavailable) {
			t.Fatalf("expected ErrModelUnavailable, got %v", err)
		}
	}
}

func TestAnalyzeReportsProviderFailureWithoutBody(t *testing.T) {
	fixture := newVisionExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"error":"sk-secret-leak-token-value"}`))
	})

	_, err := fixture.client().Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelUnavailable) {
		t.Fatalf("expected ErrModelUnavailable, got %v", err)
	}
	if strings.Contains(err.Error(), "sk-secret-leak-token-value") ||
		strings.Contains(err.Error(), testServiceToken) {
		t.Fatalf("provider error body leaked into error: %v", err)
	}
}

func writeVisionSSE(t *testing.T, writer http.ResponseWriter, payloads ...string) {
	t.Helper()
	for _, payload := range payloads {
		if _, err := writer.Write([]byte("data: " + payload + "\n\n")); err != nil {
			t.Fatalf("write SSE fixture: %v", err)
		}
	}
	if _, err := writer.Write([]byte("data: [DONE]\n\n")); err != nil {
		t.Fatalf("write SSE terminator: %v", err)
	}
}

func streamChunk(t *testing.T, content string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{"content": content},
		}},
	})
	if err != nil {
		t.Fatalf("encode SSE chunk: %v", err)
	}
	return string(payload)
}

func TestAnalyzeRejectsRejectedTicket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer "+testServiceToken {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":{"code":"account_unavailable"}}`))
	}))
	defer server.Close()

	client := New(Config{
		BaseURL:      server.URL,
		ServiceToken: testServiceToken,
		Model:        "vision-model",
		HTTPClient:   server.Client(),
	})
	_, err := client.Analyze(context.Background(), vision.ModelRequest{
		TaskID:   "task-123456",
		DeviceID: "device-1234567",
		TaskType: vision.TaskTypeObjectRecognition,
	})
	if !errors.Is(err, vision.ErrModelUnavailable) ||
		!strings.Contains(err.Error(), "status=403") {
		t.Fatalf("expected a stable status error, got %v", err)
	}
	if strings.Contains(err.Error(), "account_unavailable") {
		t.Fatalf("ticket error body leaked into error: %v", err)
	}
}
