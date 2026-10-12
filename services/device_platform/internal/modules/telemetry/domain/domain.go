// Package domain defines the read-only fleet telemetry projections used by the
// operations console.
//
// Telemetry is an aggregation boundary: heartbeats, network quality, thermal
// state, pending backlog and usage counters are owned by device_runtime and
// usage_report. This module never writes those tables and never stores raw
// device payloads, so an operator can only read bounded, already-normalized
// scalars.
package domain

import (
	"errors"
	"time"
)

// ErrInvalidWindow reports a window outside the supported bounds.
var ErrInvalidWindow = errors.New("invalid telemetry window")

// Window bounds. The dashboard reports at most a quarter, and the default is
// one rolling week.
const (
	DefaultWindowDays = 7
	MaxWindowDays     = 90
)

// DeviceTelemetry is the bounded per-device projection returned to operators.
type DeviceTelemetry struct {
	DeviceID          string    `json:"device_id"`
	ConnectionState   string    `json:"connection_state"`
	Transport         string    `json:"transport"`
	NetworkQuality    string    `json:"network_quality"`
	RSSIDBM           int       `json:"rssi_dbm"`
	LatencyMS         int       `json:"latency_ms"`
	PacketLossPercent int       `json:"packet_loss_percent"`
	TemperatureState  string    `json:"temperature_state"`
	TemperatureTenths *int      `json:"temperature_celsius_tenths"`
	PendingTelemetry  int       `json:"pending_telemetry"`
	FirmwareVersion   string    `json:"firmware_version"`
	ReportedAt        time.Time `json:"reported_at"`
	IsOnline          bool      `json:"is_online"`
}

// Count is one labelled histogram row.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}

// Summary is the fleet telemetry overview for the requested window.
type Summary struct {
	WindowDays            int               `json:"window_days"`
	OnlineThresholdSecond int               `json:"online_threshold_seconds"`
	DeviceCount           uint64            `json:"device_count"`
	OnlineCount           uint64            `json:"online_count"`
	OfflineCount          uint64            `json:"offline_count"`
	DegradedNetworkCount  uint64            `json:"degraded_network_count"`
	WarmDeviceCount       uint64            `json:"warm_device_count"`
	CriticalDeviceCount   uint64            `json:"critical_device_count"`
	PendingTelemetryTotal uint64            `json:"pending_telemetry_total"`
	ReportedTodayCount    uint64            `json:"reported_today_count"`
	NetworkQualityBuckets []Count           `json:"network_quality_buckets"`
	TemperatureBuckets    []Count           `json:"temperature_buckets"`
	TransportBuckets      []Count           `json:"transport_buckets"`
	Devices               []DeviceTelemetry `json:"devices"`
	GeneratedAt           time.Time         `json:"generated_at"`
}

// NormalizeWindow clamps a requested window to the supported bounds.
func NormalizeWindow(windowDays int) int {
	if windowDays <= 0 || windowDays > MaxWindowDays {
		return DefaultWindowDays
	}
	return windowDays
}
