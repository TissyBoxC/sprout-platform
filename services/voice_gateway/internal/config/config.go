// Package config loads voice gateway configuration.
package config

import (
	"fmt"
	"log/slog"
	"math"
	"net/url"
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
	AI        AIConfig
	Vision    VisionConfig
	Security  SecurityConfig
	WebSocket WebSocketConfig
	Intercom  IntercomConfig
	CareView  CareViewConfig
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
	// ContinuityWindowSeconds keeps a finished conversation recoverable for
	// follow-up turns without requiring a new wake word.
	ContinuityWindowSeconds int
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
	// AllowedOrigins is the exact browser Origin allowlist. Native clients
	// without Origin remain supported; a browser origin is rejected unless
	// listed here.
	AllowedOrigins []string
	// ReadTimeoutSeconds and WriteTimeoutSeconds drive ping/pong deadlines.
	ReadTimeoutSeconds int
	// PongWaitSeconds bounds how long a connection may stay silent.
	PongWaitSeconds int
	// PingIntervalSeconds is how often the gateway sends a keepalive ping.
	PingIntervalSeconds int
	// AuthRecheckSeconds is how often an established connection re-checks the
	// guardian's live consent so a withdrawal during a long session ends it.
	AuthRecheckSeconds int
	// WriteTimeoutSeconds bounds one outbound websocket write.
	WriteTimeoutSeconds int
	// IdleTimeoutSeconds ends a silent session on the gateway side.
	IdleTimeoutSeconds int
	// SessionMaxDurationSeconds bounds one continuous conversation.
	SessionMaxDurationSeconds int
}

// AudioConfig selects the audio preprocessing and provider adapters.
type AudioConfig struct {
	// VadSpeechThreshold is the ratio of frame energy to the adaptive noise
	// floor. The detector requires a value greater than 1.
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
	// ASRProvider, TTSProvider, and LLMProvider select the pipeline adapters.
	// Empty values leave that stage unconfigured instead of failing startup, so
	// health and internal endpoints keep working.
	ASRProvider string
	TTSProvider string
	LLMProvider string
	// LLMModel is the default conversation model; empty uses provider default.
	LLMModel string
	// ASRConfig and TTSConfig carry provider-specific credentials. LLMConfig
	// carries retry policy for the platform execution adapter.
	ASRConfig ASRProviderConfig
	TTSConfig TTSProviderConfig
	LLMConfig LLMProviderConfig
	// DevicePlatformBaseURL and DevicePlatformServiceToken point model calls at
	// the platform-owned execution proxy. pipeline.Build passes them to the
	// execution client alongside the retry policy.
	DevicePlatformBaseURL      string
	DevicePlatformServiceToken string
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

// LLMProviderConfig carries the conversation gateway settings.
type LLMProviderConfig struct {
	MaxAttempts int
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

// AIConfig contains the platform-owned AI execution connection.
//
// The voice gateway never receives a provider key. It presents a short-lived
// execution ticket to device_platform, which alone decrypts the guardian's
// credential and calls the upstream provider.
type AIConfig struct {
	DevicePlatformBaseURL      string
	DevicePlatformServiceToken string
}

// VisionConfig turns the internal camera/vision pipeline on or off.
//
// Vision uses the same platform execution ticket path as voice conversation.
// It stays disabled by default so a deployment without camera hardware
// exposes no vision endpoint.
type VisionConfig struct {
	Enabled bool
	// Model is the vision model identifier; empty keeps the endpoint off even
	// when Enabled is true, because a model call without a model is useless.
	Model string
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
		AI: AIConfig{
			DevicePlatformBaseURL: env(
				"VOICE_GATEWAY_DEVICE_PLATFORM_BASE_URL",
				"http://127.0.0.1:8081",
			),
			DevicePlatformServiceToken: env(
				"VOICE_GATEWAY_DEVICE_PLATFORM_SERVICE_TOKEN",
				"",
			),
		},
		Vision: VisionConfig{
			Enabled: envBool("VOICE_GATEWAY_VISION_ENABLED", false),
			Model:   env("VOICE_GATEWAY_VISION_MODEL", ""),
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
			ContinuityWindowSeconds:   envInt("VOICE_GATEWAY_WS_CONTINUITY_WINDOW_SECONDS", 60),
			MaxSessionsPerDevice:      envInt("VOICE_GATEWAY_WS_MAX_SESSIONS_PER_DEVICE", 1),
			MaxTotalSessions:          envInt("VOICE_GATEWAY_WS_MAX_TOTAL_SESSIONS", 512),
			MaxFrameBytes:             envInt("VOICE_GATEWAY_WS_MAX_FRAME_BYTES", 1024),
			MaxFramesPerSecond:        envInt("VOICE_GATEWAY_WS_MAX_FRAMES_PER_SECOND", 80),
			WriteQueueDepth:           envInt("VOICE_GATEWAY_WS_WRITE_QUEUE_DEPTH", 64),
			SegmentQueueDepth:         envInt("VOICE_GATEWAY_WS_SEGMENT_QUEUE_DEPTH", 4),
			MaxBytesPerSecond:         envInt("VOICE_GATEWAY_WS_MAX_BYTES_PER_SECOND", 128*1024),
			AllowedOrigins:            envList("VOICE_GATEWAY_WS_ALLOWED_ORIGINS"),
			ReadTimeoutSeconds:        envInt("VOICE_GATEWAY_WS_READ_TIMEOUT_SECONDS", 30),
			PongWaitSeconds:           envInt("VOICE_GATEWAY_WS_PONG_WAIT_SECONDS", 60),
			PingIntervalSeconds:       envInt("VOICE_GATEWAY_WS_PING_INTERVAL_SECONDS", 25),
			AuthRecheckSeconds:        envInt("VOICE_GATEWAY_WS_AUTH_RECHECK_SECONDS", 30),
			WriteTimeoutSeconds:       envInt("VOICE_GATEWAY_WS_WRITE_TIMEOUT_SECONDS", 10),
			IdleTimeoutSeconds:        envInt("VOICE_GATEWAY_WS_IDLE_TIMEOUT_SECONDS", 60),
			SessionMaxDurationSeconds: envInt("VOICE_GATEWAY_WS_SESSION_MAX_DURATION_SECONDS", 900),
		},
		Intercom: loadIntercomConfig(),
		CareView: loadCareViewConfig(),
		Audio: AudioConfig{
			VadSpeechThreshold:      envFloatRequired("VOICE_GATEWAY_VAD_SPEECH_THRESHOLD", 2.8),
			VadHangoverFrames:       envInt("VOICE_GATEWAY_VAD_HANGOVER_FRAMES", 15),
			VadMinSpeechFrames:      envInt("VOICE_GATEWAY_VAD_MIN_SPEECH_FRAMES", 3),
			NoiseSuppressionEnabled: envBool("VOICE_GATEWAY_NOISE_SUPPRESSION_ENABLED", true),
			EchoCancellationEnabled: envBool("VOICE_GATEWAY_ECHO_CANCELLATION_ENABLED", true),
			GainCalibrationEnabled:  envBool("VOICE_GATEWAY_GAIN_CALIBRATION_ENABLED", true),
			TargetRmsDbfs:           envFloatRequired("VOICE_GATEWAY_TARGET_RMS_DBFS", -24.0),
			SegmentQueueDepth:       envInt("VOICE_GATEWAY_SEGMENT_QUEUE_DEPTH", 4),
			IdleTimeoutSeconds:      envInt("VOICE_GATEWAY_SESSION_IDLE_TIMEOUT_SECONDS", 60),
			ASRProvider:             strings.ToLower(env("VOICE_GATEWAY_ASR_PROVIDER", "")),
			TTSProvider:             strings.ToLower(env("VOICE_GATEWAY_TTS_PROVIDER", "")),
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
				MaxAttempts: envInt("VOICE_GATEWAY_LLM_MAX_ATTEMPTS", 3),
			},
			DevicePlatformBaseURL: env(
				"VOICE_GATEWAY_DEVICE_PLATFORM_BASE_URL",
				"http://127.0.0.1:8081",
			),
			DevicePlatformServiceToken: env(
				"VOICE_GATEWAY_DEVICE_PLATFORM_SERVICE_TOKEN",
				"",
			),
		},
	}

	if cfg.Internal.Enabled && len(strings.TrimSpace(cfg.Internal.AuthToken)) < 32 {
		return Config{}, fmt.Errorf("VOICE_GATEWAY_INTERNAL_API_TOKEN must contain at least 32 characters when the internal API is enabled")
	}
	// The OpenAI Realtime adapter was imported during early provider
	// scaffolding but was never connected to the device transport. Reject the
	// retired selector so a deployment cannot appear to enable a realtime
	// provider while every turn still uses the ASR/LLM/TTS pipeline.
	if provider := strings.TrimSpace(env("VOICE_GATEWAY_REALTIME_PROVIDER", "")); provider != "" {
		return Config{}, fmt.Errorf("VOICE_GATEWAY_REALTIME_PROVIDER is not supported; configure ASR, LLM, and TTS providers instead")
	}
	if cfg.WebSocket.Enabled {
		// The realtime endpoint must never accept unauthenticated device audio,
		// so an enabled endpoint requires a strong signing secret.
		if len(strings.TrimSpace(cfg.WebSocket.AuthTokenSecret)) < 32 {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_WS_TOKEN_SECRET must contain at least 32 characters when the realtime endpoint is enabled")
		}
		// Realtime conversation cannot serve a single usable turn without the
		// whole ASR, LLM, and TTS pipeline. Reject missing providers at startup
		// instead of exposing an endpoint that fails on the first audio segment.
		if strings.TrimSpace(cfg.Audio.ASRProvider) == "" {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_ASR_PROVIDER must be set when the realtime endpoint is enabled")
		}
		if strings.TrimSpace(cfg.Audio.TTSProvider) == "" {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_TTS_PROVIDER must be set when the realtime endpoint is enabled")
		}
		if strings.TrimSpace(cfg.Audio.LLMProvider) == "" {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_LLM_PROVIDER must be set when the realtime endpoint is enabled")
		}
		// The realtime endpoint re-checks the guardian's consent against the
		// platform database. Without the database the authorizer cannot be
		// constructed, so enabling the endpoint would silently fail open on
		// consent withdrawal. Refuse that combination at configuration time.
		if strings.TrimSpace(cfg.Database.DSN) == "" {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_DATABASE_DSN must be set when the realtime endpoint is enabled")
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
			cfg.WebSocket.ContinuityWindowSeconds < 5 ||
			cfg.WebSocket.ContinuityWindowSeconds > 600 ||
			cfg.WebSocket.PongWaitSeconds < 1 ||
			cfg.WebSocket.PingIntervalSeconds < 1 ||
			cfg.WebSocket.PongWaitSeconds <= cfg.WebSocket.PingIntervalSeconds ||
			cfg.WebSocket.AuthRecheckSeconds < 5 ||
			cfg.WebSocket.AuthRecheckSeconds > 600 ||
			cfg.WebSocket.WriteTimeoutSeconds < 1 ||
			cfg.WebSocket.IdleTimeoutSeconds < 1 ||
			cfg.WebSocket.SessionMaxDurationSeconds < cfg.WebSocket.IdleTimeoutSeconds {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_WS timing and queue limits are invalid")
		}
	}
	if err := validateIntercomConfig(cfg); err != nil {
		return Config{}, err
	}
	if err := validateCareViewConfig(cfg); err != nil {
		return Config{}, err
	}
	if cfg.Security.MaxInputRunes < 64 || cfg.Security.MaxInputRunes > 8000 {
		return Config{}, fmt.Errorf("VOICE_GATEWAY_MAX_INPUT_RUNES must be between 64 and 8000")
	}
	if cfg.Audio.LLMConfig.MaxAttempts < 1 || cfg.Audio.LLMConfig.MaxAttempts > 5 {
		return Config{}, fmt.Errorf("VOICE_GATEWAY_LLM_MAX_ATTEMPTS must be between 1 and 5")
	}
	// Any path that can invoke a model must have a complete, authenticated
	// platform execution connection. Startup fails closed instead of exposing
	// a voice or vision endpoint whose first turn would fail at the network
	// boundary.
	requiresExecution := cfg.Vision.Enabled ||
		(cfg.WebSocket.Enabled && strings.TrimSpace(cfg.Audio.LLMProvider) != "")
	if requiresExecution {
		devicePlatformURL := strings.TrimSpace(cfg.AI.DevicePlatformBaseURL)
		if devicePlatformURL == "" {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_DEVICE_PLATFORM_BASE_URL must be set when a model-backed path is enabled")
		}
		parsedURL, err := url.Parse(devicePlatformURL)
		if err != nil ||
			(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") ||
			parsedURL.Host == "" ||
			parsedURL.User != nil ||
			parsedURL.RawQuery != "" ||
			parsedURL.Fragment != "" {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_DEVICE_PLATFORM_BASE_URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
		}
		if len(strings.TrimSpace(cfg.AI.DevicePlatformServiceToken)) < 32 {
			return Config{}, fmt.Errorf("VOICE_GATEWAY_DEVICE_PLATFORM_SERVICE_TOKEN must contain at least 32 characters when a model-backed path is enabled")
		}
	}
	if math.IsNaN(cfg.Audio.VadSpeechThreshold) ||
		math.IsInf(cfg.Audio.VadSpeechThreshold, 0) ||
		cfg.Audio.VadSpeechThreshold < 1.01 ||
		cfg.Audio.VadSpeechThreshold > 100 {
		return Config{}, fmt.Errorf("VOICE_GATEWAY_VAD_SPEECH_THRESHOLD must be between 1.01 and 100")
	}
	if math.IsNaN(cfg.Audio.TargetRmsDbfs) ||
		math.IsInf(cfg.Audio.TargetRmsDbfs, 0) ||
		cfg.Audio.TargetRmsDbfs < -60 ||
		cfg.Audio.TargetRmsDbfs > -3 {
		return Config{}, fmt.Errorf("VOICE_GATEWAY_TARGET_RMS_DBFS must be between -60 and -3")
	}
	if cfg.Vision.Enabled && strings.TrimSpace(cfg.Vision.Model) == "" {
		return Config{}, fmt.Errorf("VOICE_GATEWAY_VISION_MODEL must be set when vision is enabled")
	}
	if cfg.Vision.Enabled && strings.TrimSpace(cfg.Database.DSN) == "" {
		// Vision authorizes each task against the guardian's live consent, so
		// exposing the endpoints without the platform database would trust a
		// caller-supplied device identifier.
		return Config{}, fmt.Errorf("VOICE_GATEWAY_DATABASE_DSN must be set when vision is enabled")
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
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return fallback
	}
	return parsed
}

func envFloatRequired(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		// Returning NaN makes the range check in Load fail closed instead of
		// silently replacing an operator's invalid float with a default.
		return math.NaN()
	}
	return parsed
}

func envList(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	unique := make(map[string]struct{})
	items := make([]string, 0)
	for _, raw := range strings.Split(value, ",") {
		item := strings.TrimSuffix(strings.TrimSpace(raw), "/")
		if item == "" {
			continue
		}
		if _, exists := unique[item]; exists {
			continue
		}
		unique[item] = struct{}{}
		items = append(items, item)
	}
	return items
}
