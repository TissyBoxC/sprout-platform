// Package domain defines the guardian cellular policy and the bounded
// connectivity status contract for the connectivity module.
//
// The policy is per family: each family owns one editable cellular policy that
// every bound device reads over its device session. Values are bounded here as
// well as in the service so a direct write cannot install an unbounded quota or
// an unsupported radio preference.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidPolicy reports a cellular value outside the approved bounds.
	ErrInvalidPolicy = errors.New("invalid cellular policy")
	// ErrInvalidInput reports a malformed status report.
	ErrInvalidInput = errors.New("invalid connectivity input")
)

// SchemaVersion is the policy contract version shared with the firmware.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract.
const (
	MinQuotaMB = 0
	MaxQuotaMB = 512000

	MinWarnPercent = 5
	MaxWarnPercent = 100

	MinKeepaliveSeconds = 30
	MaxKeepaliveSeconds = 3600
)

// Default values served before a guardian edits the policy.
const (
	DefaultQuotaMB          = 2048
	DefaultWarnPercent      = 20
	DefaultKeepaliveSeconds = 300
)

// Preference is the preferred radio the device attaches to.
type Preference string

// The radio preferences the platform recognises.
const (
	PreferenceWiFiFirst     Preference = "wifi_first"
	PreferenceCellularFirst Preference = "cellular_first"
	PreferenceCellularOnly  Preference = "cellular_only"
)

// Bearer is one active network bearer class.
type Bearer string

// The bearer classes the platform recognises.
const (
	BearerWiFi     Bearer = "wifi"
	BearerCellular Bearer = "cellular"
	BearerNone     Bearer = "none"
)

// ESIMState is the eSIM profile lifecycle state.
type ESIMState string

// The eSIM profile states the platform recognises.
const (
	ESIMAbsent    ESIMState = "absent"
	ESIMInstalled ESIMState = "installed"
	ESIMActive    ESIMState = "active"
	ESIMDisabled  ESIMState = "disabled"
)

// SupportedPreference reports whether a preference is one the platform accepts.
func SupportedPreference(preference Preference) bool {
	switch preference {
	case PreferenceWiFiFirst, PreferenceCellularFirst, PreferenceCellularOnly:
		return true
	default:
		return false
	}
}

// SupportedBearer reports whether a bearer is one the platform accepts.
func SupportedBearer(bearer Bearer) bool {
	switch bearer {
	case BearerWiFi, BearerCellular, BearerNone:
		return true
	default:
		return false
	}
}

// SupportedESIMState reports whether an eSIM state is one the platform accepts.
func SupportedESIMState(state ESIMState) bool {
	switch state {
	case ESIMAbsent, ESIMInstalled, ESIMActive, ESIMDisabled:
		return true
	default:
		return false
	}
}

// Policy is one validated cellular policy for a family.
type Policy struct {
	SchemaVersion       int
	Enabled             bool
	RoamingAllowed      bool
	RemoteWakeEnabled   bool
	MeteredUploadLimit  bool
	Preference          Preference
	MonthlyQuotaMB      int
	LowQuotaWarnPercent int
	KeepaliveSeconds    int
	Revision            int
	UpdatedAt           time.Time
}

// Input is the editable policy payload accepted from a guardian.
type Input struct {
	Enabled             bool
	RoamingAllowed      bool
	RemoteWakeEnabled   bool
	MeteredUploadLimit  bool
	Preference          Preference
	MonthlyQuotaMB      int
	LowQuotaWarnPercent int
	KeepaliveSeconds    int
}

// Default returns the fallback policy used before a guardian edits one.
func Default() Policy {
	return Policy{
		SchemaVersion:       SchemaVersion,
		Enabled:             true,
		RoamingAllowed:      false,
		RemoteWakeEnabled:   true,
		MeteredUploadLimit:  true,
		Preference:          PreferenceWiFiFirst,
		MonthlyQuotaMB:      DefaultQuotaMB,
		LowQuotaWarnPercent: DefaultWarnPercent,
		KeepaliveSeconds:    DefaultKeepaliveSeconds,
		Revision:            0,
	}
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := input
	if !SupportedPreference(normalized.Preference) {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.MonthlyQuotaMB < MinQuotaMB ||
		normalized.MonthlyQuotaMB > MaxQuotaMB {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.LowQuotaWarnPercent < MinWarnPercent ||
		normalized.LowQuotaWarnPercent > MaxWarnPercent {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.KeepaliveSeconds < MinKeepaliveSeconds ||
		normalized.KeepaliveSeconds > MaxKeepaliveSeconds {
		return Input{}, ErrInvalidPolicy
	}
	return normalized, nil
}

// StatusInput is one bounded device connectivity status report.
type StatusInput struct {
	Bearer         Bearer
	Roaming        bool
	ESIMState      ESIMState
	SignalPercent  int
	UsedMB         int
	PolicyRevision int
}

// Status is the stored latest connectivity status for one device.
type Status struct {
	DeviceID       string
	FamilyID       string
	Bearer         Bearer
	Roaming        bool
	ESIMState      ESIMState
	SignalPercent  int
	UsedMB         int
	PolicyRevision int
	ReportedAt     time.Time
}

// ValidateStatus normalizes and validates one status report.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	if !SupportedBearer(normalized.Bearer) {
		return StatusInput{}, ErrInvalidInput
	}
	if !SupportedESIMState(normalized.ESIMState) {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.SignalPercent < 0 || normalized.SignalPercent > 100 {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.UsedMB < 0 || normalized.UsedMB > MaxQuotaMB {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	return normalized, nil
}

// Capability summarizes whether a device advertises a usable cellular radio.
type Capability struct {
	Supported       bool   `json:"supported"`
	Roaming         bool   `json:"roaming"`
	ESIMState       string `json:"esim_state"`
	SignalPercent   int    `json:"signal_percent"`
	QuotaMB         int    `json:"quota_mb"`
	UsedMB          int    `json:"used_mb"`
	QuotaExhausted  bool   `json:"quota_exhausted"`
	SchemaVersion   int    `json:"schema_version"`
	PolicyRevision  int    `json:"policy_revision"`
	AppliedRevision int    `json:"applied_revision"`
	UpToDate        bool   `json:"up_to_date"`
}

// DeviceStatus is one device row in the guardian connectivity overview.
type DeviceStatus struct {
	DeviceID      string    `json:"device_id"`
	FamilyID      string    `json:"family_id"`
	Bearer        string    `json:"bearer"`
	Roaming       bool      `json:"roaming"`
	ESIMState     string    `json:"esim_state"`
	SignalPercent int       `json:"signal_percent"`
	UsedMB        int       `json:"used_mb"`
	UpToDate      bool      `json:"up_to_date"`
	ReportedAt    time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide connectivity summary for the admin console.
type AdminStats struct {
	WindowDays     int       `json:"window_days"`
	DeviceCount    uint64    `json:"device_count"`
	CellularCount  uint64    `json:"cellular_count"`
	RoamingCount   uint64    `json:"roaming_count"`
	QuotaExhausted uint64    `json:"quota_exhausted_count"`
	AverageSignal  int       `json:"average_signal"`
	BearerBuckets  []Count   `json:"bearer_buckets"`
	ESIMBuckets    []Count   `json:"esim_buckets"`
	GeneratedAt    time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
