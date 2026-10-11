// Package service owns remote UI text use cases.
//
// The service is the only place that decides whether a copy may move through
// its lifecycle. It validates every payload before it touches the store, so a
// bad draft or an unverified signature never becomes visible to a client.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
)

// SignatureVerifier validates a detached package signature against a trusted
// keyring. Production must configure one before a package can be published.
type SignatureVerifier interface {
	Verify(algorithm string, keyID string, digest string, signature string) error
}

// auditRecorder is the narrow platform audit write surface.
type auditRecorder interface {
	RecordAudit(
		ctx context.Context,
		actorAccountID string,
		targetAccountID string,
		action string,
		detail map[string]any,
	) error
}

// Options contains remote UI text service dependencies.
type Options struct {
	Repository repository.Repository
	Verifier   SignatureVerifier
	Audit      auditRecorder
	Clock      clock.Clock
}

// Service owns draft review, publication, rollback, and package delivery.
type Service struct {
	repository repository.Repository
	verifier   SignatureVerifier
	audit      auditRecorder
	timeSource clock.Clock
}

// New creates the remote UI text service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("ui text repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository: options.Repository,
		verifier:   options.Verifier,
		audit:      options.Audit,
		timeSource: timeSource,
	}, nil
}

// CreateDraft validates and stores a new draft version.
func (s *Service) CreateDraft(
	ctx context.Context,
	input domain.DraftInput,
	actorID string,
) (*domain.PackageDetail, error) {
	normalized, err := domain.ValidateDraft(input)
	if err != nil {
		return nil, err
	}
	version, err := s.repository.NextVersion(
		ctx,
		normalized.PackageID,
		normalized.Language,
		normalized.Platform,
	)
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	detail, err := s.repository.CreateVersion(ctx, normalized, version, actorID, now)
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, actorID, "admin.ui_text.draft.created", map[string]any{
		"package_id": detail.Package.PackageID,
		"version":    detail.Package.PackageVersion,
		"language":   detail.Package.Language,
		"platform":   string(detail.Package.Platform),
	})
	return detail, nil
}

// UpdateDraft replaces the entries of one draft version.
func (s *Service) UpdateDraft(
	ctx context.Context,
	input domain.DraftInput,
	packageVersion int,
	actorID string,
) (*domain.PackageDetail, error) {
	if packageVersion < 1 {
		return nil, domain.ErrPackageNotFound
	}
	normalized, err := domain.ValidateDraft(input)
	if err != nil {
		return nil, err
	}
	detail, err := s.repository.UpdateDraft(
		ctx,
		normalized,
		packageVersion,
		s.timeSource.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, actorID, "admin.ui_text.draft.updated", map[string]any{
		"package_id": detail.Package.PackageID,
		"version":    detail.Package.PackageVersion,
	})
	return detail, nil
}

// GetVersion returns one stored version with its ordered entries.
func (s *Service) GetVersion(
	ctx context.Context,
	packageID string,
	language string,
	platform domain.Platform,
	packageVersion int,
) (*domain.PackageDetail, error) {
	packageID = strings.TrimSpace(packageID)
	language = strings.TrimSpace(language)
	if packageID == "" || language == "" || packageVersion < 1 {
		return nil, domain.ErrPackageNotFound
	}
	if !domain.IsValidPlatform(platform) {
		return nil, domain.ErrPackageNotFound
	}
	return s.repository.GetVersion(ctx, packageID, language, platform, packageVersion)
}

// ListVersions returns one operator page of version summaries.
func (s *Service) ListVersions(
	ctx context.Context,
	filter domain.VersionFilter,
) ([]domain.PackageVersion, int, error) {
	if filter.Status != "" && !domain.IsValidStatus(filter.Status) {
		return nil, 0, domain.ErrInvalidPackage
	}
	if filter.Platform != "" && !domain.IsValidPlatform(filter.Platform) {
		return nil, 0, domain.ErrInvalidPackage
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = domain.DefaultPageSize
	}
	if filter.PageSize > domain.MaxPageSize {
		filter.PageSize = domain.MaxPageSize
	}
	filter.PackageID = strings.TrimSpace(filter.PackageID)
	filter.Language = strings.TrimSpace(filter.Language)
	items, total, err := s.repository.ListVersions(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	for index := range items {
		items[index].IsNewest = index == 0
	}
	return items, total, nil
}

// Submit moves a draft into review.
func (s *Service) Submit(
	ctx context.Context,
	input domain.TransitionInput,
) (*domain.Package, error) {
	input.Action = domain.ActionSubmit
	return s.transition(ctx, input)
}

// Approve records a reviewer decision without changing the durable status.
func (s *Service) Approve(
	ctx context.Context,
	input domain.TransitionInput,
) (*domain.Package, error) {
	input.Action = domain.ActionApprove
	return s.transition(ctx, input)
}

// Reject returns a package in review to draft.
func (s *Service) Reject(
	ctx context.Context,
	input domain.TransitionInput,
	reason string,
) (*domain.Package, error) {
	if strings.TrimSpace(reason) == "" ||
		len([]rune(strings.TrimSpace(reason))) > domain.MaxReasonRunes {
		return nil, domain.ErrReasonRequired
	}
	input.Action = domain.ActionReject
	packageVersion, err := s.transition(ctx, input)
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, input.ActorID, "admin.ui_text.review.rejected", map[string]any{
		"package_id": packageVersion.PackageID,
		"version":    packageVersion.PackageVersion,
		"reason":     strings.TrimSpace(reason),
	})
	return packageVersion, nil
}

// Withdraw retires a published package version.
func (s *Service) Withdraw(
	ctx context.Context,
	input domain.TransitionInput,
) (*domain.Package, error) {
	input.Action = domain.ActionWithdraw
	return s.transition(ctx, input)
}

// Publish verifies the detached signature and publishes an approved version.
//
// Publishing withdraws the superseded publication in the same transaction, so
// a client never observes two published rows for one logical package.
func (s *Service) Publish(
	ctx context.Context,
	input domain.TransitionInput,
	publication domain.PublicationInput,
) (*domain.Package, error) {
	packageID := strings.TrimSpace(input.PackageID)
	language := strings.TrimSpace(input.Language)
	if packageID == "" ||
		language == "" ||
		input.PackageVersion < 1 ||
		!domain.IsValidPlatform(input.Platform) {
		return nil, domain.ErrPackageNotFound
	}
	keyID := strings.TrimSpace(publication.SignatureKeyID)
	signature := strings.TrimSpace(publication.Signature)
	if !domain.ValidKeyID(keyID) || !domain.ValidSignature(signature) {
		return nil, domain.ErrSignatureRequired
	}
	detail, err := s.repository.GetVersion(
		ctx,
		packageID,
		language,
		input.Platform,
		input.PackageVersion,
	)
	if err != nil {
		return nil, err
	}
	if detail.Package.Status != domain.StatusInReview {
		return nil, domain.ErrInvalidTransition
	}
	if s.verifier == nil {
		return nil, domain.ErrSignatureUnknownKey
	}
	if err := s.verifier.Verify(
		domain.SignatureAlgorithmEd25519,
		keyID,
		PackageDigest(detail),
		signature,
	); err != nil {
		return nil, err
	}
	input.Action = domain.ActionPublish
	input.SignatureKeyID = keyID
	input.Signature = signature
	published, err := s.transition(ctx, input)
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, input.ActorID, "admin.ui_text.published", map[string]any{
		"package_id": published.PackageID,
		"version":    published.PackageVersion,
		"language":   published.Language,
		"platform":   string(published.Platform),
	})
	return published, nil
}

// Revision returns the published-catalog cursor without listing packages.
func (s *Service) Revision(ctx context.Context) (int64, error) {
	return s.repository.CurrentRevision(ctx)
}

// Published returns published packages newer than one revision.
func (s *Service) Published(
	ctx context.Context,
	sinceRevision int64,
	limit int,
) (domain.RevisionPage, error) {
	if sinceRevision < 0 {
		return domain.RevisionPage{}, domain.ErrInvalidPackage
	}
	return s.repository.ListPublished(ctx, sinceRevision, limit)
}

// PublishedPackage returns the currently published version of one package.
func (s *Service) PublishedPackage(
	ctx context.Context,
	packageID string,
	language string,
	platform domain.Platform,
) (*domain.PublishedPackage, error) {
	packageID = strings.TrimSpace(packageID)
	language = strings.TrimSpace(language)
	if packageID == "" || language == "" || !domain.IsValidPlatform(platform) {
		return nil, domain.ErrPackageNotFound
	}
	return s.repository.PublishedPackage(ctx, packageID, language, platform)
}

func (s *Service) transition(
	ctx context.Context,
	input domain.TransitionInput,
) (*domain.Package, error) {
	input.PackageID = strings.TrimSpace(input.PackageID)
	input.Language = strings.TrimSpace(input.Language)
	if input.PackageID == "" ||
		input.Language == "" ||
		input.PackageVersion < 1 ||
		!domain.IsValidPlatform(input.Platform) {
		return nil, domain.ErrPackageNotFound
	}
	current, err := s.repository.GetVersion(
		ctx,
		input.PackageID,
		input.Language,
		input.Platform,
		input.PackageVersion,
	)
	if err != nil {
		return nil, err
	}
	if _, err := domain.ValidateTransition(current.Package.Status, input.Action); err != nil {
		return nil, err
	}
	updated, err := s.repository.ApplyTransition(ctx, input, s.timeSource.Now().UTC())
	if err != nil {
		return nil, err
	}
	if input.Action != domain.ActionPublish && input.Action != domain.ActionReject {
		s.recordAudit(ctx, input.ActorID, "admin.ui_text."+string(input.Action), map[string]any{
			"package_id": updated.PackageID,
			"version":    updated.PackageVersion,
		})
	}
	return updated, nil
}

// PackageDigest is the canonical signed payload for one package version: the
// SHA-256 hex digest of the deterministic entry manifest. Both the publishing
// tooling and the verifier must derive the same bytes, so the manifest is
// ordered by key and carries only immutable fields.
func PackageDigest(detail *domain.PackageDetail) string {
	if detail == nil {
		return ""
	}
	entries := append([]domain.Entry(nil), detail.Entries...)
	sort.Slice(entries, func(left int, right int) bool {
		return entries[left].UITextKey < entries[right].UITextKey
	})
	hasher := sha256.New()
	writeManifestField(hasher, detail.Package.PackageID)
	writeManifestField(hasher, detail.Package.Language)
	writeManifestField(hasher, string(detail.Package.Platform))
	writeManifestField(hasher, strconv.Itoa(detail.Package.PackageVersion))
	writeManifestField(hasher, detail.Package.MinClientVersion)
	for _, entry := range entries {
		writeManifestField(hasher, entry.UITextKey)
		writeManifestField(hasher, entry.Value)
		writeManifestField(hasher, strconv.Itoa(entry.MaxLength))
		for _, variable := range entry.Variables {
			writeManifestField(hasher, variable)
		}
		writeManifestField(hasher, "\n")
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func writeManifestField(hasher interface{ Write([]byte) (int, error) }, value string) {
	_, _ = hasher.Write([]byte(value))
	_, _ = hasher.Write([]byte{0})
}

func (s *Service) recordAudit(
	ctx context.Context,
	actorID string,
	action string,
	detail map[string]any,
) {
	if s.audit == nil {
		return
	}
	_ = s.audit.RecordAudit(ctx, actorID, "", action, detail)
}
