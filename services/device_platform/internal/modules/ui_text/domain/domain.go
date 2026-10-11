// Package domain defines remote UI text domain types.
//
// Remote UI text is a signed, immutable package of user-visible strings for
// one platform and language. The platform owns the review and publication
// lifecycle; a device or client only ever consumes a published package, so a
// half-reviewed copy can never reach a user. Packages carry copy only: never
// child conversations, audio, images, tokens, credentials, or raw upstream
// errors.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrInvalidPackage reports malformed, missing, or out-of-range package data.
	ErrInvalidPackage = errors.New("ui text package invalid")
	// ErrInvalidEntry reports a text key, value, or variable list outside bounds.
	ErrInvalidEntry = errors.New("ui text entry invalid")
	// ErrInvalidTransition reports a lifecycle move that is not allowed.
	ErrInvalidTransition = errors.New("ui text lifecycle transition invalid")
	// ErrReviewRequired reports a publish attempt before approval.
	ErrReviewRequired = errors.New("ui text review approval required")
	// ErrReasonRequired reports a reject without an operator reason.
	ErrReasonRequired = errors.New("ui text review reason required")
	// ErrPackageNotFound reports an unknown package version or publication.
	ErrPackageNotFound = errors.New("ui text package not found")
	// ErrVersionExists reports a duplicate package version allocation.
	ErrVersionExists = errors.New("ui text package version already exists")
	// ErrSignatureRequired reports a publish attempt without a detached signature.
	ErrSignatureRequired = errors.New("ui text signature required")
	// ErrSignatureUnknownKey reports a signature key id without a trusted key.
	ErrSignatureUnknownKey = errors.New("ui text signature key unknown")
	// ErrSignatureInvalid reports a signature that failed verification.
	ErrSignatureInvalid = errors.New("ui text signature invalid")
	// ErrRevisionAhead reports a client cursor newer than the published revision.
	ErrRevisionAhead = errors.New("ui text revision ahead")
)

// Status is the review and delivery lifecycle of one package version.
//
// Approval is recorded as review metadata instead of a sixth status so the
// durable state machine remains exactly: draft -> in_review -> published ->
// withdrawn.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusInReview  Status = "in_review"
	StatusPublished Status = "published"
	StatusWithdrawn Status = "withdrawn"
)

// Action identifies one explicit lifecycle transition or review decision.
type Action string

const (
	ActionSubmit   Action = "submit"
	ActionApprove  Action = "approve"
	ActionReject   Action = "reject"
	ActionPublish  Action = "publish"
	ActionWithdraw Action = "withdraw"
)

// Platform is the client surface a package targets. The set mirrors the
// package contract so the platform and the client agree on what may be served.
type Platform string

const (
	PlatformParentApp      Platform = "parent_app"
	PlatformAdminWeb       Platform = "admin_web"
	PlatformDeviceFirmware Platform = "device_firmware"
)

// SignatureAlgorithmEd25519 is the only detached signature algorithm accepted
// for a remote UI text package. The verifier implements exactly this one.
const SignatureAlgorithmEd25519 = "ed25519"

const (
	// MaxEntries bounds how many strings one package may carry.
	MaxEntries = 2000
	// MaxValueRunes bounds one user-visible value.
	MaxValueRunes = 2000
	// MaxKeyLength bounds one stable text key.
	MaxKeyLength = 128
	// MaxVariables bounds how many placeholders one value may declare.
	MaxVariables = 16
	// MaxVariableLength bounds one placeholder name.
	MaxVariableLength = 64
	// MaxLanguageLength bounds a BCP-47-ish language tag.
	MaxLanguageLength = 16
	// MaxVersionLength bounds a client version constraint.
	MaxVersionLength = 32
	// MaxSignatureLength bounds a base64 detached signature.
	MaxSignatureLength = 8192
	// MaxKeyIDLength bounds a signature key identifier.
	MaxKeyIDLength = 128
	// MaxReasonRunes bounds a review decision reason.
	MaxReasonRunes = 500
	// DefaultPageSize is the operator list page size.
	DefaultPageSize = 20
	// MaxPageSize bounds one operator list page.
	MaxPageSize = 100
)

var (
	keyPattern       = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	variablePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	languagePattern  = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)
	keyIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	base64Pattern    = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)
	packageIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

// Package is a signed, immutable version of user-visible text for one platform
// and language.
type Package struct {
	PackageID        string     `json:"package_id"`
	PackageVersion   int        `json:"package_version"`
	Language         string     `json:"language"`
	Platform         Platform   `json:"platform"`
	MinClientVersion string     `json:"min_client_version"`
	Status           Status     `json:"status"`
	SignatureKeyID   string     `json:"signature_key_id,omitempty"`
	Signature        string     `json:"signature,omitempty"`
	SubmittedAt      *time.Time `json:"submitted_at,omitempty"`
	ApprovedAt       *time.Time `json:"approved_at,omitempty"`
	ApprovedBy       string     `json:"approved_by,omitempty"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`
	PublishedBy      string     `json:"published_by,omitempty"`
	WithdrawnAt      *time.Time `json:"withdrawn_at,omitempty"`
	Revision         int64      `json:"revision"`
	CreatedBy        string     `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Entry is one user-visible value addressed by a stable key.
type Entry struct {
	UITextKey string   `json:"ui_text_key"`
	Value     string   `json:"value"`
	MaxLength int      `json:"max_length"`
	Variables []string `json:"variables"`
}

// PackageDetail couples a package with its ordered entries so the console can
// review one version without a second round trip.
type PackageDetail struct {
	Package Package `json:"package"`
	Entries []Entry `json:"entries"`
}

// DraftInput is the editable payload accepted when a draft version is created.
type DraftInput struct {
	PackageID        string
	Language         string
	Platform         Platform
	MinClientVersion string
	Entries          []Entry
}

// ReviewInput is one explicit review decision.
type ReviewInput struct {
	Action Action
	Reason string
}

// PublicationInput is the detached signature that authorizes a publish.
type PublicationInput struct {
	SignatureKeyID string
	Signature      string
}

// PackageVersion is one summary row in the operator version history.
type PackageVersion struct {
	Package      Package
	EntryCount   int
	IsPublished  bool
	IsNewest     bool
	PublishedFor bool
}

// RevisionPage is one page of the published catalog plus an incremental cursor.
type RevisionPage struct {
	Revision   int64     `json:"revision"`
	Packages   []Package `json:"packages"`
	HasMore    bool      `json:"has_more,omitempty"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// PublishedPackage is one published package with its entries, addressed by the
// revision a client already holds.
type PublishedPackage struct {
	Package Package `json:"package"`
	Entries []Entry `json:"entries"`
}

// IsValidStatus reports whether a status is one of the durable lifecycle states.
func IsValidStatus(status Status) bool {
	switch status {
	case StatusDraft, StatusInReview, StatusPublished, StatusWithdrawn:
		return true
	default:
		return false
	}
}

// IsValidPlatform reports whether a package targets a served client surface.
func IsValidPlatform(platform Platform) bool {
	switch platform {
	case PlatformParentApp, PlatformAdminWeb, PlatformDeviceFirmware:
		return true
	default:
		return false
	}
}

// ValidSignatureAlgorithm reports whether a detached signature algorithm is
// supported. The private key is never part of the package.
func ValidSignatureAlgorithm(value string) bool {
	return strings.ToLower(strings.TrimSpace(value)) == SignatureAlgorithmEd25519
}

// ValidSignature reports whether a detached signature is a bounded-base64
// value. Cryptographic verification happens against the trusted keyring before
// publication, because a regex cannot prove authenticity.
func ValidSignature(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" &&
		len(value) <= MaxSignatureLength &&
		base64Pattern.MatchString(value)
}

// ValidKeyID reports whether a signature key id is a bounded stable token.
func ValidKeyID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" &&
		len(value) <= MaxKeyIDLength &&
		keyIDPattern.MatchString(value)
}

// ValidateDraft normalizes and validates one draft payload.
func ValidateDraft(input DraftInput) (DraftInput, error) {
	normalized := DraftInput{
		PackageID:        strings.TrimSpace(input.PackageID),
		Language:         strings.TrimSpace(input.Language),
		Platform:         input.Platform,
		MinClientVersion: strings.TrimSpace(input.MinClientVersion),
		Entries:          make([]Entry, 0, len(input.Entries)),
	}
	if normalized.PackageID == "" ||
		len(normalized.PackageID) > MaxKeyLength ||
		!packageIDPattern.MatchString(normalized.PackageID) {
		return DraftInput{}, ErrInvalidPackage
	}
	if len(normalized.Language) > MaxLanguageLength ||
		!languagePattern.MatchString(normalized.Language) {
		return DraftInput{}, ErrInvalidPackage
	}
	if !IsValidPlatform(normalized.Platform) {
		return DraftInput{}, ErrInvalidPackage
	}
	if len(normalized.MinClientVersion) > MaxVersionLength {
		return DraftInput{}, ErrInvalidPackage
	}
	if len(input.Entries) == 0 || len(input.Entries) > MaxEntries {
		return DraftInput{}, ErrInvalidPackage
	}
	seen := make(map[string]struct{}, len(input.Entries))
	for _, entry := range input.Entries {
		normalizedEntry, err := ValidateEntry(entry)
		if err != nil {
			return DraftInput{}, err
		}
		if _, exists := seen[normalizedEntry.UITextKey]; exists {
			return DraftInput{}, ErrInvalidEntry
		}
		seen[normalizedEntry.UITextKey] = struct{}{}
		normalized.Entries = append(normalized.Entries, normalizedEntry)
	}
	return normalized, nil
}

// ValidateEntry normalizes and validates one text entry.
func ValidateEntry(entry Entry) (Entry, error) {
	normalized := Entry{
		UITextKey: strings.TrimSpace(entry.UITextKey),
		Value:     strings.TrimSpace(entry.Value),
		MaxLength: entry.MaxLength,
		Variables: make([]string, 0, len(entry.Variables)),
	}
	if normalized.UITextKey == "" ||
		len(normalized.UITextKey) > MaxKeyLength ||
		!keyPattern.MatchString(normalized.UITextKey) {
		return Entry{}, ErrInvalidEntry
	}
	if normalized.MaxLength < 1 || normalized.MaxLength > MaxValueRunes {
		return Entry{}, ErrInvalidEntry
	}
	if len([]rune(normalized.Value)) > normalized.MaxLength {
		return Entry{}, ErrInvalidEntry
	}
	if len(entry.Variables) > MaxVariables {
		return Entry{}, ErrInvalidEntry
	}
	seen := make(map[string]struct{}, len(entry.Variables))
	for _, variable := range entry.Variables {
		trimmed := strings.TrimSpace(variable)
		if trimmed == "" ||
			len(trimmed) > MaxVariableLength ||
			!variablePattern.MatchString(trimmed) {
			return Entry{}, ErrInvalidEntry
		}
		if _, exists := seen[trimmed]; exists {
			return Entry{}, ErrInvalidEntry
		}
		seen[trimmed] = struct{}{}
		normalized.Variables = append(normalized.Variables, trimmed)
	}
	return normalized, nil
}

// ValidateTransition reports whether one lifecycle move is permitted from the
// current status, and returns the resulting status.
func ValidateTransition(current Status, action Action) (Status, error) {
	switch action {
	case ActionSubmit:
		if current == StatusDraft {
			return StatusInReview, nil
		}
	case ActionApprove:
		if current == StatusInReview {
			return StatusInReview, nil
		}
	case ActionReject:
		if current == StatusInReview {
			return StatusDraft, nil
		}
	case ActionPublish:
		if current == StatusInReview {
			return StatusPublished, nil
		}
	case ActionWithdraw:
		if current == StatusPublished {
			return StatusWithdrawn, nil
		}
	}
	return "", ErrInvalidTransition
}
