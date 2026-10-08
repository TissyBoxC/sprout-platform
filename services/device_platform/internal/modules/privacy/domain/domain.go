// Package domain defines guardian privacy, consent, export, deletion, and
// administrator audit contract types.
package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidConsentVersion = errors.New("invalid guardian consent version")
	ErrConsentRequired       = errors.New("guardian consent is required")
	ErrDeletionPending       = errors.New("account deletion is already pending")
	ErrDeletionNotPending    = errors.New("account deletion is not pending")
	ErrDeletionNotDue        = errors.New("account deletion is not due")
	ErrDeletionNotFound      = errors.New("account deletion request not found")
	ErrInvalidDeletionReason = errors.New("invalid account deletion reason")
	ErrAccountUnavailable    = errors.New("account is unavailable")
	ErrAuditQueryInvalid     = errors.New("invalid audit query")
)

const (
	ConsentStatusActive    = "active"
	ConsentStatusWithdrawn = "withdrawn"

	ConsentTypeGuardianTerms       = "guardian_terms"
	ConsentTypeChildDataProcessing = "child_data_processing"
	ConsentTypeAIInteraction       = "ai_interaction"
	ConsentTypeEmailContact        = "email_contact"

	ConsentSourceRegistration    = "registration"
	ConsentSourceGuardian        = "guardian"
	ConsentSourceAdministrator   = "administrator"
	ConsentSourceSystem          = "system"
	DeletionStatusNone           = "none"
	DeletionStatusPending        = "pending"
	DeletionStatusProcessing     = "processing"
	DeletionStatusCancelled      = "cancelled"
	DeletionStatusCompleted      = "completed"
	DeletionStatusFailed         = "failed"
	ExportStatusCompleted        = "completed"
	ExportStatusFailed           = "failed"
	DefaultDeletionGracePeriod   = 7 * 24 * time.Hour
	DefaultDeletionBatchSize     = 20
	DefaultAuditPageSize         = 20
	MaxAuditPageSize             = 100
	MaxAuditActorFilterRunes     = 128
	MaxAuditTargetFilterRunes    = 128
	DefaultDeletionReasonMaxRune = 500
)

// ConsentEvent is one append-only guardian authorization decision.
type ConsentEvent struct {
	ID              string         `json:"id"`
	ParentAccountID string         `json:"-"`
	ConsentType     string         `json:"consent_type"`
	ConsentVersion  string         `json:"consent_version"`
	Granted         bool           `json:"granted"`
	Source          string         `json:"source"`
	Detail          map[string]any `json:"detail"`
	CreatedAt       time.Time      `json:"created_at"`
}

// ConsentStatus is the guardian-facing current consent projection.
type ConsentStatus struct {
	Version       string     `json:"version"`
	ConsentedAt   *time.Time `json:"consented_at"`
	WithdrawnAt   *time.Time `json:"withdrawn_at"`
	Status        string     `json:"status"`
	Active        bool       `json:"active"`
	ReconsentPath string     `json:"reconsent_path"`
}

// RetentionPolicy is the disclosed retention window for sensitive data.
type RetentionPolicy struct {
	AudioRetentionDays        int `json:"audio_retention_days"`
	ImageRetentionDays        int `json:"image_retention_days"`
	ConversationRetentionDays int `json:"conversation_retention_days"`
}

// DeletionRequest is one cancellable account deletion lifecycle.
type DeletionRequest struct {
	ParentAccountID string     `json:"-"`
	ID              string     `json:"id"`
	Status          string     `json:"status"`
	Reason          string     `json:"reason"`
	RequestedAt     time.Time  `json:"requested_at"`
	ExecuteAfter    time.Time  `json:"execute_after"`
	CancelledAt     *time.Time `json:"cancelled_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	FailureReason   string     `json:"failure_reason"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Cancellable     bool       `json:"cancellable"`
	ScheduledFor    *time.Time `json:"scheduled_for"`
}

// ExportReceipt is one completed guardian data export.
type ExportReceipt struct {
	ParentAccountID string         `json:"-"`
	ID              string         `json:"id"`
	Format          string         `json:"format"`
	Status          string         `json:"status"`
	ItemCounts      map[string]int `json:"item_counts"`
	CreatedAt       time.Time      `json:"created_at"`
	ExpiresAt       time.Time      `json:"expires_at"`
}

// Status is the combined privacy control state returned to the guardian.
type Status struct {
	Consent  ConsentStatus   `json:"consent"`
	Deletion DeletionRequest `json:"deletion"`
	Export   ExportStatus    `json:"export"`
	Policy   RetentionPolicy `json:"policy"`
}

// ExportStatus tells the client whether a data copy can be obtained now.
type ExportStatus struct {
	Available bool           `json:"available"`
	Last      *ExportReceipt `json:"last,omitempty"`
}

// DataExport is the redacted, guardian-owned data copy.
type DataExport struct {
	SchemaVersion string             `json:"schema_version"`
	ExportedAt    time.Time          `json:"exported_at"`
	Account       ExportAccount      `json:"account"`
	Children      []ExportChild      `json:"children"`
	Devices       []ExportDevice     `json:"devices"`
	Usage         ExportUsageSummary `json:"usage"`
	Consents      []ConsentEvent     `json:"consent_history"`
	Policy        RetentionPolicy    `json:"retention"`
}

// ExportAccount deliberately contains no password hash, token, credential, or
// provider account identifier.
type ExportAccount struct {
	ID                 string     `json:"id"`
	Phone              string     `json:"phone"`
	Email              string     `json:"email"`
	DisplayName        string     `json:"display_name"`
	GuardianFamilyName string     `json:"guardian_family_name"`
	ChildNickname      string     `json:"child_nickname"`
	ChildBirthday      string     `json:"child_birthday"`
	Status             string     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	LastLoginAt        *time.Time `json:"last_login_at"`
}

// ExportChild is an owner-visible child profile.
type ExportChild struct {
	ID                     string    `json:"id"`
	Nickname               string    `json:"nickname"`
	AgeTier                string    `json:"age_tier"`
	Interests              []string  `json:"interests"`
	ContentCategories      []string  `json:"content_categories"`
	GuardianConsentVersion string    `json:"guardian_consent_version"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// ExportDevice is an owner-visible device binding summary.
type ExportDevice struct {
	DeviceID        string    `json:"device_id"`
	DeviceName      string    `json:"device_name"`
	HardwareModel   string    `json:"hardware_model"`
	FirmwareVersion string    `json:"firmware_version"`
	CapabilitySet   []string  `json:"capability_set"`
	BoundAt         time.Time `json:"bound_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Online          bool      `json:"online"`
}

// ExportUsageSummary contains aggregate usage rather than raw conversations.
type ExportUsageSummary struct {
	Days                int     `json:"days"`
	ConversationCount   int64   `json:"conversation_count"`
	ActiveSeconds       int64   `json:"active_seconds"`
	ConversationSeconds int64   `json:"conversation_seconds"`
	ContentPlayCount    int64   `json:"content_play_count"`
	ContentSeconds      int64   `json:"content_seconds"`
	SpentUSD            float64 `json:"spent_usd"`
}

// AuditEntry is one safe administrator-visible operation event.
type AuditEntry struct {
	ID                string         `json:"id"`
	ActorAccountID    string         `json:"actor_account_id"`
	ActorDisplayName  string         `json:"actor_display_name"`
	TargetAccountID   string         `json:"target_account_id"`
	TargetDisplayName string         `json:"target_display_name"`
	Action            string         `json:"action"`
	Detail            map[string]any `json:"detail"`
	CreatedAt         time.Time      `json:"created_at"`
}

// AuditPage is the paginated administrator audit response.
type AuditPage struct {
	Items    []AuditEntry `json:"items"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Actions  []string     `json:"actions"`
}

// AuditFilter is the validated administrator query.
type AuditFilter struct {
	Action          string
	ActorAccountID  string
	TargetAccountID string
	From            *time.Time
	To              *time.Time
	Page            int
	PageSize        int
}

// DeletionCandidate is one due request claimed by the worker.
type DeletionCandidate struct {
	Request         DeletionRequest
	ParentAccountID string
}
