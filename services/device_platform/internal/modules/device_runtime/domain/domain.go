// Package domain contains device runtime status and command types.
package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidHeartbeat = errors.New("invalid device heartbeat")
	ErrDeviceNotFound   = errors.New("device runtime not found")
	ErrCommandNotFound  = errors.New("device runtime command not found")
	ErrInvalidCommand   = errors.New("invalid device runtime command")
	// ErrFirmwareUpdateAlreadyQueued reports an idempotent retry for the same
	// device and release while an active firmware_update command still exists.
	ErrFirmwareUpdateAlreadyQueued = errors.New("firmware update command already queued")
	// ErrFirmwareUpdateUnavailable is returned when the OTA authority is not
	// assembled or cannot validate a release. It fails closed so a deployment
	// can never be guessed from a client-supplied version.
	ErrFirmwareUpdateUnavailable = errors.New("firmware update authorization unavailable")
)

// ConnectionState is the transport state reported by a device.
type ConnectionState string

const (
	ConnectionStateOffline    ConnectionState = "offline"
	ConnectionStateConnecting ConnectionState = "connecting"
	ConnectionStateOnline     ConnectionState = "online"
)

// Transport identifies the active network path without exposing local network
// details such as the SSID or IP address.
type Transport string

const (
	TransportNone       Transport = "none"
	TransportWiFi       Transport = "wifi"
	TransportCellular4G Transport = "cellular_4g"
)

// NetworkQualityLevel is the normalized quality band shown to guardians and
// operators.
type NetworkQualityLevel string

const (
	NetworkQualityUnknown   NetworkQualityLevel = "unknown"
	NetworkQualityPoor      NetworkQualityLevel = "poor"
	NetworkQualityFair      NetworkQualityLevel = "fair"
	NetworkQualityGood      NetworkQualityLevel = "good"
	NetworkQualityExcellent NetworkQualityLevel = "excellent"
)

// TimeSyncState describes whether the device clock is trusted.
type TimeSyncState string

const (
	TimeSyncUnsynchronized TimeSyncState = "unsynchronized"
	TimeSyncSynchronizing  TimeSyncState = "synchronizing"
	TimeSyncSynchronized   TimeSyncState = "synchronized"
)

// TimeSyncSource identifies where the clock was calibrated.
type TimeSyncSource string

const (
	TimeSyncSourceNone     TimeSyncSource = "none"
	TimeSyncSourceSNTP     TimeSyncSource = "sntp"
	TimeSyncSourcePlatform TimeSyncSource = "platform"
)

// OfflineState is used by the device when the cloud is unavailable.
type OfflineState string

const (
	OfflineStateOnline  OfflineState = "online"
	OfflineStateGrace   OfflineState = "grace"
	OfflineStateOffline OfflineState = "offline"
)

// OfflineReason explains why fallback mode is active.
type OfflineReason string

const (
	OfflineReasonNone                 OfflineReason = "none"
	OfflineReasonNetworkUnavailable   OfflineReason = "network_unavailable"
	OfflineReasonAuthenticationFailed OfflineReason = "authentication_failed"
	OfflineReasonTimeNotSynchronized  OfflineReason = "time_not_synchronized"
	OfflineReasonServiceUnavailable   OfflineReason = "service_unavailable"
)

// ProvisioningState is the current network bring-up step reported by a device.
type ProvisioningState string

const (
	ProvisioningStateUnprovisioned ProvisioningState = "unprovisioned"
	ProvisioningStateProvisioning  ProvisioningState = "provisioning"
	ProvisioningStateProvisioned   ProvisioningState = "provisioned"
)

// SessionState describes whether the device session is usable. A device that
// was revoked or must re-authenticate reports this so guardians see the exact
// reason instead of a generic offline state.
type SessionState string

const (
	SessionStateReady          SessionState = "ready"
	SessionStateReauthRequired SessionState = "reauth_required"
	SessionStateRevoked        SessionState = "revoked"
)

// TemperatureState is the normalized thermal band reported by the device.
// 'unknown' means the device has no reading (no sensor or feature disabled).
type TemperatureState string

const (
	TemperatureStateUnknown  TemperatureState = "unknown"
	TemperatureStateNormal   TemperatureState = "normal"
	TemperatureStateWarm     TemperatureState = "warm"
	TemperatureStateHot      TemperatureState = "hot"
	TemperatureStateCritical TemperatureState = "critical"
)

// Heartbeat contains one complete device runtime snapshot.
type Heartbeat struct {
	DeviceID           string
	HeartbeatID        string
	ReportedAt         time.Time
	FirmwareVersion    string
	Capabilities       []string
	CapabilityRevision uint64
	ConnectionState    ConnectionState
	Transport          Transport
	NetworkQuality     NetworkQualityLevel
	RSSIDBM            int
	LatencyMS          int
	PacketLossPercent  int
	Temperature        TemperatureState
	// TemperatureTenths is the reading in tenths of a degree Celsius, or nil
	// when the device reported no measurement.
	TemperatureTenths *int
	TimeSyncState     TimeSyncState
	TimeSyncSource    TimeSyncSource
	TimeSyncedAt      *time.Time
	TimeOffsetMS      int
	OfflineState      OfflineState
	OfflineReason     OfflineReason
	FallbackActive    bool
	PendingTelemetry  int
	ReceivedAt        time.Time
	UpdatedAt         time.Time
}

// RuntimeStatus is the latest heartbeat plus a server-derived online flag.
type RuntimeStatus struct {
	Heartbeat
	IsOnline bool
	// Diagnostic summarizes the device's recent abnormal restarts so the
	// guardian app can show one actionable, non-technical hint. It is derived
	// from the audit crash stream and never contains raw crash detail.
	Diagnostic DiagnosticHint
	// Provisioning mirrors the provisioning columns on the runtime status row.
	// They are read-only projections of the provisioning event stream and are
	// never written through a heartbeat.
	ProvisioningState ProvisioningState
	WiFiConfigured    bool
	SessionState      SessionState
	LastProvisionedAt *time.Time
	DroppedEvents     uint64
}

// DiagnosticHint is a bounded, guardian-safe view of a device's recent
// stability. RecentCrashCount counts abnormal restarts inside the platform's
// retention window; it is zero when the device is stable or has never
// reported.
type DiagnosticHint struct {
	RecentCrashCount uint64
}

// CommandType is an operator-initiated network maintenance action.
type CommandType string

const (
	CommandRefreshConfiguration CommandType = "refresh_configuration"
	CommandReconnectNetwork     CommandType = "reconnect_network"
	CommandResyncTime           CommandType = "resync_time"
	// CommandFactoryReset is a destructive, guard-on-device action. The device
	// must apply its own local confirmation flow before erasing configuration.
	CommandFactoryReset CommandType = "factory_reset"
	// CommandDisplayMessage shows a guardian-authored message on the device
	// screen for a bounded duration. It carries only authored copy and never
	// child conversation content.
	CommandDisplayMessage CommandType = "display_message"
	// CommandSyncPrivacyConsent pushes the guardian's current per-type consent
	// decisions to the device so local privacy gates and cached child data stay
	// in step with the platform. The payload carries only the four booleans and
	// never any child content.
	CommandSyncPrivacyConsent CommandType = "sync_privacy_consent"
	// CommandFirmwareUpdate asks the OTA worker to apply an already-authorized
	// release. The command carries only a release id after OTA verifies the
	// device's active deployment. Release existence, hardware, rollout,
	// signature, and rollback checks remain owned by the OTA module.
	CommandFirmwareUpdate CommandType = "firmware_update"
	// CommandVoiceIntercomCall tells the device that a guardian is calling it.
	//
	// It is the ring, not the call: the device shows and sounds a prompt and
	// captures nothing. Audio only starts after the child accepts on the device.
	// The payload carries the call identifier and the ring deadline, never audio.
	CommandVoiceIntercomCall CommandType = "voice_intercom_call"
	// CommandVoiceIntercomCallControl ends a call from the guardian side.
	//
	// Only hangup and decline are accepted; an "accept" sent from the platform is
	// ignored on the device, because consent has to be the child's own action.
	CommandVoiceIntercomCallControl CommandType = "voice_intercom_call_control"
)

// CommandTypeIsValid reports whether a command is part of the stable contract.
func CommandTypeIsValid(commandType CommandType) bool {
	switch commandType {
	case CommandRefreshConfiguration,
		CommandReconnectNetwork,
		CommandResyncTime,
		CommandFactoryReset,
		CommandDisplayMessage,
		CommandSyncPrivacyConsent,
		CommandFirmwareUpdate,
		CommandVoiceIntercomCall,
		CommandVoiceIntercomCallControl:
		return true
	default:
		return false
	}
}

// FirmwareUpdateReleaseID returns the only payload value accepted by a
// firmware_update command. Unknown fields are rejected by the service before
// persistence, so a caller cannot smuggle a signature, URL, actor, or
// deployment kind into the device command.
func FirmwareUpdateReleaseID(payload map[string]any) (string, bool) {
	if len(payload) != 1 {
		return "", false
	}
	value, exists := payload["release_id"]
	if !exists {
		return "", false
	}
	releaseID, ok := value.(string)
	if !ok || releaseID == "" {
		return "", false
	}
	return releaseID, true
}

// CommandStatus tracks delivery and acknowledgement of one maintenance action.
type CommandStatus string

const (
	CommandStatusPending      CommandStatus = "pending"
	CommandStatusDelivered    CommandStatus = "delivered"
	CommandStatusAcknowledged CommandStatus = "acknowledged"
	CommandStatusFailed       CommandStatus = "failed"
	CommandStatusExpired      CommandStatus = "expired"
)

// Command is a durable, idempotent network maintenance request.
type Command struct {
	ID          string
	DeviceID    string
	Type        CommandType
	Payload     map[string]any
	Status      CommandStatus
	RequestedBy string
	RequestID   string
	CreatedAt   time.Time
	// ExpiresAt is the durable delivery deadline. A pending or delivered
	// command at this instant is terminal and must not be sent or acknowledged.
	ExpiresAt      time.Time
	DeliveredAt    *time.Time
	AcknowledgedAt *time.Time
	ResultCode     string
}

// HeartbeatInput is a validated device report ready for persistence.
type HeartbeatInput struct {
	DeviceID           string
	HeartbeatID        string
	ReportedAt         time.Time
	FirmwareVersion    string
	Capabilities       []string
	CapabilityRevision uint64
	ConnectionState    ConnectionState
	Transport          Transport
	NetworkQuality     NetworkQualityLevel
	RSSIDBM            int
	LatencyMS          int
	PacketLossPercent  int
	Temperature        TemperatureState
	TemperatureTenths  *int
	TimeSyncState      TimeSyncState
	TimeSyncSource     TimeSyncSource
	TimeSyncedAt       *time.Time
	TimeOffsetMS       int
	OfflineState       OfflineState
	OfflineReason      OfflineReason
	FallbackActive     bool
	PendingTelemetry   int
}

// DeviceStatus is the guardian-facing combination of a binding and the
// latest runtime snapshot. Runtime is nil until the device reports once.
type DeviceStatus struct {
	ParentAccountID    string
	DeviceID           string
	DeviceName         string
	HardwareModel      string
	FirmwareVersion    string
	Capabilities       []string
	CapabilityRevision uint64
	LifecycleStatus    string
	BoundAt            time.Time
	UpdatedAt          time.Time
	Runtime            *RuntimeStatus
}
