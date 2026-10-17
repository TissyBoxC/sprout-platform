package sub2api_client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
)

func TestChatStreamsOpenAIContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization header was not forwarded")
		}
		var body struct {
			Model    string        `json:"model"`
			Messages []llm.Message `json:"messages"`
			Stream   bool          `json:"stream"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !body.Stream || body.Model != "sprout-chat" || len(body.Messages) != 1 {
			t.Fatalf("unexpected request body: %+v", body)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n"))
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n"))
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	stream, err := client.Chat(context.Background(), llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "你好"}},
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
}

// The AI gateway only recognizes robot traffic and applies its stable
// degradation contract when the X-Sprout-* provenance headers are present.
func TestChatSendsSproutProvenanceHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("X-Sprout-Device-ID"); got != "72453d1e-dce3-401a-96db-e96db54c9fd5" {
			t.Errorf("X-Sprout-Device-ID = %q", got)
		}
		if got := request.Header.Get("X-Sprout-Session-ID"); got != "session_1" {
			t.Errorf("X-Sprout-Session-ID = %q", got)
		}
		if got := request.Header.Get("X-Sprout-Project"); got != "voice" {
			t.Errorf("X-Sprout-Project = %q", got)
		}
		if got := request.Header.Get("X-Sprout-Policy-Version"); got != "age_5_6" {
			t.Errorf("X-Sprout-Policy-Version = %q", got)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: [DONE]\n"))
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	stream, err := client.Chat(context.Background(), llm.Request{
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
}

// A request without provenance must not invent labels: an untagged call keeps
// the gateway's legacy behavior and cannot attribute usage to a fake device.
func TestChatOmitsProvenanceWhenUnset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("X-Sprout-Device-ID"); got != "" {
			t.Errorf("X-Sprout-Device-ID = %q, want empty", got)
		}
		if got := request.Header.Get("X-Sprout-Session-ID"); got != "" {
			t.Errorf("X-Sprout-Session-ID = %q, want empty", got)
		}
		if got := request.Header.Get("X-Sprout-Policy-Version"); got != "" {
			t.Errorf("X-Sprout-Policy-Version = %q, want empty", got)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: [DONE]\n"))
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	stream, err := client.Chat(context.Background(), llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "你好"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestChatReturnsProviderStatusSynchronously(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "upstream unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	_, err := client.Chat(context.Background(), llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "status=502") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestChatRetriesTransientProviderStatus(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if requests < 3 {
			http.Error(writer, "temporarily unavailable", http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n"))
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	client.RetryBackoff = time.Millisecond
	stream, err := client.Chat(context.Background(), llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
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
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestChatDoesNotRetryPermanentProviderStatus(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		http.Error(writer, "invalid request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	client.RetryBackoff = time.Millisecond
	_, err := client.Chat(context.Background(), llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "status=400") {
		t.Fatalf("expected status error, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestChatStopsRetryWhenContextCancels(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		http.Error(writer, "temporarily unavailable", http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := New(server.URL, "secret")
	client.RetryBackoff = time.Hour
	client.Sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}
	_, err := client.Chat(ctx, llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled retry, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestChatRetriesTransientNetworkError(t *testing.T) {
	attempts := 0
	client := New("http://sub2api.invalid", "secret")
	client.MaxAttempts = 2
	client.RetryBackoff = time.Millisecond
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return nil, &net.DNSError{Err: "temporary failure", Name: "sub2api.invalid", IsTemporary: true}
		}),
	}
	_, err := client.Chat(context.Background(), llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("expected request error, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestChatReportsMalformedSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: not-json\n"))
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	stream, err := client.Chat(context.Background(), llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
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

func TestChatRespectsContextCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := New(server.URL, "secret")
	stream, err := client.Chat(ctx, llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
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
	if err := stream.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("close: %v", err)
	}
}

func TestChatRejectsMissingKey(t *testing.T) {
	client := New("http://127.0.0.1", "")
	_, err := client.Chat(context.Background(), llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected missing key error")
	}
}
