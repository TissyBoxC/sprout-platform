// Package domain defines the guardian video-session media policy and the
// bounded per-device session status for the video_session module.
//
// The policy is per family and mirrors the firmware contract field for field:
// the ring window, the session ceiling, the child-consent gate and the camera
// and microphone permissions. The platform stores only negotiation metadata;
// no audio or video media is persisted here, so a revoked permission ends the
// session without any silent capture.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidPolicy reports a policy value outside the bounds.
	ErrInvalidPolicy = errors.New("invalid video session policy")
	// ErrInvalidInput reports a malformed device status report.
	ErrInvalidInput = errors.New("invalid video session input")
)

// SchemaVersion is the media-negotiation contract version shared with the
// firmware component and the call gateway.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract exactly.
const (
	MinRingTimeoutSeconds = 5
	MaxRingTimeoutSeconds = 120
	MinMaxSessionSeconds  = 60
	MaxMaxSessionSeconds  = 7200
)

// Default values served before a guardian edits the policy. The camera and
// microphone default to denied so a device never captures without an explicit
// guardian decision.
const (
	DefaultEnabled             = true
	DefaultCameraAllowed       = false
	DefaultMicrophoneAllowed   = false
	DefaultRequireChildConsent = true
	DefaultQualityFloor        = QualityPoor
	DefaultRingTimeoutSeconds  = 30
	DefaultMaxSessionSeconds   = 1800
)

// The bounded media quality bands shared with the firmware.
const (
	QualityUnknown = "unknown"
	QualityGood    = "good"
	QualityFair    = "fair"
	QualityPoor    = "poor"
)

// The normalized call states shared with the firmware.
const (
	StateIdle     = "idle"
	StateRinging  = "ringing"
	StateActive   = "active"
	StateEnded    = "ended"
	StateRejected = "rejected"
	StateFailed   = "failed"
)

// The stable end reasons shared with the firmware.
const (
	EndNone              = "none"
	EndHangup            = "hangup"
	EndRejected          = "rejected"
	EndTimeout           = "timeout"
	EndNetwork           = "network"
	EndPermissionRevoked = "permission_revoked"
)

// Qualities is the ordered quality band catalog exposed to clients.
var Qualities = []string{QualityUnknown, QualityGood, QualityFair, QualityPoor}

// States is the ordered call state catalog exposed to clients.
var States = []string{
	StateIdle,
	StateRinging,
	StateActive,
	StateEnded,
	StateRejected,
	StateFailed,
}

// Policy is one validated video-session media policy for a family.
type Policy struct {
	SchemaVersion       int
	Enabled             bool
	CameraAllowed       bool
	MicrophoneAllowed   bool
	RequireChildConsent bool
	QualityFloor        string
	RingTimeoutSeconds  int
	MaxSessionSeconds   int
	Revision            int
	UpdatedAt           time.Time
}

// Input is the editable policy payload accepted from a guardian.
type Input struct {
	Enabled             bool
	CameraAllowed       bool
	MicrophoneAllowed   bool
	RequireChildConsent bool
	QualityFloor        string
	RingTimeoutSeconds  int
	MaxSessionSeconds   int
}

// Default returns the fallback policy used before a guardian edits one. The
// camera and microphone stay off so capture is opt-in.
func Default() Policy {
	return Policy{
		SchemaVersion:       SchemaVersion,
		Enabled:             DefaultEnabled,
		CameraAllowed:       DefaultCameraAllowed,
		MicrophoneAllowed:   DefaultMicrophoneAllowed,
		RequireChildConsent: DefaultRequireChildConsent,
		QualityFloor:        DefaultQualityFloor,
		RingTimeoutSeconds:  DefaultRingTimeoutSeconds,
		MaxSessionSeconds:   DefaultMaxSessionSeconds,
	}
}

// IsQuality reports whether a band is in the fixed quality catalog.
func IsQuality(quality string) bool {
	for _, candidate := range Qualities {
		if candidate == quality {
			return true
		}
	}
	return false
}

// IsState reports whether a call state is accepted.
func IsState(state string) bool {
	for _, candidate := range States {
		if candidate == state {
			return true
		}
	}
	return false
}

// IsEndReason reports whether an end reason is accepted.
func IsEndReason(reason string) bool {
	switch reason {
	case EndNone, EndHangup, EndRejected, EndTimeout, EndNetwork,
		EndPermissionRevoked:
		return true
	default:
		return false
	}
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := Input{
		Enabled:             input.Enabled,
		CameraAllowed:       input.CameraAllowed,
		MicrophoneAllowed:   input.MicrophoneAllowed,
		RequireChildConsent: input.RequireChildConsent,
		QualityFloor:        strings.TrimSpace(input.QualityFloor),
		RingTimeoutSeconds:  input.RingTimeoutSeconds,
		MaxSessionSeconds:   input.MaxSessionSeconds,
	}
	if !IsQuality(normalized.QualityFloor) {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.RingTimeoutSeconds < MinRingTimeoutSeconds ||
		normalized.RingTimeoutSeconds > MaxRingTimeoutSeconds {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.MaxSessionSeconds < MinMaxSessionSeconds ||
		normalized.MaxSessionSeconds > MaxMaxSessionSeconds {
		return Input{}, ErrInvalidPolicy
	}
	return normalized, nil
}

// StatusInput is one bounded device session status report.
type StatusInput struct {
	SessionSupported  bool
	CameraAllowed     bool
	MicrophoneAllowed bool
	CallState         string
	Quality           string
	LastEndReason     string
	PolicyRevision    int
}

// Status is the stored latest session status for one device.
type Status struct {
	DeviceID          string
	FamilyID          string
	SessionSupported  bool
	CameraAllowed     bool
	MicrophoneAllowed bool
	CallState         string
	Quality           string
	LastEndReason     string
	PolicyRevision    int
	ReportedAt        time.Time
}

// ValidateStatus normalizes and validates one device status report.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	normalized.CallState = strings.TrimSpace(input.CallState)
	normalized.Quality = strings.TrimSpace(input.Quality)
	normalized.LastEndReason = strings.TrimSpace(input.LastEndReason)
	// A device that does not support the session reports no active call, so a
	// stale state never reads as a live capture.
	if !input.SessionSupported {
		normalized.CallState = StateIdle
		normalized.Quality = QualityUnknown
		normalized.LastEndReason = EndNone
		normalized.CameraAllowed = false
		normalized.MicrophoneAllowed = false
	}
	if !IsState(normalized.CallState) {
		return StatusInput{}, ErrInvalidInput
	}
	if !IsQuality(normalized.Quality) {
		return StatusInput{}, ErrInvalidInput
	}
	if !IsEndReason(normalized.LastEndReason) {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	return normalized, nil
}

// Capability summarizes whether a device can carry a media session and whether
// it has applied the current family policy revision.
type Capability struct {
	Supported         bool   `json:"supported"`
	CameraAllowed     bool   `json:"camera_allowed"`
	MicrophoneAllowed bool   `json:"microphone_allowed"`
	CallState         string `json:"call_state"`
	Quality           string `json:"quality"`
	SchemaVersion     int    `json:"schema_version"`
	PolicyRevision    int    `json:"policy_revision"`
	AppliedRevision   int    `json:"applied_revision"`
	UpToDate          bool   `json:"up_to_date"`
}

// DeviceStatus is one device row in the guardian and admin session overview.
type DeviceStatus struct {
	DeviceID          string    `json:"device_id"`
	FamilyID          string    `json:"family_id"`
	SessionSupported  bool      `json:"session_supported"`
	CameraAllowed     bool      `json:"camera_allowed"`
	MicrophoneAllowed bool      `json:"microphone_allowed"`
	CallState         string    `json:"call_state"`
	Quality           string    `json:"quality"`
	LastEndReason     string    `json:"last_end_reason"`
	UpToDate          bool      `json:"up_to_date"`
	ReportedAt        time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide video-session summary for the admin console.
type AdminStats struct {
	WindowDays       int       `json:"window_days"`
	DeviceCount      uint64    `json:"device_count"`
	SupportedCount   uint64    `json:"supported_count"`
	CaptureAllowed   uint64    `json:"capture_allowed_count"`
	OutdatedCount    uint64    `json:"outdated_count"`
	PoorQualityCount uint64    `json:"poor_quality_count"`
	StateBuckets     []Count   `json:"state_buckets"`
	ReasonBuckets    []Count   `json:"reason_buckets"`
	GeneratedAt      time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
