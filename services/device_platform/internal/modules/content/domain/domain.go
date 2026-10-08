// Package domain defines content-library models and lifecycle rules.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Status is the review and delivery lifecycle of one package version.
//
// Approval is recorded as review metadata instead of a sixth status so the
// durable state machine remains exactly: draft -> in_review -> published ->
// withdrawn -> archived.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusInReview  Status = "in_review"
	StatusPublished Status = "published"
	StatusWithdrawn Status = "withdrawn"
	StatusArchived  Status = "archived"
)

// Action identifies one explicit lifecycle transition or review decision.
type Action string

const (
	ActionSubmit   Action = "submit"
	ActionApprove  Action = "approve"
	ActionReject   Action = "reject"
	ActionPublish  Action = "publish"
	ActionWithdraw Action = "withdraw"
	ActionArchive  Action = "archive"
)

// Category is the platform-approved content taxonomy.
type Category string

const (
	CategoryStory        Category = "story"
	CategoryNurseryRhyme Category = "nursery_rhyme"
	CategoryPoetry       Category = "poetry"
	CategoryEnglish      Category = "english"
	CategoryEncyclopedia Category = "encyclopedia"
	CategoryBedtime      Category = "bedtime"
)

// AgeTier is a guardian-selectable age band for a content package.
type AgeTier string

const (
	AgeTier3To4 AgeTier = "age_3_4"
	AgeTier5To6 AgeTier = "age_5_6"
	AgeTier7To8 AgeTier = "age_7_8"
)

var (
	// ErrInvalidTransition reports a lifecycle move that is not allowed.
	ErrInvalidTransition = errors.New("content lifecycle transition invalid")
	// ErrReviewRequired reports a publish attempt before approval.
	ErrReviewRequired = errors.New("content review approval required")
	// ErrReasonRequired reports a reject without an operator reason.
	ErrReasonRequired = errors.New("content review reason required")
	// ErrInvalidMetadata reports malformed, missing, or out-of-range content data.
	ErrInvalidMetadata = errors.New("content metadata invalid")
	// ErrPackageNotFound reports an unknown package, version, or published item.
	ErrPackageNotFound = errors.New("content package not found")
	// ErrVersionExists reports a duplicate package version allocation.
	ErrVersionExists = errors.New("content package version already exists")
	// ErrRevisionAhead reports a client cursor newer than the catalog.
	ErrRevisionAhead = errors.New("content catalog revision ahead")
	// ErrAssetNotFound reports a missing blob in the shared download store.
	ErrAssetNotFound = errors.New("content asset not found")
	// ErrAssetChecksumMismatch reports a stored blob that no longer matches metadata.
	ErrAssetChecksumMismatch = errors.New("content asset checksum mismatch")
)

var (
	packageIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$`)
	sha256Pattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	assetKeyPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)
)

// PackageVersion is one immutable identity plus mutable review metadata.
type PackageVersion struct {
	PackageID       string     `json:"package_id"`
	PackageVersion  int        `json:"package_version"`
	Title           string     `json:"title"`
	Category        Category   `json:"category"`
	AgeTiers        []AgeTier  `json:"age_tiers"`
	AssetKey        string     `json:"asset_key"`
	SHA256          string     `json:"sha256"`
	SizeBytes       int64      `json:"size_bytes"`
	Status          Status     `json:"status"`
	SubmittedAt     *time.Time `json:"submitted_at,omitempty"`
	ApprovedAt      *time.Time `json:"approved_at,omitempty"`
	ApprovedBy      string     `json:"approved_by,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	CatalogRevision int64      `json:"catalog_revision"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ReviewLog is one auditable lifecycle decision.
type ReviewLog struct {
	ID             string    `json:"id"`
	PackageID      string    `json:"package_id"`
	PackageVersion int       `json:"package_version"`
	Action         Action    `json:"action"`
	FromStatus     Status    `json:"from_status"`
	ToStatus       Status    `json:"to_status"`
	Reason         string    `json:"reason,omitempty"`
	ActorAccountID string    `json:"actor_account_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// PackageDetail is the admin view of every version and its review history.
type PackageDetail struct {
	PackageID string           `json:"package_id"`
	Versions  []PackageVersion `json:"versions"`
	History   []ReviewLog      `json:"history"`
}

// PackageListFilter controls the admin package list.
type PackageListFilter struct {
	Category string
	AgeTier  string
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

// PackagePage is one stable admin list page.
type PackagePage struct {
	Packages []PackageVersion `json:"packages"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int              `json:"total"`
}

// MetadataInput is the validated payload for draft creation and update.
type MetadataInput struct {
	PackageID string
	Title     string
	Category  Category
	AgeTiers  []AgeTier
	AssetKey  string
	SHA256    string
	SizeBytes int64
}

// Transition is one validated lifecycle mutation applied atomically by the
// repository, including its audit record and catalog revision increment.
type Transition struct {
	PackageID      string
	PackageVersion int
	FromStatus     Status
	ToStatus       Status
	Action         Action
	Reason         string
	ActorAccountID string
	Now            time.Time
}

// TransitionError couples a repository transition failure to the package row
// that was read before the write so the service can report a stable conflict.
type TransitionError struct {
	Current *PackageVersion
	Err     error
}

func (e *TransitionError) Error() string {
	if e == nil || e.Err == nil {
		return "content transition failed"
	}
	return e.Err.Error()
}

func (e *TransitionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CatalogQuery is the incremental device/parent manifest filter.
type CatalogQuery struct {
	SinceRevision int64
	AgeTier       string
	Category      string
	// FamilyID is set only on the device-facing path. When present, the
	// service intersects the requested category with the family policy.
	FamilyID          string
	AllowedCategories []string
}

// Catalog is the incremental manifest returned to devices and parent clients.
type Catalog struct {
	Revision            int64            `json:"catalog_revision"`
	Packages            []PackageVersion `json:"packages"`
	WithdrawnPackageIDs []string         `json:"withdrawn_package_ids"`
	GeneratedAt         time.Time        `json:"generated_at"`
}

// Asset is the verified download-store projection used by content packages.
type Asset struct {
	Key         string
	SHA256      string
	SizeBytes   int64
	DownloadURL string
}

// DownloadInfo describes one published package download.
type DownloadInfo struct {
	PackageID      string    `json:"package_id"`
	PackageVersion int       `json:"package_version"`
	Title          string    `json:"title"`
	AssetKey       string    `json:"asset_key"`
	SHA256         string    `json:"sha256"`
	SizeBytes      int64     `json:"size_bytes"`
	DownloadURL    string    `json:"download_url"`
	PublishedAt    time.Time `json:"published_at"`
}

// ValidateMetadata normalizes and validates one package version payload.
func ValidateMetadata(input MetadataInput) (MetadataInput, error) {
	input.PackageID = strings.TrimSpace(input.PackageID)
	input.Title = strings.TrimSpace(input.Title)
	input.AssetKey = strings.TrimSpace(input.AssetKey)
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))

	if !packageIDPattern.MatchString(input.PackageID) {
		return MetadataInput{}, ErrInvalidMetadata
	}
	if input.Title == "" || len([]rune(input.Title)) > 128 {
		return MetadataInput{}, ErrInvalidMetadata
	}
	if !isCategory(input.Category) {
		return MetadataInput{}, ErrInvalidMetadata
	}
	if len(input.AgeTiers) == 0 || len(input.AgeTiers) > 3 {
		return MetadataInput{}, ErrInvalidMetadata
	}
	seenAgeTiers := make(map[AgeTier]struct{}, len(input.AgeTiers))
	normalizedAgeTiers := make([]AgeTier, 0, len(input.AgeTiers))
	for _, ageTier := range input.AgeTiers {
		if !isAgeTier(ageTier) {
			return MetadataInput{}, ErrInvalidMetadata
		}
		if _, exists := seenAgeTiers[ageTier]; exists {
			return MetadataInput{}, ErrInvalidMetadata
		}
		seenAgeTiers[ageTier] = struct{}{}
		normalizedAgeTiers = append(normalizedAgeTiers, ageTier)
	}
	if !assetKeyPattern.MatchString(input.AssetKey) ||
		strings.Contains(input.AssetKey, "..") {
		return MetadataInput{}, ErrInvalidMetadata
	}
	if !sha256Pattern.MatchString(input.SHA256) {
		return MetadataInput{}, ErrInvalidMetadata
	}
	if input.SizeBytes < 1 || input.SizeBytes > 4*1024*1024*1024 {
		return MetadataInput{}, ErrInvalidMetadata
	}
	input.AgeTiers = normalizedAgeTiers
	return input, nil
}

// NextStatus applies one lifecycle transition to the current status.
//
// Approve is intentionally not a status transition: it records approval while
// keeping the package in review so publish remains an explicit operator action.
func NextStatus(current Status, action Action) (Status, error) {
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
	case ActionArchive:
		if current == StatusWithdrawn {
			return StatusArchived, nil
		}
	}
	return "", ErrInvalidTransition
}

// IsCategory reports whether value is one of the supported categories.
func IsCategory(value Category) bool {
	return isCategory(value)
}

// IsAgeTier reports whether value is one of the supported age tiers.
func IsAgeTier(value string) bool {
	return isAgeTier(AgeTier(value))
}

func isCategory(value Category) bool {
	switch value {
	case CategoryStory,
		CategoryNurseryRhyme,
		CategoryPoetry,
		CategoryEnglish,
		CategoryEncyclopedia,
		CategoryBedtime:
		return true
	default:
		return false
	}
}

func isAgeTier(value AgeTier) bool {
	switch value {
	case AgeTier3To4, AgeTier5To6, AgeTier7To8:
		return true
	default:
		return false
	}
}
