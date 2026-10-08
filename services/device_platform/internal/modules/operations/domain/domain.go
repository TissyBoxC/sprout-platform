// Package domain contains platform operations projections and policy.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrSettingsNotFound        = errors.New("platform settings not found")
	ErrInvalidSettings         = errors.New("invalid platform settings")
	ErrSettingsVersionConflict = errors.New("platform settings version conflict")
	ErrReleaseNotFound         = errors.New("platform release not found")
	ErrReleaseAlreadyExists    = errors.New("platform release already exists")
	ErrReleaseNotPublishable   = errors.New("platform release is not publishable")
	ErrUpdateNotAvailable      = errors.New("application update not available")
)

// SettingsVersionConflictError reports a rejected optimistic update and the
// version that must be loaded before retrying.
type SettingsVersionConflictError struct {
	ExpectedVersion int64
	CurrentVersion  int64
}

// Error implements error.
func (e *SettingsVersionConflictError) Error() string {
	if e == nil {
		return ErrSettingsVersionConflict.Error()
	}
	return "platform settings version conflict: expected version does not match current version"
}

// Is allows errors.Is(err, ErrSettingsVersionConflict).
func (e *SettingsVersionConflictError) Is(target error) bool {
	return target == ErrSettingsVersionConflict
}

const (
	ReleaseKindResource = "resource"
	ReleaseKindClient   = "client"
	ReleaseKindFirmware = "firmware"

	ReleaseChannelStable = "stable"
	ReleaseChannelBeta   = "beta"
	ReleaseChannelCanary = "canary"

	ReleaseStatusDraft     = "draft"
	ReleaseStatusPublished = "published"
	ReleaseStatusRetired   = "retired"
)

// Overview is the operations dashboard projection.
//
// Monetary values are reported in USD because the AI provider settles in USD.
type Overview struct {
	ParentAccountCount     int64     `json:"parent_account_count"`
	ActiveDeviceCount      int64     `json:"active_device_count"`
	OnlineDeviceCount      int64     `json:"online_device_count"`
	TodayConversationCount int64     `json:"today_conversation_count"`
	TodaySpentUSD          float64   `json:"today_spent_usd"`
	TotalBalanceUSD        float64   `json:"total_balance_usd"`
	PendingReleaseCount    int64     `json:"pending_release_count"`
	RecentReleases         []Release `json:"recent_releases"`
}

// FamilyAccount is the management projection that keeps a guardian and its
// dependent AI account in one row. AIAccount is nil until provisioning
// succeeds, which lets operators distinguish a missing projection from a
// suspended AI account.
type FamilyAccount struct {
	ParentAccountID    string           `json:"parent_account_id"`
	Phone              string           `json:"phone"`
	Email              string           `json:"email"`
	DisplayName        string           `json:"display_name"`
	GuardianFamilyName string           `json:"guardian_family_name"`
	ChildNickname      string           `json:"child_nickname"`
	ChildBirthday      string           `json:"child_birthday"`
	Status             string           `json:"status"`
	CreatedAt          time.Time        `json:"created_at"`
	LastLoginAt        *time.Time       `json:"last_login_at"`
	AIAccount          *FamilyAIAccount `json:"ai_account"`
}

// FamilyAIAccount is the operator-visible subset of the dependent AI account.
type FamilyAIAccount struct {
	ProviderAccountID string    `json:"provider_account_id"`
	Status            string    `json:"status"`
	BalanceUSD        float64   `json:"balance_usd"`
	ConcurrencyLimit  int       `json:"concurrency_limit"`
	AvailableModels   []string  `json:"available_models"`
	SelectedModels    []string  `json:"selected_models"`
	AllowedModels     []string  `json:"allowed_models"`
	CredentialReady   bool      `json:"credential_ready"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Settings is the editable operations policy document.
//
// Unknown fields are intentionally ignored today so forward-compatible
// clients can add options before every service is upgraded.
type Settings struct {
	AI        AISettings        `json:"ai"`
	Account   AccountSettings   `json:"account"`
	Safety    SafetySettings    `json:"safety"`
	Update    UpdateSettings    `json:"update"`
	Retention RetentionSettings `json:"retention"`
}

// AISettings controls defaults applied to new parent AI accounts.
type AISettings struct {
	DefaultBalanceUSD  float64  `json:"default_balance_usd"`
	DefaultConcurrency int      `json:"default_concurrency"`
	DefaultModels      []string `json:"default_models"`
}

// AccountSettings controls registration and sign-in behavior.
type AccountSettings struct {
	RegistrationEnabled       bool `json:"registration_enabled"`
	PhoneVerificationRequired bool `json:"phone_verification_required"`
	EmailLoginEnabled         bool `json:"email_login_enabled"`
}

// SafetySettings controls child safety defaults and media retention.
type SafetySettings struct {
	MinorModeDefault          bool `json:"minor_mode_default"`
	OutputModerationEnabled   bool `json:"output_moderation_enabled"`
	CrisisInterventionEnabled bool `json:"crisis_intervention_enabled"`
	AllowAudioUpload          bool `json:"allow_audio_upload"`
	AllowImageUpload          bool `json:"allow_image_upload"`
}

// UpdateSettings controls the default update channel and minimum versions.
type UpdateSettings struct {
	Channel           string `json:"channel"`
	MinClientVersion  string `json:"min_client_version"`
	ForceUpgradeBelow string `json:"force_upgrade_below"`
}

// RetentionSettings controls how long sensitive data is kept.
type RetentionSettings struct {
	AudioDays        int `json:"audio_days"`
	ImageDays        int `json:"image_days"`
	ConversationDays int `json:"conversation_days"`
}

// Release describes one resource, client, or firmware delivery.
type Release struct {
	ID                  string     `json:"id"`
	Version             string     `json:"version"`
	Channel             string     `json:"channel"`
	Kind                string     `json:"kind"`
	Platform            string     `json:"platform"`
	DownloadURL         string     `json:"download_url"`
	SHA256              string     `json:"sha256"`
	ReleaseNotes        string     `json:"release_notes"`
	IsMandatory         bool       `json:"is_mandatory"`
	MinSupportedVersion string     `json:"min_supported_version"`
	Status              string     `json:"status"`
	PublishedAt         *time.Time `json:"published_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// RuntimePolicy is the narrow policy projection consumed by auth and AI
// provisioning. It deliberately excludes operator-only update and retention
// fields so those services cannot depend on unrelated configuration.
type RuntimePolicy struct {
	RegistrationEnabled       bool
	PhoneVerificationRequired bool
	EmailLoginEnabled         bool
	DefaultBalanceUSD         float64
	DefaultConcurrency        int
	DefaultModels             []string
	MinorModeDefault          bool
	OutputModerationEnabled   bool
	CrisisInterventionEnabled bool
}

// AppUpdate is the client-facing update decision. A nil result means the
// caller is already current.
type AppUpdate struct {
	Kind                string `json:"kind"`
	Version             string `json:"version"`
	DownloadURL         string `json:"download_url"`
	SHA256              string `json:"sha256"`
	ReleaseNotes        string `json:"release_notes"`
	IsMandatory         bool   `json:"is_mandatory"`
	MinSupportedVersion string `json:"min_supported_version"`
}

// ReleaseInput is a validated release registration request.
type ReleaseInput struct {
	Version             string
	Channel             string
	Kind                string
	Platform            string
	DownloadURL         string
	SHA256              string
	ReleaseNotes        string
	IsMandatory         bool
	MinSupportedVersion string
}

// ReleaseArtifact identifies one downloadable release file for the management
// console. It is resolved from persisted release records by version, kind, and
// platform; callers never supply the URL themselves.
type ReleaseArtifact struct {
	Version     string `json:"version"`
	Kind        string `json:"kind"`
	Platform    string `json:"platform"`
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
}

// CompareVersions compares dotted semantic versions numerically. A release
// without a pre-release suffix sorts after the same release with one.
func CompareVersions(left string, right string) int {
	leftParts := strings.SplitN(strings.TrimPrefix(left, "v"), "-", 2)
	rightParts := strings.SplitN(strings.TrimPrefix(right, "v"), "-", 2)
	leftNumbers := strings.Split(leftParts[0], ".")
	rightNumbers := strings.Split(rightParts[0], ".")
	for index := 0; index < 3; index++ {
		leftValue := versionPart(leftNumbers, index)
		rightValue := versionPart(rightNumbers, index)
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	// Both values are equal without a pre-release suffix; indexing the split
	// result here would panic because SplitN returns one element.
	leftPrerelease := versionPrerelease(leftParts)
	rightPrerelease := versionPrerelease(rightParts)
	switch {
	case leftPrerelease == "" && rightPrerelease == "":
		return 0
	case leftPrerelease == "":
		return 1
	case rightPrerelease == "":
		return -1
	default:
		return strings.Compare(leftPrerelease, rightPrerelease)
	}
}

// versionPrerelease returns the suffix after the first hyphen, or an empty
// string when the version has no pre-release part.
func versionPrerelease(parts []string) string {
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func versionPart(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	value := 0
	for _, character := range parts[index] {
		if character < '0' || character > '9' {
			break
		}
		value = value*10 + int(character-'0')
		if value > 1000000 {
			return 1000000
		}
	}
	return value
}
