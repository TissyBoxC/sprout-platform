package aliyun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
)

func TestOpenBuildsAlibabaRequestAndParsesResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("appkey") != "app" ||
			request.URL.Query().Get("format") != "pcm" ||
			request.URL.Query().Get("sample_rate") != "16000" {
			t.Fatalf("unexpected query %q", request.URL.RawQuery)
		}
		if request.Header.Get("X-NLS-Token") != "token" {
			t.Fatalf("missing token header")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":20000000,"result":"你好"}`))
	}))
	defer server.Close()

	client := New(Config{
		Endpoint:   server.URL,
		AppKey:     "app",
		Token:      "token",
		HTTPClient: server.Client(),
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

func TestOpenRequiresCredentials(t *testing.T) {
	client := New(Config{Endpoint: "http://127.0.0.1", HTTPClient: http.DefaultClient})
	if _, err := client.Open(context.Background(), asr.Config{}); err == nil {
		t.Fatal("expected credentials error")
	}
}
