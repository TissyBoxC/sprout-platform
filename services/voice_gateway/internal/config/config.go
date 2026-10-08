// Package config loads voice gateway configuration.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Config contains runtime settings for the voice gateway.
type Config struct {
	HTTP      HTTPConfig
	Internal  InternalAPIConfig
	Log       LogConfig
	Redis     RedisConfig
	Database  DatabaseConfig
	Sub2API   Sub2APIConfig
	Security  SecurityConfig
	WebSocket WebSocketConfig
	Audio     AudioConfig
}

// WebSocketConfig bounds the public device realtime transport.
//
// Every limit is enforced per connection and per device so one misbehaving
// device cannot exhaust gateway memory or starve other families.
type WebSocketConfig struct {
	// Enabled keeps the realtime device endpoint off until service identity
	// and a device token verifier are configured.
	Enabled bool
	// Path is the upgrade route; it stays private to the gateway host.
	Path string
	// AuthTokenSecret signs and verifies short-lived device session tokens.
	AuthTokenSecret string
	// SessionTokenTTLSeconds bounds how long a device session token stays valid.
	SessionTokenTTLSeconds int
	// SessionTimeoutSeconds ends an idle session even if the socket stays open.
	SessionTimeoutSeconds int
	// MaxSessionsPerDevice caps concurrent conversations for one device.
	MaxSessionsPerDevice int
	// MaxTotalSessions caps concurrent conversations across the gateway.
	MaxTotalSessions int
	// MaxFrameBytes bounds one inbound binary audio frame.
	MaxFrameBytes int
	// MaxFramesPerSecond throttles inbound audio to reject flooding.
	MaxFramesPerSecond int
	// WriteQueueDepth bounds the outbound queue so a slow device cannot grow memory.
	WriteQueueDepth int
	// SegmentQueueDepth bounds completed utterances buffered per connection.
	SegmentQueueDepth int
	// MaxBytesPerSecond bounds inbound audio bytes per connection.
	MaxBytesPerSecond int
	// ReadTimeoutSeconds and WriteTimeoutSeconds drive ping/pong deadlines.
	ReadTimeoutSeconds int
	// PongWaitSeconds bounds how long a connection may stay silent.
	PongWaitSeconds int
	// PingIntervalSeconds is how often the gateway sends a keepalive ping.
	PingIntervalSeconds int
	// WriteTimeoutSeconds bounds one outbound websocket write.
	WriteTimeoutSeconds int
	// IdleTimeoutSeconds ends a silent session on the gateway side.
	IdleTimeoutSeconds int
	// SessionMaxDurationSeconds bounds one continuous conversation.
	SessionMaxDurationSeconds int
}

// AudioConfig selects the audio preprocessing and provider adapters.
type AudioConfig struct {
	// VadSpeechThreshold is the normalized speech energy threshold.
	VadSpeechThreshold float64
	// VadHangoverFrames keeps a short trailing silence inside one utterance.
	VadHangoverFrames int
	// VadMinSpeechFrames rejects clicks and other sub-frame transients.
	VadMinSpeechFrames int
	// NoiseSuppressionEnabled toggles gateway-side noise suppression.
	NoiseSuppressionEnabled bool
	// EchoCancellationEnabled toggles gateway-side echo cancellation.
	EchoCancellationEnabled bool
	// GainCalibrationEnabled toggles gateway-side gain normalization.
	GainCalibrationEnabled bool
	// TargetRmsDbfs is the desired speech level for gain calibration.
	TargetRmsDbfs float64
	// SegmentQueueDepth bounds completed utterances awaiting conversation.
	SegmentQueueDepth int
	// IdleTimeoutSeconds ends a silent conversation on the gateway.
	IdleTimeoutSeconds int
	// ASRProvider, TTSProvider, RealtimeProvider, and LLMProvider select the
	// pipeline adapters. Empty values leave that stage unconfigured instead of
	// failing startup, so health and internal endpoints keep working.
	ASRProvider      string
	TTSProvider      string
	RealtimeProvider string
	LLMProvider      string
	// LLMModel is the default conversation model; empty uses provider default.
	LLMModel string
	// ASRConfig, TTSConfig, and LLMConfig carry provider-specific credentials.
	ASRConfig ASRProviderConfig
	TTSConfig TTSProviderConfig
	LLMConfig LLMProviderConfig
}

// ASRProviderConfig carries the credentials of one speech recognition provider.
type ASRProviderConfig struct {
	Endpoint    string
	Region      string
	APIKey      string
	AppKey      string
	Token       string
	SecretID    string
	SecretKey   string
	AppID       string
	AccessToken string
	Cluster     string
	Model       string
}

// TTSProviderConfig carries the credentials of one speech synthesis provider.
type TTSProviderConfig struct {
	Endpoint    string
	Region      string
	APIKey      string
	AppKey      string
	Token       string
	SecretID    string
	SecretKey   string
	AppID       string
	AccessToken string
	Cluster     string
	Model       string
	Voice       string
}

// LLMProviderConfig carries the conversation gateway credentials.
type LLMProviderConfig struct {
	BaseURL string
	APIKey  string
}

// DatabaseConfig contains the read/write connection used for usage records.
type DatabaseConfig struct {
	DSN string
}

// HTTPConfig contains HTTP server settings.
type HTTPConfig struct {
	Host string
	Port string
}

// Address returns the HTTP listen address.
func (c HTTPConfig) Address() string {
	return c.Host + ":" + c.Port
}

// InternalAPIConfig contains service-to-service management API settings.
type InternalAPIConfig struct {
	Enabled   bool
	AuthToken string
}

// LogConfig contains logging settings.
type LogConfig struct {
	Level string
}

// SlogLevel returns the configured slog level.
func (c LogConfig) SlogLevel() slog.Level {
	switch strings.ToLower(c.Level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// RedisConfig contains Redis settings.
type RedisConfig struct {
	Address  string
	Password string
}

// Sub2APIConfig contains the AI gateway connection.
type Sub2APIConfig struct {
	BaseURL string
	APIKey  string
}

// SecurityConfig contains content safety settings.
type SecurityConfig struct {
	ContentPolicyEnabled bool
	// InputFilterEnabled and OutputFilterEnabled are explicit operator
	// switches. Production defaults keep both enabled; disabling one leaves a
	// visible, auditable configuration rather than silently weakening policy.
	InputFilterEnabled  bool
	OutputFilterEnabled bool
	// MaxInputRunes bounds recognized speech before prompt construction.
	MaxInputRunes int
}

// Load reads configuration from environment variables with local defaults.
func Load() (Config, error) {
	cfg := Config{
		HTTP: HTTPConfig{
			Host: env("VOICE_GATEWAY_HTTP_HOST", "0.0.0.0"),
			Port: env("VOICE_GATEWAY_HTTP_PORT", "8082"),
		},
		Internal: InternalAPIConfig{
			Enabled:   envBool("VOICE_GATEWAY_INTERNAL_API_ENABLED", false),
			AuthToken: env("VOICE_GATEWAY_INTERNAL_API_TOKEN", ""),
		},
		Log: LogConfig{
			Level: env("VOICE_GATEWAY_LOG_LEVEL", "info"),
		},
		Redis: RedisConfig{
			Address:  env("VOICE_GATEWAY_REDIS_ADDRESS", "127.0.0.1:6379"),
			Password: env("VOICE_GATEWAY_REDIS_PASSWORD", ""),
		},
		Database: DatabaseConfig{
			DSN: env("VOICE_GATEWAY_DATABASE_DSN", ""),
		},
		Sub2API: Sub2APIConfig{
			BaseURL: env("VOICE_GATEWAY_SUB2API_BASE_URL", "http://127.0.0.1:8080"),
			APIKey:  env("VOICE_GATEWAY_SUB2API_API_KEY", ""),
		},
		Security: SecurityConfig{
			ContentPolicyEnabled: envBool("VOICE_GATEWAY_CONTENT_POLICY_ENABLED", true),
			InputFilterEnabled:   envBool("VOICE_GATEWAY_INPUT_FILTER_ENABLED", true),
			OutputFilterEnabled:  envBool("VOICE_GATEWAY_OUTPUT_FILTER_ENABLED", true),
			MaxInputRunes:        envInt("VOICE_GATEWAY_MAX_INPUT_RUNES", 1200),
		},
		WebSocket: WebSocketConfig{
			Enabled:                   envBool("VOICE_GATEWAY_WS_ENABLED", false),
			Path:                      env("VOICE_GATEWAY_WS_PATH", "/v1/voice"),
			AuthTokenSecret:           env("VOICE_GATEWAY_WS_TOKEN_SECRET", ""),
			SessionTokenTTLSeconds:    envInt("VOICE_GATEWAY_WS_SESSION_TOKEN_TTL_SECONDS", 300),
			SessionTimeoutSeconds:     envInt("VOICE_GATEWAY_WS_SESSION_TIMEOUT_SECONDS", 120),
			MaxSessionsPerDevice:      envInt("VOICE_GATEWAY_WS_MAX_SESSIONS_PER_DEVICE", 1),
			MaxTotalSessions:          envInt("VOICE_GATEWAY_WS_MAX_TOTAL_SESSIONS", 512),
			MaxFrameBytes:             envInt("VOICE_GATEWAY_WS_MAX_FRAME_BYTES", 1024),
			MaxFramesPerSecond:        envInt("VOICE_GATEWAY_WS_MAX_FRAMES_PER_SECOND", 80),
			WriteQueueDepth:           envInt("VOICE_GATEWAY_WS_WRITE_QUEUE_DEPTH", 64),
			SegmentQueueDepth:         envInt("VOICE_GATEWAY_WS_SEGMENT_QUEUE_DEPTH", 4),
			MaxBytesPerSecond:         envInt("VOICE_GATEWAY_WS_MAX_BYTES_PER_SECOND", 128*1024),
			ReadTimeoutSeconds:        envInt("VOICE_GATEWAY_WS_READ_TIMEOUT_SECONDS", 30),
			PongWaitSeconds:           envInt("VOICE_GATEWAY_WS_PONG_WAIT_SECONDS", 60),
			PingIntervalSeconds:       envInt("VOICE_GATEWAY_WS_PING_INTERVAL_SECONDS", 25),
			WriteTimeoutSeconds:       envInt("VOICE_GATEWAY_WS_WRITE_TIMEOUT_SECONDS", 10),
			IdleTimeoutSeconds:        envInt("VOICE_GATEWAY_WS_IDLE_TIMEOUT_SECONDS", 60),
			SessionMaxDurationSeconds: envInt("VOICE_GATEWAY_WS_SESSION_MAX_DURATION_SECONDS", 900),
		},
		Audio: AudioConfig{
			VadSpeechThreshold:      envFloat("VOICE_GATEWAY_VAD_SPEECH_THRESHOLD", 0.02),
			VadHangoverFrames:       envInt("VOICE_GATEWAY_VAD_HANGOVER_FRAMES", 15),
			VadMinSpeechFrames:      envInt("VOICE_GATEWAY_VAD_MIN_SPEECH_FRAMES", 3),
			NoiseSuppressionEnabled: envBool("VOICE_GATEWAY_NOISE_SUPPRESSION_ENABLED", true),
			EchoCancellationEnabled: envBool("VOICE_GATEWAY_ECHO_CANCELLATION_ENABLED", true),
			GainCalibrationEnabled:  envBool("VOICE_GATEWAY_GAIN_CALIBRATION_ENABLED", true),
			TargetRmsDbfs:           envFloat("VOICE_GATEWAY_TARGET_RMS_DBFS", -24.0),
			SegmentQueueDepth:       envInt("VOICE_GATEWAY_SEGMENT_QUEUE_DEPTH", 4),
			IdleTimeoutSeconds:      envInt("VOICE_GATEWAY_SESSION_IDLE_TIMEOUT_SECONDS", 60),
			ASRProvider:             strings.ToLower(env("VOICE_GATEWAY_ASR_PROVIDER", "")),
			TTSProvider:             strings.ToLower(env("VOICE_GATEWAY_TTS_PROVIDER", "")),
			RealtimeProvider:        strings.ToLower(env("VOICE_GATEWAY_REALTIME_PROVIDER", "")),
			LLMProvider:             strings.ToLower(env("VOICE_GATEWAY_LLM_PROVIDER", "")),
			LLMModel:                env("VOICE_GATEWAY_LLM_MODEL", ""),
			ASRConfig: ASRProviderConfig{
				Endpoint:    env("VOICE_GATEWAY_ASR_ENDPOINT", ""),
				Region:      env("VOICE_GATEWAY_ASR_REGION", ""),
				APIKey:      env("VOICE_GATEWAY_ASR_API_KEY", ""),
				AppKey:      env("VOICE_GATEWAY_ASR_APP_KEY", ""),
				Token:       env("VOICE_GATEWAY_ASR_TOKEN", ""),
				SecretID:    env("VOICE_GATEWAY_ASR_SECRET_ID", ""),
				SecretKey:   env("VOICE_GATEWAY_ASR_SECRET_KEY", ""),
				AppID:       env("VOICE_GATEWAY_ASR_APP_ID", ""),
				AccessToken: env("VOICE_GATEWAY_ASR_ACCESS_TOKEN", ""),
				Cluster:     env("VOICE_GATEWAY_ASR_CLUSTER", ""),
				Model:       env("VOICE_GATEWAY_ASR_MODEL", ""),
			},
			TTSConfig: TTSProviderConfig{
				Endpoint:    env("VOICE_GATEWAY_TTS_ENDPOINT", ""),
				Region:      env("VOICE_GATEWAY_TTS_REGION", ""),
				APIKey:      env("VOICE_GATEWAY_TTS_API_KEY", ""),
				AppKey:      env("VOICE_GATEWAY_TTS_APP_KEY", ""),
				Token:       env("VOICE_GATEWAY_TTS_TOKEN", ""),
				SecretID:    env("VOICE_GATEWAY_TTS_SECRET_ID", ""),
				SecretKey:   env("VOICE_GATEWAY_TTS_SECRET_KEY", ""),
				AppID:       env("VOICE_GATEWAY_TTS_APP_ID", ""),
				AccessToken: env("VOICE_GATEWAY_TTS_ACCESS_TOKEN", ""),
				Cluster:     env("VOICE_GATEWAY_TTS_CLUSTER", ""),
				Model:       env("VOICE_GATEWAY_TTS_MODEL", ""),
				Voice:       env("VOICE_GATEWAY_TTS_VOICE", ""),
			},
			LLMConfig: LLMProviderConfig{
				BaseURL: env("VOICE_GATEWAY_LLM_BASE_URL", "http://127.0.0.1:8080"),
				APIKey:  env("VOICE_GATEWAY_LLM_API_KEY", ""),
			},
		},
	}

	if cfg.Internal.Enabled && len(strings.TrimSpace(cfg.Internal.AuthToken)) < 32 {
		return Config{}, fmt.Errorf("VOICE_GATEWAY_INTERNAL_API_TOKEN must contain at least 32 characters when the internal API is enabled")
	}
	if cfg.WebSocket.Enabled {
		// The realtime endpoint must never accept unauthenticated device audio,
		// so an enabled endpoint requires a strong signing secret.
		if len(strings.TrimSpace(cfg.WebSocket.AuthTokenSecret)) < 32 {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_WS_TOKEN_SECRET must contain at least 32 characters when the realtime endpoint is enabled")
		}
		if !strings.HasPrefix(cfg.WebSocket.Path, "/") {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_WS_PATH must start with a slash")
		}
		if cfg.WebSocket.MaxFrameBytes < 1 ||
			cfg.WebSocket.MaxTotalSessions < 1 ||
			cfg.WebSocket.MaxSessionsPerDevice < 1 ||
			cfg.WebSocket.MaxFramesPerSecond < 1 {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_WS limits must be positive")
		}
		if cfg.WebSocket.SegmentQueueDepth < 1 ||
			cfg.WebSocket.MaxBytesPerSecond < 1 ||
			cfg.WebSocket.WriteQueueDepth < 1 ||
			cfg.WebSocket.SessionTokenTTLSeconds < 30 ||
			cfg.WebSocket.SessionTokenTTLSeconds > 900 ||
			cfg.WebSocket.PongWaitSeconds < 1 ||
			cfg.WebSocket.PingIntervalSeconds < 1 ||
			cfg.WebSocket.PongWaitSeconds <= cfg.WebSocket.PingIntervalSeconds ||
			cfg.WebSocket.WriteTimeoutSeconds < 1 ||
			cfg.WebSocket.IdleTimeoutSeconds < 1 ||
			cfg.WebSocket.SessionMaxDurationSeconds < cfg.WebSocket.IdleTimeoutSeconds {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_WS timing and queue limits are invalid")
		}
	}
	if cfg.Security.MaxInputRunes < 64 || cfg.Security.MaxInputRunes > 8000 {
		return Config{}, fmt.Errorf("VOICE_GATEWAY_MAX_INPUT_RUNES must be between 64 and 8000")
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
