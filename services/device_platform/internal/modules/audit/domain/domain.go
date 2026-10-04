// Package domain contains device diagnostic event types and projections.
//
// Diagnostics are a bounded, non-sensitive subset of device state. They never
// carry credentials, media, child content, or provider payloads. The module
// owns startup history, module failures, recovery events, and the operator
// facing aggregate used by the brand management console.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidDiagnostics rejects a malformed or out-of-contract payload.
	ErrInvalidDiagnostics = errors.New("invalid device diagnostics")
	// ErrEventNotFound means no diagnostic records exist for the device.
	ErrEventNotFound = errors.New("device diagnostic event not found")
)

// Diagnostic event types. The values are stable contract strings shared with
// the firmware heartbeat extension and the admin console.
const (
	EventTypeBoot      = "boot"
	EventTypeFailure   = "module_failure"
	EventTypeRecovered = "module_recovered"
)

// Interaction event types. These mirror the values that the wake word,
// button, indicator, and factory reset modules emit on the device. They are
// stable contract strings shared with the firmware heartbeat extension and
// the brand management console.
const (
	InteractionEventWakeDetected          = "wake_detected"
	InteractionEventWakeRejected          = "wake_rejected"
	InteractionEventButtonGesture         = "button_gesture"
	InteractionEventIndicatorState        = "indicator_state"
	InteractionEventFactoryResetRequested = "factory_reset_requested"
	InteractionEventFactoryResetCancelled = "factory_reset_cancelled"
	InteractionEventFactoryResetCompleted = "factory_reset_completed"
	InteractionEventFactoryResetFailed    = "factory_reset_failed"
)

// Health state values derived from the newest failure and recovery events.
const (
	HealthHealthy  = "healthy"
	HealthDegraded = "degraded"
	HealthFaulted  = "faulted"
	HealthUnknown  = "unknown"
)

// BootEvent is one startup record reported by a device.
type BootEvent struct {
	EventType       string    `json:"event_type"`
	EventID         string    `json:"event_id"`
	Sequence        uint64    `json:"sequence"`
	UptimeMS        uint64    `json:"uptime_ms"`
	BootCount       uint64    `json:"boot_count"`
	ResetReason     string    `json:"reset_reason"`
	FirmwareVersion string    `json:"firmware_version"`
	ReportedAt      time.Time `json:"reported_at"`
}

// ModuleFailure is the latest removable-module failure reported by a device.
type ModuleFailure struct {
	EventType       string    `json:"event_type"`
	EventID         string    `json:"event_id"`
	Sequence        uint64    `json:"sequence"`
	ModuleName      string    `json:"module_name"`
	ErrorCode       string    `json:"error_code"`
	FailureCount    uint64    `json:"failure_count"`
	FirmwareVersion string    `json:"firmware_version"`
	ReportedAt      time.Time `json:"reported_at"`
}

// RecoveryEvent records that a previously failing module reported a
// successful state again, so operators can distinguish ongoing faults from
// transient ones.
type RecoveryEvent struct {
	EventType       string    `json:"event_type"`
	EventID         string    `json:"event_id"`
	Sequence        uint64    `json:"sequence"`
	ModuleName      string    `json:"module_name"`
	FirmwareVersion string    `json:"firmware_version"`
	ReportedAt      time.Time `json:"reported_at"`
}

// InteractionEvent is one bounded user-visible device interaction. The
// detail code is a stable symbolic value such as the trigger word, the button
// gesture, the indicator state, or the reset reason; it never contains free
// text, audio, or child data.
type InteractionEvent struct {
	EventType       string    `json:"event_type"`
	EventID         string    `json:"event_id"`
	Sequence        uint64    `json:"sequence"`
	DetailCode      string    `json:"detail_code"`
	DurationMS      uint64    `json:"duration_ms"`
	FirmwareVersion string    `json:"firmware_version"`
	ReportedAt      time.Time `json:"reported_at"`
}

// Diagnostics is the optional heartbeat extension accepted by the platform.
type Diagnostics struct {
	SchemaVersion     string             `json:"schema_version"`
	NewestSequence    uint64             `json:"newest_sequence"`
	DroppedBootEvents uint64             `json:"dropped_boot_events"`
	BootEvents        []BootEvent        `json:"boot_events"`
	LatestFailure     *ModuleFailure     `json:"latest_failure"`
	RecoveryEvents    []RecoveryEvent    `json:"recovery_events"`
	InteractionEvents []InteractionEvent `json:"interaction_events"`
}

// Snapshot is the administrator-facing diagnostic history for one device.
type Snapshot struct {
	DeviceID             string             `json:"device_id"`
	BootEvents           []BootEvent        `json:"boot_events"`
	Failures             []ModuleFailure    `json:"failures"`
	RecoveryEvents       []RecoveryEvent    `json:"recovery_events"`
	InteractionEvents    []InteractionEvent `json:"interaction_events"`
	LatestFailure        *ModuleFailure     `json:"latest_failure"`
	ErrorCount           uint64             `json:"error_count"`
	RecoveryCount        uint64             `json:"recovery_count"`
	UpdatedAt            time.Time          `json:"updated_at"`
	HealthState          string             `json:"health_state"`
	RetentionBoot        int                `json:"retention_boot_events"`
	RetentionFailures    int                `json:"retention_failures"`
	RetentionRecovery    int                `json:"retention_recovery_events"`
	RetentionInteraction int                `json:"retention_interaction_events"`
}
