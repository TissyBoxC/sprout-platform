// Package domain defines the guardian image-privacy policy and the bounded
// per-device image pipeline status for the image_privacy module.
//
// The policy is per family and mirrors the firmware contract field for field:
// whether images are processed at all, whether upload is authorised, whether
// processing stays local, the processing mode, the upload scope, the retention
// window, the redaction level and the child-consent gate. The platform stores
// only the policy and bounded status scalars; no image bytes, thumbnails or
// face templates are ever persisted here, so the pipeline cannot become an
// image store or a tracking surface.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidPolicy reports a policy value outside the bounds.
	ErrInvalidPolicy = errors.New("invalid image privacy policy")
	// ErrInvalidInput reports a malformed image pipeline status report.
	ErrInvalidInput = errors.New("invalid image privacy input")
)

// SchemaVersion is the image-privacy contract version shared with the firmware
// component.
const SchemaVersion = 1

// Approved bounds, mirroring the firmware contract exactly.
const (
	MinRetentionDays = 0
	MaxRetentionDays = 30
	// MaxImageCount bounds one counter in a device status report.
	MaxImageCount = 1000000
)

// Default values served before a guardian edits the policy. Upload and any
// non-local processing default off so a device never uploads a frame without an
// explicit guardian decision.
const (
	DefaultEnabled             = false
	DefaultUploadAllowed       = false
	DefaultLocalProcessingOnly = true
	DefaultProcessingMode      = ModeMetadata
	DefaultUploadScope         = ScopeNone
	DefaultRetentionDays       = 0
	DefaultRedactionLevel      = RedactionStrict
	DefaultRequireChildConsent = true
)

// The fixed processing-mode catalog shared with the firmware.
const (
	ModeOff       = "off"
	ModeMetadata  = "metadata"
	ModeLocalFull = "local_full"
)

// The fixed upload-scope catalog shared with the firmware.
const (
	ScopeNone       = "none"
	ScopeThumbnails = "thumbnails"
	ScopeAnalysis   = "analysis"
)

// The fixed redaction-level catalog shared with the firmware.
const (
	RedactionNone     = "none"
	RedactionStandard = "standard"
	RedactionStrict   = "strict"
)

// The stable block reasons shared with the firmware.
const (
	BlockNone     = "none"
	BlockDisabled = "disabled"
	BlockConsent  = "consent"
	BlockScope    = "scope"
	BlockQuota    = "quota"
	BlockError    = "error"
)

// ProcessingModes is the ordered processing-mode catalog exposed to clients.
var ProcessingModes = []string{ModeOff, ModeMetadata, ModeLocalFull}

// UploadScopes is the ordered upload-scope catalog exposed to clients.
var UploadScopes = []string{ScopeNone, ScopeThumbnails, ScopeAnalysis}

// RedactionLevels is the ordered redaction-level catalog exposed to clients.
var RedactionLevels = []string{RedactionNone, RedactionStandard, RedactionStrict}

// BlockReasons is the ordered block-reason catalog exposed to clients.
var BlockReasons = []string{
	BlockNone,
	BlockDisabled,
	BlockConsent,
	BlockScope,
	BlockQuota,
	BlockError,
}

// Policy is one validated image-privacy policy for a family.
type Policy struct {
	SchemaVersion       int
	Enabled             bool
	UploadAllowed       bool
	LocalProcessingOnly bool
	ProcessingMode      string
	UploadScope         string
	RetentionDays       int
	RedactionLevel      string
	RequireChildConsent bool
	Revision            int
	UpdatedAt           time.Time
}

// Input is the editable policy payload accepted from a guardian.
type Input struct {
	Enabled             bool
	UploadAllowed       bool
	LocalProcessingOnly bool
	ProcessingMode      string
	UploadScope         string
	RetentionDays       int
	RedactionLevel      string
	RequireChildConsent bool
}

// Default returns the fallback policy used before a guardian edits one. The
// feature and upload stay off so the pipeline is opt-in.
func Default() Policy {
	return Policy{
		SchemaVersion:       SchemaVersion,
		Enabled:             DefaultEnabled,
		UploadAllowed:       DefaultUploadAllowed,
		LocalProcessingOnly: DefaultLocalProcessingOnly,
		ProcessingMode:      DefaultProcessingMode,
		UploadScope:         DefaultUploadScope,
		RetentionDays:       DefaultRetentionDays,
		RedactionLevel:      DefaultRedactionLevel,
		RequireChildConsent: DefaultRequireChildConsent,
	}
}

// IsProcessingMode reports whether a mode is in the fixed catalog.
func IsProcessingMode(mode string) bool {
	for _, candidate := range ProcessingModes {
		if candidate == mode {
			return true
		}
	}
	return false
}

// IsUploadScope reports whether a scope is in the fixed catalog.
func IsUploadScope(scope string) bool {
	for _, candidate := range UploadScopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

// IsRedactionLevel reports whether a redaction level is in the fixed catalog.
func IsRedactionLevel(level string) bool {
	for _, candidate := range RedactionLevels {
		if candidate == level {
			return true
		}
	}
	return false
}

// IsBlockReason reports whether a block reason is accepted.
func IsBlockReason(reason string) bool {
	for _, candidate := range BlockReasons {
		if candidate == reason {
			return true
		}
	}
	return false
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := Input{
		Enabled:             input.Enabled,
		UploadAllowed:       input.UploadAllowed,
		LocalProcessingOnly: input.LocalProcessingOnly,
		ProcessingMode:      strings.TrimSpace(input.ProcessingMode),
		UploadScope:         strings.TrimSpace(input.UploadScope),
		RetentionDays:       input.RetentionDays,
		RedactionLevel:      strings.TrimSpace(input.RedactionLevel),
		RequireChildConsent: input.RequireChildConsent,
	}
	if !IsProcessingMode(normalized.ProcessingMode) {
		return Input{}, ErrInvalidPolicy
	}
	if !IsUploadScope(normalized.UploadScope) {
		return Input{}, ErrInvalidPolicy
	}
	if !IsRedactionLevel(normalized.RedactionLevel) {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.RetentionDays < MinRetentionDays ||
		normalized.RetentionDays > MaxRetentionDays {
		return Input{}, ErrInvalidPolicy
	}
	// A device cannot upload while the feature is off or the upload is
	// unauthorised; the derived fields are pinned so a stale upload control never
	// survives the switch.
	if !normalized.Enabled {
		normalized.UploadAllowed = false
		normalized.UploadScope = ScopeNone
		normalized.ProcessingMode = ModeOff
	}
	if !normalized.UploadAllowed {
		normalized.UploadScope = ScopeNone
	}
	// Local-only processing is a hard constraint: it never coexists with an
	// upload scope.
	if normalized.LocalProcessingOnly {
		normalized.UploadAllowed = false
		normalized.UploadScope = ScopeNone
	}
	return normalized, nil
}

// StatusInput is one bounded device image pipeline status report.
type StatusInput struct {
	CameraSupported bool
	LocalProcessing bool
	UploadAllowed   bool
	ProcessingMode  string
	UploadScope     string
	RedactionLevel  string
	ProcessedCount  int
	UploadedCount   int
	BlockedCount    int
	LastBlockReason string
	PolicyRevision  int
}

// Status is the stored latest image pipeline status for one device.
type Status struct {
	DeviceID        string
	FamilyID        string
	CameraSupported bool
	LocalProcessing bool
	UploadAllowed   bool
	ProcessingMode  string
	UploadScope     string
	RedactionLevel  string
	ProcessedCount  int
	UploadedCount   int
	BlockedCount    int
	LastBlockReason string
	PolicyRevision  int
	ReportedAt      time.Time
}

// ValidateStatus normalizes and validates one device status report.
//
// A device that does not support the camera pipeline reports a fully off
// pipeline, so a stale counter never reads as an active upload.
func ValidateStatus(input StatusInput) (StatusInput, error) {
	normalized := input
	normalized.ProcessingMode = strings.TrimSpace(input.ProcessingMode)
	normalized.UploadScope = strings.TrimSpace(input.UploadScope)
	normalized.RedactionLevel = strings.TrimSpace(input.RedactionLevel)
	normalized.LastBlockReason = strings.TrimSpace(input.LastBlockReason)
	if !input.CameraSupported {
		normalized.LocalProcessing = false
		normalized.UploadAllowed = false
		normalized.ProcessingMode = ModeOff
		normalized.UploadScope = ScopeNone
		normalized.RedactionLevel = RedactionStrict
		normalized.ProcessedCount = 0
		normalized.UploadedCount = 0
		normalized.BlockedCount = 0
		normalized.LastBlockReason = BlockNone
	}
	if !IsProcessingMode(normalized.ProcessingMode) {
		return StatusInput{}, ErrInvalidInput
	}
	if !IsUploadScope(normalized.UploadScope) {
		return StatusInput{}, ErrInvalidInput
	}
	if !IsRedactionLevel(normalized.RedactionLevel) {
		return StatusInput{}, ErrInvalidInput
	}
	if !IsBlockReason(normalized.LastBlockReason) {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.ProcessedCount < 0 || normalized.ProcessedCount > MaxImageCount ||
		normalized.UploadedCount < 0 || normalized.UploadedCount > MaxImageCount ||
		normalized.BlockedCount < 0 || normalized.BlockedCount > MaxImageCount {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.UploadedCount > normalized.ProcessedCount {
		return StatusInput{}, ErrInvalidInput
	}
	if normalized.PolicyRevision < 0 {
		return StatusInput{}, ErrInvalidInput
	}
	// An upload scope only makes sense while upload is authorised; a device that
	// reports upload off is coerced to the empty scope.
	if !normalized.UploadAllowed {
		normalized.UploadScope = ScopeNone
	}
	return normalized, nil
}

// Capability summarizes whether a device supports the image pipeline and
// whether it has applied the current family policy revision.
type Capability struct {
	Supported       bool   `json:"supported"`
	LocalProcessing bool   `json:"local_processing"`
	UploadAllowed   bool   `json:"upload_allowed"`
	ProcessingMode  string `json:"processing_mode"`
	UploadScope     string `json:"upload_scope"`
	RedactionLevel  string `json:"redaction_level"`
	SchemaVersion   int    `json:"schema_version"`
	PolicyRevision  int    `json:"policy_revision"`
	AppliedRevision int    `json:"applied_revision"`
	UpToDate        bool   `json:"up_to_date"`
}

// DeviceStatus is one device row in the guardian and admin overview.
type DeviceStatus struct {
	DeviceID        string    `json:"device_id"`
	FamilyID        string    `json:"family_id"`
	CameraSupported bool      `json:"camera_supported"`
	LocalProcessing bool      `json:"local_processing"`
	UploadAllowed   bool      `json:"upload_allowed"`
	ProcessingMode  string    `json:"processing_mode"`
	UploadScope     string    `json:"upload_scope"`
	RedactionLevel  string    `json:"redaction_level"`
	ProcessedCount  int       `json:"processed_count"`
	UploadedCount   int       `json:"uploaded_count"`
	BlockedCount    int       `json:"blocked_count"`
	LastBlockReason string    `json:"last_block_reason"`
	UpToDate        bool      `json:"up_to_date"`
	ReportedAt      time.Time `json:"reported_at"`
}

// AdminStats is the fleet-wide image-privacy summary for the admin console.
type AdminStats struct {
	WindowDays         int       `json:"window_days"`
	DeviceCount        uint64    `json:"device_count"`
	CameraCount        uint64    `json:"camera_count"`
	LocalOnlyCount     uint64    `json:"local_only_count"`
	UploadAllowedCount uint64    `json:"upload_allowed_count"`
	OutdatedCount      uint64    `json:"outdated_count"`
	ProcessedTotal     uint64    `json:"processed_total"`
	UploadedTotal      uint64    `json:"uploaded_total"`
	BlockedTotal       uint64    `json:"blocked_total"`
	ModeBuckets        []Count   `json:"mode_buckets"`
	ReasonBuckets      []Count   `json:"reason_buckets"`
	GeneratedAt        time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}
