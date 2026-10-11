// Package domain defines the video-companionship contacts, calls and device
// call status for the video_companion module.
//
// A family owns the contacts a guardian can call and a bounded call history.
// The platform never stores media: only call metadata, the negotiated quality
// band and the end reason, so a revoked camera or microphone permission ends
// the session without any silent capture.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidContact reports a malformed contact.
	ErrInvalidContact = errors.New("invalid video contact")
	// ErrContactNotFound reports a missing contact.
	ErrContactNotFound = errors.New("video contact not found")
	// ErrInvalidCall reports a malformed call request or report.
	ErrInvalidCall = errors.New("invalid video call")
	// ErrCallNotFound reports a missing call.
	ErrCallNotFound = errors.New("video call not found")
)

// SchemaVersion is the media-negotiation contract version shared with the
// firmware and the gateway.
const SchemaVersion = 1

// Contact roles the platform recognises.
const (
	RoleGuardian = "guardian"
	RoleFamily   = "family"
	RoleDevice   = "device"
)

// Call states the platform recognises.
const (
	StateRinging  = "ringing"
	StateActive   = "active"
	StateEnded    = "ended"
	StateRejected = "rejected"
	StateMissed   = "missed"
	StateFailed   = "failed"
)

// Quality bands the platform recognises.
const (
	QualityUnknown = "unknown"
	QualityGood    = "good"
	QualityFair    = "fair"
	QualityPoor    = "poor"
)

// End reasons the platform recognises. A permission revocation and a device
// going offline are first-class reasons so a call never ends ambiguously.
const (
	EndHangup            = "hangup"
	EndRejected          = "rejected"
	EndMissed            = "missed"
	EndTimeout           = "timeout"
	EndNetwork           = "network"
	EndPermissionRevoked = "permission_revoked"
	EndDeviceOffline     = "device_offline"
)

// Initiators the platform recognises.
const (
	InitiatorGuardian = "guardian"
	InitiatorDevice   = "device"
)

// ContactNameMaxLength bounds a contact display name.
const ContactNameMaxLength = 40

// CallHistoryDefaultLimit and CallHistoryMaxLimit bound a history read.
const (
	CallHistoryDefaultLimit = 50
	CallHistoryMaxLimit     = 200
)

// SupportedRole reports whether a contact role is accepted.
func SupportedRole(role string) bool {
	switch role {
	case RoleGuardian, RoleFamily, RoleDevice:
		return true
	default:
		return false
	}
}

// SupportedState reports whether a call state is accepted.
func SupportedState(state string) bool {
	switch state {
	case StateRinging, StateActive, StateEnded, StateRejected, StateMissed, StateFailed:
		return true
	default:
		return false
	}
}

// SupportedQuality reports whether a quality band is accepted.
func SupportedQuality(quality string) bool {
	switch quality {
	case QualityUnknown, QualityGood, QualityFair, QualityPoor:
		return true
	default:
		return false
	}
}

// SupportedEndReason reports whether an end reason is accepted.
func SupportedEndReason(reason string) bool {
	switch reason {
	case EndHangup, EndRejected, EndMissed, EndTimeout, EndNetwork,
		EndPermissionRevoked, EndDeviceOffline:
		return true
	default:
		return false
	}
}

// Contact is one guardian-managed video contact.
type Contact struct {
	ID          string
	FamilyID    string
	ChildID     string
	DisplayName string
	Role        string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ContactInput is one editable contact payload.
type ContactInput struct {
	ChildID     string
	DisplayName string
	Role        string
	Enabled     bool
}

// Call is one stored call metadata row.
type Call struct {
	ID         string
	FamilyID   string
	DeviceID   string
	ContactID  string
	Initiator  string
	State      string
	Quality    string
	EndReason  string
	StartedAt  time.Time
	AnsweredAt *time.Time
	EndedAt    *time.Time
}

// CallInput is one request to start a call.
type CallInput struct {
	DeviceID  string
	ContactID string
	Initiator string
}

// CallStatusInput is one bounded device call status report.
type CallStatusInput struct {
	CallState         string
	Quality           string
	CameraAllowed     bool
	MicrophoneAllowed bool
	LastCallID        string
}

// DeviceStatus is the stored latest video status for one device.
type DeviceStatus struct {
	DeviceID          string
	FamilyID          string
	CallState         string
	Quality           string
	CameraAllowed     bool
	MicrophoneAllowed bool
	LastCallID        string
	ReportedAt        time.Time
}

// Capability summarizes whether a device can place a video call.
type Capability struct {
	Supported         bool   `json:"supported"`
	CallState         string `json:"call_state"`
	Quality           string `json:"quality"`
	CameraAllowed     bool   `json:"camera_allowed"`
	MicrophoneAllowed bool   `json:"microphone_allowed"`
	SchemaVersion     int    `json:"schema_version"`
}

// DeviceCallStatus is one device row in the guardian call overview.
type DeviceCallStatus struct {
	DeviceID          string    `json:"device_id"`
	FamilyID          string    `json:"family_id"`
	CallState         string    `json:"call_state"`
	Quality           string    `json:"quality"`
	CameraAllowed     bool      `json:"camera_allowed"`
	MicrophoneAllowed bool      `json:"microphone_allowed"`
	ReportedAt        time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide video-companionship summary for the admin
// console.
type AdminStats struct {
	WindowDays       int       `json:"window_days"`
	CallCount        uint64    `json:"call_count"`
	ActiveCount      uint64    `json:"active_count"`
	EndedCount       uint64    `json:"ended_count"`
	MissedCount      uint64    `json:"missed_count"`
	RejectedCount    uint64    `json:"rejected_count"`
	FailedCount      uint64    `json:"failed_count"`
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

// ValidateContactInput normalizes and validates one contact payload.
func ValidateContactInput(input ContactInput) (ContactInput, error) {
	normalized := input
	if len([]rune(normalized.DisplayName)) == 0 ||
		len([]rune(normalized.DisplayName)) > ContactNameMaxLength {
		return ContactInput{}, ErrInvalidContact
	}
	if !SupportedRole(normalized.Role) {
		return ContactInput{}, ErrInvalidContact
	}
	return normalized, nil
}

// ValidateCallInput normalizes and validates one call request.
func ValidateCallInput(input CallInput) (CallInput, error) {
	normalized := input
	if normalized.DeviceID == "" {
		return CallInput{}, ErrInvalidCall
	}
	if normalized.Initiator != InitiatorGuardian &&
		normalized.Initiator != InitiatorDevice {
		return CallInput{}, ErrInvalidCall
	}
	return normalized, nil
}

// ValidateCallStatus normalizes and validates one device status report.
func ValidateCallStatus(input CallStatusInput) (CallStatusInput, error) {
	normalized := input
	if !SupportedState(normalized.CallState) {
		return CallStatusInput{}, ErrInvalidCall
	}
	if !SupportedQuality(normalized.Quality) {
		return CallStatusInput{}, ErrInvalidCall
	}
	return normalized, nil
}
