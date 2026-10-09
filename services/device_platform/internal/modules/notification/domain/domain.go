// Package domain defines guardian notification and remote device message
// contract types plus their validation rules.
//
// Notifications never carry child conversation text, audio, images, tokens,
// credentials, or raw upstream errors. Administrators author platform notices;
// guardians author family messages to a bound device.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidNotification   = errors.New("invalid notification")
	ErrNotificationNotFound  = errors.New("notification not found")
	ErrNotificationExpired   = errors.New("notification is not visible")
	ErrNotificationForbidden = errors.New("notification is not owned by this account")
	ErrInvalidPage           = errors.New("invalid notification page request")
)

const (
	// CategoryAccountSecurity is used for sign-in, password, and MFA events.
	CategoryAccountSecurity = "account_security"
	// CategoryDeviceStatus reports binding, provisioning, and online changes.
	CategoryDeviceStatus = "device_status"
	// CategoryContentRelease announces new content packages.
	CategoryContentRelease = "content_release"
	// CategoryServiceUpdate reports platform and firmware releases.
	CategoryServiceUpdate = "service_update"
	// CategoryUsageReport delivers the daily usage summary.
	CategoryUsageReport = "usage_report"
	// CategoryFamilyMessage is a guardian-authored message to a device.
	CategoryFamilyMessage = "family_message"
	// CategorySystemAnnouncement is an operator broadcast.
	CategorySystemAnnouncement = "system_announcement"
)

const (
	SeverityInfo     = "info"
	SeveritySuccess  = "success"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

const (
	// AudienceParent targets exactly one guardian.
	AudienceParent = "parent"
	// AudienceAllParents broadcasts to every active guardian.
	AudienceAllParents = "all_parents"
	// AudienceFamilyDevices targets the bound devices of one guardian. The
	// notification is still owned by the guardian, who must see it in the app.
	AudienceFamilyDevices = "family_devices"
)

const (
	SourceAdministrator = "administrator"
	SourceGuardian      = "guardian"
	SourceSystem        = "system"
)

const (
	ChannelInApp  = "in_app"
	ChannelPush   = "push"
	ChannelSMS    = "sms"
	ChannelEmail  = "email"
	ChannelDevice = "device"
)

const (
	DeliveryStatusPending   = "pending"
	DeliveryStatusDelivered = "delivered"
	DeliveryStatusRead      = "read"
	DeliveryStatusFailed    = "failed"
	DeliveryStatusExpired   = "expired"
)

const (
	// DefaultPageSize is the guardian inbox page size.
	DefaultPageSize = 20
	// MaxPageSize bounds one guardian inbox page.
	MaxPageSize = 100
	// MaxUnreadCount bounds the unread badge value.
	MaxUnreadCount = 9999
	// MaxTitleRunes bounds the authored title length.
	MaxTitleRunes = 120
	// MaxBodyRunes bounds the authored body length.
	MaxBodyRunes = 2000
	// MaxActionLabelRunes bounds the call-to-action label length.
	MaxActionLabelRunes = 24
	// MaxActionPathRunes bounds the in-app deep link length.
	MaxActionPathRunes = 200
	// MaxListLimit bounds administrator list queries.
	MaxListLimit = 200
	// MaxDeviceMessageSeconds bounds how long a remote message stays on a
	// device before the firmware clears it.
	MaxDeviceMessageSeconds = 300
	// DefaultDeviceMessageSeconds is applied when a guardian does not choose.
	DefaultDeviceMessageSeconds = 30
	// RetentionDays bounds how long a notification stays in an inbox.
	RetentionDays = 90
)

// Notification is one authored notice plus its audience.
type Notification struct {
	ID                     string     `json:"id"`
	Category               string     `json:"category"`
	Severity               string     `json:"severity"`
	Title                  string     `json:"title"`
	Body                   string     `json:"body"`
	ActionPath             string     `json:"action_path,omitempty"`
	ActionLabel            string     `json:"action_label,omitempty"`
	Audience               string     `json:"audience"`
	ParentAccountID        string     `json:"parent_account_id,omitempty"`
	DeviceID               string     `json:"device_id,omitempty"`
	DisplayDurationSeconds int        `json:"display_duration_seconds"`
	Channels               []string   `json:"channels"`
	Source                 string     `json:"source"`
	CreatedBy              string     `json:"created_by,omitempty"`
	PublishAt              time.Time  `json:"publish_at"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

// InboxItem is the guardian-facing projection of one notification.
//
// It carries the authored copy and the read state of the current guardian and
// never exposes another guardian's delivery row.
type InboxItem struct {
	ID          string     `json:"id"`
	Category    string     `json:"category"`
	Severity    string     `json:"severity"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	ActionPath  string     `json:"action_path,omitempty"`
	ActionLabel string     `json:"action_label,omitempty"`
	DeviceID    string     `json:"device_id,omitempty"`
	Channels    []string   `json:"channels"`
	Read        bool       `json:"read"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
	PublishAt   time.Time  `json:"publish_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// InboxPage is one bounded page of guardian notifications.
type InboxPage struct {
	Items       []InboxItem `json:"items"`
	UnreadCount int         `json:"unread_count"`
	NextCursor  string      `json:"next_cursor,omitempty"`
	HasMore     bool        `json:"has_more"`
}

// Delivery records one notification-channel-guardian delivery attempt.
type Delivery struct {
	ID              string     `json:"id"`
	NotificationID  string     `json:"notification_id"`
	ParentAccountID string     `json:"parent_account_id"`
	Channel         string     `json:"channel"`
	Status          string     `json:"status"`
	ReadAt          *time.Time `json:"read_at,omitempty"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
	FailureCode     string     `json:"failure_code,omitempty"`
	RetryCount      int        `json:"retry_count"`
	CommandID       string     `json:"command_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// AdminItem is the operator projection: the authored notice plus aggregate
// delivery counters. It never includes per-guardian personal data beyond
// counts.
type AdminItem struct {
	Notification
	DeliveryTotal     int `json:"delivery_total"`
	DeliveryPending   int `json:"delivery_pending"`
	DeliveryDelivered int `json:"delivery_delivered"`
	DeliveryRead      int `json:"delivery_read"`
	DeliveryFailed    int `json:"delivery_failed"`
}

// AdminPage is one bounded page of administrator notifications.
type AdminPage struct {
	Items      []AdminItem `json:"items"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

// AdminStats is the operator dashboard counter projection.
type AdminStats struct {
	Total                int `json:"total"`
	BroadcastCount       int `json:"broadcast_count"`
	PendingDeliveryCount int `json:"pending_delivery_count"`
	FailedDeliveryCount  int `json:"failed_delivery_count"`
}

// AdminFilter bounds and scopes an administrator notification query.
type AdminFilter struct {
	Category string
	Audience string
	Source   string
	Query    string
	Page     int
	PageSize int
}

// AdministratorInput is a validated operator-authored notification.
type AdministratorInput struct {
	Category    string
	Severity    string
	Title       string
	Body        string
	ActionPath  string
	ActionLabel string
	Audience    string
	Channels    []string
	// ParentAccountID is required when Audience is AudienceParent.
	ParentAccountID string
	// DeviceID and DisplayDurationSeconds are required for family message
	// delivery to a bound device.
	DeviceID               string
	DisplayDurationSeconds int
	// ExpiresAt is optional. A nil value keeps the default retention window.
	ExpiresAt *time.Time
}

// GuardianMessageInput is a validated guardian-authored family message.
type GuardianMessageInput struct {
	DeviceID               string
	Body                   string
	DisplayDurationSeconds int
	// SendToDevice arms the device command channel in addition to the in-app
	// inbox entry.
	SendToDevice bool
}

// DeviceMessage is the fixed, firmware-consumable device display contract.
type DeviceMessage struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Body            string `json:"body"`
	Severity        string `json:"severity"`
	DurationSeconds int    `json:"duration_seconds"`
	Category        string `json:"category"`
}

// CategoryIsValid reports whether a category is part of the stable contract.
func CategoryIsValid(category string) bool {
	switch category {
	case CategoryAccountSecurity,
		CategoryDeviceStatus,
		CategoryContentRelease,
		CategoryServiceUpdate,
		CategoryUsageReport,
		CategoryFamilyMessage,
		CategorySystemAnnouncement:
		return true
	default:
		return false
	}
}

// SeverityIsValid reports whether a severity is part of the stable contract.
func SeverityIsValid(severity string) bool {
	switch severity {
	case SeverityInfo, SeveritySuccess, SeverityWarning, SeverityCritical:
		return true
	default:
		return false
	}
}

// AudienceIsValid reports whether an audience is part of the stable contract.
func AudienceIsValid(audience string) bool {
	switch audience {
	case AudienceParent, AudienceAllParents, AudienceFamilyDevices:
		return true
	default:
		return false
	}
}

// ChannelIsValid reports whether a channel is part of the stable contract.
func ChannelIsValid(channel string) bool {
	switch channel {
	case ChannelInApp, ChannelPush, ChannelSMS, ChannelEmail, ChannelDevice:
		return true
	default:
		return false
	}
}

// NormalizeChannels trims, lowercases, de-duplicates, and validates channels.
// An empty list becomes in_app so a notification is always at least readable
// in the guardian inbox.
func NormalizeChannels(channels []string) ([]string, error) {
	if len(channels) == 0 {
		return []string{ChannelInApp}, nil
	}
	seen := make(map[string]struct{}, len(channels))
	result := make([]string, 0, len(channels))
	for _, channel := range channels {
		normalized := strings.ToLower(strings.TrimSpace(channel))
		if normalized == "" {
			continue
		}
		if !ChannelIsValid(normalized) {
			return nil, ErrInvalidNotification
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		return []string{ChannelInApp}, nil
	}
	return result, nil
}

// HasChannel reports whether a normalized channel list contains a channel.
func HasChannel(channels []string, channel string) bool {
	for _, candidate := range channels {
		if candidate == channel {
			return true
		}
	}
	return false
}

// NormalizeExpiry returns the validated absolute expiry for a new
// notification. A zero value applies the default retention window; an expiry
// that is already in the past is rejected.
func NormalizeExpiry(expiresAt *time.Time, now time.Time) (*time.Time, error) {
	if expiresAt == nil {
		fallback := now.Add(RetentionDays * 24 * time.Hour)
		return &fallback, nil
	}
	value := expiresAt.UTC()
	if !value.After(now) {
		return nil, ErrInvalidNotification
	}
	return &value, nil
}

// RuneCount returns the trimmed rune length for length validation.
func RuneCount(value string) int {
	return len([]rune(strings.TrimSpace(value)))
}

// ClampPage normalizes a requested page and page size into supported bounds.
func ClampPage(page int, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxListLimit {
		pageSize = MaxListLimit
	}
	return page, pageSize
}
