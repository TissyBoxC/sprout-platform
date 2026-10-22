package config

import (
	"strings"
	"testing"
	"time"
)

func validIntercomConfig() Config {
	return Config{
		Internal: InternalAPIConfig{
			Enabled:   true,
			AuthToken: strings.Repeat("t", 32),
		},
		Intercom: IntercomConfig{
			Enabled:                true,
			Path:                   "/v1/intercom",
			MaxCallDurationSeconds: 600,
			MaxFrameBytes:          4096,
			MaxFramesPerSecond:     80,
			MaxBytesPerSecond:      128 * 1024,
			WriteQueueDepth:        64,
			MaxRelays:              256,
			WriteTimeoutSeconds:    10,
			PongWaitSeconds:        60,
			PingIntervalSeconds:    25,
		},
	}
}

func TestValidateIntercomConfig(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{
			name:   "valid relay",
			mutate: func(*Config) {},
		},
		{
			name:   "disabled relay needs no internal API",
			mutate: func(cfg *Config) { cfg.Intercom.Enabled = false; cfg.Internal = InternalAPIConfig{} },
		},
		{
			name:    "enabled relay without internal API",
			mutate:  func(cfg *Config) { cfg.Internal.Enabled = false },
			wantErr: true,
		},
		{
			name:    "enabled relay with weak token",
			mutate:  func(cfg *Config) { cfg.Internal.AuthToken = "short" },
			wantErr: true,
		},
		{
			name:    "path without leading slash",
			mutate:  func(cfg *Config) { cfg.Intercom.Path = "v1/intercom" },
			wantErr: true,
		},
		{
			name:    "duration below one second",
			mutate:  func(cfg *Config) { cfg.Intercom.MaxCallDurationSeconds = 0 },
			wantErr: true,
		},
		{
			name:    "duration above the platform limit",
			mutate:  func(cfg *Config) { cfg.Intercom.MaxCallDurationSeconds = 601 },
			wantErr: true,
		},
		{
			name:    "non-positive frame limit",
			mutate:  func(cfg *Config) { cfg.Intercom.MaxFrameBytes = 0 },
			wantErr: true,
		},
		{
			name:    "non-positive relay cap",
			mutate:  func(cfg *Config) { cfg.Intercom.MaxRelays = 0 },
			wantErr: true,
		},
		{
			name:    "pong wait shorter than ping interval",
			mutate:  func(cfg *Config) { cfg.Intercom.PongWaitSeconds = 10; cfg.Intercom.PingIntervalSeconds = 25 },
			wantErr: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := validIntercomConfig()
			testCase.mutate(&cfg)
			err := validateIntercomConfig(cfg)
			if testCase.wantErr && err == nil {
				t.Fatal("expected the intercom configuration to be rejected")
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
		})
	}
}

func TestIntercomConfigMaxCallDuration(t *testing.T) {
	cfg := IntercomConfig{MaxCallDurationSeconds: 600}
	if got := cfg.MaxCallDuration(); got != 10*time.Minute {
		t.Fatalf("MaxCallDuration() = %v, want 10m", got)
	}
}

// The relay must stay off unless an operator opts in.
func TestIntercomDisabledByDefault(t *testing.T) {
	for _, key := range []string{
		"VOICE_GATEWAY_INTERCOM_ENABLED",
		"VOICE_GATEWAY_INTERCOM_PATH",
		"VOICE_GATEWAY_INTERCOM_MAX_CALL_DURATION_SECONDS",
	} {
		t.Setenv(key, "")
	}
	cfg := loadIntercomConfig()
	if cfg.Enabled {
		t.Fatal("intercom relay must be disabled by default")
	}
	if cfg.Path != "/v1/intercom" {
		t.Fatalf("default path = %q, want /v1/intercom", cfg.Path)
	}
	if cfg.MaxCallDurationSeconds != 600 {
		t.Fatalf("default max call duration = %d, want 600", cfg.MaxCallDurationSeconds)
	}
}
