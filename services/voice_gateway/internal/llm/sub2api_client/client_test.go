package sub2api_client

import (
	"context"
	"encoding/json"
	"errors"
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
