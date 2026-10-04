package tencent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
)

func TestSynthesizeSignsTencentRequestAndDecodesAudio(t *testing.T) {
	audio := []byte("RIFFaudio")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-TC-Action") != "TextToVoice" ||
			request.Header.Get("Authorization") == "" {
			t.Fatalf("missing Tencent action or signature")
		}
		var body struct {
			Text string `json:"Text"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Text != "你好" {
			t.Fatalf("unexpected text %q", body.Text)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"Response":{"Audio":"` +
			base64.StdEncoding.EncodeToString(audio) +
			`","RequestId":"req-1"}}`))
	}))
	defer server.Close()

	fixedTime := time.Unix(1700000000, 0)
	client := New(Config{
		Endpoint:   server.URL,
		SecretID:   "id",
		SecretKey:  "key",
		HTTPClient: server.Client(),
		Now:        func() time.Time { return fixedTime },
	})
	stream, err := client.Synthesize(context.Background(), tts.Request{Text: "你好"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	defer stream.Close()
	for range stream.Audio() {
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
}

func TestSynthesizeRequiresTencentCredentials(t *testing.T) {
	client := New(Config{Endpoint: "http://127.0.0.1", HTTPClient: http.DefaultClient})
	if _, err := client.Synthesize(context.Background(), tts.Request{Text: "你好"}); err == nil {
		t.Fatal("expected credentials error")
	}
}
