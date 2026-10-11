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
	EventTypeCrash     = "crash"
)

// Crash fault causes. These are symbolic categories derived on-device from the
// reset reason and panic type. They are stable contract strings shared with the
// firmware crash reporter and the admin console. A raw memory dump is never
// part of the contract: only a bounded, redacted signature is transmitted.
const (
	CrashCausePanic             = "panic"
	CrashCauseWatchdog          = "watchdog"
	CrashCauseTaskWatchdog      = "task_watchdog"
	CrashCauseInterruptWatchdog = "interrupt_watchdog"
	CrashCauseBrownout          = "brownout"
	CrashCauseStackOverflow     = "stack_overflow"
	CrashCauseHeapCorruption    = "heap_corruption"
	CrashCauseAssert            = "assert"
	CrashCauseUnknown           = "unknown"
)

// CrashFaultKind values classify the faulting context narrowly so operators
// can triage without any child data.
const (
	CrashFaultKindException = "exception"
	CrashFaultKindWatchdog  = "watchdog"
	CrashFaultKindPowerLoss = "power_loss"
	CrashFaultKindMemory    = "memory"
	CrashFaultKindSoftware  = "software"
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

// Provisioning event types. These mirror the network bring-up steps the
// firmware reports while it configures Wi-Fi, binds a guardian, synchronizes
// time, and maintains its device session. The detail code carries a symbolic
// value only; SSIDs, passwords, and keys never appear in the audit trail.
const (
	ProvisioningEventStarted            = "provisioning_started"
	ProvisioningEventWiFiConfigured     = "wifi_configured"
	ProvisioningEventWiFiFailed         = "wifi_failed"
	ProvisioningEventBindingCompleted   = "binding_completed"
	ProvisioningEventBindingRemoved     = "binding_removed"
	ProvisioningEventNetworkReconnected = "network_reconnected"
	ProvisioningEventNetworkLost        = "network_lost"
	ProvisioningEventTimeSynced         = "time_synced"
	ProvisioningEventAuthRevoked        = "auth_revoked"
	ProvisioningEventAuthRestored       = "auth_restored"
	ProvisioningEventBindingConfirmed   = "binding_confirmed"
	ProvisioningEventBindingPending     = "binding_pending"
)

// Provisioning state and session state values shared with the runtime status
// columns and the admin console.
const (
	ProvisioningStateUnprovisioned = "unprovisioned"
	ProvisioningStateProvisioning  = "provisioning"
	ProvisioningStateProvisioned   = "provisioned"

	SessionStateReady          = "ready"
	SessionStateReauthRequired = "reauth_required"
	SessionStateRevoked        = "revoked"
)

// Health state values derived from the newest failure and recovery events.
const (
	HealthHealthy  = "healthy"
	HealthDegraded = "degraded"
	HealthFaulted  = "faulted"
	HealthUnknown  = "unknown"
)

// Diagnostic retention is part of the administrator-facing contract, so empty
// and populated responses must report the same limits.
const (
	RetentionBootEvents   = 200
	RetentionFailures     = 100
	RetentionRecovery     = 100
	RetentionInteraction  = 300
	RetentionProvisioning = 300
	RetentionCrashes      = 50
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

// CrashEvent is one redacted, bounded crash record. It deliberately carries
// only a symbolic cause, faulting task name, program counter, and frame count.
// It never carries raw memory, registers, backtrace addresses, audio, images,
// credentials, or child content, so the record stays safe to display.
type CrashEvent struct {
	EventType       string    `json:"event_type"`
	EventID         string    `json:"event_id"`
	Sequence        uint64    `json:"sequence"`
	Cause           string    `json:"cause"`
	FaultKind       string    `json:"fault_kind"`
	TaskName        string    `json:"task_name"`
	ProgramCounter  uint32    `json:"program_counter"`
	FrameCount      uint32    `json:"frame_count"`
	CoreID          uint32    `json:"core_id"`
	FirmwareVersion string    `json:"firmware_version"`
	ReportedAt      time.Time `json:"reported_at"`
}

// ProvisioningEvent is one bounded network bring-up step reported by a device.
type ProvisioningEvent struct {
	EventType       string    `json:"event_type"`
	EventID         string    `json:"event_id"`
	Sequence        uint64    `json:"sequence"`
	DetailCode      string    `json:"detail_code"`
	DurationMS      uint64    `json:"duration_ms"`
	FirmwareVersion string    `json:"firmware_version"`
	ReportedAt      time.Time `json:"reported_at"`
}

// Provisioning is the optional heartbeat extension carrying the current
// provisioning state plus the bounded step history. The current state is
// mirrored into the runtime status row so the admin list can render it
// without reading the event table.
type Provisioning struct {
	State             string              `json:"state"`
	WiFiConfigured    bool                `json:"wifi_configured"`
	SessionState      string              `json:"session_state"`
	LastProvisionedAt *time.Time          `json:"last_provisioned_at"`
	DroppedEvents     uint64              `json:"dropped_events"`
	Events            []ProvisioningEvent `json:"events"`
}

// ProvisioningSnapshot is the administrator-facing provisioning history for
// one device.
type ProvisioningSnapshot struct {
	DeviceID          string              `json:"device_id"`
	State             string              `json:"state"`
	WiFiConfigured    bool                `json:"wifi_configured"`
	SessionState      string              `json:"session_state"`
	LastProvisionedAt *time.Time          `json:"last_provisioned_at"`
	NewestSequence    uint64              `json:"newest_sequence"`
	DroppedEvents     uint64              `json:"dropped_events"`
	Events            []ProvisioningEvent `json:"events"`
	UpdatedAt         time.Time           `json:"updated_at"`
	RetentionEvents   int                 `json:"retention_provisioning_events"`
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
	CrashEvents       []CrashEvent       `json:"crash_events"`
}

// Snapshot is the administrator-facing diagnostic history for one device.
type Snapshot struct {
	DeviceID             string             `json:"device_id"`
	BootEvents           []BootEvent        `json:"boot_events"`
	Failures             []ModuleFailure    `json:"failures"`
	RecoveryEvents       []RecoveryEvent    `json:"recovery_events"`
	InteractionEvents    []InteractionEvent `json:"interaction_events"`
	CrashEvents          []CrashEvent       `json:"crash_events"`
	LatestFailure        *ModuleFailure     `json:"latest_failure"`
	LatestCrash          *CrashEvent        `json:"latest_crash"`
	ErrorCount           uint64             `json:"error_count"`
	RecoveryCount        uint64             `json:"recovery_count"`
	CrashCount           uint64             `json:"crash_count"`
	UpdatedAt            time.Time          `json:"updated_at"`
	HealthState          string             `json:"health_state"`
	RetentionBoot        int                `json:"retention_boot_events"`
	RetentionFailures    int                `json:"retention_failures"`
	RetentionRecovery    int                `json:"retention_recovery_events"`
	RetentionInteraction int                `json:"retention_interaction_events"`
	RetentionCrash       int                `json:"retention_crash_events"`
}

// CrashCauseCount is one row of the crash-cause breakdown in a summary.
type CrashCauseCount struct {
	Cause string `json:"cause"`
	Count uint64 `json:"count"`
}

// Summary is the bounded, windowed diagnostic aggregate for one device. It
// powers the operator-facing trend view without exposing individual records
// or any child data. Counts are limited to the requested window and the
// crash-cause breakdown is capped so a crafted payload cannot grow it.
type Summary struct {
	DeviceID       string            `json:"device_id"`
	WindowDays     int               `json:"window_days"`
	BootCount      uint64            `json:"boot_count"`
	FailureCount   uint64            `json:"failure_count"`
	RecoveryCount  uint64            `json:"recovery_count"`
	CrashCount     uint64            `json:"crash_count"`
	CrashCauses    []CrashCauseCount `json:"crash_causes"`
	HealthState    string            `json:"health_state"`
	LastReportedAt *time.Time        `json:"last_reported_at"`
	GeneratedAt    time.Time         `json:"generated_at"`
}
