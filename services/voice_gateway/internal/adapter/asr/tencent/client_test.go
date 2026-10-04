package tencent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
)

func TestOpenSignsAndParsesTencentResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-TC-Action") != "SentenceRecognition" {
			t.Fatalf("unexpected action %q", request.Header.Get("X-TC-Action"))
		}
		if request.Header.Get("Authorization") == "" {
			t.Fatalf("missing authorization signature")
		}
		var body struct {
			Data    string `json:"Data"`
			DataLen int    `json:"DataLen"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Data == "" || body.DataLen != 4 {
			t.Fatalf("unexpected body %+v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"Response":{"Result":"你好","RequestId":"req-1"}}`))
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

func TestOpenRequiresTencentCredentials(t *testing.T) {
	client := New(Config{Endpoint: "http://127.0.0.1", HTTPClient: http.DefaultClient})
	if _, err := client.Open(context.Background(), asr.Config{}); err == nil {
		t.Fatal("expected credentials error")
	}
}
