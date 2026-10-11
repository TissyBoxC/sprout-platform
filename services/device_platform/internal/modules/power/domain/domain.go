// Package domain defines the guardian battery and low-power policy and the
// bounded device power status contract for the power module.
//
// The policy is per family: each family owns one editable policy that every
// bound device reads over its device session. Values are bounded here as well
// as in the service so a direct write cannot install an unsafe deep-sleep
// window or an unsupported charge priority.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidPolicy reports a power value outside the approved bounds.
	ErrInvalidPolicy = errors.New("invalid power policy")
	// ErrInvalidInput reports a malformed status report.
	ErrInvalidInput = errors.New("invalid power input")
)

// SchemaVersion is the policy contract version shared with the firmware.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract.
const (
	MinWarnPercent = 5
	MaxWarnPercent = 50

	MinLowPowerPercent = 5
	MaxLowPowerPercent = 50

	MinSleepAfterSeconds = 60
	MaxSleepAfterSeconds = 86400
)

// Default values served before a guardian edits the policy.
const (
	DefaultWarnPercent      = 20
	DefaultLowPowerPercent  = 15
	DefaultSleepAfterSecond = 1800
)

// ChargeState is the normalized charge state reported by a device.
type ChargeState string

// The charge states the platform recognises.
const (
	ChargeUnknown     ChargeState = "unknown"
	ChargeDischarging ChargeState = "discharging"
	ChargeCharging    ChargeState = "charging"
	ChargeFull        ChargeState = "full"
)

// PowerMode is the normalized power mode reported by a device.
type PowerMode string

// The power modes the platform recognises.
const (
	ModeNormal    PowerMode = "normal"
	ModeLowPower  PowerMode = "low_power"
	ModeDeepSleep PowerMode = "deep_sleep"
)

// SupportedChargeState reports whether a charge state is accepted.
func SupportedChargeState(state ChargeState) bool {
	switch state {
	case ChargeUnknown, ChargeDischarging, ChargeCharging, ChargeFull:
		return true
	default:
		return false
	}
}

// SupportedPowerMode reports whether a power mode is accepted.
func SupportedPowerMode(mode PowerMode) bool {
	switch mode {
	case ModeNormal, ModeLowPower, ModeDeepSleep:
		return true
	default:
		return false
	}
}

// Policy is one validated battery and low-power policy for a family.
type Policy struct {
	SchemaVersion         int
	Enabled               bool
	LowPowerEnabled       bool
	DeepSleepEnabled      bool
	ChargePriority        bool
	LowBatteryWarnPercent int
	LowPowerEnterPercent  int
	SleepAfterSeconds     int
	Revision              int
	UpdatedAt             time.Time
}

// Input is the editable policy payload accepted from a guardian.
type Input struct {
	Enabled               bool
	LowPowerEnabled       bool
	DeepSleepEnabled      bool
	ChargePriority        bool
	LowBatteryWarnPercent int
	LowPowerEnterPercent  int
	SleepAfterSeconds     int
}

// Default returns the fallback policy used before a guardian edits one.
func Default() Policy {
	return Policy{
		SchemaVersion:         SchemaVersion,
		Enabled:               true,
		LowPowerEnabled:       true,
		DeepSleepEnabled:      true,
		ChargePriority:        true,
		LowBatteryWarnPercent: DefaultWarnPercent,
		LowPowerEnterPercent:  DefaultLowPowerPercent,
		SleepAfterSeconds:     DefaultSleepAfterSecond,
		Revision:              0,
	}
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := input
	if normalized.LowBatteryWarnPercent < MinWarnPercent ||
		normalized.LowBatteryWarnPercent > MaxWarnPercent {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.LowPowerEnterPercent < MinLowPowerPercent ||
		normalized.LowPowerEnterPercent > MaxLowPowerPercent {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.SleepAfterSeconds < MinSleepAfterSeconds ||
		normalized.SleepAfterSeconds > MaxSleepAfterSeconds {
		return Input{}, ErrInvalidPolicy
	}
	return normalized, nil
}

// StatusInput is one bounded device power status report.
type StatusInput struct {
	ChargeState    ChargeState
	PowerMode      PowerMode
	BatteryPercent int
	Millivolts     int
	Charging       bool
	PolicyRevision int
}

// Status is the stored latest power status for one device.
type Status struct {
	DeviceID       string
	FamilyID       string
	ChargeState    ChargeState
	PowerMode      PowerMode
	BatteryPercent int
	Millivolts     int
	Charging       bool
	PolicyRevision int
	ReportedAt     time.Time
}

// ValidateStatus normalizes and validates one status report.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	if !SupportedChargeState(normalized.ChargeState) {
		return StatusInput{}, ErrInvalidInput
	}
	if !SupportedPowerMode(normalized.PowerMode) {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.BatteryPercent < 0 || normalized.BatteryPercent > 100 {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.Millivolts < 0 || normalized.Millivolts > 6000 {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	return normalized, nil
}

// Capability summarizes whether a device advertises a usable battery gauge.
type Capability struct {
	Supported       bool   `json:"supported"`
	ChargeState     string `json:"charge_state"`
	PowerMode       string `json:"power_mode"`
	BatteryPercent  int    `json:"battery_percent"`
	Charging        bool   `json:"charging"`
	LowWarning      bool   `json:"low_warning"`
	SchemaVersion   int    `json:"schema_version"`
	PolicyRevision  int    `json:"policy_revision"`
	AppliedRevision int    `json:"applied_revision"`
	UpToDate        bool   `json:"up_to_date"`
}

// DeviceStatus is one device row in the guardian power overview.
type DeviceStatus struct {
	DeviceID       string    `json:"device_id"`
	FamilyID       string    `json:"family_id"`
	ChargeState    string    `json:"charge_state"`
	PowerMode      string    `json:"power_mode"`
	BatteryPercent int       `json:"battery_percent"`
	Charging       bool      `json:"charging"`
	UpToDate       bool      `json:"up_to_date"`
	ReportedAt     time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide power summary for the admin console.
type AdminStats struct {
	WindowDays        int       `json:"window_days"`
	DeviceCount       uint64    `json:"device_count"`
	ChargingCount     uint64    `json:"charging_count"`
	LowBatteryCount   uint64    `json:"low_battery_count"`
	LowPowerCount     uint64    `json:"low_power_count"`
	DeepSleepCount    uint64    `json:"deep_sleep_count"`
	AveragePercent    int       `json:"average_percent"`
	PercentageBuckets []Count   `json:"percentage_buckets"`
	GeneratedAt       time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
