package volcano

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
)

func TestOpenBuildsVolcanoRequestAndParsesResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Api-App-Key") != "app" ||
			request.Header.Get("X-Api-Access-Key") != "token" {
			t.Fatalf("missing required headers")
		}
		var body struct {
			Audio struct {
				Data string `json:"data"`
			} `json:"audio"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Audio.Data != "AQACAA==" {
			t.Fatalf("unexpected encoded audio %q", body.Audio.Data)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"result":{"text":"你好"}}`))
	}))
	defer server.Close()

	client := New(Config{
		Endpoint:    server.URL,
		AppID:       "app",
		AccessToken: "token",
		HTTPClient:  server.Client(),
	})
	stream, err := client.Open(context.Background(), asr.Config{Language: "zh-CN"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := stream.WriteAudio([]byte{1, 0, 2, 0}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestOpenRequiresVolcanoCredentials(t *testing.T) {
	client := New(Config{Endpoint: "http://127.0.0.1", HTTPClient: http.DefaultClient})
	if _, err := client.Open(context.Background(), asr.Config{}); err == nil {
		t.Fatal("expected credentials error")
	}
}
