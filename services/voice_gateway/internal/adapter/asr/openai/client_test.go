package openai

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
)

func TestOpenTranscribesMultipartAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing bearer authorization")
		}
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("unexpected content type %q", request.Header.Get("Content-Type"))
		}
		reader, err := request.MultipartReader()
		if err != nil {
			t.Fatalf("multipart reader: %v", err)
		}
		foundAudio := false
		foundModel := false
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("next part: %v", err)
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "file":
				foundAudio = len(data) > 44 && strings.HasPrefix(string(data[:4]), "RIFF")
			case "model":
				foundModel = string(data) == "whisper-1"
			}
		}
		if !foundAudio || !foundModel {
			t.Fatalf("missing multipart fields audio=%v model=%v", foundAudio, foundModel)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("x-request-id", "req-1")
		_, _ = writer.Write([]byte(`{"text":"你好"}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "test-key", HTTPClient: server.Client()})
	stream, err := client.Open(context.Background(), asr.Config{Language: "zh"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := stream.WriteAudio([]byte{1, 0, 2, 0}); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	result := <-stream.Results()
	if result.Text != "你好" || !result.IsFinal || result.RequestID != "req-1" {
		t.Fatalf("unexpected result %+v", result)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
}

func TestOpenReturnsConfigurationError(t *testing.T) {
	client := New(Config{BaseURL: "http://127.0.0.1", HTTPClient: http.DefaultClient})
	if _, err := client.Open(context.Background(), asr.Config{}); err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestOpenMapsProviderStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, APIKey: "key", HTTPClient: server.Client()})
	stream, err := client.Open(context.Background(), asr.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := stream.WriteAudio([]byte{1, 0}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := stream.Close(); err == nil {
		t.Fatal("expected provider status error")
	}
}
