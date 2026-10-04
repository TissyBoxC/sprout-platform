package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
)

func TestSynthesizeStreamsAudio(t *testing.T) {
	audio := []byte("RIFFlocal-audio")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/audio/speech" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("missing authorization")
		}
		var body struct {
			Model string `json:"model"`
			Input string `json:"input"`
			Voice string `json:"voice"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Input != "你好" || body.Voice != "nova" || body.Model != "tts-model" {
			t.Fatalf("unexpected request %+v", body)
		}
		writer.Header().Set("Content-Type", "audio/wav")
		_, _ = writer.Write(audio)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "key", Model: "tts-model", HTTPClient: server.Client()})
	stream, err := client.Synthesize(context.Background(), tts.Request{Text: "你好", Voice: "nova"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	defer stream.Close()
	var output bytes.Buffer
	for chunk := range stream.Audio() {
		output.Write(chunk)
	}
	if output.String() != string(audio) {
		t.Fatalf("unexpected audio %q", output.String())
	}
}

func TestSynthesizeMapsStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, APIKey: "key", HTTPClient: server.Client()})
	_, err := client.Synthesize(context.Background(), tts.Request{Text: "你好"})
	if err == nil || !strings.Contains(err.Error(), "status=400") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestSynthesizeRejectsEmptyText(t *testing.T) {
	client := New(Config{BaseURL: "http://127.0.0.1", APIKey: "key", HTTPClient: http.DefaultClient})
	if _, err := client.Synthesize(context.Background(), tts.Request{}); err == nil {
		t.Fatal("expected empty text error")
	}
}
