package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
)

// An enabled realtime endpoint without a device authorizer would fail open on
// consent withdrawal, so the composition root must refuse to build it.
func TestBuildRealtimeTransportRejectsMissingAuthorizer(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := config.Config{}
	cfg.WebSocket.Enabled = true

	_, _, _, err := buildRealtimeTransport(cfg, logger, nil, nil, nil)
	if err == nil {
		t.Fatal("expected a missing device authorizer to fail the realtime transport build")
	}
}

// A disabled endpoint stays a no-op so health and internal endpoints keep
// working without the platform database.
func TestBuildRealtimeTransportDisabledIsNoOp(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler, manager, shutdown, err := buildRealtimeTransport(config.Config{}, logger, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildRealtimeTransport() error = %v", err)
	}
	if handler != nil || manager != nil {
		t.Fatalf("disabled endpoint must not return a handler or manager")
	}
	shutdown()
}
