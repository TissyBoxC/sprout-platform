// Package domain defines the guardian touch configuration and the bounded
// touch capability contract for the input module.
//
// The configuration is per family: each family owns one editable touch
// configuration that every bound device reads over its device session. Values
// are bounded here as well as in the service so a direct write cannot install
// an unsafe sensitivity, an unsupported rotation, or an unbounded gesture
// threshold.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidConfig reports a touch value outside the approved bounds.
	ErrInvalidConfig = errors.New("invalid touch config")
	// ErrInvalidInput reports a malformed capability report.
	ErrInvalidInput = errors.New("invalid touch input")
)

// SchemaVersion is the configuration contract version shared with the firmware.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract.
const (
	MinSensitivity = 10
	MaxSensitivity = 100

	MinLongPressMS = 250
	MaxLongPressMS = 3000

	MinDoubleTapMS = 120
	MaxDoubleTapMS = 600

	MinSwipeDistancePX = 16
	MaxSwipeDistancePX = 480
)

// Default values served before a guardian edits the configuration.
const (
	DefaultSensitivity     = 60
	DefaultLongPressMS     = 700
	DefaultDoubleTapMS     = 300
	DefaultSwipeDistancePX = 60
)

// Preset names one logical gesture classification used by admin statistics.
type Preset string

// The gesture presets the platform recognises.
const (
	PresetTap       Preset = "tap"
	PresetDoubleTap Preset = "double_tap"
	PresetLongPress Preset = "long_press"
	PresetSwipe     Preset = "swipe"
)

// AllPresets returns the gesture presets in a stable order.
func AllPresets() []Preset {
	return []Preset{PresetTap, PresetDoubleTap, PresetLongPress, PresetSwipe}
}

// SupportedRotation reports whether a rotation is one the controller accepts.
func SupportedRotation(degrees int) bool {
	switch degrees {
	case 0, 90, 180, 270:
		return true
	default:
		return false
	}
}

// Config is one validated touch configuration for a family.
type Config struct {
	SchemaVersion    int
	Enabled          bool
	TapEnabled       bool
	SwipeEnabled     bool
	LongPressEnabled bool
	HapticEnabled    bool
	Sensitivity      int
	RotationDegrees  int
	LongPressMS      int
	DoubleTapMS      int
	SwipeDistancePX  int
	Revision         int
	UpdatedAt        time.Time
}

// Input is the editable configuration payload accepted from a guardian.
type Input struct {
	Enabled          bool
	TapEnabled       bool
	SwipeEnabled     bool
	LongPressEnabled bool
	HapticEnabled    bool
	Sensitivity      int
	RotationDegrees  int
	LongPressMS      int
	DoubleTapMS      int
	SwipeDistancePX  int
}

// Default returns the fallback configuration used before a guardian edits one.
func Default() Config {
	return Config{
		SchemaVersion:    SchemaVersion,
		Enabled:          true,
		TapEnabled:       true,
		SwipeEnabled:     true,
		LongPressEnabled: true,
		HapticEnabled:    true,
		Sensitivity:      DefaultSensitivity,
		RotationDegrees:  0,
		LongPressMS:      DefaultLongPressMS,
		DoubleTapMS:      DefaultDoubleTapMS,
		SwipeDistancePX:  DefaultSwipeDistancePX,
		Revision:         0,
	}
}

// ValidateInput normalizes and validates one configuration payload.
func ValidateInput(input Input) (Input, error) {
	normalized := input
	if normalized.Sensitivity < MinSensitivity ||
		normalized.Sensitivity > MaxSensitivity {
		return Input{}, ErrInvalidConfig
	}
	if !SupportedRotation(normalized.RotationDegrees) {
		return Input{}, ErrInvalidConfig
	}
	if normalized.LongPressMS < MinLongPressMS ||
		normalized.LongPressMS > MaxLongPressMS {
		return Input{}, ErrInvalidConfig
	}
	if normalized.DoubleTapMS < MinDoubleTapMS ||
		normalized.DoubleTapMS > MaxDoubleTapMS {
		return Input{}, ErrInvalidConfig
	}
	if normalized.SwipeDistancePX < MinSwipeDistancePX ||
		normalized.SwipeDistancePX > MaxSwipeDistancePX {
		return Input{}, ErrInvalidConfig
	}
	return normalized, nil
}

// StatusInput is one bounded device touch status report.
type StatusInput struct {
	TouchSupported  bool
	GestureCount    int
	HapticSupported bool
	PolicyRevision  int
}

// Status is the stored latest touch status for one device.
type Status struct {
	DeviceID        string
	FamilyID        string
	TouchSupported  bool
	GestureCount    int
	HapticSupported bool
	PolicyRevision  int
	ReportedAt      time.Time
}

// ValidateStatus normalizes and validates one status report.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	if normalized.GestureCount < 0 || normalized.GestureCount > 1_000_000 {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	return normalized, nil
}

// Capability summarizes whether a device advertises a usable touch panel.
type Capability struct {
	Supported       bool `json:"supported"`
	HapticSupported bool `json:"haptic_supported"`
	GestureCount    int  `json:"gesture_count"`
	SchemaVersion   int  `json:"schema_version"`
	PolicyRevision  int  `json:"policy_revision"`
	AppliedRevision int  `json:"applied_revision"`
	UpToDate        bool `json:"up_to_date"`
}

// DeviceStatus is one device row in the guardian touch overview.
type DeviceStatus struct {
	DeviceID        string    `json:"device_id"`
	FamilyID        string    `json:"family_id"`
	TouchSupported  bool      `json:"touch_supported"`
	HapticSupported bool      `json:"haptic_supported"`
	GestureCount    int       `json:"gesture_count"`
	UpToDate        bool      `json:"up_to_date"`
	ReportedAt      time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide touch summary for the admin console.
type AdminStats struct {
	WindowDays         int           `json:"window_days"`
	DeviceCount        uint64        `json:"device_count"`
	TouchDeviceCount   uint64        `json:"touch_device_count"`
	HapticDeviceCount  uint64        `json:"haptic_device_count"`
	OutdatedCount      uint64        `json:"outdated_count"`
	AverageSensitivity int           `json:"average_sensitivity"`
	PresetCoverage     []PresetCount `json:"preset_coverage"`
	GeneratedAt        time.Time     `json:"generated_at"`
}

// PresetCount is one histogram row for gesture presets.
type PresetCount struct {
	Preset string `json:"preset"`
	Count  uint64 `json:"count"`
}
