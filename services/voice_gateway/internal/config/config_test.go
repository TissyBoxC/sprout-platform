package config

import (
	"strings"
	"testing"
)

func TestLoadReadsServiceSettings(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_HTTP_PORT", "9090")
	t.Setenv("VOICE_GATEWAY_DEVICE_PLATFORM_BASE_URL", "http://device-platform.internal")
	t.Setenv("VOICE_GATEWAY_DEVICE_PLATFORM_SERVICE_TOKEN", strings.Repeat("t", 32))
	t.Setenv("VOICE_GATEWAY_LLM_MAX_ATTEMPTS", "4")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.HTTP.Port != "9090" {
		t.Fatalf("expected HTTP port 9090, got %q", cfg.HTTP.Port)
	}
	if cfg.AI.DevicePlatformBaseURL != "http://device-platform.internal" {
		t.Fatalf("unexpected device platform URL: %q", cfg.AI.DevicePlatformBaseURL)
	}
	if cfg.Audio.LLMConfig.MaxAttempts != 4 {
		t.Fatalf("LLM max attempts = %d, want 4", cfg.Audio.LLMConfig.MaxAttempts)
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

func TestLoadKeepsVisionDisabledByDefault(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.Vision.Enabled {
		t.Fatal("vision must be disabled by default")
	}
}

func TestLoadReadsVisionSettings(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_VISION_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_VISION_MODEL", "vision-model")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
	setExecutionServiceEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if !cfg.Vision.Enabled || cfg.Vision.Model != "vision-model" {
		t.Fatalf("unexpected vision config: %+v", cfg.Vision)
	}
}

func TestLoadRejectsEnabledVisionWithoutModel(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_VISION_ENABLED", "true")

	if _, err := Load(); err == nil {
		t.Fatal("expected enabled vision without a model to fail")
	}
}

func TestLoadRejectsEnabledVisionWithoutDatabase(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_VISION_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_VISION_MODEL", "vision-model")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "")
	setExecutionServiceEnvironment(t)

	if _, err := Load(); err == nil {
		t.Fatal("expected enabled vision without a database to fail")
	}
}

func TestLoadRejectsEnabledInternalAPIWithoutStrongToken(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_INTERNAL_API_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_INTERNAL_API_TOKEN", "short-token")

	if _, err := Load(); err == nil {
		t.Fatal("expected weak internal API token to be rejected")
	}
}

func TestLoadRejectsUnsupportedRealtimeProvider(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_REALTIME_PROVIDER", "openai_realtime")

	if _, err := Load(); err == nil {
		t.Fatal("expected the retired realtime provider selector to be rejected")
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

func TestLoadRejectsEnabledWebsiteSocketWithoutDatabase(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
	t.Setenv("VOICE_GATEWAY_ASR_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_TTS_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_LLM_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected realtime endpoint without a database to be rejected")
	}
}

func TestLoadAcceptsEnabledWebsiteSocketWithDatabase(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
	t.Setenv("VOICE_GATEWAY_ASR_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_TTS_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_LLM_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
	setExecutionServiceEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if !cfg.WebSocket.Enabled {
		t.Fatal("expected realtime endpoint to stay enabled with a database configured")
	}
}

func TestLoadRejectsUnsafeSessionTokenTTL(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
	t.Setenv("VOICE_GATEWAY_ASR_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_TTS_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_LLM_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
	setExecutionServiceEnvironment(t)
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

func TestLoadRejectsInvalidLLMMaxAttempts(t *testing.T) {
	for _, value := range []string{"0", "6"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("VOICE_GATEWAY_LLM_MAX_ATTEMPTS", value)
			if _, err := Load(); err == nil {
				t.Fatalf("expected max attempts %s to be rejected", value)
			}
		})
	}
}

func TestLoadReadsWebsiteSocketLimits(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
	t.Setenv("VOICE_GATEWAY_ASR_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_TTS_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_LLM_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
	setExecutionServiceEnvironment(t)
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

func TestLoadReadsAllowedOriginsAndDeduplicates(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ALLOWED_ORIGINS", " https://admin.example.com/ ,https://app.example.com,https://admin.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	want := []string{"https://admin.example.com", "https://app.example.com"}
	if len(cfg.WebSocket.AllowedOrigins) != len(want) {
		t.Fatalf("allowed origins = %v, want %v", cfg.WebSocket.AllowedOrigins, want)
	}
	for index := range want {
		if cfg.WebSocket.AllowedOrigins[index] != want[index] {
			t.Fatalf("allowed origins = %v, want %v", cfg.WebSocket.AllowedOrigins, want)
		}
	}
}

func TestLoadRejectsNonFiniteAndInvalidAudioFloats(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.Audio.VadSpeechThreshold != 2.8 {
		t.Fatalf("VAD threshold = %v, want 2.8", cfg.Audio.VadSpeechThreshold)
	}

	testCases := []struct {
		name  string
		key   string
		value string
	}{
		{name: "vad nan", key: "VOICE_GATEWAY_VAD_SPEECH_THRESHOLD", value: "NaN"},
		{name: "vad infinity", key: "VOICE_GATEWAY_VAD_SPEECH_THRESHOLD", value: "+Inf"},
		{name: "vad below ratio", key: "VOICE_GATEWAY_VAD_SPEECH_THRESHOLD", value: "0.02"},
		{name: "target rms nan", key: "VOICE_GATEWAY_TARGET_RMS_DBFS", value: "NaN"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(testCase.key, testCase.value)
			if _, loadErr := Load(); loadErr == nil {
				t.Fatalf("expected %s to be rejected", testCase.value)
			}
		})
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
			t.Setenv("VOICE_GATEWAY_ASR_PROVIDER", "mock")
			t.Setenv("VOICE_GATEWAY_TTS_PROVIDER", "mock")
			t.Setenv("VOICE_GATEWAY_LLM_PROVIDER", "mock")
			t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
			setExecutionServiceEnvironment(t)
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

func TestAuthRecheckDefaultsAndBounds(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.WebSocket.AuthRecheckSeconds != 30 {
		t.Fatalf("auth recheck = %d, want 30", cfg.WebSocket.AuthRecheckSeconds)
	}

	testCases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "minimum", value: "5"},
		{name: "maximum", value: "600"},
		{name: "too short", value: "4", wantErr: true},
		{name: "too long", value: "601", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
			t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
			t.Setenv("VOICE_GATEWAY_ASR_PROVIDER", "mock")
			t.Setenv("VOICE_GATEWAY_TTS_PROVIDER", "mock")
			t.Setenv("VOICE_GATEWAY_LLM_PROVIDER", "mock")
			t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
			setExecutionServiceEnvironment(t)
			t.Setenv("VOICE_GATEWAY_WS_AUTH_RECHECK_SECONDS", testCase.value)
			if _, err := Load(); testCase.wantErr && err == nil {
				t.Fatalf("expected auth recheck %s to be rejected", testCase.value)
			}
		})
	}
}

func TestLoadRejectsEnabledWebsiteSocketWithoutASRProvider(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
	t.Setenv("VOICE_GATEWAY_ASR_PROVIDER", "")
	t.Setenv("VOICE_GATEWAY_TTS_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_LLM_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
	setExecutionServiceEnvironment(t)

	if _, err := Load(); err == nil {
		t.Fatal("expected realtime endpoint without an ASR provider to be rejected")
	}
}

func TestLoadRejectsEnabledWebsiteSocketWithoutTTSProvider(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
	t.Setenv("VOICE_GATEWAY_ASR_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_TTS_PROVIDER", "")
	t.Setenv("VOICE_GATEWAY_LLM_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
	setExecutionServiceEnvironment(t)

	if _, err := Load(); err == nil {
		t.Fatal("expected realtime endpoint without a TTS provider to be rejected")
	}
}

func TestLoadRejectsEnabledWebsiteSocketWithoutLLMProvider(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_WS_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_WS_TOKEN_SECRET", strings.Repeat("s", 40))
	t.Setenv("VOICE_GATEWAY_ASR_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_TTS_PROVIDER", "mock")
	t.Setenv("VOICE_GATEWAY_LLM_PROVIDER", "")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
	setExecutionServiceEnvironment(t)

	if _, err := Load(); err == nil {
		t.Fatal("expected realtime endpoint without an LLM provider to be rejected")
	}
}

func TestLoadRejectsModelPathWithoutDevicePlatformServiceToken(t *testing.T) {
	t.Setenv("VOICE_GATEWAY_VISION_ENABLED", "true")
	t.Setenv("VOICE_GATEWAY_VISION_MODEL", "vision-model")
	t.Setenv("VOICE_GATEWAY_DATABASE_DSN", "postgres://voice:voice@localhost:5432/voice")
	t.Setenv("VOICE_GATEWAY_DEVICE_PLATFORM_BASE_URL", "http://device-platform:8081")
	t.Setenv("VOICE_GATEWAY_DEVICE_PLATFORM_SERVICE_TOKEN", "short")

	if _, err := Load(); err == nil {
		t.Fatal("expected a weak device platform service token to fail closed")
	}
}

func setExecutionServiceEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("VOICE_GATEWAY_DEVICE_PLATFORM_BASE_URL", "http://device-platform:8081")
	t.Setenv(
		"VOICE_GATEWAY_DEVICE_PLATFORM_SERVICE_TOKEN",
		strings.Repeat("t", 32),
	)
}
