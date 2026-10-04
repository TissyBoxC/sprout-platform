package local_piper

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
)

func TestSynthesizeStreamsLocalPiperAudio(t *testing.T) {
	audio := []byte("RIFFpiper")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/synthesize" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		var body struct {
			Text  string `json:"text"`
			Voice string `json:"voice"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Text != "你好" || body.Voice != "local" {
			t.Fatalf("unexpected body %+v", body)
		}
		writer.Header().Set("Content-Type", "audio/wav")
		_, _ = writer.Write(audio)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	stream, err := client.Synthesize(context.Background(), tts.Request{Text: "你好", Voice: "local"})
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

func TestSynthesizeRejectsEmptyText(t *testing.T) {
	client := New(Config{BaseURL: "http://127.0.0.1", HTTPClient: http.DefaultClient})
	if _, err := client.Synthesize(context.Background(), tts.Request{}); err == nil {
		t.Fatal("expected empty text error")
	}
}
