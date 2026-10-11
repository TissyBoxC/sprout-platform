// Package domain defines the guardian learning-visualization policy and the
// bounded per-device visualization status for the learning_visualization
// module.
//
// The policy is per family and mirrors the firmware contract field for field:
// whether the feature is enabled, the feedback style, the animation level, the
// touch/reward/lesson-linkage toggles and the session ceiling. The platform
// stores only the policy and bounded status scalars; no frame buffers, touch
// traces or captured media are ever persisted here, so the visualization stays
// a presentation layer over the learning content rather than a data collector.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidPolicy reports a policy value outside the bounds.
	ErrInvalidPolicy = errors.New("invalid learning visualization policy")
	// ErrInvalidInput reports a malformed visualization status report.
	ErrInvalidInput = errors.New("invalid learning visualization input")
)

// SchemaVersion is the learning-visualization contract version shared with the
// firmware component.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract exactly.
const (
	MinSessionMinutes = 1
	MaxSessionMinutes = 60
	// MaxFeedbackEvents bounds the reported feedback counter.
	MaxFeedbackEvents = 100000
	// MaxSessionMinutesReport bounds one device's reported session length.
	MaxSessionMinutesReport = 1440
	// LessonIDMaxLength bounds the linked lesson identifier.
	LessonIDMaxLength = 64
)

// Default values served before a guardian edits the policy. The feature and the
// animation start off so a device never animates a learning surface without an
// explicit guardian decision.
const (
	DefaultEnabled              = false
	DefaultFeedbackStyle        = StyleNeutral
	DefaultAnimationLevel       = AnimationOff
	DefaultTouchFeedbackEnabled = true
	DefaultRewardEffectsEnabled = true
	DefaultLessonLinkageEnabled = true
	DefaultMaxSessionMinutes    = 20
)

// The fixed feedback-style catalog shared with the firmware.
const (
	StyleNeutral     = "neutral"
	StyleEncouraging = "encouraging"
	StylePlayful     = "playful"
	StyleCelebration = "celebration"
)

// The fixed animation-level catalog shared with the firmware.
const (
	AnimationOff    = "off"
	AnimationSubtle = "subtle"
	AnimationFull   = "full"
)

// The normalized visual states shared with the firmware.
const (
	VisualIdle     = "idle"
	VisualLesson   = "lesson"
	VisualFeedback = "feedback"
	VisualReward   = "reward"
	VisualPaused   = "paused"
	VisualError    = "error"
)

// FeedbackStyles is the ordered feedback-style catalog exposed to clients.
var FeedbackStyles = []string{
	StyleNeutral,
	StyleEncouraging,
	StylePlayful,
	StyleCelebration,
}

// AnimationLevels is the ordered animation-level catalog exposed to clients.
var AnimationLevels = []string{AnimationOff, AnimationSubtle, AnimationFull}

// VisualStates is the ordered visual-state catalog exposed to clients.
var VisualStates = []string{
	VisualIdle,
	VisualLesson,
	VisualFeedback,
	VisualReward,
	VisualPaused,
	VisualError,
}

// Policy is one validated learning-visualization policy for a family.
type Policy struct {
	SchemaVersion         int
	Enabled               bool
	FeedbackStyle         string
	AnimationLevel        string
	TouchFeedbackEnabled  bool
	RewardEffectsEnabled  bool
	LessonLinkageEnabled  bool
	MaxSessionMinutes     int
	Revision              int
	UpdatedAt             time.Time
}

// Input is the editable policy payload accepted from a guardian.
type Input struct {
	Enabled              bool
	FeedbackStyle        string
	AnimationLevel       string
	TouchFeedbackEnabled bool
	RewardEffectsEnabled bool
	LessonLinkageEnabled bool
	MaxSessionMinutes    int
}

// Default returns the fallback policy used before a guardian edits one. The
// feature and the animation stay off so the visualization is opt-in.
func Default() Policy {
	return Policy{
		SchemaVersion:        SchemaVersion,
		Enabled:              DefaultEnabled,
		FeedbackStyle:        DefaultFeedbackStyle,
		AnimationLevel:       DefaultAnimationLevel,
		TouchFeedbackEnabled: DefaultTouchFeedbackEnabled,
		RewardEffectsEnabled: DefaultRewardEffectsEnabled,
		LessonLinkageEnabled: DefaultLessonLinkageEnabled,
		MaxSessionMinutes:    DefaultMaxSessionMinutes,
	}
}

// IsFeedbackStyle reports whether a style is in the fixed catalog.
func IsFeedbackStyle(style string) bool {
	for _, candidate := range FeedbackStyles {
		if candidate == style {
			return true
		}
	}
	return false
}

// IsAnimationLevel reports whether an animation level is in the fixed catalog.
func IsAnimationLevel(level string) bool {
	for _, candidate := range AnimationLevels {
		if candidate == level {
			return true
		}
	}
	return false
}

// IsVisualState reports whether a visual state is accepted.
func IsVisualState(state string) bool {
	for _, candidate := range VisualStates {
		if candidate == state {
			return true
		}
	}
	return false
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := Input{
		Enabled:              input.Enabled,
		FeedbackStyle:        strings.TrimSpace(input.FeedbackStyle),
		AnimationLevel:       strings.TrimSpace(input.AnimationLevel),
		TouchFeedbackEnabled: input.TouchFeedbackEnabled,
		RewardEffectsEnabled: input.RewardEffectsEnabled,
		LessonLinkageEnabled: input.LessonLinkageEnabled,
		MaxSessionMinutes:    input.MaxSessionMinutes,
	}
	if !IsFeedbackStyle(normalized.FeedbackStyle) {
		return Input{}, ErrInvalidPolicy
	}
	if !IsAnimationLevel(normalized.AnimationLevel) {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.MaxSessionMinutes < MinSessionMinutes ||
		normalized.MaxSessionMinutes > MaxSessionMinutes {
		return Input{}, ErrInvalidPolicy
	}
	// A disabled feature cannot animate; the derived level is pinned off here so
	// a stale animation setting never survives the switch.
	if !normalized.Enabled {
		normalized.AnimationLevel = AnimationOff
	}
	return normalized, nil
}

// StatusInput is one bounded device visualization status report.
type StatusInput struct {
	VisualizationSupported bool
	TouchSupported         bool
	LinkedLessonID         string
	VisualState            string
	FeedbackStyle          string
	AnimationLevel         string
	FeedbackEvents         int
	SessionMinutes         int
	PolicyRevision         int
}

// Status is the stored latest visualization status for one device.
type Status struct {
	DeviceID               string
	FamilyID               string
	VisualizationSupported bool
	TouchSupported         bool
	LinkedLessonID         string
	VisualState            string
	FeedbackStyle          string
	AnimationLevel         string
	FeedbackEvents         int
	SessionMinutes         int
	PolicyRevision         int
	ReportedAt             time.Time
}

// ValidateStatus normalizes and validates one device status report.
//
// A device that does not support the visualization reports a fully idle,
// animation-off surface, so a stale state never reads as an active animation.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	normalized.LinkedLessonID = strings.TrimSpace(input.LinkedLessonID)
	normalized.VisualState = strings.TrimSpace(input.VisualState)
	normalized.FeedbackStyle = strings.TrimSpace(input.FeedbackStyle)
	normalized.AnimationLevel = strings.TrimSpace(input.AnimationLevel)
	if len([]rune(normalized.LinkedLessonID)) > LessonIDMaxLength {
		return StatusInput{}, ErrInvalidInput
	}
	if !input.VisualizationSupported {
		normalized.TouchSupported = false
		normalized.LinkedLessonID = ""
		normalized.VisualState = VisualIdle
		normalized.FeedbackStyle = StyleNeutral
		normalized.AnimationLevel = AnimationOff
		normalized.FeedbackEvents = 0
		normalized.SessionMinutes = 0
	}
	if !IsVisualState(normalized.VisualState) {
		return StatusInput{}, ErrInvalidInput
	}
	if !IsFeedbackStyle(normalized.FeedbackStyle) {
		return StatusInput{}, ErrInvalidInput
	}
	if !IsAnimationLevel(normalized.AnimationLevel) {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.FeedbackEvents < 0 ||
		normalized.FeedbackEvents > MaxFeedbackEvents {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.SessionMinutes < 0 ||
		normalized.SessionMinutes > MaxSessionMinutesReport {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	// An active animation only makes sense while the surface is running a lesson
	// or feedback; a paused or idle surface reports animation off.
	if normalized.VisualState == VisualIdle || normalized.VisualState == VisualPaused {
		normalized.AnimationLevel = AnimationOff
	}
	return normalized, nil
}

// Capability summarizes whether a device supports the visualization and whether
// it has applied the current family policy revision.
type Capability struct {
	Supported       bool   `json:"supported"`
	TouchSupported  bool   `json:"touch_supported"`
	LinkedLessonID  string `json:"linked_lesson_id"`
	VisualState     string `json:"visual_state"`
	FeedbackStyle   string `json:"feedback_style"`
	AnimationLevel  string `json:"animation_level"`
	SchemaVersion   int    `json:"schema_version"`
	PolicyRevision  int    `json:"policy_revision"`
	AppliedRevision int    `json:"applied_revision"`
	UpToDate        bool   `json:"up_to_date"`
}

// DeviceStatus is one device row in the guardian and admin overview.
type DeviceStatus struct {
	DeviceID               string    `json:"device_id"`
	FamilyID               string    `json:"family_id"`
	VisualizationSupported bool      `json:"visualization_supported"`
	TouchSupported         bool      `json:"touch_supported"`
	LinkedLessonID         string    `json:"linked_lesson_id"`
	VisualState            string    `json:"visual_state"`
	FeedbackStyle          string    `json:"feedback_style"`
	AnimationLevel         string    `json:"animation_level"`
	FeedbackEvents         int       `json:"feedback_events"`
	SessionMinutes         int       `json:"session_minutes"`
	UpToDate               bool      `json:"up_to_date"`
	ReportedAt             time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide learning-visualization summary for the admin
// console.
type AdminStats struct {
	WindowDays       int       `json:"window_days"`
	DeviceCount      uint64    `json:"device_count"`
	SupportedCount   uint64    `json:"supported_count"`
	TouchCount       uint64    `json:"touch_count"`
	LessonLinkedCount uint64   `json:"lesson_linked_count"`
	OutdatedCount    uint64    `json:"outdated_count"`
	FeedbackEvents   uint64    `json:"feedback_events"`
	StateBuckets     []Count   `json:"state_buckets"`
	StyleBuckets     []Count   `json:"style_buckets"`
	GeneratedAt      time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
