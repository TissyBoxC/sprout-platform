// Package domain defines the guardian screen policy, the bounded screen
// capability contract, the family point-to-read dictionary and the device video
// playback state.
//
// The policy is per family: each family owns one editable policy that every
// bound device reads over its device session. Values are bounded here as well as
// in the service, so a direct write cannot install an unsupported frame rate or
// an unbounded video profile. Expressions and animations are a fixed catalog,
// so a guardian can only select a name the firmware actually renders.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidPolicy reports a screen policy value outside the bounds.
	ErrInvalidPolicy = errors.New("invalid screen policy")
	// ErrInvalidInput reports a malformed capability report.
	ErrInvalidInput = errors.New("invalid screen input")
	// ErrInvalidDictionaryEntry reports a malformed point-to-read entry.
	ErrInvalidDictionaryEntry = errors.New("invalid point-read entry")
	// ErrDictionaryNotFound reports a missing point-to-read entry.
	ErrDictionaryNotFound = errors.New("point-read entry not found")
	// ErrDictionaryFull reports a dictionary at its entry bound.
	ErrDictionaryFull = errors.New("point-read dictionary is full")
	// ErrInvalidPlayback reports an unsupported video playback transition.
	ErrInvalidPlayback = errors.New("invalid video playback transition")
)

// SchemaVersion is the policy contract version shared with the firmware.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract.
const (
	MinVideoMaxFPS        = 5
	MaxVideoMaxFPS        = 60
	MinVideoMaxBitrateKBS = 100
	MaxVideoMaxBitrateKBS = 8000
	MinAnimationFrameMS   = 40
	MaxAnimationFrameMS   = 1000

	PointReadTermMaxLength = 48
	PointReadTextMaxLength = 96
	PointReadCapacity      = 32
)

// Default values served before a guardian edits the policy.
const (
	DefaultExpression      = ExpressionHappy
	DefaultVideoMaxFPS     = 30
	DefaultVideoMaxBitrate = 1500
	DefaultAnimationFrame  = 200
)

// The fixed expression catalog shared with the firmware.
const (
	ExpressionNeutral   = "neutral"
	ExpressionHappy     = "happy"
	ExpressionSad       = "sad"
	ExpressionCurious   = "curious"
	ExpressionSleepy    = "sleepy"
	ExpressionSurprised = "surprised"
	ExpressionExcited   = "excited"
)

// The fixed animation catalog shared with the firmware.
const (
	AnimationNone  = "none"
	AnimationIdle  = "idle"
	AnimationBlink = "blink"
	AnimationWave  = "wave"
	AnimationNod   = "nod"
	AnimationDance = "dance"
)

// The video playback states shared with the firmware.
const (
	VideoIdle    = "idle"
	VideoLoading = "loading"
	VideoPlaying = "playing"
	VideoPaused  = "paused"
	VideoEnded   = "ended"
	VideoFailed  = "failed"
)

// Expressions is the ordered expression catalog exposed to clients.
var Expressions = []string{
	ExpressionNeutral,
	ExpressionHappy,
	ExpressionSad,
	ExpressionCurious,
	ExpressionSleepy,
	ExpressionSurprised,
	ExpressionExcited,
}

// Animations is the ordered animation catalog exposed to clients.
var Animations = []string{
	AnimationIdle,
	AnimationBlink,
	AnimationWave,
	AnimationNod,
	AnimationDance,
}

// Policy is one validated screen policy for a family.
type Policy struct {
	SchemaVersion      int
	DefaultExpression  string
	AnimationEnabled   bool
	PointReadEnabled   bool
	VideoEnabled       bool
	VideoMaxFPS        int
	VideoMaxBitrateKBS int
	AnimationFrameMS   int
	Revision           int
	UpdatedAt          time.Time
}

// Input is the editable policy payload accepted from a guardian.
type Input struct {
	DefaultExpression  string
	AnimationEnabled   bool
	PointReadEnabled   bool
	VideoEnabled       bool
	VideoMaxFPS        int
	VideoMaxBitrateKBS int
	AnimationFrameMS   int
}

// Default returns the fallback policy used before a guardian edits one.
func Default() Policy {
	return Policy{
		SchemaVersion:      SchemaVersion,
		DefaultExpression:  DefaultExpression,
		AnimationEnabled:   true,
		PointReadEnabled:   true,
		VideoEnabled:       true,
		VideoMaxFPS:        DefaultVideoMaxFPS,
		VideoMaxBitrateKBS: DefaultVideoMaxBitrate,
		AnimationFrameMS:   DefaultAnimationFrame,
	}
}

// IsExpression reports whether a name is in the fixed expression catalog.
func IsExpression(name string) bool {
	for _, expression := range Expressions {
		if expression == name {
			return true
		}
	}
	return false
}

// IsAnimation reports whether a name is in the fixed animation catalog.
func IsAnimation(name string) bool {
	for _, animation := range Animations {
		if animation == name {
			return true
		}
	}
	return false
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := Input{
		DefaultExpression:  strings.TrimSpace(input.DefaultExpression),
		AnimationEnabled:   input.AnimationEnabled,
		PointReadEnabled:   input.PointReadEnabled,
		VideoEnabled:       input.VideoEnabled,
		VideoMaxFPS:        input.VideoMaxFPS,
		VideoMaxBitrateKBS: input.VideoMaxBitrateKBS,
		AnimationFrameMS:   input.AnimationFrameMS,
	}
	if !IsExpression(normalized.DefaultExpression) {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.VideoMaxFPS < MinVideoMaxFPS ||
		normalized.VideoMaxFPS > MaxVideoMaxFPS {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.VideoMaxBitrateKBS < MinVideoMaxBitrateKBS ||
		normalized.VideoMaxBitrateKBS > MaxVideoMaxBitrateKBS {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.AnimationFrameMS < MinAnimationFrameMS ||
		normalized.AnimationFrameMS > MaxAnimationFrameMS {
		return Input{}, ErrInvalidPolicy
	}
	return normalized, nil
}

// StatusInput is one bounded device screen capability report.
type StatusInput struct {
	ScreenSupported      bool
	ExpressionsSupported bool
	AnimationSupported   bool
	PointReadSupported   bool
	VideoSupported       bool
	Expression           string
	Animation            string
	VideoState           string
	PolicyRevision       int
}

// Status is the stored latest screen status for one device.
type Status struct {
	DeviceID             string
	FamilyID             string
	ScreenSupported      bool
	ExpressionsSupported bool
	AnimationSupported   bool
	PointReadSupported   bool
	VideoSupported       bool
	Expression           string
	Animation            string
	VideoState           string
	PolicyRevision       int
	ReportedAt           time.Time
}

// ValidateStatus normalizes and validates one status report.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	normalized.Expression = strings.TrimSpace(input.Expression)
	normalized.Animation = strings.TrimSpace(input.Animation)
	normalized.VideoState = strings.TrimSpace(input.VideoState)
	// An unsupported surface reports no active expression, animation or video,
	// so a stale name never reads as an active capability.
	if !input.ScreenSupported {
		normalized.Expression = ExpressionNeutral
		normalized.Animation = AnimationNone
		normalized.VideoState = VideoIdle
	}
	if !IsExpression(normalized.Expression) {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.Animation != AnimationNone && !IsAnimation(normalized.Animation) {
		return StatusInput{}, ErrInvalidInput
	}
	if !isVideoState(normalized.VideoState) {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	return normalized, nil
}

func isVideoState(state string) bool {
	switch state {
	case VideoIdle, VideoLoading, VideoPlaying, VideoPaused, VideoEnded, VideoFailed:
		return true
	default:
		return false
	}
}

// Capability summarizes whether a device advertises a usable screen.
type Capability struct {
	Supported            bool `json:"supported"`
	ExpressionsSupported bool `json:"expressions_supported"`
	AnimationSupported   bool `json:"animation_supported"`
	PointReadSupported   bool `json:"point_read_supported"`
	VideoSupported       bool `json:"video_supported"`
	SchemaVersion        int  `json:"schema_version"`
	PolicyRevision       int  `json:"policy_revision"`
	AppliedRevision      int  `json:"applied_revision"`
	UpToDate             bool `json:"up_to_date"`
}

// DeviceStatus is one device row in the admin screen overview.
type DeviceStatus struct {
	DeviceID         string    `json:"device_id"`
	FamilyID         string    `json:"family_id"`
	ScreenSupported  bool      `json:"screen_supported"`
	PointReadSupport bool      `json:"point_read_supported"`
	VideoSupported   bool      `json:"video_supported"`
	Expression       string    `json:"expression"`
	VideoState       string    `json:"video_state"`
	UpToDate         bool      `json:"up_to_date"`
	ReportedAt       time.Time `json:"reported_at"`
}

// DictionaryEntry is one family point-to-read entry.
type DictionaryEntry struct {
	ID        string    `json:"id"`
	FamilyID  string    `json:"family_id"`
	Term      string    `json:"term"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DictionaryInput is one point-to-read entry payload.
type DictionaryInput struct {
	Term string
	Text string
}

// ValidateDictionaryInput normalizes and validates one dictionary entry.
func ValidateDictionaryInput(input DictionaryInput) (DictionaryInput, error) {
	normalized := DictionaryInput{
		Term: strings.TrimSpace(input.Term),
		Text: strings.TrimSpace(input.Text),
	}
	if normalized.Term == "" || normalized.Text == "" {
		return DictionaryInput{}, ErrInvalidDictionaryEntry
	}
	if len([]rune(normalized.Term)) > PointReadTermMaxLength {
		return DictionaryInput{}, ErrInvalidDictionaryEntry
	}
	if len([]rune(normalized.Text)) > PointReadTextMaxLength {
		return DictionaryInput{}, ErrInvalidDictionaryEntry
	}
	return normalized, nil
}

// PlaybackPlan is the clamped video profile a device will actually play.
type PlaybackPlan struct {
	BitrateKBS int `json:"bitrate_kbps"`
	FPS        int `json:"fps"`
}

// AdminStats is the fleet-wide screen summary for the admin console.
type AdminStats struct {
	WindowDays           int       `json:"window_days"`
	DeviceCount          uint64    `json:"device_count"`
	VideoDeviceCount     uint64    `json:"video_device_count"`
	PointReadDeviceCount uint64    `json:"point_read_device_count"`
	OutdatedCount        uint64    `json:"outdated_count"`
	StateBuckets         []Count   `json:"state_buckets"`
	GeneratedAt          time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
