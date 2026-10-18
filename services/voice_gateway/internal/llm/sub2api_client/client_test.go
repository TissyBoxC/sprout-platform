package sub2api_client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	platformobservability "github.com/TissyBoxC/sprout-platform/packages/go/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
)

const testServiceToken = "voice-device-platform-service-token-0123456789"

type executionFixture struct {
	server        *httptest.Server
	requests      atomic.Int32
	tickets       atomic.Int32
	lastBody      []byte
	lastProject   string
	lastSessionID string
	lastModel     string
	lastTicket    string
	lastHeaders   http.Header
}

func newExecutionFixture(
	t *testing.T,
	handler func(http.ResponseWriter, *http.Request, *executionFixture),
) *executionFixture {
	t.Helper()
	fixture := &executionFixture{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer "+testServiceToken {
			t.Errorf("service authorization = %q", request.Header.Get("Authorization"))
		}
		switch {
		case request.Method == http.MethodGet &&
			strings.Contains(request.URL.Path, "/ai-execution-ticket"):
			fixture.tickets.Add(1)
			fixture.lastProject = request.URL.Query().Get("project")
			fixture.lastSessionID = request.URL.Query().Get("session_id")
			fixture.lastModel = request.URL.Query().Get("model")
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(
				writer,
				`{"data":{"ticket":"ticket-%d","expires_in":120,"model":%q},"error":null}`,
				fixture.tickets.Load(),
				request.URL.Query().Get("model"),
			)
		case request.Method == http.MethodPost &&
			request.URL.Path == "/internal/v1/ai-execution":
			fixture.requests.Add(1)
			fixture.lastTicket = request.URL.Query().Get("ticket")
			var decodeErr error
			fixture.lastBody, decodeErr = io.ReadAll(request.Body)
			if decodeErr != nil {
				t.Errorf("read execution body: %v", decodeErr)
			}
			fixture.lastHeaders = request.Header.Clone()
			handler(writer, request, fixture)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.String())
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (fixture *executionFixture) client() *Client {
	client := New(fixture.server.URL, testServiceToken)
	client.HTTPClient = fixture.server.Client()
	client.RetryBackoff = time.Millisecond
	return client
}

func TestChatStreamsOpenAIContent(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		fixture *executionFixture,
	) {
		var body struct {
			Model    string        `json:"model"`
			Messages []llm.Message `json:"messages"`
			Stream   bool          `json:"stream"`
		}
		if err := json.Unmarshal(fixture.lastBody, &body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !body.Stream || body.Model != "sprout-chat" || len(body.Messages) != 1 {
			t.Fatalf("unexpected request body: %+v", body)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n"))
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "你好"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	defer stream.Close()

	var output strings.Builder
	for chunk := range stream.Chunks() {
		output.WriteString(chunk)
	}
	if output.String() != "你好" {
		t.Fatalf("unexpected output %q", output.String())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if fixture.tickets.Load() != 1 ||
		fixture.requests.Load() != 1 ||
		fixture.lastTicket != "ticket-1" ||
		fixture.lastProject != "voice" ||
		fixture.lastSessionID != "session-alpha" ||
		fixture.lastModel != "sprout-chat" {
		t.Fatalf(
			"unexpected execution exchange: tickets=%d requests=%d ticket=%q project=%q session=%q model=%q",
			fixture.tickets.Load(),
			fixture.requests.Load(),
			fixture.lastTicket,
			fixture.lastProject,
			fixture.lastSessionID,
			fixture.lastModel,
		)
	}
}

func TestChatTicketCarriesDeviceSessionModelAndProject(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: [DONE]\n"))
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:         "sprout-chat",
		Messages:      []llm.Message{{Role: "user", Content: "你好"}},
		DeviceID:      "72453d1e-dce3-401a-96db-e96db54c9fd5",
		PolicyVersion: "age_5_6",
		SessionID:     "session_1",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if fixture.lastProject != "voice" ||
		fixture.lastSessionID != "session_1" ||
		fixture.lastModel != "sprout-chat" {
		t.Fatalf(
			"unexpected ticket scope: project=%q session=%q model=%q",
			fixture.lastProject,
			fixture.lastSessionID,
			fixture.lastModel,
		)
	}
}

func TestChatForwardsOnlyValidatedPolicyVersionAsTrustedHint(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		request *http.Request,
		_ *executionFixture,
	) {
		if request.Header.Get(platformobservability.PolicyVersionHeader) != "age_5_6" {
			t.Errorf(
				"policy version header = %q",
				request.Header.Get(platformobservability.PolicyVersionHeader),
			)
		}
		if request.Header.Get("X-Sprout-Project") != "" {
			t.Errorf("relay must not choose the project header")
		}
		if request.Header.Get("X-Sprout-Device-ID") != "" {
			t.Errorf("relay must not choose the device header")
		}
		if request.Header.Get("X-Sprout-Session-ID") != "" {
			t.Errorf("relay must not choose the session header")
		}
		if request.Header.Get("X-Sprout-Tenant-ID") != "" {
			t.Errorf("relay must not choose the tenant header")
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: [DONE]\n"))
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:         "sprout-chat",
		Messages:      []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:      "device-alpha",
		PolicyVersion: "age_5_6",
		SessionID:     "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := fixture.lastHeaders.Get(platformobservability.PolicyVersionHeader); got != "age_5_6" {
		t.Fatalf("recorded policy header = %q, want age_5_6", got)
	}
	for _, header := range []string{
		"X-Sprout-Tenant-ID",
		"X-Sprout-Device-ID",
		"X-Sprout-Session-ID",
		"X-Sprout-Project",
	} {
		if value := fixture.lastHeaders.Get(header); value != "" {
			t.Fatalf("%s = %q, relay must not send caller-controlled provenance", header, value)
		}
	}
}

func TestChatRejectsInvalidPolicyVersionBeforeExecution(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		_ http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		t.Fatal("execution must not be called for an invalid policy version")
	})
	_, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:         "sprout-chat",
		Messages:      []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:      "device-alpha",
		PolicyVersion: "age 5_6",
		SessionID:     "session-alpha",
	})
	if err == nil || !strings.Contains(err.Error(), "policy version is invalid") {
		t.Fatalf("expected invalid policy error, got %v", err)
	}
	if fixture.tickets.Load() != 0 || fixture.requests.Load() != 0 {
		t.Fatalf(
			"tickets = %d requests = %d, want no execution",
			fixture.tickets.Load(),
			fixture.requests.Load(),
		)
	}
}

func TestChatRequiresRealDeviceAndSessionIdentity(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		request llm.Request
	}{
		{
			name: "missing device",
			request: llm.Request{
				Model:     "sprout-chat",
				Messages:  []llm.Message{{Role: "user", Content: "你好"}},
				SessionID: "session_1",
			},
		},
		{
			name: "missing session",
			request: llm.Request{
				Model:    "sprout-chat",
				Messages: []llm.Message{{Role: "user", Content: "你好"}},
				DeviceID: "device-alpha",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := New("http://127.0.0.1:1", testServiceToken)
			if _, err := client.Chat(context.Background(), testCase.request); err == nil {
				t.Fatal("expected missing execution identity to be rejected")
			}
		})
	}
}

func TestChatReturnsProviderStatusSynchronously(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		http.Error(writer, "upstream unavailable", http.StatusBadGateway)
	})

	_, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err == nil || !strings.Contains(err.Error(), "status=502") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestChatRetriesTransientProviderStatus(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		fixture *executionFixture,
	) {
		if fixture.requests.Load() < 3 {
			http.Error(writer, "temporarily unavailable", http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n"))
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat after retries: %v", err)
	}
	defer stream.Close()
	var output strings.Builder
	for chunk := range stream.Chunks() {
		output.WriteString(chunk)
	}
	if output.String() != "好" || stream.Err() != nil {
		t.Fatalf("output = %q, error = %v", output.String(), stream.Err())
	}
	if fixture.requests.Load() != 3 || fixture.tickets.Load() != 3 {
		t.Fatalf(
			"requests = %d tickets = %d, want 3 each",
			fixture.requests.Load(),
			fixture.tickets.Load(),
		)
	}
}

func TestChatDoesNotRetryPermanentProviderStatus(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		http.Error(writer, "invalid request", http.StatusBadRequest)
	})

	client := fixture.client()
	client.RetryBackoff = time.Millisecond
	_, err := client.Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err == nil || !strings.Contains(err.Error(), "status=400") {
		t.Fatalf("expected status error, got %v", err)
	}
	if fixture.requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", fixture.requests.Load())
	}
}

func TestChatStopsRetryWhenContextCancels(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		http.Error(writer, "temporarily unavailable", http.StatusTooManyRequests)
	})

	ctx, cancel := context.WithCancel(context.Background())
	client := fixture.client()
	client.RetryBackoff = time.Hour
	client.Sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}
	_, err := client.Chat(ctx, llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled retry, got %v", err)
	}
	if fixture.requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", fixture.requests.Load())
	}
}

func TestChatRetriesTransientNetworkError(t *testing.T) {
	attempts := 0
	tickets := 0
	client := New("http://device-platform.invalid", testServiceToken)
	client.MaxAttempts = 2
	client.RetryBackoff = time.Millisecond
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if strings.Contains(request.URL.Path, "ai-execution-ticket") {
				tickets++
				return jsonResponse(
					request,
					http.StatusOK,
					fmt.Sprintf(`{"data":{"ticket":"ticket-%d","expires_in":120},"error":null}`, tickets),
				), nil
			}
			attempts++
			return nil, &net.DNSError{Err: "temporary failure", Name: "device-platform.invalid", IsTemporary: true}
		}),
	}
	_, err := client.Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("expected request error, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if tickets != 2 {
		t.Fatalf("tickets = %d, want 2", tickets)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestChatReportsMalformedSSE(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: not-json\n"))
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	for range stream.Chunks() {
	}
	if err := stream.Err(); err == nil {
		t.Fatal("expected malformed SSE error")
	}
}

func TestChatReportsInterruptedStreamWithoutDone(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"半\"}}]}\n\n"))
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	defer stream.Close()
	var output strings.Builder
	for chunk := range stream.Chunks() {
		output.WriteString(chunk)
	}
	if output.String() != "半" {
		t.Fatalf("partial output = %q", output.String())
	}
	if err := stream.Err(); !errors.Is(err, llm.ErrStreamInterrupted) {
		t.Fatalf("stream error = %v, want ErrStreamInterrupted", err)
	}
}

func TestChatReportsLengthFinishReasonAsInterrupted(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"半\"},\"finish_reason\":\"length\"}]}\n\n",
		))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	defer stream.Close()
	for range stream.Chunks() {
	}
	if err := stream.Err(); !errors.Is(err, llm.ErrStreamInterrupted) {
		t.Fatalf("stream error = %v, want ErrStreamInterrupted", err)
	}
}

func TestChatReportsUpstreamErrorEvent(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(
			"data: {\"error\":{\"code\":\"provider-secret-code\"}}\n\n",
		))
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	for range stream.Chunks() {
	}
	streamErr := stream.Err()
	if streamErr == nil {
		t.Fatal("expected an upstream stream error")
	}
	if strings.Contains(streamErr.Error(), "provider-secret-code") {
		t.Fatalf("stream error leaked provider details: %v", streamErr)
	}
	if err := stream.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("close: %v", err)
	}
}

func TestChatReportsNamedUpstreamErrorEvent(t *testing.T) {
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		_ *http.Request,
		_ *executionFixture,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: error\n"))
		_, _ = writer.Write([]byte("data: {\"message\":\"provider-secret-value\"}\n\n"))
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	for range stream.Chunks() {
	}
	streamErr := stream.Err()
	if streamErr == nil || !strings.Contains(streamErr.Error(), "upstream error") {
		t.Fatalf("expected a stable upstream error, got %v", streamErr)
	}
	if strings.Contains(streamErr.Error(), "provider-secret-value") {
		t.Fatalf("stream error leaked provider details: %v", streamErr)
	}
}

func TestChatCloseDoesNotReportInterruptedStream(t *testing.T) {
	started := make(chan struct{})
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		request *http.Request,
		_ *executionFixture,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-request.Context().Done()
	})

	stream, err := fixture.client().Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server did not receive the stream")
	}
	if err := stream.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("close: %v", err)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("intentional close reported a stream failure: %v", err)
	}
}

func TestChatRespectsContextCancellation(t *testing.T) {
	started := make(chan struct{})
	contextCanceled := make(chan struct{})
	fixture := newExecutionFixture(t, func(
		writer http.ResponseWriter,
		request *http.Request,
		_ *executionFixture,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-request.Context().Done()
		close(contextCanceled)
	})

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := fixture.client().Chat(ctx, llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server did not receive the stream")
	}
	cancel()
	select {
	case <-contextCanceled:
	case <-time.After(time.Second):
		t.Fatal("upstream execution request was not canceled")
	}
	if err := stream.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("close: %v", err)
	}
	if err := stream.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation error = %v, want context.Canceled", err)
	}
}

func TestChatRejectsMissingServiceToken(t *testing.T) {
	client := New("http://127.0.0.1", "")
	_, err := client.Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err == nil {
		t.Fatal("expected missing service token error")
	}
}

func TestChatUsesDefaultDevicePlatformBaseURLForTicketAndExecution(t *testing.T) {
	var ticketURL string
	var executionURL string
	client := New("", testServiceToken)
	client.MaxAttempts = 1
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.Method == http.MethodGet &&
				strings.HasSuffix(request.URL.Path, "/ai-execution-ticket"):
				ticketURL = request.URL.String()
				return jsonResponse(
					request,
					http.StatusOK,
					`{"data":{"ticket":"default-base-ticket","expires_in":120},"error":null}`,
				), nil
			case request.Method == http.MethodPost &&
				request.URL.Path == "/internal/v1/ai-execution":
				executionURL = request.URL.String()
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/event-stream"},
					},
					Body:    io.NopCloser(strings.NewReader("data: [DONE]\n")),
					Request: request,
				}, nil
			default:
				return nil, fmt.Errorf("unexpected request %s %s", request.Method, request.URL.String())
			}
		}),
	}

	stream, err := client.Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat with default base URL: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	for name, endpoint := range map[string]string{
		"ticket":    ticketURL,
		"execution": executionURL,
	} {
		parsed, parseErr := url.Parse(endpoint)
		if parseErr != nil {
			t.Fatalf("parse %s endpoint %q: %v", name, endpoint, parseErr)
		}
		if parsed.Scheme != "http" ||
			parsed.Host != "127.0.0.1:8081" ||
			parsed.Path == "" {
			t.Fatalf("%s endpoint = %q, want the default device platform address", name, endpoint)
		}
	}
}

func TestChatDoesNotRetryRejectedTicket(t *testing.T) {
	var ticketRequests atomic.Int32
	client := New("http://device-platform.invalid", testServiceToken)
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			ticketRequests.Add(1)
			return jsonResponse(
				request,
				http.StatusForbidden,
				`{"data":null,"error":{"code":"account_unavailable","retryable":false}}`,
			), nil
		}),
	}
	_, err := client.Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err == nil || strings.Contains(err.Error(), "account_unavailable") {
		t.Fatalf("expected a stable ticket error, got %v", err)
	}
	if ticketRequests.Load() != 1 {
		t.Fatalf("ticket requests = %d, want 1", ticketRequests.Load())
	}
}

func TestChatRetriesTicketMarkedRetryable(t *testing.T) {
	var ticketRequests atomic.Int32
	var executionRequests atomic.Int32
	client := New("http://device-platform.invalid", testServiceToken)
	client.RetryBackoff = time.Millisecond
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(request.URL.Path, "ai-execution-ticket"):
				attempt := ticketRequests.Add(1)
				if attempt == 1 {
					return jsonResponse(
						request,
						http.StatusServiceUnavailable,
						`{"data":null,"error":{"code":"execution_unavailable","retryable":true}}`,
					), nil
				}
				return jsonResponse(
					request,
					http.StatusOK,
					`{"data":{"ticket":"ticket-retry","expires_in":120},"error":null}`,
				), nil
			case request.URL.Path == "/internal/v1/ai-execution":
				executionRequests.Add(1)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/event-stream"},
					},
					Body:    io.NopCloser(strings.NewReader("data: [DONE]\n")),
					Request: request,
				}, nil
			default:
				return nil, errors.New("unexpected request")
			}
		}),
	}

	stream, err := client.Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("chat after retryable ticket failure: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if ticketRequests.Load() != 2 || executionRequests.Load() != 1 {
		t.Fatalf(
			"ticket requests = %d, execution requests = %d, want 2 and 1",
			ticketRequests.Load(),
			executionRequests.Load(),
		)
	}
}

func TestChatRejectsMalformedExecutionTicket(t *testing.T) {
	for _, payload := range []string{
		`{"data":{},"error":null}`,
		`{"data":{"ticket":"ticket","expires_in":0},"error":null}`,
		`{"data":{"ticket":"","expires_in":120},"error":null}`,
	} {
		client := New("http://device-platform.invalid", testServiceToken)
		client.HTTPClient = &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return jsonResponse(request, http.StatusOK, payload), nil
			}),
		}
		_, err := client.Chat(context.Background(), llm.Request{
			Model:     "sprout-chat",
			Messages:  []llm.Message{{Role: "user", Content: "hello"}},
			DeviceID:  "device-alpha",
			SessionID: "session-alpha",
		})
		if err == nil {
			t.Fatalf("expected malformed ticket %q to fail", payload)
		}
	}
}

func jsonResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(body)),
		Request: request,
	}
}
