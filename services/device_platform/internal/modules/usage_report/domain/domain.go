// Package domain contains device usage report types and validation rules.
package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidUsageReport  = errors.New("invalid usage report")
	ErrUsageReportNotFound = errors.New("usage report not found")
)

// UsageUpload is one device-local usage day uploaded by firmware.
//
// Uploads are idempotent by DeviceID and ReportDate, so a retry or offline
// backfill replaces the stored day instead of double counting.
type UsageUpload struct {
	ReportDate            string
	TimezoneOffsetMinutes int
	ActiveSeconds         int
	ConversationCount     int
	ConversationSeconds   int
	ContentPlayCount      int
	ContentSeconds        int
	Categories            []CategoryUsage
	Blocked               BlockedCounts
}

// CategoryUsage is the per-category content usage for one device day.
type CategoryUsage struct {
	Category  string `json:"category"`
	PlayCount int    `json:"play_count"`
	Seconds   int    `json:"seconds"`
}

// BlockedCounts records why the device rejected child activity.
type BlockedCounts struct {
	DisabledPeriod int `json:"disabled_period"`
	DailyLimit     int `json:"daily_limit"`
	CategoryDenied int `json:"category_denied"`
	TimeUntrusted  int `json:"time_untrusted"`
}

// DailyUsage is one persisted device usage day.
type DailyUsage struct {
	ReportDate            string
	ParentAccountID       string
	DeviceID              string
	DeviceName            string
	TimezoneOffsetMinutes int
	ActiveSeconds         int
	ConversationCount     int
	ConversationSeconds   int
	ContentPlayCount      int
	ContentSeconds        int
	Categories            []CategoryUsage
	Blocked               BlockedCounts
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// Report is one guardian-facing report day aggregated across devices.
//
// It intentionally carries counters and durations only. Conversation text,
// audio, images, tokens, and child identifiers never enter this projection.
type Report struct {
	SchemaVersion         string
	ReportDate            string
	TimezoneOffsetMinutes int
	ActiveMinutes         int
	ConversationCount     int
	ConversationMinutes   int
	ContentPlayCount      int
	ContentMinutes        int
	DailyLimitMinutes     int
	RemainingMinutes      int
	LimitReached          bool
	Categories            []CategoryUsage
	Blocked               BlockedCounts
	Devices               []DeviceUsage
	UpdatedAt             time.Time
}

// DeviceUsage is the per-device breakdown inside one report day.
type DeviceUsage struct {
	DeviceID          string
	DeviceName        string
	ActiveMinutes     int
	ConversationCount int
	ContentPlayCount  int
}
