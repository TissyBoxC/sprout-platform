package config

import (
	"strings"
	"testing"
)

func TestLoadReadsServiceSettings(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_HTTP_PORT", "9090")
	t.Setenv("VOICE_GATEWAY_SUB2API_BASE_URL", "http://sub2api.internal")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.HTTP.Port != "9090" {
		t.Fatalf("expected HTTP port 9090, got %q", cfg.HTTP.Port)
	}
	if cfg.Sub2API.BaseURL != "http://sub2api.internal" {
		t.Fatalf("unexpected sub2api URL: %q", cfg.Sub2API.BaseURL)
	}
}

func TestLoadUsesConservativeSecurityDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if !cfg.Security.ContentPolicyEnabled {
		t.Fatal("content policy must be enabled by default")
	}
	if !cfg.Security.InputFilterEnabled || !cfg.Security.OutputFilterEnabled {
		t.Fatal("input and output moderation must be enabled by default")
	}
	if cfg.Security.MaxInputRunes != 1200 {
		t.Fatalf("max input runes = %d, want 1200", cfg.Security.MaxInputRunes)
	}
	if cfg.Internal.Enabled {
		t.Fatal("internal API must be disabled by default")
	}
}

func TestLoadReadsInternalAPISettings(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_INTERNAL_API_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_INTERNAL_API_TOKEN", strings.Repeat("t", 32))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if !cfg.Internal.Enabled {
		t.Fatal("expected internal API to be enabled")
	}
	if cfg.Internal.AuthToken != strings.Repeat("t", 32) {
		t.Fatalf("unexpected internal API token: %q", cfg.Internal.AuthToken)
	}
}

func TestLoadRejectsEnabledInternalAPIWithoutStrongToken(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_INTERNAL_API_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_INTERNAL_API_TOKEN", "short-token")

	if _, err := Load(); err == nil {
		t.Fatal("expected weak internal API token to be rejected")
	}
}

func TestLoadKeepsWebsiteSocketDisabledByDefault(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.WebSocket.Enabled {
		t.Fatal("realtime endpoint must stay disabled until a token secret is configured")
	}
	if cfg.WebSocket.Path != "/v1/voice" {
		t.Fatalf("unexpected default websocket path: %q", cfg.WebSocket.Path)
	}
	if !cfg.Audio.NoiseSuppressionEnabled || !cfg.Audio.EchoCancellationEnabled {
		t.Fatal("audio preprocessing must be enabled by default")
	}
}

func TestLoadRejectsEnabledWebsiteSocketWithoutStrongSecret(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", "short-secret")

	if _, err := Load(); err == nil {
		t.Fatal("expected weak websocket secret to be rejected")
	}
}

func TestLoadRejectsUnsafeSessionTokenTTL(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
	t.Setenv("VOICE_GATEWAY_WS_SESSION_TOKEN_TTL_SECONDS", "3600")

	if _, err := Load(); err == nil {
		t.Fatal("expected excessive session token ttl to be rejected")
	}
}

func TestLoadRejectsInvalidMaxInputRunes(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_MAX_INPUT_RUNES", "8")

	if _, err := Load(); err == nil {
		t.Fatal("expected tiny max input runes to be rejected")
	}
}

func TestLoadReadsWebsiteSocketLimits(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
	t.Setenv("VOICE_GATEWAY_WS_MAX_TOTAL_SESSIONS", "128")
	t.Setenv("VOICE_GATEWAY_WS_MAX_FRAME_BYTES", "2048")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.WebSocket.MaxTotalSessions != 128 {
		t.Fatalf("expected 128 total sessions, got %d", cfg.WebSocket.MaxTotalSessions)
	}
	if cfg.WebSocket.MaxFrameBytes != 2048 {
		t.Fatalf("expected 2048 max frame bytes, got %d", cfg.WebSocket.MaxFrameBytes)
	}
}

func TestContinuityWindowDefaultsAndBounds(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.WebSocket.ContinuityWindowSeconds != 60 {
		t.Fatalf(
			"continuity window = %d, want 60",
			cfg.WebSocket.ContinuityWindowSeconds,
		)
	}

	testCases := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "minimum", value: "5", want: 5},
		{name: "maximum", value: "600", want: 600},
		{name: "too short", value: "4", wantErr: true},
		{name: "too long", value: "601", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
			t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
			t.Setenv("VOICE_GATEWAY_WS_CONTINUITY_WINDOW_SECONDS", testCase.value)
			cfg, err := Load()
			if testCase.wantErr && err == nil {
				t.Fatalf("expected continuity window %s to be rejected", testCase.value)
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("continuity window %s was rejected: %v", testCase.value, err)
			}
			if !testCase.wantErr && cfg.WebSocket.ContinuityWindowSeconds != testCase.want {
				t.Fatalf(
					"continuity window = %d, want %d",
					cfg.WebSocket.ContinuityWindowSeconds,
					testCase.want,
				)
			}
		})
	}
}
