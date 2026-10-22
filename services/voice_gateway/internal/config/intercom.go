package config

import (
	"fmt"
	"strings"
	"time"
)

// IntercomConfig bounds the guardian-to-device intercom relay.
//
// The relay is a separate transport from the device conversation endpoint: a
// conversation is started by the device, while an intercom call is started by
// a guardian and may only carry audio after the child accepts on the device.
// It therefore has its own path and its own limits instead of reusing the
// conversation WebSocket settings.
type IntercomConfig struct {
	// Enabled keeps the relay endpoint off until the device_platform service
	// token is configured.
	Enabled bool
	// Path is the guardian relay upgrade route.
	Path string
	// MaxCallDurationSeconds bounds one relay. The platform domain model caps a
	// call at ten minutes; the relay re-applies the cap independently so a
	// lost close message cannot leave a channel open.
	MaxCallDurationSeconds int
	// MaxFrameBytes bounds one inbound audio frame.
	MaxFrameBytes int
	// MaxFramesPerSecond throttles inbound audio to reject flooding.
	MaxFramesPerSecond int
	// MaxBytesPerSecond bounds inbound audio bytes per relay.
	MaxBytesPerSecond int
	// WriteQueueDepth bounds the guardian outbound queue so a slow guardian
	// client cannot grow gateway memory.
	WriteQueueDepth int
	// MaxRelays caps concurrent relays across the gateway.
	MaxRelays int
	// WriteTimeoutSeconds bounds one outbound websocket write.
	WriteTimeoutSeconds int
	// PongWaitSeconds bounds how long a idle guardian connection may stay silent.
	PongWaitSeconds int
	// PingIntervalSeconds is how often the gateway sends a keepalive ping.
	PingIntervalSeconds int
}

// MaxCallDuration returns the configured relay cap as a duration.
func (c IntercomConfig) MaxCallDuration() time.Duration {
	return time.Duration(c.MaxCallDurationSeconds) * time.Second
}

// loadIntercomConfig reads the intercom relay settings from the environment.
func loadIntercomConfig() IntercomConfig {
	return IntercomConfig{
		Enabled:                envBool("VOICE_GATEWAY_INTERCOM_ENABLED", false),
		Path:                   env("VOICE_GATEWAY_INTERCOM_PATH", "/v1/intercom"),
		MaxCallDurationSeconds: envInt("VOICE_GATEWAY_INTERCOM_MAX_CALL_DURATION_SECONDS", 600),
		MaxFrameBytes:          envInt("VOICE_GATEWAY_INTERCOM_MAX_FRAME_BYTES", 4096),
		MaxFramesPerSecond:     envInt("VOICE_GATEWAY_INTERCOM_MAX_FRAMES_PER_SECOND", 80),
		MaxBytesPerSecond:      envInt("VOICE_GATEWAY_INTERCOM_MAX_BYTES_PER_SECOND", 128*1024),
		WriteQueueDepth:        envInt("VOICE_GATEWAY_INTERCOM_WRITE_QUEUE_DEPTH", 64),
		MaxRelays:              envInt("VOICE_GATEWAY_INTERCOM_MAX_RELAYS", 256),
		WriteTimeoutSeconds:    envInt("VOICE_GATEWAY_INTERCOM_WRITE_TIMEOUT_SECONDS", 10),
		PongWaitSeconds:        envInt("VOICE_GATEWAY_INTERCOM_PONG_WAIT_SECONDS", 60),
		PingIntervalSeconds:    envInt("VOICE_GATEWAY_INTERCOM_PING_INTERVAL_SECONDS", 25),
	}
}

// validateIntercomConfig rejects an intercom relay that could run unbounded.
//
// The relay endpoint is also an internal API route, so an enabled relay
// without a service token would let any caller authorize a call.
func validateIntercomConfig(cfg Config) error {
	intercom := cfg.Intercom
	if !intercom.Enabled {
		return nil
	}
	if !cfg.Internal.Enabled || len(strings.TrimSpace(cfg.Internal.AuthToken)) < 32 {
		return fmt.Errorf("the internal API must be enabled with a strong token when the intercom relay is enabled")
	}
	if !strings.HasPrefix(intercom.Path, "/") {
		return fmt.Errorf("VOICE_GATEWAY_INTERCOM_PATH must start with a slash")
	}
	// The platform domain model already refuses a call longer than ten
	// minutes, so a longer relay cap would never be reached and would only
	// hide a misconfiguration.
	if intercom.MaxCallDurationSeconds < 1 ||
		intercom.MaxCallDurationSeconds > 600 {
		return fmt.Errorf("VOICE_GATEWAY_INTERCOM_MAX_CALL_DURATION_SECONDS must be between 1 and 600")
	}
	if intercom.MaxFrameBytes < 1 ||
		intercom.MaxFramesPerSecond < 1 ||
		intercom.MaxBytesPerSecond < 1 ||
		intercom.WriteQueueDepth < 1 ||
		intercom.MaxRelays < 1 {
		return fmt.Errorf("VOICE_GATEWAY_INTERCOM limits must be positive")
	}
	if intercom.WriteTimeoutSeconds < 1 ||
		intercom.PingIntervalSeconds < 1 ||
		intercom.PongWaitSeconds <= intercom.PingIntervalSeconds {
		return fmt.Errorf("VOICE_GATEWAY_INTERCOM timing limits are invalid")
	}
	return nil
}
