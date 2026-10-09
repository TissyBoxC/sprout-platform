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

// Heartbeat contains one complete device runtime snapshot.
type Heartbeat struct {
	DeviceID          string
	HeartbeatID       string
	ReportedAt        time.Time
	FirmwareVersion   string
	ConnectionState   ConnectionState
	Transport         Transport
	NetworkQuality    NetworkQualityLevel
	RSSIDBM           int
	LatencyMS         int
	PacketLossPercent int
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
	// Provisioning mirrors the provisioning columns on the runtime status row.
	// They are read-only projections of the provisioning event stream and are
	// never written through a heartbeat.
	ProvisioningState ProvisioningState
	WiFiConfigured    bool
	SessionState      SessionState
	LastProvisionedAt *time.Time
	DroppedEvents     uint64
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
)

// CommandTypeIsValid reports whether a command is part of the stable contract.
func CommandTypeIsValid(commandType CommandType) bool {
	switch commandType {
	case CommandRefreshConfiguration,
		CommandReconnectNetwork,
		CommandResyncTime,
		CommandFactoryReset,
		CommandDisplayMessage:
		return true
	default:
		return false
	}
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
	ID             string
	DeviceID       string
	Type           CommandType
	Payload        map[string]any
	Status         CommandStatus
	RequestedBy    string
	RequestID      string
	CreatedAt      time.Time
	DeliveredAt    *time.Time
	AcknowledgedAt *time.Time
	ResultCode     string
}

// HeartbeatInput is a validated device report ready for persistence.
type HeartbeatInput struct {
	DeviceID          string
	HeartbeatID       string
	ReportedAt        time.Time
	FirmwareVersion   string
	ConnectionState   ConnectionState
	Transport         Transport
	NetworkQuality    NetworkQualityLevel
	RSSIDBM           int
	LatencyMS         int
	PacketLossPercent int
	TimeSyncState     TimeSyncState
	TimeSyncSource    TimeSyncSource
	TimeSyncedAt      *time.Time
	TimeOffsetMS      int
	OfflineState      OfflineState
	OfflineReason     OfflineReason
	FallbackActive    bool
	PendingTelemetry  int
}

// DeviceStatus is the guardian-facing combination of a binding and the
// latest runtime snapshot. Runtime is nil until the device reports once.
type DeviceStatus struct {
	ParentAccountID string
	DeviceID        string
	DeviceName      string
	HardwareModel   string
	FirmwareVersion string
	Capabilities    []string
	LifecycleStatus string
	BoundAt         time.Time
	UpdatedAt       time.Time
	Runtime         *RuntimeStatus
}
