// Package domain defines the guardian motion and motor policy and the bounded
// device motion status contract for the motion module.
//
// The policy is per family: each family owns one editable policy that every
// bound device reads over its device session. Values are bounded here as well
// as in the service so a direct write cannot install an unsafe run time or an
// unrestricted speed, and locomotion stays off until a guardian turns it on.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidPolicy reports a motion value outside the approved bounds.
	ErrInvalidPolicy = errors.New("invalid motion policy")
	// ErrInvalidInput reports a malformed status report.
	ErrInvalidInput = errors.New("invalid motion input")
)

// SchemaVersion is the policy contract version shared with the firmware.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract.
const (
	MinSpeedPercent = 1
	MaxSpeedPercent = 100

	MinRuntimeMS = 200
	MaxRuntimeMS = 60000

	MaxStopGraceMS = 60000
)

// Default values served before a guardian edits the policy. Locomotion is off
// by default so an unconfigured body never walks on its own.
const (
	DefaultSpeedPercent = 60
	DefaultRuntimeMS    = 5000
	DefaultStopGraceMS  = 300
)

// State is the normalized motion lifecycle state reported by a device.
type State string

// The lifecycle states the platform recognises.
const (
	StateIdle    State = "idle"
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateTimeout State = "timeout"
	StateFault   State = "fault"
)

// Action is the normalized motion action reported by a device.
type Action string

// The actions the platform recognises.
const (
	ActionIdle    Action = "idle"
	ActionWalk    Action = "walk"
	ActionDance   Action = "dance"
	ActionTailWag Action = "tail_wag"
	ActionGesture Action = "gesture"
)

// SupportedState reports whether a lifecycle state is accepted.
func SupportedState(state State) bool {
	switch state {
	case StateIdle, StateRunning, StateStopped, StateTimeout, StateFault:
		return true
	default:
		return false
	}
}

// SupportedAction reports whether an action is accepted.
func SupportedAction(action Action) bool {
	switch action {
	case ActionIdle, ActionWalk, ActionDance, ActionTailWag, ActionGesture:
		return true
	default:
		return false
	}
}

// IsLocomotion reports whether an action moves the body through space.
func IsLocomotion(action Action) bool {
	return action == ActionWalk || action == ActionDance
}

// Policy is one validated motion and motor policy for a family.
type Policy struct {
	SchemaVersion   int
	Enabled         bool
	RequireStopAck  bool
	AllowLocomotion bool
	MaxSpeedPercent int
	MaxRuntimeMS    int
	StopGraceMS     int
	Revision        int
	UpdatedAt       time.Time
}

// Input is the editable policy payload accepted from a guardian.
type Input struct {
	Enabled         bool
	RequireStopAck  bool
	AllowLocomotion bool
	MaxSpeedPercent int
	MaxRuntimeMS    int
	StopGraceMS     int
}

// Default returns the fallback policy used before a guardian edits one.
func Default() Policy {
	return Policy{
		SchemaVersion:   SchemaVersion,
		Enabled:         true,
		RequireStopAck:  true,
		AllowLocomotion: false,
		MaxSpeedPercent: DefaultSpeedPercent,
		MaxRuntimeMS:    DefaultRuntimeMS,
		StopGraceMS:     DefaultStopGraceMS,
		Revision:        0,
	}
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := input
	if normalized.MaxSpeedPercent < MinSpeedPercent ||
		normalized.MaxSpeedPercent > MaxSpeedPercent {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.MaxRuntimeMS < MinRuntimeMS ||
		normalized.MaxRuntimeMS > MaxRuntimeMS {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.StopGraceMS < 0 || normalized.StopGraceMS > MaxStopGraceMS {
		return Input{}, ErrInvalidPolicy
	}
	return normalized, nil
}

// StatusInput is one bounded device motion status report.
type StatusInput struct {
	State          State
	Action         Action
	ActuatorCount  int
	Faulted        bool
	PolicyRevision int
}

// Status is the stored latest motion status for one device.
type Status struct {
	DeviceID       string
	FamilyID       string
	State          State
	Action         Action
	ActuatorCount  int
	Faulted        bool
	PolicyRevision int
	ReportedAt     time.Time
}

// ValidateStatus normalizes and validates one status report.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	if !SupportedState(normalized.State) {
		return StatusInput{}, ErrInvalidInput
	}
	if !SupportedAction(normalized.Action) {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.ActuatorCount < 0 || normalized.ActuatorCount > 32 {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	return normalized, nil
}

// Capability summarizes whether a device advertises a usable motion body.
type Capability struct {
	Supported       bool   `json:"supported"`
	State           string `json:"state"`
	Action          string `json:"action"`
	ActuatorCount   int    `json:"actuator_count"`
	Faulted         bool   `json:"faulted"`
	AllowLocomotion bool   `json:"allow_locomotion"`
	SchemaVersion   int    `json:"schema_version"`
	PolicyRevision  int    `json:"policy_revision"`
	AppliedRevision int    `json:"applied_revision"`
	UpToDate        bool   `json:"up_to_date"`
}

// DeviceStatus is one device row in the guardian motion overview.
type DeviceStatus struct {
	DeviceID      string    `json:"device_id"`
	FamilyID      string    `json:"family_id"`
	State         string    `json:"state"`
	Action        string    `json:"action"`
	ActuatorCount int       `json:"actuator_count"`
	Faulted       bool      `json:"faulted"`
	UpToDate      bool      `json:"up_to_date"`
	ReportedAt    time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide motion summary for the admin console.
type AdminStats struct {
	WindowDays    int       `json:"window_days"`
	DeviceCount   uint64    `json:"device_count"`
	RunningCount  uint64    `json:"running_count"`
	FaultedCount  uint64    `json:"faulted_count"`
	IdleCount     uint64    `json:"idle_count"`
	ActionBuckets []Count   `json:"action_buckets"`
	StateBuckets  []Count   `json:"state_buckets"`
	GeneratedAt   time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
