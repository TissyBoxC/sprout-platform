package config

import (
	"fmt"
	"strings"
	"time"
)

// CareViewConfig bounds the guardian live camera view relay.
//
// A care view is a separate transport from the device conversation endpoint: a
// conversation is started by the device, while a view is started by a guardian,
// and no frame may flow until the device confirms it is showing its own visible
// and audible indication. It therefore has its own path, its own deadline, and
// its own frame limits instead of reusing the conversation WebSocket settings.
//
// The relay never stores a frame. Size and rate limits exist so one guardian
// connection cannot exhaust gateway memory or bandwidth.
type CareViewConfig struct {
	// Enabled keeps the relay endpoint off until the internal API and the
	// device_platform service token are configured.
	Enabled bool
	// Path is the guardian live-view upgrade route.
	Path string
	// MaxDurationSeconds bounds one view. device_platform caps a view at five
	// minutes; the relay re-applies the cap independently so a lost close
	// message cannot leave a camera running.
	MaxDurationSeconds int
	// MaxFrameBytes bounds one frame in either direction.
	MaxFrameBytes int
	// MaxFramesPerSecond throttles guardian frames so a client cannot flood the
	// device or the gateway.
	MaxFramesPerSecond int
	// WriteQueueDepth bounds the device-bound queue and the guardian outbound
	// queue, so a slow peer applies backpressure instead of growing memory.
	WriteQueueDepth int
	// MaxRelays caps concurrent views across the gateway.
	MaxRelays int
	// WriteTimeoutSeconds bounds one outbound websocket write.
	WriteTimeoutSeconds int
	// PongWaitSeconds bounds how long an idle guardian connection may stay
	// silent.
	PongWaitSeconds int
	// PingIntervalSeconds is how often the gateway sends a keepalive ping.
	PingIntervalSeconds int
	// StateRecheckSeconds is how often the relay re-reads the platform state
	// while frames flow, so a view the guardian or the device ended stops
	// delivering pictures within one short window.
	StateRecheckSeconds int
}

// MaxViewDuration returns the configured relay cap as a duration.
func (c CareViewConfig) MaxViewDuration() time.Duration {
	return time.Duration(c.MaxDurationSeconds) * time.Second
}

// loadCareViewConfig reads the care-view relay settings from the environment.
func loadCareViewConfig() CareViewConfig {
	return CareViewConfig{
		Enabled:             envBool("VOICE_GATEWAY_CARE_VIEW_ENABLED", false),
		Path:                env("VOICE_GATEWAY_CARE_VIEW_PATH", "/v1/care-view"),
		MaxDurationSeconds:  envInt("VOICE_GATEWAY_CARE_VIEW_MAX_DURATION_SECONDS", 300),
		MaxFrameBytes:       envInt("VOICE_GATEWAY_CARE_VIEW_MAX_FRAME_BYTES", 256*1024),
		MaxFramesPerSecond:  envInt("VOICE_GATEWAY_CARE_VIEW_MAX_FRAMES_PER_SECOND", 8),
		WriteQueueDepth:     envInt("VOICE_GATEWAY_CARE_VIEW_WRITE_QUEUE_DEPTH", 64),
		MaxRelays:           envInt("VOICE_GATEWAY_CARE_VIEW_MAX_RELAYS", 256),
		WriteTimeoutSeconds: envInt("VOICE_GATEWAY_CARE_VIEW_WRITE_TIMEOUT_SECONDS", 10),
		PongWaitSeconds:     envInt("VOICE_GATEWAY_CARE_VIEW_PONG_WAIT_SECONDS", 60),
		PingIntervalSeconds: envInt("VOICE_GATEWAY_CARE_VIEW_PING_INTERVAL_SECONDS", 25),
		StateRecheckSeconds: envInt("VOICE_GATEWAY_CARE_VIEW_STATE_RECHECK_SECONDS", 15),
	}
}

// validateCareViewConfig rejects a care-view relay that could run unbounded.
//
// The relay is also an internal API route, so an enabled relay without a
// service token would let any caller authorize a view, and without a strong
// token any caller could re-use the platform credential.
func validateCareViewConfig(cfg Config) error {
	careView := cfg.CareView
	if !careView.Enabled {
		return nil
	}
	if !cfg.Internal.Enabled || len(strings.TrimSpace(cfg.Internal.AuthToken)) < 32 {
		return fmt.Errorf("the internal API must be enabled with a strong token when the care view relay is enabled")
	}
	if !strings.HasPrefix(careView.Path, "/") {
		return fmt.Errorf("VOICE_GATEWAY_CARE_VIEW_PATH must start with a slash")
	}
	// device_platform already refuses a view longer than five minutes, so a
	// longer relay cap would never be reached and would only hide a
	// misconfiguration.
	if careView.MaxDurationSeconds < 1 || careView.MaxDurationSeconds > 300 {
		return fmt.Errorf("VOICE_GATEWAY_CARE_VIEW_MAX_DURATION_SECONDS must be between 1 and 300")
	}
	// A single frame is a live picture; the ceiling keeps one client from
	// turning the relay into a bulk image channel.
	if careView.MaxFrameBytes < 1 || careView.MaxFrameBytes > 2*1024*1024 {
		return fmt.Errorf("VOICE_GATEWAY_CARE_VIEW_MAX_FRAME_BYTES must be between 1 and 2097152")
	}
	if careView.MaxFramesPerSecond < 1 || careView.MaxFramesPerSecond > 30 {
		return fmt.Errorf("VOICE_GATEWAY_CARE_VIEW_MAX_FRAMES_PER_SECOND must be between 1 and 30")
	}
	if careView.WriteQueueDepth < 1 || careView.MaxRelays < 1 {
		return fmt.Errorf("VOICE_GATEWAY_CARE_VIEW limits must be positive")
	}
	if careView.WriteTimeoutSeconds < 1 ||
		careView.PingIntervalSeconds < 1 ||
		careView.PongWaitSeconds <= careView.PingIntervalSeconds {
		return fmt.Errorf("VOICE_GATEWAY_CARE_VIEW timing limits are invalid")
	}
	if careView.StateRecheckSeconds < 1 || careView.StateRecheckSeconds > 60 {
		return fmt.Errorf("VOICE_GATEWAY_CARE_VIEW_STATE_RECHECK_SECONDS must be between 1 and 60")
	}
	return nil
}
