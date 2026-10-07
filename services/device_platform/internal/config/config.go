// Package config loads device platform configuration.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains runtime settings for the device platform service.
type Config struct {
	HTTP           HTTPConfig
	Internal       InternalAPIConfig
	Auth           AuthConfig
	AI             AIConfig
	OTA            OTAConfig
	Log            LogConfig
	Database       DatabaseConfig
	Redis          RedisConfig
	MQTT           MQTTConfig
	DeviceRuntime  DeviceRuntimeConfig
	ServiceVersion ServiceVersionConfig
	ReleaseStore   ReleaseStoreConfig
	VoiceGateway   VoiceGatewayConfig
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
	Enabled            bool
	AuthToken          string
	ReleaseUploadToken string
}

// AuthConfig contains parent authentication settings.
type AuthConfig struct {
	AccessTokenSecret string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	CredentialKey     string
	CredentialKeyID   int
	MFACredentialKey  string
	MFAChallengeTTL   time.Duration
	// PhoneVerificationMode is disabled by default. Local mode is an explicit
	// development bypass; production must configure a real provider.
	PhoneVerificationMode string
}

// AIConfig contains the internal sub2api account provisioning settings.
type AIConfig struct {
	BaseURL            string
	ServiceToken       string
	DefaultBalanceUSD  float64
	DefaultConcurrency int
	DefaultModels      []string
}

// OTAConfig contains the public object-storage prefixes used when publishing
// resource and client updates. Credentials never belong in these values.
type OTAConfig struct {
	ManifestBaseURL string
	ResourceBaseURL string
	ClientBaseURL   string
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

// DatabaseConfig contains PostgreSQL settings.
type DatabaseConfig struct {
	DSN string
}

// RedisConfig contains Redis settings.
type RedisConfig struct {
	Address  string
	Password string
	DB       int
}

// MQTTConfig contains MQTT broker settings.
type MQTTConfig struct {
	Broker                string
	ClientID              string
	Username              string
	Password              string
	CAFile                string
	ClientCertificateFile string
	ClientKeyFile         string
	InsecureSkipVerify    bool
}

// DeviceRuntimeConfig contains heartbeat freshness and command policy.
type DeviceRuntimeConfig struct {
	OfflineThreshold time.Duration
	CommandTTL       time.Duration
}

// ServiceVersionConfig points at the shared state directory owned by the
// upgrade worker. The device platform only reads status and writes command
// files there; it never receives the Docker socket.
type ServiceVersionConfig struct {
	StateDir string
}

// ReleaseStoreConfig points at the shared release volume consumed by the
// download service. PublicBaseURL is the same non-secret HTTPS prefix used by
// release manifests; credentials never belong here.
type ReleaseStoreConfig struct {
	RootDir       string
	PublicBaseURL string
}

// VoiceGatewayConfig contains the public realtime voice endpoint and the
// shared signing settings used to issue short-lived device tokens.
type VoiceGatewayConfig struct {
	WSTokenSecret string
	WSTokenTTL    time.Duration
	WebSocketURL  string
}

// Load reads configuration from environment variables with local defaults.
func Load() (Config, error) {
	cfg := Config{
		HTTP: HTTPConfig{
			Host: env("DEVICE_PLATFORM_HTTP_HOST", "0.0.0.0"),
			Port: env("DEVICE_PLATFORM_HTTP_PORT", "8081"),
		},
		Internal: InternalAPIConfig{
			Enabled:   envBool("DEVICE_PLATFORM_INTERNAL_API_ENABLED", false),
			AuthToken: env("DEVICE_PLATFORM_INTERNAL_API_TOKEN", ""),
			ReleaseUploadToken: env(
				"DEVICE_PLATFORM_RELEASE_UPLOAD_TOKEN",
				"",
			),
		},
		Auth: AuthConfig{
			AccessTokenSecret: env("DEVICE_PLATFORM_AUTH_ACCESS_TOKEN_SECRET", ""),
			AccessTokenTTL:    envDuration("DEVICE_PLATFORM_AUTH_ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTokenTTL:   envDuration("DEVICE_PLATFORM_AUTH_REFRESH_TOKEN_TTL", 30*24*time.Hour),
			CredentialKey:     env("DEVICE_PLATFORM_AI_CREDENTIAL_KEY", ""),
			CredentialKeyID:   envInt("DEVICE_PLATFORM_AI_CREDENTIAL_KEY_ID", 1),
			MFACredentialKey:  env("DEVICE_PLATFORM_MFA_CREDENTIAL_KEY", ""),
			MFAChallengeTTL:   envDuration("DEVICE_PLATFORM_MFA_CHALLENGE_TTL", 5*time.Minute),
			PhoneVerificationMode: env(
				"DEVICE_PLATFORM_PHONE_VERIFICATION_MODE",
				"disabled",
			),
		},
		AI: AIConfig{
			BaseURL:            env("DEVICE_PLATFORM_SUB2API_BASE_URL", "http://127.0.0.1:8080"),
			ServiceToken:       env("DEVICE_PLATFORM_SUB2API_SERVICE_TOKEN", ""),
			DefaultBalanceUSD:  envFloat("DEVICE_PLATFORM_AI_DEFAULT_BALANCE_USD", 0),
			DefaultConcurrency: envInt("DEVICE_PLATFORM_AI_DEFAULT_CONCURRENCY", 1),
			DefaultModels:      envList("DEVICE_PLATFORM_AI_DEFAULT_MODELS", []string{}),
		},
		OTA: OTAConfig{
			ManifestBaseURL: env("DEVICE_PLATFORM_OTA_MANIFEST_BASE_URL", ""),
			ResourceBaseURL: env("DEVICE_PLATFORM_OTA_RESOURCE_BASE_URL", ""),
			ClientBaseURL:   env("DEVICE_PLATFORM_OTA_CLIENT_BASE_URL", ""),
		},
		Log: LogConfig{
			Level: env("DEVICE_PLATFORM_LOG_LEVEL", "info"),
		},
		Database: DatabaseConfig{
			DSN: env("DEVICE_PLATFORM_DATABASE_DSN", ""),
		},
		Redis: RedisConfig{
			Address:  env("DEVICE_PLATFORM_REDIS_ADDRESS", "127.0.0.1:6379"),
			Password: env("DEVICE_PLATFORM_REDIS_PASSWORD", ""),
			DB:       envInt("DEVICE_PLATFORM_REDIS_DB", 0),
		},
		MQTT: MQTTConfig{
			Broker:                env("DEVICE_PLATFORM_MQTT_BROKER", "tls://127.0.0.1:8883"),
			ClientID:              env("DEVICE_PLATFORM_MQTT_CLIENT_ID", "device-platform"),
			Username:              env("DEVICE_PLATFORM_MQTT_USERNAME", ""),
			Password:              env("DEVICE_PLATFORM_MQTT_PASSWORD", ""),
			CAFile:                env("DEVICE_PLATFORM_MQTT_CA_FILE", ""),
			ClientCertificateFile: env("DEVICE_PLATFORM_MQTT_CLIENT_CERTIFICATE_FILE", ""),
			ClientKeyFile:         env("DEVICE_PLATFORM_MQTT_CLIENT_KEY_FILE", ""),
			InsecureSkipVerify:    envBool("DEVICE_PLATFORM_MQTT_INSECURE_SKIP_VERIFY", false),
		},
		DeviceRuntime: DeviceRuntimeConfig{
			OfflineThreshold: envDuration(
				"DEVICE_PLATFORM_RUNTIME_OFFLINE_THRESHOLD",
				90*time.Second,
			),
			CommandTTL: envDuration(
				"DEVICE_PLATFORM_RUNTIME_COMMAND_TTL",
				15*time.Minute,
			),
		},
		ServiceVersion: ServiceVersionConfig{
			StateDir: env(
				"DEVICE_PLATFORM_SERVICE_VERSION_STATE_DIR",
				"/var/lib/sprout-upgrades",
			),
		},
		ReleaseStore: ReleaseStoreConfig{
			RootDir: env(
				"DEVICE_PLATFORM_DOWNLOAD_STORE_DIR",
				"/srv/releases",
			),
			PublicBaseURL: env(
				"DEVICE_PLATFORM_DOWNLOAD_PUBLIC_BASE_URL",
				"",
			),
		},
		VoiceGateway: VoiceGatewayConfig{
			WSTokenSecret: env(
				"DEVICE_PLATFORM_VOICE_GATEWAY_WS_TOKEN_SECRET",
				"",
			),
			WSTokenTTL: time.Duration(
				envInt("DEVICE_PLATFORM_VOICE_TOKEN_TTL_SECONDS", 300),
			) * time.Second,
			WebSocketURL: env(
				"DEVICE_PLATFORM_VOICE_GATEWAY_WS_URL",
				"wss://voice.clarkhub.cn/v1/voice",
			),
		},
	}

	if cfg.Internal.Enabled && len(strings.TrimSpace(cfg.Internal.AuthToken)) < 32 {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_INTERNAL_API_TOKEN must contain at least 32 characters when the internal API is enabled")
	}
	if token := strings.TrimSpace(cfg.Internal.ReleaseUploadToken); token != "" && len(token) < 32 {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_RELEASE_UPLOAD_TOKEN must contain at least 32 characters")
	}
	if cfg.MQTT.InsecureSkipVerify {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_MQTT_INSECURE_SKIP_VERIFY must remain false")
	}
	if len(strings.TrimSpace(cfg.Auth.AccessTokenSecret)) < 32 {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_AUTH_ACCESS_TOKEN_SECRET must contain at least 32 characters")
	}
	if len(strings.TrimSpace(cfg.Auth.CredentialKey)) < 32 {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_AI_CREDENTIAL_KEY must contain at least 32 characters")
	}
	if len(strings.TrimSpace(cfg.Auth.MFACredentialKey)) < 32 {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_MFA_CREDENTIAL_KEY must contain at least 32 characters")
	}
	if cfg.Auth.MFAChallengeTTL <= 0 {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_MFA_CHALLENGE_TTL must be positive")
	}
	switch cfg.Auth.PhoneVerificationMode {
	case "disabled", "local":
	default:
		return Config{}, fmt.Errorf(
			"DEVICE_PLATFORM_PHONE_VERIFICATION_MODE must be disabled or local",
		)
	}
	if cfg.Auth.PhoneVerificationMode == "local" &&
		strings.TrimSpace(os.Getenv("DEVICE_PLATFORM_ALLOW_LOCAL_SMS_BYPASS")) != "true" {
		return Config{}, fmt.Errorf(
			"DEVICE_PLATFORM_PHONE_VERIFICATION_MODE=local requires DEVICE_PLATFORM_ALLOW_LOCAL_SMS_BYPASS=true",
		)
	}
	if cfg.AI.DefaultConcurrency < 1 {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_AI_DEFAULT_CONCURRENCY must be greater than zero")
	}
	if cfg.DeviceRuntime.OfflineThreshold <= 0 {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_RUNTIME_OFFLINE_THRESHOLD must be positive")
	}
	if cfg.DeviceRuntime.CommandTTL <= 0 {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_RUNTIME_COMMAND_TTL must be positive")
	}
	if !strings.HasPrefix(strings.TrimSpace(cfg.ServiceVersion.StateDir), "/") {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_SERVICE_VERSION_STATE_DIR must be an absolute path")
	}
	if !strings.HasPrefix(strings.TrimSpace(cfg.ReleaseStore.RootDir), "/") {
		return Config{}, fmt.Errorf("DEVICE_PLATFORM_DOWNLOAD_STORE_DIR must be an absolute path")
	}
	if secret := strings.TrimSpace(cfg.VoiceGateway.WSTokenSecret); secret != "" &&
		len(secret) < 32 {
		return Config{}, fmt.Errorf(
			"DEVICE_PLATFORM_VOICE_GATEWAY_WS_TOKEN_SECRET must contain at least 32 characters when set",
		)
	}
	if cfg.VoiceGateway.WSTokenTTL <= 0 {
		return Config{}, fmt.Errorf(
			"DEVICE_PLATFORM_VOICE_TOKEN_TTL_SECONDS must be positive",
		)
	}
	voiceWebSocketURL, err := url.ParseRequestURI(
		strings.TrimSpace(cfg.VoiceGateway.WebSocketURL),
	)
	if err != nil || voiceWebSocketURL.Scheme != "wss" ||
		voiceWebSocketURL.Host == "" || voiceWebSocketURL.User != nil ||
		voiceWebSocketURL.Fragment != "" {
		return Config{}, fmt.Errorf(
			"DEVICE_PLATFORM_VOICE_GATEWAY_WS_URL must be a public wss URL without credentials or fragments",
		)
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
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

func envFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envList(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}
