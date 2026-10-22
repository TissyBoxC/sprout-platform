package config

import (
	"strings"
	"testing"
	"time"
)

func validCareViewConfig() Config {
	return Config{
		Internal: InternalAPIConfig{
			Enabled:   true,
			AuthToken: strings.Repeat("t", 32),
		},
		CareView: CareViewConfig{
			Enabled:             true,
			Path:                "/v1/care-view",
			MaxDurationSeconds:  300,
			MaxFrameBytes:       256 * 1024,
			MaxFramesPerSecond:  8,
			WriteQueueDepth:     64,
			MaxRelays:           256,
			WriteTimeoutSeconds: 10,
			PongWaitSeconds:     60,
			PingIntervalSeconds: 25,
			StateRecheckSeconds: 15,
		},
	}
}

func TestValidateCareViewConfig(t *testing.T) {
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
			name: "disabled relay needs no internal API",
			mutate: func(cfg *Config) {
				cfg.CareView.Enabled = false
				cfg.Internal = InternalAPIConfig{}
			},
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
			mutate:  func(cfg *Config) { cfg.CareView.Path = "v1/care-view" },
			wantErr: true,
		},
		{
			name:    "duration below one second",
			mutate:  func(cfg *Config) { cfg.CareView.MaxDurationSeconds = 0 },
			wantErr: true,
		},
		{
			name:    "duration above the platform limit",
			mutate:  func(cfg *Config) { cfg.CareView.MaxDurationSeconds = 301 },
			wantErr: true,
		},
		{
			name:    "non-positive frame limit",
			mutate:  func(cfg *Config) { cfg.CareView.MaxFrameBytes = 0 },
			wantErr: true,
		},
		{
			name:    "frame above the hard ceiling",
			mutate:  func(cfg *Config) { cfg.CareView.MaxFrameBytes = 8 * 1024 * 1024 },
			wantErr: true,
		},
		{
			name:    "frame rate above the hard ceiling",
			mutate:  func(cfg *Config) { cfg.CareView.MaxFramesPerSecond = 60 },
			wantErr: true,
		},
		{
			name:    "non-positive relay cap",
			mutate:  func(cfg *Config) { cfg.CareView.MaxRelays = 0 },
			wantErr: true,
		},
		{
			name:    "pong wait shorter than ping interval",
			mutate:  func(cfg *Config) { cfg.CareView.PongWaitSeconds = 10; cfg.CareView.PingIntervalSeconds = 25 },
			wantErr: true,
		},
		{
			name:    "state recheck above the ceiling",
			mutate:  func(cfg *Config) { cfg.CareView.StateRecheckSeconds = 120 },
			wantErr: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := validCareViewConfig()
			testCase.mutate(&cfg)
			err := validateCareViewConfig(cfg)
			if testCase.wantErr && err == nil {
				t.Fatal("expected the care view configuration to be rejected")
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
		})
	}
}

// Startup must fail closed when the relay is enabled without the internal API
// or with a token too weak for a service credential.
func TestLoadRejectsCareViewWithoutInternalAPI(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		internal  string
		token     string
		wantError bool
	}{
		{name: "internal API disabled", internal: "", token: strings.Repeat("t", 32), wantError: true},
		{name: "weak token", internal: "true", token: "short", wantError: true},
		{name: "enabled with strong token", internal: "true", token: strings.Repeat("t", 32)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("VOICE_GATEWAY_CARE_VIEW_ENABLED", "true")
			t.Setenv("VOICE_GATEWAY_INTERNAL_API_ENABLED", testCase.internal)
			t.Setenv("VOICE_GATEWAY_INTERNAL_API_TOKEN", testCase.token)

			_, err := Load()
			if testCase.wantError && err == nil {
				t.Fatal("expected startup to be refused")
			}
			if !testCase.wantError && err != nil {
				t.Fatalf("unexpected startup failure: %v", err)
			}
		})
	}
}

func TestCareViewConfigMaxViewDuration(t *testing.T) {
	cfg := CareViewConfig{MaxDurationSeconds: 300}
	if got := cfg.MaxViewDuration(); got != 5*time.Minute {
		t.Fatalf("MaxViewDuration() = %v, want 5m", got)
	}
}

// The relay must stay off unless an operator opts in, and the defaults must be
// the documented 256 KiB / 8 fps.
func TestCareViewDefaults(t *testing.T) {
	for _, key := range []string{
		"VOICE_GATEWAY_CARE_VIEW_ENABLED",
		"VOICE_GATEWAY_CARE_VIEW_PATH",
		"VOICE_GATEWAY_CARE_VIEW_MAX_DURATION_SECONDS",
		"VOICE_GATEWAY_CARE_VIEW_MAX_FRAME_BYTES",
		"VOICE_GATEWAY_CARE_VIEW_MAX_FRAMES_PER_SECOND",
	} {
		t.Setenv(key, "")
	}
	cfg := loadCareViewConfig()
	if cfg.Enabled {
		t.Fatal("care view relay must be disabled by default")
	}
	if cfg.Path != "/v1/care-view" {
		t.Fatalf("default path = %q, want /v1/care-view", cfg.Path)
	}
	if cfg.MaxDurationSeconds != 300 {
		t.Fatalf("default max duration = %d, want 300", cfg.MaxDurationSeconds)
	}
	if cfg.MaxFrameBytes != 256*1024 {
		t.Fatalf("default max frame bytes = %d, want %d", cfg.MaxFrameBytes, 256*1024)
	}
	if cfg.MaxFramesPerSecond != 8 {
		t.Fatalf("default max frames per second = %d, want 8", cfg.MaxFramesPerSecond)
	}
}
