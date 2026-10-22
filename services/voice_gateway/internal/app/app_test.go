package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/careview"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
)

// An enabled realtime endpoint without a device authorizer would fail open on
// consent withdrawal, so the composition root must refuse to build it.
func TestBuildRealtimeTransportRejectsMissingAuthorizer(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := config.Config{}
	cfg.WebSocket.Enabled = true

	_, _, _, err := buildRealtimeTransport(cfg, logger, nil, nil, nil, nil, careview.NewDeviceConnectionRegistry())
	if err == nil {
		t.Fatal("expected a missing device authorizer to fail the realtime transport build")
	}
}

// A disabled endpoint stays a no-op so health and internal endpoints keep
// working without the platform database.
func TestBuildRealtimeTransportDisabledIsNoOp(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler, manager, shutdown, err := buildRealtimeTransport(config.Config{}, logger, nil, nil, nil, nil, careview.NewDeviceConnectionRegistry())
	if err != nil {
		t.Fatalf("buildRealtimeTransport() error = %v", err)
	}
	if handler != nil || manager != nil {
		t.Fatalf("disabled endpoint must not return a handler or manager")
	}
	shutdown()
}

// An enabled endpoint without the full ASR/LLM/TTS pipeline must fail closed at
// the composition root instead of exposing a socket that fails on first audio.
func TestBuildRealtimeTransportRejectsIncompleteConversationPipeline(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := config.Config{}
	cfg.WebSocket.Enabled = true
	cfg.WebSocket.AuthTokenSecret = strings.Repeat("s", 40)
	cfg.Audio.ASRProvider = "openai"
	cfg.Audio.TTSProvider = "openai"
	cfg.Audio.LLMProvider = ""
	cfg.AI.DevicePlatformBaseURL = "http://device-platform:8081"
	cfg.AI.DevicePlatformServiceToken = strings.Repeat("t", 32)

	_, _, _, err := buildRealtimeTransport(cfg, logger, nil, stubAuthorizer{}, nil, nil, careview.NewDeviceConnectionRegistry())
	if err == nil {
		t.Fatal("expected an incomplete conversation pipeline to fail the realtime transport build")
	}
}

type stubAuthorizer struct{}

func (stubAuthorizer) AuthorizeDevice(context.Context, string) error {
	return nil
}
