package volcano

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
)

func TestSynthesizeBuildsVolcanoRequestAndDecodesAudio(t *testing.T) {
	audio := []byte("RIFFvolcano")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer; token" {
			t.Fatalf("unexpected authorization header")
		}
		var body struct {
			App struct {
				AppID string `json:"appid"`
			} `json:"app"`
			Request struct {
				Text string `json:"text"`
			} `json:"request"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.App.AppID != "app" || body.Request.Text != "你好" {
			t.Fatalf("unexpected body %+v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"data":"` +
			base64.StdEncoding.EncodeToString(audio) + `"}`))
	}))
	defer server.Close()

	client := New(Config{
		Endpoint:    server.URL,
		AppID:       "app",
		AccessToken: "token",
		HTTPClient:  server.Client(),
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

func TestSynthesizeRequiresVolcanoCredentials(t *testing.T) {
	client := New(Config{Endpoint: "http://127.0.0.1", HTTPClient: http.DefaultClient})
	if _, err := client.Synthesize(context.Background(), tts.Request{Text: "你好"}); err == nil {
		t.Fatal("expected credentials error")
	}
}
