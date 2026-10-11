// Package domain defines the guardian eye-care display policy and the bounded
// screen capability contract.
//
// The policy is per family: each family owns one editable policy that the
// device reads over its device session. Values are bounded here as well as in
// the service so a direct write cannot install an unsafe brightness or an
// unbounded screen-time limit.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidPolicy reports a policy value outside the approved bounds.
	ErrInvalidPolicy = errors.New("invalid display policy")
	// ErrInvalidInput reports a malformed capability report.
	ErrInvalidInput = errors.New("invalid display input")
)

// SchemaVersion is the policy contract version shared with the firmware.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract.
const (
	MinBrightnessPercent = 5
	MaxBrightnessPercent = 100

	MinColorTemperatureKelvin = 2700
	MaxColorTemperatureKelvin = 6500

	MinViewDistanceCM = 20
	MaxViewDistanceCM = 120

	MinDailyLimitMinutes = 5
	MaxDailyLimitMinutes = 600

	MinBreakIntervalMinutes = 5
	MaxBreakIntervalMinutes = 240

	MinDimAfterSeconds = 10
	MaxDimAfterSeconds = 600
)

// Default values served before a guardian edits the policy.
const (
	DefaultBrightnessPercent    = 60
	DefaultColorTemperature     = 4500
	DefaultViewDistanceCM       = 40
	DefaultDailyLimitMinutes    = 60
	DefaultBreakIntervalMinutes = 20
	DefaultDimAfterSeconds      = 60
)

// Policy is one validated eye-care policy for a family.
type Policy struct {
	SchemaVersion          int
	BrightnessPercent      int
	ColorTemperatureKelvin int
	MinViewDistanceCM      int
	DailyLimitMinutes      int
	BreakIntervalMinutes   int
	DimAfterSeconds        int
	AutoBrightness         bool
	NightLight             bool
	DistanceReminder       bool
	Revision               int
	UpdatedAt              time.Time
}

// Input is the editable policy payload accepted from a guardian or operator.
type Input struct {
	BrightnessPercent      int
	ColorTemperatureKelvin int
	MinViewDistanceCM      int
	DailyLimitMinutes      int
	BreakIntervalMinutes   int
	DimAfterSeconds        int
	AutoBrightness         bool
	NightLight             bool
	DistanceReminder       bool
}

// Default returns the fallback policy used before a guardian edits one.
func Default() Policy {
	return Policy{
		SchemaVersion:          SchemaVersion,
		BrightnessPercent:      DefaultBrightnessPercent,
		ColorTemperatureKelvin: DefaultColorTemperature,
		MinViewDistanceCM:      DefaultViewDistanceCM,
		DailyLimitMinutes:      DefaultDailyLimitMinutes,
		BreakIntervalMinutes:   DefaultBreakIntervalMinutes,
		DimAfterSeconds:        DefaultDimAfterSeconds,
		AutoBrightness:         true,
		NightLight:             true,
		DistanceReminder:       true,
		Revision:               0,
	}
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := Input{
		BrightnessPercent:      input.BrightnessPercent,
		ColorTemperatureKelvin: input.ColorTemperatureKelvin,
		MinViewDistanceCM:      input.MinViewDistanceCM,
		DailyLimitMinutes:      input.DailyLimitMinutes,
		BreakIntervalMinutes:   input.BreakIntervalMinutes,
		DimAfterSeconds:        input.DimAfterSeconds,
		AutoBrightness:         input.AutoBrightness,
		NightLight:             input.NightLight,
		DistanceReminder:       input.DistanceReminder,
	}
	if normalized.BrightnessPercent < MinBrightnessPercent ||
		normalized.BrightnessPercent > MaxBrightnessPercent {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.ColorTemperatureKelvin < MinColorTemperatureKelvin ||
		normalized.ColorTemperatureKelvin > MaxColorTemperatureKelvin {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.MinViewDistanceCM < MinViewDistanceCM ||
		normalized.MinViewDistanceCM > MaxViewDistanceCM {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.DailyLimitMinutes < MinDailyLimitMinutes ||
		normalized.DailyLimitMinutes > MaxDailyLimitMinutes {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.BreakIntervalMinutes < MinBreakIntervalMinutes ||
		normalized.BreakIntervalMinutes > MaxBreakIntervalMinutes {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.DimAfterSeconds < MinDimAfterSeconds ||
		normalized.DimAfterSeconds > MaxDimAfterSeconds {
		return Input{}, ErrInvalidPolicy
	}
	return normalized, nil
}

// StatusInput is one bounded device display status report.
type StatusInput struct {
	BrightnessPercent int
	ScreenWidth       int
	ScreenHeight      int
	TouchSupported    bool
	PolicyRevision    int
}

// Status is the stored latest display status for one device.
type Status struct {
	DeviceID          string
	FamilyID          string
	BrightnessPercent int
	ScreenWidth       int
	ScreenHeight      int
	TouchSupported    bool
	PolicyRevision    int
	ReportedAt        time.Time
}

// ValidateStatus normalizes and validates one status report.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	if normalized.BrightnessPercent < 0 ||
		normalized.BrightnessPercent > MaxBrightnessPercent {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.ScreenWidth < 0 || normalized.ScreenWidth > 8192 {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.ScreenHeight < 0 || normalized.ScreenHeight > 8192 {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	return normalized, nil
}

// Capability summarizes whether a device advertises a usable display.
type Capability struct {
	Supported       bool `json:"supported"`
	TouchSupported  bool `json:"touch_supported"`
	ScreenWidth     int  `json:"screen_width"`
	ScreenHeight    int  `json:"screen_height"`
	SchemaVersion   int  `json:"schema_version"`
	PolicyRevision  int  `json:"policy_revision"`
	AppliedRevision int  `json:"applied_revision"`
	UpToDate        bool `json:"up_to_date"`
}

// DeviceStatus is one device row in the admin display overview.
type DeviceStatus struct {
	DeviceID          string    `json:"device_id"`
	FamilyID          string    `json:"family_id"`
	BrightnessPercent int       `json:"brightness_percent"`
	ScreenWidth       int       `json:"screen_width"`
	ScreenHeight      int       `json:"screen_height"`
	TouchSupported    bool      `json:"touch_supported"`
	UpToDate          bool      `json:"up_to_date"`
	ReportedAt        time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide display summary for the admin console.
type AdminStats struct {
	WindowDays        int            `json:"window_days"`
	DeviceCount       uint64         `json:"device_count"`
	TouchDeviceCount  uint64         `json:"touch_device_count"`
	OutdatedCount     uint64         `json:"outdated_count"`
	AverageBrightness int            `json:"average_brightness"`
	ScreenSizeBuckets []ScreenBucket `json:"screen_size_buckets"`
	GeneratedAt       time.Time      `json:"generated_at"`
}

// ScreenBucket is one histogram row for screen classes.
type ScreenBucket struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
