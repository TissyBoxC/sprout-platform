package sub2api_client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
)

func TestChatProviderErrorDoesNotExposeBodySecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte("upstream key sk-secret_1234567890abcdef child@example.com"))
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	_, err := client.Chat(context.Background(), llm.Request{
		Model:    "sprout-chat",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
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
}
