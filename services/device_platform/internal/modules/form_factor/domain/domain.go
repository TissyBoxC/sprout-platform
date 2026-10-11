// Package domain defines the capability-composed form-factor contract for the
// form_factor module.
//
// A form factor is named technically and never binds to a character or an
// appearance. A device reports the capability mask its body exposes; the
// platform composes and stores the action mask from it, so swapping a body only
// swaps the profile and its assets, never the voice, content or business
// modules.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidProfile reports a malformed form-factor report.
	ErrInvalidProfile = errors.New("invalid form factor profile")
)

// SchemaVersion is the profile contract version shared with the firmware.
const SchemaVersion = 1

// Capability bit masks shared with the firmware.
const (
	CapDisplay    uint32 = 1 << 0
	CapCamera     uint32 = 1 << 1
	CapMicrophone uint32 = 1 << 2
	CapSpeaker    uint32 = 1 << 3
	CapTouch      uint32 = 1 << 4
	CapLocomotion uint32 = 1 << 5
	CapServo      uint32 = 1 << 6
	CapCellular   uint32 = 1 << 7
	CapBattery    uint32 = 1 << 8
	CapWearable   uint32 = 1 << 9

	// CapAll is every capability bit the platform recognises.
	CapAll uint32 = 0x3FF
)

// Action bit masks shared with the firmware.
const (
	ActionExpression   uint32 = 1 << 0
	ActionVideoDisplay uint32 = 1 << 1
	ActionTouch        uint32 = 1 << 2
	ActionWalk         uint32 = 1 << 3
	ActionDance        uint32 = 1 << 4
	ActionTail         uint32 = 1 << 5
	ActionGesture      uint32 = 1 << 6
	ActionVoice        uint32 = 1 << 7
	ActionVideoCall    uint32 = 1 << 8
	ActionLocation     uint32 = 1 << 9

	// ActionAll is every action bit the platform recognises.
	ActionAll uint32 = 0x3FF
)

// MaxNameLength is the longest accepted technical name in runes.
const MaxNameLength = 23

// Capability is one named capability bit.
type Capability struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Action is one named action bit.
type Action struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Profile is one composed form-factor profile reported by a device.
type Profile struct {
	DeviceID       string
	FamilyID       string
	TechnicalName  string
	CapabilityMask uint32
	ActionMask     uint32
	SchemaVersion  int
	ReportedAt     time.Time
}

// Input is one bounded form-factor report accepted from a device.
type Input struct {
	TechnicalName  string
	CapabilityMask uint32
	SchemaVersion  int
}

// CapabilityName returns the stable key for one capability bit.
func CapabilityName(bit uint32) string {
	switch bit {
	case CapDisplay:
		return "display"
	case CapCamera:
		return "camera"
	case CapMicrophone:
		return "microphone"
	case CapSpeaker:
		return "speaker"
	case CapTouch:
		return "touch"
	case CapLocomotion:
		return "locomotion"
	case CapServo:
		return "servo"
	case CapCellular:
		return "cellular"
	case CapBattery:
		return "battery"
	case CapWearable:
		return "wearable"
	default:
		return "unknown"
	}
}

// ActionName returns the stable key for one action bit.
func ActionName(bit uint32) string {
	switch bit {
	case ActionExpression:
		return "expression"
	case ActionVideoDisplay:
		return "video_display"
	case ActionTouch:
		return "touch"
	case ActionWalk:
		return "walk"
	case ActionDance:
		return "dance"
	case ActionTail:
		return "tail"
	case ActionGesture:
		return "gesture"
	case ActionVoice:
		return "voice"
	case ActionVideoCall:
		return "video_call"
	case ActionLocation:
		return "location"
	default:
		return "unknown"
	}
}

// ComposeActions returns the action mask implied by a capability mask.
//
// This is the single source of truth and mirrors the firmware composition
// exactly: a locomotion body gains walk, dance and gesture; a servo gains a tail
// and gesture; a display gains expressions and video; a microphone and speaker
// together gain voice; a camera and display together gain a video call; and a
// wearable gains location.
func ComposeActions(capabilityMask uint32) uint32 {
	caps := capabilityMask & CapAll
	var actions uint32
	if caps&CapDisplay != 0 {
		actions |= ActionExpression | ActionVideoDisplay
	}
	if caps&CapTouch != 0 {
		actions |= ActionTouch
	}
	if caps&CapLocomotion != 0 {
		actions |= ActionWalk | ActionDance | ActionGesture
	}
	if caps&CapServo != 0 {
		actions |= ActionTail | ActionGesture
	}
	voiceInputs := CapMicrophone | CapSpeaker
	if caps&voiceInputs == voiceInputs {
		actions |= ActionVoice
	}
	if caps&CapCamera != 0 && caps&CapDisplay != 0 {
		actions |= ActionVideoCall
	}
	if caps&CapWearable != 0 {
		actions |= ActionLocation
	}
	return actions
}

// Capabilities returns every capability in a mask in a stable order.
func Capabilities(mask uint32) []Capability {
	ordered := []uint32{
		CapDisplay,
		CapCamera,
		CapMicrophone,
		CapSpeaker,
		CapTouch,
		CapLocomotion,
		CapServo,
		CapCellular,
		CapBattery,
		CapWearable,
	}
	result := make([]Capability, 0, len(ordered))
	for _, bit := range ordered {
		if mask&bit != 0 {
			key := CapabilityName(bit)
			result = append(result, Capability{Key: key, Name: key})
		}
	}
	return result
}

// Actions returns every action in a mask in a stable order.
func Actions(mask uint32) []Action {
	ordered := []uint32{
		ActionExpression,
		ActionVideoDisplay,
		ActionTouch,
		ActionWalk,
		ActionDance,
		ActionTail,
		ActionGesture,
		ActionVoice,
		ActionVideoCall,
		ActionLocation,
	}
	result := make([]Action, 0, len(ordered))
	for _, bit := range ordered {
		if mask&bit != 0 {
			key := ActionName(bit)
			result = append(result, Action{Key: key, Name: key})
		}
	}
	return result
}

// ValidateInput normalizes and validates one form-factor report.
func ValidateInput(input Input) (Input, error) {
	normalized := input
	if len([]rune(normalized.TechnicalName)) == 0 ||
		len([]rune(normalized.TechnicalName)) > MaxNameLength {
		return Input{}, ErrInvalidProfile
	}
	if normalized.SchemaVersion != SchemaVersion {
		return Input{}, ErrInvalidProfile
	}
	if normalized.CapabilityMask&^CapAll != 0 {
		return Input{}, ErrInvalidProfile
	}
	return normalized, nil
}

// DeviceProfile is one device row in the form-factor overview.
type DeviceProfile struct {
	DeviceID       string       `json:"device_id"`
	FamilyID       string       `json:"family_id"`
	TechnicalName  string       `json:"technical_name"`
	CapabilityMask uint32       `json:"capability_mask"`
	ActionMask     uint32       `json:"action_mask"`
	Capabilities   []Capability `json:"capabilities"`
	Actions        []Action     `json:"actions"`
	SchemaVersion  int          `json:"schema_version"`
	ReportedAt     time.Time    `json:"reported_at"`
}

// Capability summarizes whether a device advertises a known form factor.
type CapabilitySummary struct {
	Supported      bool         `json:"supported"`
	TechnicalName  string       `json:"technical_name"`
	CapabilityMask uint32       `json:"capability_mask"`
	ActionMask     uint32       `json:"action_mask"`
	Capabilities   []Capability `json:"capabilities"`
	Actions        []Action     `json:"actions"`
	SchemaVersion  int          `json:"schema_version"`
}

// AdminStats is the fleet-wide form-factor summary for the admin console.
type AdminStats struct {
	WindowDays       int       `json:"window_days"`
	DeviceCount      uint64    `json:"device_count"`
	LocomotionCount  uint64    `json:"locomotion_count"`
	VoiceCount       uint64    `json:"voice_count"`
	VideoCallCount   uint64    `json:"video_call_count"`
	WearableCount    uint64    `json:"wearable_count"`
	NameBuckets      []Count   `json:"name_buckets"`
	CapabilityCounts []Count   `json:"capability_counts"`
	GeneratedAt      time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
