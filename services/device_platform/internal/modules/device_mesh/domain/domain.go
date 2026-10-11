// Package domain defines the multi-device, family-linkage and content-sync
// contracts for the device_mesh module.
//
// A family owns device groups, the family linkage that carries messages across
// its devices, and the content versions that must reach eventual consistency.
// The platform stores only the metadata it needs: group membership, a message
// envelope and a content version. Deleting a device from a group only changes
// that membership and its group assignments; the other devices keep their data,
// so removing one device never cascades into another device's content.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidGroup reports a malformed device group.
	ErrInvalidGroup = errors.New("invalid device group")
	// ErrGroupNotFound reports a missing device group.
	ErrGroupNotFound = errors.New("device group not found")
	// ErrInvalidMember reports a malformed group member.
	ErrInvalidMember = errors.New("invalid device group member")
	// ErrMemberNotFound reports a missing group member.
	ErrMemberNotFound = errors.New("device group member not found")
	// ErrInvalidMessage reports a malformed routed message.
	ErrInvalidMessage = errors.New("invalid device message")
	// ErrInvalidSync reports a malformed content sync.
	ErrInvalidSync = errors.New("invalid content sync")
)

// SchemaVersion is the multi-device contract version shared with the firmware
// and both front ends.
const SchemaVersion = 1

// Route scopes the platform recognises.
const (
	ScopeDevice = "device"
	ScopeGroup  = "group"
	ScopeFamily = "family"
)

// Conflict strategies the platform recognises. A higher version always wins;
// the strategy only breaks a same-version tie.
const (
	ConflictLocalWins  = "local_wins"
	ConflictRemoteWins = "remote_wins"
	ConflictDefer      = "defer"
)

// Sync outcomes the platform recognises.
const (
	SyncPending  = "pending"
	SyncApplied  = "applied"
	SyncConflict = "conflict"
	SyncFailed   = "failed"
)

// Bounds mirrored from the firmware so a policy never leaves the documented
// range.
const (
	MinGroupMembers    = 1
	MaxGroupMembers    = 16
	MinSyncRetrySecond = 5
	MaxSyncRetrySecond = 3600
	MaxSyncAttempts    = 8
)

// GroupNameMaxLength bounds a device group name and GroupMemberMax bounds the
// membership of one group.
const (
	GroupNameMaxLength = 40
	GroupMemberMax     = 16
)

// MessageBodyMaxLength bounds a routed message body.
const MessageBodyMaxLength = 2048

// SupportedScope reports whether a route scope is accepted.
func SupportedScope(scope string) bool {
	switch scope {
	case ScopeDevice, ScopeGroup, ScopeFamily:
		return true
	default:
		return false
	}
}

// SupportedConflict reports whether a conflict strategy is accepted.
func SupportedConflict(strategy string) bool {
	switch strategy {
	case ConflictLocalWins, ConflictRemoteWins, ConflictDefer:
		return true
	default:
		return false
	}
}

// DeviceGroup is one family-scoped device group.
type DeviceGroup struct {
	ID          string
	FamilyID    string
	Name        string
	RouteScope  string
	MemberCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Member is one device in a group.
type Member struct {
	GroupID  string
	DeviceID string
	FamilyID string
	AddedAt  time.Time
}

// RoutedMessage is one message routed across the family linkage.
type RoutedMessage struct {
	ID           string
	FamilyID     string
	SourceDevice string
	Scope        string
	Target       string
	Body         string
	CreatedAt    time.Time
}

// ContentState is the stored sync state for one content key in a family.
type ContentState struct {
	FamilyID      string
	ContentKey    string
	Version       uint64
	ConflictCount int
	SyncState     string
	UpdatedAt     time.Time
}

// AdminStats is the fleet-wide multi-device summary for the admin console.
type AdminStats struct {
	WindowDays         int       `json:"window_days"`
	GroupCount         int       `json:"group_count"`
	DeviceCount        int       `json:"device_count"`
	MessageCount       int       `json:"message_count"`
	SyncedContentCount int       `json:"synced_content_count"`
	ConflictCount      int       `json:"conflict_count"`
	ScopeBuckets       []Count   `json:"scope_buckets"`
	StateBuckets       []Count   `json:"state_buckets"`
	GeneratedAt        time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}

// ValidateGroupInput normalizes and validates one group payload.
func ValidateGroupInput(name string, routeScope string) (string, string, error) {
	normalized := name
	if len([]rune(normalized)) == 0 ||
		len([]rune(normalized)) > GroupNameMaxLength {
		return "", "", ErrInvalidGroup
	}
	if !SupportedScope(routeScope) || routeScope == ScopeDevice {
		// A group is a shared scope; a device scope belongs on a direct message,
		// not on a group.
		return "", "", ErrInvalidGroup
	}
	return normalized, routeScope, nil
}

// ValidateMessage normalizes and validates one routed message.
func ValidateMessage(message RoutedMessage) (RoutedMessage, error) {
	normalized := message
	if !SupportedScope(normalized.Scope) {
		return RoutedMessage{}, ErrInvalidMessage
	}
	if normalized.Scope == ScopeGroup || normalized.Scope == ScopeFamily {
		if normalized.Target == "" {
			return RoutedMessage{}, ErrInvalidMessage
		}
	}
	bodyLength := len([]rune(normalized.Body))
	if bodyLength == 0 || bodyLength > MessageBodyMaxLength {
		return RoutedMessage{}, ErrInvalidMessage
	}
	return normalized, nil
}

// ResolveConflict returns the winning strategy for a version pair. A higher
// version always wins; the strategy only breaks a same-version tie.
func ResolveConflict(
	strategy string,
	localVersion uint64,
	remoteVersion uint64,
) string {
	if localVersion > remoteVersion {
		return ConflictLocalWins
	}
	if remoteVersion > localVersion {
		return ConflictRemoteWins
	}
	if !SupportedConflict(strategy) {
		return ConflictRemoteWins
	}
	return strategy
}

// SyncOutcome returns the sync outcome for a version pair. Equal versions are
// consistent when the strategy is deterministic and a conflict when it defers.
func SyncOutcome(
	strategy string,
	localVersion uint64,
	remoteVersion uint64,
) string {
	resolved := ResolveConflict(strategy, localVersion, remoteVersion)
	if resolved == ConflictDefer {
		return SyncConflict
	}
	return SyncApplied
}
