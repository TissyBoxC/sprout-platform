// Package content_policy enforces child content rules.
//
// The device declares the content category for one voice session. The gateway
// resolves the family policy from the platform database and refuses a session
// when the category is not explicitly allowed. A policy that cannot be
// resolved fails closed for ordinary content while crisis and safety requests
// remain available.
package content_policy

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Stable decision reasons are returned to the conversation layer and are safe
// to record as metrics labels. They never contain child identifiers or policy
// internals.
const (
	ReasonAllowed            = "allowed"
	ReasonCategoryNotAllowed = "content_category_not_allowed"
	ReasonCategoryInvalid    = "content_category_invalid"
	ReasonPolicyUnavailable  = "parent_policy_unavailable"
	ReasonPolicyNotFound     = "parent_policy_not_found"
)

// ErrPolicyUnavailable means the policy provider could not reach its source of
// truth. Callers must treat this as a fail-closed condition for ordinary
// content.
var ErrPolicyUnavailable = errors.New("parent policy is unavailable")

// ErrPolicyNotFound means the device has no family policy to execute.
var ErrPolicyNotFound = errors.New("parent policy not found")

// DisabledPeriod is one local-time window when ordinary content must stay
// quiet. A period may cross midnight.
type DisabledPeriod struct {
	StartTime string
	EndTime   string
}

// Profile is one resolved effective policy for a device's family.
//
// FamilyID and child identifiers are deliberately absent: the gateway only
// needs the aggregated execution inputs, not child-level identity.
type Profile struct {
	PolicyVersion     int64
	AgeTier           string
	AllowedCategories []string
	DisabledPeriods   []DisabledPeriod
	MaxVolumePercent  int
	SourceChildCount  int
	UpdatedAt         time.Time
}

// Resolver loads the effective policy for one authenticated device.
type Resolver interface {
	Resolve(ctx context.Context, deviceID string) (Profile, error)
}

// Decision is the result of one content policy check.
type Decision struct {
	Allowed bool
	Reason  string
}

// Policy checks input and output text.
//
// It remains the compatibility surface for callers that only apply text
// moderation. Device-aware enforcement uses DevicePolicy.
type Policy interface {
	CheckText(text string) Decision
}

// DevicePolicy evaluates content against the authenticated device's effective
// family policy.
type DevicePolicy interface {
	// CheckDeviceCategory reports whether the category is explicitly allowed
	// for the device's family.
	CheckDeviceCategory(ctx context.Context, deviceID string, category string) Decision
	// ComposeSystemPrompt returns the system prompt constrained by the
	// effective age tier and allowed categories.
	ComposeSystemPrompt(
		ctx context.Context,
		deviceID string,
		basePrompt string,
	) (string, Decision)
}

// AllowedCategories is the canonical platform category pool. The order is
// stable so prompt output and tests do not depend on map iteration.
var AllowedCategories = []string{
	"story",
	"nursery_rhyme",
	"poetry",
	"english",
	"encyclopedia",
	"bedtime",
}

// IsKnownCategory reports whether the value is in the platform-approved pool.
func IsKnownCategory(category string) bool {
	for _, candidate := range AllowedCategories {
		if candidate == category {
			return true
		}
	}
	return false
}

// IsKnownAgeTier reports whether the value is a supported child age tier.
func IsKnownAgeTier(ageTier string) bool {
	switch ageTier {
	case "age_3_4", "age_5_6", "age_7_8":
		return true
	default:
		return false
	}
}

// IsSafetyIntent reports whether an utterance is likely to need a protective
// crisis response. Safety prompts must never be blocked by a family category
// or a policy lookup failure.
func IsSafetyIntent(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	for _, marker := range safetyIntentMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

var safetyIntentMarkers = []string{
	"自杀",
	"自残",
	"不想活",
	"想死",
	"伤害自己",
	"被打了",
	"有人打我",
	"虐待我",
	"猥亵",
	"性侵",
	"非常害怕",
	"特别害怕",
	"极度害怕",
	"救命",
	"suicide",
	"kill myself",
	"hurt myself",
	"self harm",
	"molest",
	"abuse me",
}
