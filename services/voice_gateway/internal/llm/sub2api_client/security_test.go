package sub2api_client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
)

func TestChatProviderErrorDoesNotExposeBodySecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+testServiceToken {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch {
		case request.Method == http.MethodGet &&
			strings.Contains(request.URL.Path, "/ai-execution-ticket"):
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"data":{"ticket":"ticket-secret-test","expires_in":120},"error":null}`))
		case request.Method == http.MethodPost &&
			request.URL.Path == "/internal/v1/ai-execution":
			writer.WriteHeader(http.StatusBadGateway)
			_, _ = writer.Write([]byte("upstream key sk-secret_1234567890abcdef child@example.com"))
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.String())
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := New(server.URL, testServiceToken)
	client.HTTPClient = server.Client()
	client.MaxAttempts = 1
	client.RetryBackoff = time.Millisecond
	_, err := client.Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err == nil {
		t.Fatal("expected provider status error")
	}
	if !strings.Contains(err.Error(), "status=502") {
		t.Fatalf("error = %q, want stable status code", err.Error())
	}
	if strings.Contains(err.Error(), "sk-secret_") || strings.Contains(err.Error(), "child@example.com") {
		t.Fatalf("provider error exposed sensitive content: %q", err.Error())
	}
	if strings.Contains(err.Error(), testServiceToken) || strings.Contains(err.Error(), "ticket-secret-test") {
		t.Fatalf("provider error exposed an internal credential: %q", err.Error())
	}
}
