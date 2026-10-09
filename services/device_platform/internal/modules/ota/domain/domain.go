// Package domain contains device OTA releases, deployments, and events.
package domain

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrInvalidRelease reports a malformed or unsafe release manifest.
	ErrInvalidRelease = errors.New("invalid OTA release")
	// ErrReleaseNotFound reports a release that does not exist.
	ErrReleaseNotFound = errors.New("OTA release not found")
	// ErrReleaseAlreadyExists reports a duplicate release identity.
	ErrReleaseAlreadyExists = errors.New("OTA release already exists")
	// ErrReleaseVersionConflict reports an optimistic-locking conflict.
	ErrReleaseVersionConflict = errors.New("OTA release version conflict")
	// ErrReleaseStateConflict reports an illegal release state transition.
	ErrReleaseStateConflict = errors.New("OTA release state conflict")
	// ErrRollbackNotAllowed reports a rollback that was not published as safe.
	ErrRollbackNotAllowed = errors.New("OTA rollback is not allowed")

	// ErrInvalidDeployment reports malformed deployment input.
	ErrInvalidDeployment = errors.New("invalid OTA deployment")
	// ErrDeploymentNotFound reports a deployment that does not exist.
	ErrDeploymentNotFound = errors.New("OTA deployment not found")
	// ErrDeploymentAlreadyExists reports an existing release/device task.
	ErrDeploymentAlreadyExists = errors.New("OTA deployment already exists")
	// ErrDeploymentVersionConflict reports an optimistic-locking conflict.
	ErrDeploymentVersionConflict = errors.New("OTA deployment version conflict")
	// ErrDeploymentStateConflict reports an illegal deployment transition.
	ErrDeploymentStateConflict = errors.New("OTA deployment state conflict")

	// ErrInvalidEvent reports malformed or regressing device progress.
	ErrInvalidEvent = errors.New("invalid OTA event")
	// ErrEventConflict reports an event identifier reused for another payload.
	ErrEventConflict = errors.New("OTA event conflict")
	// ErrSignatureInvalid reports a firmware signature that failed verification.
	ErrSignatureInvalid = errors.New("OTA signature invalid")
	// ErrSignatureKeyUnknown reports a signature key id without a trusted key.
	ErrSignatureKeyUnknown = errors.New("OTA signature key unknown")
	// ErrSignatureRequired reports a publish attempt without a detached signature.
	ErrSignatureRequired = errors.New("OTA signature required")

	// ErrInvalidTarget reports an invalid release audience.
	ErrInvalidTarget = errors.New("invalid OTA target")
	// ErrInvalidActor reports a missing or malformed operator identity.
	ErrInvalidActor = errors.New("invalid OTA actor")
	// ErrDeviceNotFound reports a device without a registered credential.
	ErrDeviceNotFound = errors.New("OTA device not found")
	// ErrDeviceNotEligible reports a device outside the release audience.
	ErrDeviceNotEligible = errors.New("device is not eligible for OTA release")
	// ErrNoUpdateAvailable reports that the device is already current.
	ErrNoUpdateAvailable = errors.New("no OTA update available")
	// ErrInvalidGroup reports a malformed or missing device group.
	ErrInvalidGroup = errors.New("invalid OTA group")
	// ErrGroupAlreadyExists reports a duplicate group name.
	ErrGroupAlreadyExists = errors.New("OTA group already exists")
	// ErrGroupNotFound reports a group that does not exist.
	ErrGroupNotFound = errors.New("OTA group not found")
	// ErrGroupVersionConflict reports an optimistic-locking conflict.
	ErrGroupVersionConflict = errors.New("OTA group version conflict")
)

var (
	versionPattern = regexp.MustCompile(
		`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`,
	)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	base64Pattern = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)
)

// Channel is the release audience channel.
type Channel string

const (
	ChannelStable   Channel = "stable"
	ChannelCanary   Channel = "canary"
	ChannelInternal Channel = "internal"
)

// Valid reports whether the channel is part of the stable device contract.
func (channel Channel) Valid() bool {
	switch channel {
	case ChannelStable, ChannelCanary, ChannelInternal:
		return true
	default:
		return false
	}
}

// ReleaseStatus tracks the operator-controlled release lifecycle.
type ReleaseStatus string

const (
	ReleaseStatusDraft     ReleaseStatus = "draft"
	ReleaseStatusPublished ReleaseStatus = "published"
	ReleaseStatusPaused    ReleaseStatus = "paused"
	ReleaseStatusWithdrawn ReleaseStatus = "withdrawn"
)

// Detached signature verification states persisted with a release manifest.
const (
	SignatureStatusPending  = "pending"
	SignatureStatusVerified = "verified"
	SignatureStatusFailed   = "failed"
	SignatureStatusMissing  = "missing"
)

// DeploymentStatus tracks one device's installation lifecycle.
type DeploymentStatus string

const (
	DeploymentStatusQueued        DeploymentStatus = "queued"
	DeploymentStatusOffered       DeploymentStatus = "offered"
	DeploymentStatusDownloading   DeploymentStatus = "downloading"
	DeploymentStatusValidating    DeploymentStatus = "validating"
	DeploymentStatusInstalling    DeploymentStatus = "installing"
	DeploymentStatusPendingVerify DeploymentStatus = "pending_verify"
	DeploymentStatusSucceeded     DeploymentStatus = "succeeded"
	DeploymentStatusFailed        DeploymentStatus = "failed"
	DeploymentStatusRolledBack    DeploymentStatus = "rolled_back"
)

// EventType is the stable device progress event vocabulary.
type EventType string

const (
	EventTypeStarted         EventType = "started"
	EventTypeDownloading     EventType = "downloading"
	EventTypeDownloaded      EventType = "downloaded"
	EventTypeValidated       EventType = "validated"
	EventTypeInstalling      EventType = "installing"
	EventTypePendingVerify   EventType = "pending_verify"
	EventTypeSucceeded       EventType = "succeeded"
	EventTypeFailed          EventType = "failed"
	EventTypeRollbackStarted EventType = "rollback_started"
	EventTypeRolledBack      EventType = "rolled_back"
)

// Valid reports whether the event type is part of the stable contract.
func (eventType EventType) Valid() bool {
	switch eventType {
	case EventTypeStarted,
		EventTypeDownloading,
		EventTypeDownloaded,
		EventTypeValidated,
		EventTypeInstalling,
		EventTypePendingVerify,
		EventTypeSucceeded,
		EventTypeFailed,
		EventTypeRollbackStarted,
		EventTypeRolledBack:
		return true
	default:
		return false
	}
}

// TargetScope selects which devices may receive a release.
type TargetScope string

const (
	TargetScopeAll    TargetScope = "all"
	TargetScopeGroup  TargetScope = "group"
	TargetScopeDevice TargetScope = "device"
	TargetScopeCanary TargetScope = "canary"
)

// Valid reports whether the target scope is supported.
func (scope TargetScope) Valid() bool {
	switch scope {
	case TargetScopeAll, TargetScopeGroup, TargetScopeDevice, TargetScopeCanary:
		return true
	default:
		return false
	}
}

// Target is the immutable audience of a release.
type Target struct {
	Scope         TargetScope `json:"scope"`
	GroupID       string      `json:"group_id,omitempty"`
	DeviceID      string      `json:"device_id,omitempty"`
	CanaryPercent int         `json:"canary_percent,omitempty"`
}

// Validate checks the complete target policy.
func (target Target) Validate() error {
	if !target.Scope.Valid() {
		return ErrInvalidTarget
	}
	switch target.Scope {
	case TargetScopeGroup:
		if strings.TrimSpace(target.GroupID) == "" ||
			strings.TrimSpace(target.DeviceID) != "" ||
			target.CanaryPercent != 0 {
			return ErrInvalidTarget
		}
	case TargetScopeDevice:
		if strings.TrimSpace(target.DeviceID) == "" ||
			strings.TrimSpace(target.GroupID) != "" ||
			target.CanaryPercent != 0 {
			return ErrInvalidTarget
		}
	case TargetScopeCanary:
		if target.CanaryPercent < 1 ||
			target.CanaryPercent > 99 ||
			strings.TrimSpace(target.GroupID) != "" ||
			strings.TrimSpace(target.DeviceID) != "" {
			return ErrInvalidTarget
		}
	case TargetScopeAll:
		if strings.TrimSpace(target.GroupID) != "" ||
			strings.TrimSpace(target.DeviceID) != "" ||
			target.CanaryPercent != 0 {
			return ErrInvalidTarget
		}
	}
	return nil
}

// Release is an immutable firmware manifest after publication.
type Release struct {
	ID                  string        `json:"id"`
	Version             string        `json:"firmware_version"`
	Channel             Channel       `json:"channel"`
	HardwareRevision    string        `json:"hardware_revision"`
	MinSourceVersion    string        `json:"min_source_version"`
	ArtifactURL         string        `json:"artifact_url"`
	ArtifactKey         string        `json:"artifact_key"`
	SHA256              string        `json:"sha256"`
	SizeBytes           int64         `json:"size_bytes"`
	SignatureKeyID      string        `json:"signature_key_id"`
	SignatureAlgorithm  string        `json:"signature_algorithm"`
	Signature           string        `json:"signature"`
	SignatureStatus     string        `json:"signature_status"`
	SignatureVerifiedAt *time.Time    `json:"signature_verified_at,omitempty"`
	RollbackAllowed     bool          `json:"rollback_allowed"`
	Mandatory           bool          `json:"mandatory"`
	ReleaseNotes        string        `json:"release_notes"`
	Status              ReleaseStatus `json:"status"`
	Target              Target        `json:"target"`
	RollbackReleaseID   string        `json:"rollback_release_id,omitempty"`
	RecordVersion       int64         `json:"record_version"`
	CreatedBy           string        `json:"created_by"`
	UpdatedBy           string        `json:"updated_by"`
	PublishedBy         string        `json:"published_by,omitempty"`
	PausedBy            string        `json:"paused_by,omitempty"`
	WithdrawnBy         string        `json:"withdrawn_by,omitempty"`
	RolledBackBy        string        `json:"rolled_back_by,omitempty"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	PublishedAt         *time.Time    `json:"published_at,omitempty"`
	PausedAt            *time.Time    `json:"paused_at,omitempty"`
	WithdrawnAt         *time.Time    `json:"withdrawn_at,omitempty"`
	RolledBackAt        *time.Time    `json:"rolled_back_at,omitempty"`
}

// DeviceRelease is the minimal manifest sent to a device.
type DeviceRelease struct {
	ReleaseID          string        `json:"release_id"`
	Version            string        `json:"firmware_version"`
	Channel            Channel       `json:"channel"`
	HardwareRevision   string        `json:"hardware_revision"`
	MinSourceVersion   string        `json:"min_source_version,omitempty"`
	ArtifactURL        string        `json:"artifact_url"`
	ArtifactKey        string        `json:"artifact_key"`
	SHA256             string        `json:"sha256"`
	SizeBytes          int64         `json:"size_bytes"`
	SignatureKeyID     string        `json:"signature_key_id"`
	SignatureAlgorithm string        `json:"signature_algorithm"`
	Signature          string        `json:"signature"`
	RollbackAllowed    bool          `json:"rollback_allowed"`
	Mandatory          bool          `json:"mandatory"`
	Status             ReleaseStatus `json:"status"`
	ReleaseNotes       string        `json:"release_notes,omitempty"`
	PublishedAt        *time.Time    `json:"published_at,omitempty"`
}

// ReleaseInput is the validated operator request for a release draft.
type ReleaseInput struct {
	ID                 string
	Version            string
	Channel            Channel
	HardwareRevision   string
	MinSourceVersion   string
	ArtifactURL        string
	ArtifactKey        string
	SHA256             string
	SizeBytes          int64
	SignatureKeyID     string
	SignatureAlgorithm string
	Signature          string
	RollbackAllowed    bool
	Mandatory          bool
	ReleaseNotes       string
	Target             Target
	ActorID            string
	ExpectedVersion    int64
}

// Deployment is one release task assigned to one device.
type Deployment struct {
	ID                     string           `json:"id"`
	ReleaseID              string           `json:"release_id"`
	RollbackReleaseID      string           `json:"rollback_release_id,omitempty"`
	RollbackOfDeploymentID string           `json:"rollback_of_deployment_id,omitempty"`
	DeviceID               string           `json:"device_id"`
	GroupID                string           `json:"group_id,omitempty"`
	Status                 DeploymentStatus `json:"status"`
	RequestedBy            string           `json:"requested_by"`
	RequestID              string           `json:"request_id"`
	ProgressPercent        int              `json:"progress_percent"`
	BytesReceived          int64            `json:"bytes_received"`
	BytesTotal             int64            `json:"bytes_total"`
	FailureCode            string           `json:"failure_code,omitempty"`
	FailureMessage         string           `json:"failure_message,omitempty"`
	RetryCount             int              `json:"retry_count"`
	RecordVersion          int64            `json:"record_version"`
	LastEventSequence      int64            `json:"last_event_sequence"`
	CreatedAt              time.Time        `json:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at"`
	OfferedAt              *time.Time       `json:"offered_at,omitempty"`
	DownloadStartedAt      *time.Time       `json:"download_started_at,omitempty"`
	ValidatedAt            *time.Time       `json:"validated_at,omitempty"`
	InstallStartedAt       *time.Time       `json:"install_started_at,omitempty"`
	VerificationStartedAt  *time.Time       `json:"verification_started_at,omitempty"`
	CompletedAt            *time.Time       `json:"completed_at,omitempty"`
	FailedAt               *time.Time       `json:"failed_at,omitempty"`
	RolledBackAt           *time.Time       `json:"rolled_back_at,omitempty"`
}

// Event is one idempotent device progress update.
type Event struct {
	ID              string           `json:"id"`
	DeploymentID    string           `json:"deployment_id"`
	EventID         string           `json:"event_id"`
	Type            EventType        `json:"type"`
	Sequence        int64            `json:"sequence"`
	Status          DeploymentStatus `json:"status"`
	ProgressPercent int              `json:"progress_percent"`
	BytesReceived   int64            `json:"bytes_received"`
	BytesTotal      int64            `json:"bytes_total"`
	ErrorCode       string           `json:"error_code,omitempty"`
	Message         string           `json:"message,omitempty"`
	Detail          map[string]any   `json:"detail,omitempty"`
	ReportedAt      time.Time        `json:"reported_at"`
	ReceivedAt      time.Time        `json:"received_at"`
}

// EventInput is the device-authenticated progress request.
type EventInput struct {
	DeploymentID    string
	DeviceID        string
	EventID         string
	Type            EventType
	Sequence        int64
	ProgressPercent int
	BytesReceived   int64
	BytesTotal      int64
	ErrorCode       string
	Message         string
	Detail          map[string]any
	ReportedAt      time.Time
}

// DeviceContext is the OTA-relevant projection of one registered device.
type DeviceContext struct {
	DeviceID         string
	HardwareRevision string
	CurrentVersion   string
	GroupIDs         []string
}

// Group is a stable named audience for firmware rollouts.
type Group struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Channel          Channel   `json:"channel"`
	HardwareRevision string    `json:"hardware_revision"`
	RecordVersion    int64     `json:"record_version"`
	CreatedBy        string    `json:"created_by"`
	UpdatedBy        string    `json:"updated_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// GroupInput is a validated group write.
type GroupInput struct {
	ID               string
	Name             string
	Description      string
	Channel          Channel
	HardwareRevision string
	ActorID          string
	ExpectedVersion  int64
}

// ReleaseFilter controls bounded, operator-facing release queries.
type ReleaseFilter struct {
	Status           ReleaseStatus
	Channel          Channel
	Version          string
	HardwareRevision string
	GroupID          string
	DeviceID         string
	Page             int
	PageSize         int
}

// DeploymentFilter controls bounded deployment and progress queries.
type DeploymentFilter struct {
	ReleaseID string
	DeviceID  string
	GroupID   string
	Status    DeploymentStatus
	Channel   Channel
	Version   string
	Page      int
	PageSize  int
}

// ReleasePage is one bounded release result.
type ReleasePage struct {
	Items    []Release `json:"items"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

// DeploymentPage is one bounded deployment result.
type DeploymentPage struct {
	Items    []Deployment `json:"items"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

// Statistics is the management console's aggregate rollout projection.
type Statistics struct {
	Total           int64            `json:"total"`
	ByStatus        map[string]int64 `json:"by_status"`
	ByChannel       map[string]int64 `json:"by_channel"`
	ByVersion       map[string]int64 `json:"by_version"`
	Failed          int64            `json:"failed"`
	RolledBack      int64            `json:"rolled_back"`
	FailureRate     float64          `json:"failure_rate"`
	ProgressAverage float64          `json:"progress_average"`
}

// ReleaseVersionConflictError carries the compared versions without exposing
// database details.
type ReleaseVersionConflictError struct {
	ExpectedVersion int64
	CurrentVersion  int64
}

// Error implements error.
func (e *ReleaseVersionConflictError) Error() string {
	return "OTA release version conflict"
}

// Is allows errors.Is(err, ErrReleaseVersionConflict).
func (e *ReleaseVersionConflictError) Is(target error) bool {
	return target == ErrReleaseVersionConflict
}

// DeploymentVersionConflictError carries the compared versions without
// exposing database details.
type DeploymentVersionConflictError struct {
	ExpectedVersion int64
	CurrentVersion  int64
}

// Error implements error.
func (e *DeploymentVersionConflictError) Error() string {
	return "OTA deployment version conflict"
}

// Is allows errors.Is(err, ErrDeploymentVersionConflict).
func (e *DeploymentVersionConflictError) Is(target error) bool {
	return target == ErrDeploymentVersionConflict
}

// GroupVersionConflictError carries the compared versions without exposing
// database details.
type GroupVersionConflictError struct {
	ExpectedVersion int64
	CurrentVersion  int64
}

// Error implements error.
func (e *GroupVersionConflictError) Error() string {
	return "OTA group version conflict"
}

// Is allows errors.Is(err, ErrGroupVersionConflict).
func (e *GroupVersionConflictError) Is(target error) bool {
	return target == ErrGroupVersionConflict
}

// ValidVersion reports whether value is a supported dotted firmware version.
func ValidVersion(value string) bool {
	return versionPattern.MatchString(strings.TrimSpace(value))
}

// NormalizeSHA256 trims and lowercases a digest before validation.
func NormalizeSHA256(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// ValidSHA256 reports whether value is a complete lowercase SHA-256 digest.
func ValidSHA256(value string) bool {
	return sha256Pattern.MatchString(value)
}

// CompareVersions compares dotted semantic versions numerically.
func CompareVersions(left string, right string) int {
	leftNumbers, leftPrerelease := splitVersion(left)
	rightNumbers, rightPrerelease := splitVersion(right)
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

func splitVersion(value string) ([]string, string) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.SplitN(value, "-", 2)
	numbers := strings.Split(parts[0], ".")
	if len(parts) == 1 {
		return numbers, ""
	}
	return numbers, parts[1]
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

// CanTransitionRelease reports whether a release status transition is legal.
func CanTransitionRelease(from ReleaseStatus, to ReleaseStatus) bool {
	switch from {
	case ReleaseStatusDraft:
		return to == ReleaseStatusPublished || to == ReleaseStatusWithdrawn
	case ReleaseStatusPublished:
		return to == ReleaseStatusPaused || to == ReleaseStatusWithdrawn
	case ReleaseStatusPaused:
		return to == ReleaseStatusPublished || to == ReleaseStatusWithdrawn
	case ReleaseStatusWithdrawn:
		return false
	default:
		return false
	}
}

// CanTransitionDeployment reports whether a deployment transition is legal.
func CanTransitionDeployment(from DeploymentStatus, to DeploymentStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case DeploymentStatusQueued:
		return to == DeploymentStatusOffered || to == DeploymentStatusFailed
	case DeploymentStatusOffered:
		return to == DeploymentStatusDownloading || to == DeploymentStatusFailed
	case DeploymentStatusDownloading:
		return to == DeploymentStatusValidating ||
			to == DeploymentStatusFailed ||
			to == DeploymentStatusRolledBack
	case DeploymentStatusValidating:
		return to == DeploymentStatusInstalling ||
			to == DeploymentStatusFailed ||
			to == DeploymentStatusRolledBack
	case DeploymentStatusInstalling:
		return to == DeploymentStatusPendingVerify ||
			to == DeploymentStatusFailed ||
			to == DeploymentStatusRolledBack
	case DeploymentStatusPendingVerify:
		return to == DeploymentStatusSucceeded ||
			to == DeploymentStatusFailed ||
			to == DeploymentStatusRolledBack
	case DeploymentStatusFailed:
		return to == DeploymentStatusQueued || to == DeploymentStatusRolledBack
	default:
		return false
	}
}

// ValidateRelease checks the complete immutable manifest before persistence.
func ValidateRelease(release *Release) error {
	if release == nil {
		return ErrInvalidRelease
	}
	release.Version = strings.TrimSpace(release.Version)
	release.HardwareRevision = strings.TrimSpace(release.HardwareRevision)
	release.MinSourceVersion = strings.TrimSpace(release.MinSourceVersion)
	release.ArtifactURL = strings.TrimSpace(release.ArtifactURL)
	release.ArtifactKey = strings.TrimSpace(release.ArtifactKey)
	release.SHA256 = NormalizeSHA256(release.SHA256)
	release.SignatureKeyID = strings.TrimSpace(release.SignatureKeyID)
	release.SignatureAlgorithm = strings.TrimSpace(release.SignatureAlgorithm)
	release.Signature = strings.TrimSpace(release.Signature)
	release.ReleaseNotes = strings.TrimSpace(release.ReleaseNotes)

	if !ValidVersion(release.Version) ||
		!release.Channel.Valid() ||
		release.HardwareRevision == "" ||
		len(release.HardwareRevision) > 128 ||
		(release.MinSourceVersion != "" && !ValidVersion(release.MinSourceVersion)) ||
		release.ArtifactKey == "" ||
		len(release.ArtifactKey) > 512 ||
		!ValidSHA256(release.SHA256) ||
		release.SizeBytes <= 0 ||
		release.SizeBytes > 512*1024*1024 ||
		release.SignatureKeyID == "" ||
		len(release.SignatureKeyID) > 128 ||
		!ValidSignatureAlgorithm(release.SignatureAlgorithm) ||
		!ValidSignature(release.Signature) ||
		len([]rune(release.ReleaseNotes)) > 4000 ||
		release.Target.Validate() != nil {
		return ErrInvalidRelease
	}
	if !isHTTPSArtifactURL(release.ArtifactURL) {
		return ErrInvalidRelease
	}
	return nil
}

// ValidSignature reports whether a detached signature is a bounded-base64
// value. Cryptographic verification happens against the trusted keyring before
// publication, because a regex cannot prove authenticity.
func ValidSignature(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" &&
		len(value) <= 8192 &&
		base64Pattern.MatchString(value)
}

// ValidateDeployment checks a new release/device assignment.
func ValidateDeployment(deployment *Deployment) error {
	if deployment == nil ||
		strings.TrimSpace(deployment.ReleaseID) == "" ||
		strings.TrimSpace(deployment.DeviceID) == "" ||
		strings.TrimSpace(deployment.RequestID) == "" ||
		len(deployment.RequestID) > 128 {
		return ErrInvalidDeployment
	}
	if deployment.Status == "" {
		deployment.Status = DeploymentStatusQueued
	}
	if deployment.Status != DeploymentStatusQueued {
		return ErrInvalidDeployment
	}
	if deployment.BytesTotal < 0 ||
		deployment.BytesReceived < 0 ||
		deployment.BytesReceived > deployment.BytesTotal {
		return ErrInvalidDeployment
	}
	return nil
}

// ValidateEvent checks a device event before it reaches persistence.
func ValidateEvent(event *Event) error {
	if event == nil ||
		strings.TrimSpace(event.DeploymentID) == "" ||
		strings.TrimSpace(event.EventID) == "" ||
		len(event.EventID) > 128 ||
		event.Sequence <= 0 ||
		!event.Type.Valid() ||
		event.ProgressPercent < 0 ||
		event.ProgressPercent > 100 ||
		event.BytesReceived < 0 ||
		event.BytesTotal < 0 ||
		event.BytesReceived > event.BytesTotal ||
		len(event.ErrorCode) > 128 ||
		len([]rune(event.Message)) > 1000 ||
		event.ReportedAt.IsZero() {
		return ErrInvalidEvent
	}
	return nil
}

// ValidSignatureAlgorithm reports whether a firmware signature algorithm is
// supported. The private key is never part of the manifest.
func ValidSignatureAlgorithm(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ed25519":
		return true
	default:
		return false
	}
}

func isHTTPSArtifactURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil &&
		parsed.Scheme == "https" &&
		parsed.Host != "" &&
		parsed.User == nil
}

// CanaryContains applies a deterministic percentage rollout. Keeping the hash
// over version and device id means a device stays in the same bucket across
// retries and platform restarts.
func CanaryContains(version string, deviceID string, percent int) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(version) + "\x00" + strings.TrimSpace(deviceID)))
	bucket := binary.BigEndian.Uint64(sum[:8]) % 10000
	return bucket < uint64(percent*100)
}

// EligibleForRelease checks hardware, source version, audience, and rollout
// bucket without mutating the release.
func EligibleForRelease(release *Release, device DeviceContext) bool {
	if release == nil ||
		release.Status != ReleaseStatusPublished ||
		device.DeviceID == "" ||
		release.HardwareRevision != "" &&
			release.HardwareRevision != device.HardwareRevision ||
		release.MinSourceVersion != "" &&
			CompareVersions(device.CurrentVersion, release.MinSourceVersion) < 0 ||
		CompareVersions(release.Version, device.CurrentVersion) <= 0 {
		return false
	}
	switch release.Target.Scope {
	case TargetScopeAll:
		return true
	case TargetScopeDevice:
		return release.Target.DeviceID == device.DeviceID
	case TargetScopeGroup:
		for _, groupID := range device.GroupIDs {
			if groupID == release.Target.GroupID {
				return true
			}
		}
		return false
	case TargetScopeCanary:
		return CanaryContains(release.Version, device.DeviceID, release.Target.CanaryPercent)
	default:
		return false
	}
}

// SelectRelease returns the highest eligible published release.
func SelectRelease(releases []Release, device DeviceContext, channel Channel) (*Release, error) {
	var selected *Release
	for index := range releases {
		release := &releases[index]
		if channel != "" && release.Channel != channel {
			continue
		}
		if !EligibleForRelease(release, device) {
			continue
		}
		if selected == nil ||
			CompareVersions(release.Version, selected.Version) > 0 ||
			CompareVersions(release.Version, selected.Version) == 0 &&
				release.PublishedAt != nil &&
				(selected.PublishedAt == nil || release.PublishedAt.After(*selected.PublishedAt)) {
			copy := *release
			selected = &copy
		}
	}
	if selected == nil {
		return nil, ErrNoUpdateAvailable
	}
	return selected, nil
}

// DeviceManifest converts a release to the device-facing response.
func (release Release) DeviceManifest() DeviceRelease {
	return DeviceRelease{
		ReleaseID:          release.ID,
		Version:            release.Version,
		Channel:            release.Channel,
		HardwareRevision:   release.HardwareRevision,
		MinSourceVersion:   release.MinSourceVersion,
		ArtifactURL:        release.ArtifactURL,
		ArtifactKey:        release.ArtifactKey,
		SHA256:             release.SHA256,
		SizeBytes:          release.SizeBytes,
		SignatureKeyID:     release.SignatureKeyID,
		SignatureAlgorithm: release.SignatureAlgorithm,
		Signature:          release.Signature,
		RollbackAllowed:    release.RollbackAllowed,
		Mandatory:          release.Mandatory,
		Status:             release.Status,
		ReleaseNotes:       release.ReleaseNotes,
		PublishedAt:        release.PublishedAt,
	}
}

// StatusForEvent maps a progress event to the resulting deployment status.
func StatusForEvent(eventType EventType) DeploymentStatus {
	switch eventType {
	case EventTypeStarted:
		return DeploymentStatusOffered
	case EventTypeDownloading:
		return DeploymentStatusDownloading
	case EventTypeDownloaded:
		return DeploymentStatusValidating
	case EventTypeValidated:
		return DeploymentStatusValidating
	case EventTypeInstalling:
		return DeploymentStatusInstalling
	case EventTypePendingVerify:
		return DeploymentStatusPendingVerify
	case EventTypeSucceeded:
		return DeploymentStatusSucceeded
	case EventTypeFailed:
		return DeploymentStatusFailed
	case EventTypeRollbackStarted:
		return DeploymentStatusRolledBack
	case EventTypeRolledBack:
		return DeploymentStatusRolledBack
	default:
		return ""
	}
}
