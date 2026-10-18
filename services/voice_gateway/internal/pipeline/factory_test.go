package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
	sub2apiclient "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm/sub2api_client"
)

const factoryServiceToken = "voice-device-platform-service-token-0123456789"

func TestBuildConfiguredLLMSub2APIUsesDevicePlatformExecution(t *testing.T) {
	var ticketRequests atomic.Int32
	var executionRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer "+factoryServiceToken {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == "/internal/v1/devices/device-alpha/ai-execution-ticket":
			ticketRequests.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(
				`{"data":{"ticket":"factory-ticket","expires_in":120},"error":null}`,
			))
		case request.Method == http.MethodPost &&
			request.URL.Path == "/internal/v1/ai-execution":
			executionRequests.Add(1)
			if request.URL.Query().Get("ticket") != "factory-ticket" {
				t.Errorf("ticket = %q", request.URL.Query().Get("ticket"))
			}
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if body["model"] != "sprout-chat" {
				t.Errorf("model = %v", body["model"])
			}
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = writer.Write([]byte("data: [DONE]\n"))
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.String())
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	adapters, err := Build(config.AudioConfig{
		LLMProvider:                ProviderSub2API,
		LLMConfig:                  config.LLMProviderConfig{MaxAttempts: 1},
		DevicePlatformBaseURL:      server.URL,
		DevicePlatformServiceToken: factoryServiceToken,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if adapters.LLM == nil {
		t.Fatal("expected a configured LLM client")
	}
	client, ok := adapters.LLM.(*sub2apiclient.Client)
	if !ok {
		t.Fatalf("unexpected LLM adapter type %T", adapters.LLM)
	}
	client.HTTPClient = server.Client()

	stream, err := adapters.LLM.Chat(context.Background(), llm.Request{
		Model:     "sprout-chat",
		Messages:  []llm.Message{{Role: "user", Content: "hello"}},
		DeviceID:  "device-alpha",
		SessionID: "session-alpha",
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	defer stream.Close()
	if ticketRequests.Load() != 1 || executionRequests.Load() != 1 {
		t.Fatalf(
			"ticket requests = %d, execution requests = %d, want 1 each",
			ticketRequests.Load(),
			executionRequests.Load(),
		)
	}
}

func TestBuildLLMRejectsUnknownProvider(t *testing.T) {
	_, err := Build(config.AudioConfig{LLMProvider: "legacy-global-key"})
	if err == nil || !strings.Contains(err.Error(), "unsupported llm provider") {
		t.Fatalf("expected an unsupported provider error, got %v", err)
	}
}

func TestBuildLLMHasNoGlobalCredentialConfiguration(t *testing.T) {
	adapters, err := Build(config.AudioConfig{
		LLMProvider:                ProviderSub2API,
		DevicePlatformBaseURL:      "http://device-platform.invalid",
		DevicePlatformServiceToken: factoryServiceToken,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if adapters.LLM == nil {
		t.Fatal("expected a configured LLM client")
	}
}
