package aliyun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
)

func TestSynthesizeBuildsAlibabaRequestAndStreamsAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-NLS-Token") != "token" {
			t.Fatalf("missing token header")
		}
		var body struct {
			AppKey string `json:"appkey"`
			Text   string `json:"text"`
			Voice  string `json:"voice"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.AppKey != "app" || body.Text != "你好" || body.Voice != "xiaoyun" {
			t.Fatalf("unexpected body %+v", body)
		}
		writer.Header().Set("Content-Type", "audio/wav")
		_, _ = writer.Write([]byte("RIFFaudio"))
	}))
	defer server.Close()

	client := New(Config{
		Endpoint:   server.URL,
		AppKey:     "app",
		Token:      "token",
		HTTPClient: server.Client(),
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

func TestSynthesizeRequiresAlibabaCredentials(t *testing.T) {
	client := New(Config{Endpoint: "http://127.0.0.1", HTTPClient: http.DefaultClient})
	if _, err := client.Synthesize(context.Background(), tts.Request{Text: "你好"}); err == nil {
		t.Fatal("expected credentials error")
	}
}
