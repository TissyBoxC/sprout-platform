// Package domain defines the guardian Bluetooth audio policy and the bounded
// per-device Bluetooth adapter status for the bluetooth_audio module.
//
// The policy is per family and mirrors the firmware contract field for field:
// whether Bluetooth audio is enabled, whether the device may act as a speaker,
// whether a new pairing is allowed, auto-reconnect, the preferred codec and the
// volume ceiling. The platform stores only the policy and bounded status
// scalars; no audio, pairing keys or peer addresses are ever persisted here, so
// Bluetooth never becomes a silent capture or tracking surface.
package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrInvalidPolicy reports a policy value outside the bounds.
	ErrInvalidPolicy = errors.New("invalid bluetooth audio policy")
	// ErrInvalidInput reports a malformed device status report.
	ErrInvalidInput = errors.New("invalid bluetooth audio input")
)

// SchemaVersion is the Bluetooth audio contract version shared with the
// firmware component.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract exactly.
const (
	DisplayNameMaxLength = 48
	MaxPairedDevices     = 16
)

// Default values served before a guardian edits the policy. Bluetooth audio and
// pairing default to off so a device never advertises or accepts a pairing
// without an explicit guardian decision.
const (
	DefaultEnabled            = false
	DefaultSpeakerModeAllowed = true
	DefaultPairingAllowed     = false
	DefaultAutoReconnect      = true
	DefaultPreferredCodec     = CodecAuto
	DefaultMaxVolumePercent   = 70
	DefaultDisplayName        = ""
)

// The bounded preferred codec catalog shared with the firmware.
const (
	CodecAuto = "auto"
	CodecSBC  = "sbc"
	CodecAAC  = "aac"
	CodecLDAC = "ldac"
)

// The normalized Bluetooth adapter states shared with the firmware.
const (
	AdapterOff         = "off"
	AdapterIdle        = "idle"
	AdapterAdvertising = "advertising"
	AdapterConnecting  = "connecting"
	AdapterConnected   = "connected"
	AdapterError       = "error"
)

// The stable disconnect reasons shared with the firmware.
const (
	DisconnectNone   = "none"
	DisconnectUser   = "user"
	DisconnectRange  = "range"
	DisconnectPeer   = "peer"
	DisconnectPolicy = "policy"
	DisconnectError  = "error"
)

// The bounded active codec catalog. Unlike the preferred codec, an active codec
// is "unknown" until a link is established.
const (
	ActiveCodecUnknown = "unknown"
	ActiveCodecSBC     = "sbc"
	ActiveCodecAAC     = "aac"
	ActiveCodecLDAC    = "ldac"
)

// PreferredCodecs is the ordered preferred-codec catalog exposed to clients.
var PreferredCodecs = []string{CodecAuto, CodecSBC, CodecAAC, CodecLDAC}

// ActiveCodecs is the ordered active-codec catalog exposed to clients.
var ActiveCodecs = []string{
	ActiveCodecUnknown,
	ActiveCodecSBC,
	ActiveCodecAAC,
	ActiveCodecLDAC,
}

// AdapterStates is the ordered adapter state catalog exposed to clients.
var AdapterStates = []string{
	AdapterOff,
	AdapterIdle,
	AdapterAdvertising,
	AdapterConnecting,
	AdapterConnected,
	AdapterError,
}

// DisconnectReasons is the ordered disconnect reason catalog exposed to clients.
var DisconnectReasons = []string{
	DisconnectNone,
	DisconnectUser,
	DisconnectRange,
	DisconnectPeer,
	DisconnectPolicy,
	DisconnectError,
}

// Policy is one validated Bluetooth audio policy for a family.
type Policy struct {
	SchemaVersion      int
	Enabled            bool
	SpeakerModeAllowed bool
	PairingAllowed     bool
	AutoReconnect      bool
	PreferredCodec     string
	MaxVolumePercent   int
	DisplayName        string
	Revision           int
	UpdatedAt          time.Time
}

// Input is the editable policy payload accepted from a guardian.
type Input struct {
	Enabled            bool
	SpeakerModeAllowed bool
	PairingAllowed     bool
	AutoReconnect      bool
	PreferredCodec     string
	MaxVolumePercent   int
	DisplayName        string
}

// Default returns the fallback policy used before a guardian edits one.
// Bluetooth audio and pairing stay off so the feature is opt-in.
func Default() Policy {
	return Policy{
		SchemaVersion:      SchemaVersion,
		Enabled:            DefaultEnabled,
		SpeakerModeAllowed: DefaultSpeakerModeAllowed,
		PairingAllowed:     DefaultPairingAllowed,
		AutoReconnect:      DefaultAutoReconnect,
		PreferredCodec:     DefaultPreferredCodec,
		MaxVolumePercent:   DefaultMaxVolumePercent,
		DisplayName:        DefaultDisplayName,
	}
}

// IsPreferredCodec reports whether a codec is in the fixed preferred catalog.
func IsPreferredCodec(codec string) bool {
	for _, candidate := range PreferredCodecs {
		if candidate == codec {
			return true
		}
	}
	return false
}

// IsActiveCodec reports whether a codec is in the fixed active catalog.
func IsActiveCodec(codec string) bool {
	for _, candidate := range ActiveCodecs {
		if candidate == codec {
			return true
		}
	}
	return false
}

// IsAdapterState reports whether an adapter state is accepted.
func IsAdapterState(state string) bool {
	for _, candidate := range AdapterStates {
		if candidate == state {
			return true
		}
	}
	return false
}

// IsDisconnectReason reports whether a disconnect reason is accepted.
func IsDisconnectReason(reason string) bool {
	for _, candidate := range DisconnectReasons {
		if candidate == reason {
			return true
		}
	}
	return false
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := Input{
		Enabled:            input.Enabled,
		SpeakerModeAllowed: input.SpeakerModeAllowed,
		PairingAllowed:     input.PairingAllowed,
		AutoReconnect:      input.AutoReconnect,
		PreferredCodec:     strings.TrimSpace(input.PreferredCodec),
		MaxVolumePercent:   input.MaxVolumePercent,
		DisplayName:        strings.TrimSpace(input.DisplayName),
	}
	if !IsPreferredCodec(normalized.PreferredCodec) {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.MaxVolumePercent < 0 || normalized.MaxVolumePercent > 100 {
		return Input{}, ErrInvalidPolicy
	}
	if utf8.RuneCountInString(normalized.DisplayName) > DisplayNameMaxLength {
		return Input{}, ErrInvalidPolicy
	}
	// A device cannot advertise as a speaker while the feature is off; keeping
	// the derived flag false here prevents a stale speaker control.
	if !normalized.Enabled {
		normalized.SpeakerModeAllowed = false
		normalized.PairingAllowed = false
	}
	return normalized, nil
}

// StatusInput is one bounded device adapter status report.
type StatusInput struct {
	BluetoothSupported   bool
	SpeakerMode          bool
	AdapterState         string
	PairedDeviceCount    int
	ConnectedDeviceCount int
	ActiveCodec          string
	VolumePercent        int
	LastDisconnectReason string
	PolicyRevision       int
}

// Status is the stored latest adapter status for one device.
type Status struct {
	DeviceID             string
	FamilyID             string
	BluetoothSupported   bool
	SpeakerMode          bool
	AdapterState         string
	PairedDeviceCount    int
	ConnectedDeviceCount int
	ActiveCodec          string
	VolumePercent        int
	LastDisconnectReason string
	PolicyRevision       int
	ReportedAt           time.Time
}

// ValidateStatus normalizes and validates one device status report.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	normalized.AdapterState = strings.TrimSpace(input.AdapterState)
	normalized.ActiveCodec = strings.TrimSpace(input.ActiveCodec)
	normalized.LastDisconnectReason = strings.TrimSpace(input.LastDisconnectReason)
	// A device that does not support Bluetooth audio reports a fully off
	// adapter, so a stale state never reads as an active speaker.
	if !input.BluetoothSupported {
		normalized.SpeakerMode = false
		normalized.AdapterState = AdapterOff
		normalized.PairedDeviceCount = 0
		normalized.ConnectedDeviceCount = 0
		normalized.ActiveCodec = ActiveCodecUnknown
		normalized.VolumePercent = 0
		normalized.LastDisconnectReason = DisconnectNone
	}
	if !IsAdapterState(normalized.AdapterState) {
		return StatusInput{}, ErrInvalidInput
	}
	if !IsActiveCodec(normalized.ActiveCodec) {
		return StatusInput{}, ErrInvalidInput
	}
	if !IsDisconnectReason(normalized.LastDisconnectReason) {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PairedDeviceCount < 0 ||
		normalized.PairedDeviceCount > MaxPairedDevices {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.ConnectedDeviceCount < 0 ||
		normalized.ConnectedDeviceCount > MaxPairedDevices ||
		normalized.ConnectedDeviceCount > normalized.PairedDeviceCount {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.VolumePercent < 0 || normalized.VolumePercent > 100 {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	// A speaker mode report only makes sense while the adapter is connected.
	if normalized.SpeakerMode && normalized.AdapterState != AdapterConnected {
		normalized.SpeakerMode = false
	}
	return normalized, nil
}

// Capability summarizes whether a device supports Bluetooth audio and whether
// it has applied the current family policy revision.
type Capability struct {
	Supported       bool   `json:"supported"`
	SpeakerMode     bool   `json:"speaker_mode"`
	AdapterState    string `json:"adapter_state"`
	ActiveCodec     string `json:"active_codec"`
	VolumePercent   int    `json:"volume_percent"`
	SchemaVersion   int    `json:"schema_version"`
	PolicyRevision  int    `json:"policy_revision"`
	AppliedRevision int    `json:"applied_revision"`
	UpToDate        bool   `json:"up_to_date"`
}

// DeviceStatus is one device row in the guardian and admin Bluetooth overview.
type DeviceStatus struct {
	DeviceID             string    `json:"device_id"`
	FamilyID             string    `json:"family_id"`
	BluetoothSupported   bool      `json:"bluetooth_supported"`
	SpeakerMode          bool      `json:"speaker_mode"`
	AdapterState         string    `json:"adapter_state"`
	PairedDeviceCount    int       `json:"paired_device_count"`
	ConnectedDeviceCount int       `json:"connected_device_count"`
	ActiveCodec          string    `json:"active_codec"`
	VolumePercent        int       `json:"volume_percent"`
	LastDisconnectReason string    `json:"last_disconnect_reason"`
	UpToDate             bool      `json:"up_to_date"`
	ReportedAt           time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide Bluetooth audio summary for the admin console.
type AdminStats struct {
	WindowDays        int       `json:"window_days"`
	DeviceCount       uint64    `json:"device_count"`
	SupportedCount    uint64    `json:"supported_count"`
	SpeakerModeCount  uint64    `json:"speaker_mode_count"`
	ConnectedCount    uint64    `json:"connected_count"`
	PairedDeviceTotal uint64    `json:"paired_device_total"`
	OutdatedCount     uint64    `json:"outdated_count"`
	StateBuckets      []Count   `json:"state_buckets"`
	CodecBuckets      []Count   `json:"codec_buckets"`
	GeneratedAt       time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
