package local_whisper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
)

func TestOpenUsesLocalTranscriptionEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if _, _, err := request.FormFile("file"); err != nil {
			t.Fatalf("missing file field: %v", err)
		}
		if request.FormValue("model") != "whisper-local" {
			t.Fatalf("unexpected model %q", request.FormValue("model"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"text":"本地识别"}`))
	}))
	defer server.Close()

	client := New(Config{
		BaseURL:    server.URL,
		Model:      "whisper-local",
		HTTPClient: server.Client(),
	})
	stream, err := client.Open(context.Background(), asr.Config{Language: "zh"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := stream.WriteAudio([]byte{1, 0, 2, 0}); err != nil {
		t.Fatalf("write: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = stream.Close()
	}()
	<-done
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
}

func TestOpenRejectsNoAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, err := client.Open(context.Background(), asr.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := stream.Close(); err == nil || !strings.Contains(err.Error(), "no audio") {
		t.Fatalf("expected no-audio error, got %v", err)
	}
}
