// Package service owns the content-package lifecycle and catalog delivery.
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
)

// AssetReader resolves a verified blob from the shared download store.
//
// The content service never reads asset bytes itself; it delegates to the
// release-store owner so the storage path remains single-sourced.
type AssetReader interface {
	FindContentAsset(ctx context.Context, assetKey string) (*domain.Asset, error)
}

// Options contains content-service dependencies.
type Options struct {
	Repository  repository.Repository
	AssetReader AssetReader
	Clock       clock.Clock
}

// Service owns the content-package lifecycle.
type Service struct {
	repository  repository.Repository
	assetReader AssetReader
	clock       clock.Clock
}

// New creates the content service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("content repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository:  options.Repository,
		assetReader: options.AssetReader,
		clock:       timeSource,
	}, nil
}

// CreateDraft creates version 1 of a new package.
func (s *Service) CreateDraft(
	ctx context.Context,
	input domain.MetadataInput,
) (*domain.PackageVersion, error) {
	normalized, err := domain.ValidateMetadata(input)
	if err != nil {
		return nil, err
	}
	detail, err := s.repository.GetDetail(ctx, normalized.PackageID)
	if errors.Is(err, domain.ErrPackageNotFound) {
		return s.repository.CreateDraft(ctx, normalized, s.clock.Now().UTC())
	}
	if err != nil {
		return nil, err
	}
	if len(detail.Versions) > 0 {
		return nil, domain.ErrVersionExists
	}
	return s.repository.CreateDraft(ctx, normalized, s.clock.Now().UTC())
}

// CreateVersion allocates the next draft version for an existing package.
//
// Version numbers are allocated from the current maximum while holding the
// package's row-level write transaction; the repository enforces the primary
// key so concurrent operators cannot silently reuse a version number.
func (s *Service) CreateVersion(
	ctx context.Context,
	packageID string,
	input domain.MetadataInput,
) (*domain.PackageVersion, error) {
	normalized, err := domain.ValidateMetadata(input)
	if err != nil {
		return nil, err
	}
	packageID = strings.TrimSpace(packageID)
	if normalized.PackageID != packageID {
		return nil, domain.ErrInvalidMetadata
	}
	detail, err := s.repository.GetDetail(ctx, packageID)
	if err != nil {
		return nil, err
	}
	nextVersion := 1
	for _, version := range detail.Versions {
		if version.PackageVersion >= nextVersion {
			nextVersion = version.PackageVersion + 1
		}
	}
	return s.repository.CreateVersion(
		ctx,
		normalized,
		nextVersion,
		s.clock.Now().UTC(),
	)
}

// UpdateDraft replaces metadata only while the version is still a draft.
func (s *Service) UpdateDraft(
	ctx context.Context,
	packageID string,
	packageVersion int,
	input domain.MetadataInput,
) (*domain.PackageVersion, error) {
	normalized, err := domain.ValidateMetadata(input)
	if err != nil {
		return nil, err
	}
	if normalized.PackageID != packageID {
		return nil, domain.ErrInvalidMetadata
	}
	current, err := s.repository.GetVersion(ctx, packageID, packageVersion)
	if err != nil {
		return nil, err
	}
	if current.Status != domain.StatusDraft {
		return nil, domain.ErrInvalidTransition
	}
	return s.repository.UpdateDraft(
		ctx,
		packageID,
		packageVersion,
		normalized,
		s.clock.Now().UTC(),
	)
}

// List returns a filtered admin page.
func (s *Service) List(
	ctx context.Context,
	filter domain.PackageListFilter,
) (domain.PackagePage, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	if filter.Category != "" && !domain.IsCategory(domain.Category(filter.Category)) {
		return domain.PackagePage{}, domain.ErrInvalidMetadata
	}
	if filter.AgeTier != "" && !domain.IsAgeTier(filter.AgeTier) {
		return domain.PackagePage{}, domain.ErrInvalidMetadata
	}
	if filter.Status != "" {
		switch domain.Status(filter.Status) {
		case domain.StatusDraft,
			domain.StatusInReview,
			domain.StatusPublished,
			domain.StatusWithdrawn,
			domain.StatusArchived:
		default:
			return domain.PackagePage{}, domain.ErrInvalidMetadata
		}
	}
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	return s.repository.List(ctx, filter)
}

// GetDetail returns every version plus the complete review history.
func (s *Service) GetDetail(
	ctx context.Context,
	packageID string,
) (*domain.PackageDetail, error) {
	return s.repository.GetDetail(ctx, strings.TrimSpace(packageID))
}

// Submit moves one draft into review.
func (s *Service) Submit(
	ctx context.Context,
	packageID string,
	packageVersion int,
	actorAccountID string,
) (*domain.PackageVersion, error) {
	return s.transition(
		ctx,
		packageID,
		packageVersion,
		domain.ActionSubmit,
		actorAccountID,
		"",
	)
}

// Approve records reviewer approval while keeping the package in review.
//
// Approval is a decision, not a status; publishing remains a separate explicit
// operation so the operator who approves can differ from the one who ships.
func (s *Service) Approve(
	ctx context.Context,
	packageID string,
	packageVersion int,
	actorAccountID string,
	reason string,
) (*domain.PackageVersion, error) {
	return s.transition(
		ctx,
		packageID,
		packageVersion,
		domain.ActionApprove,
		actorAccountID,
		reason,
	)
}

// Reject returns a package to draft and requires an operator reason.
func (s *Service) Reject(
	ctx context.Context,
	packageID string,
	packageVersion int,
	actorAccountID string,
	reason string,
) (*domain.PackageVersion, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, domain.ErrReasonRequired
	}
	return s.transition(
		ctx,
		packageID,
		packageVersion,
		domain.ActionReject,
		actorAccountID,
		strings.TrimSpace(reason),
	)
}

// Publish verifies the storage asset and promotes one approved version.
//
// Any older published version of the same package is archived in the same
// repository transaction, so the catalog only ever exposes one published
// version per package ID.
func (s *Service) Publish(
	ctx context.Context,
	packageID string,
	packageVersion int,
	actorAccountID string,
) (*domain.PackageVersion, error) {
	current, err := s.repository.GetVersion(ctx, packageID, packageVersion)
	if err != nil {
		return nil, err
	}
	if current.Status != domain.StatusInReview {
		return nil, domain.ErrInvalidTransition
	}
	if current.ApprovedAt == nil || strings.TrimSpace(current.ApprovedBy) == "" {
		return nil, domain.ErrReviewRequired
	}
	if s.assetReader == nil {
		return nil, domain.ErrAssetNotFound
	}
	asset, err := s.assetReader.FindContentAsset(ctx, current.AssetKey)
	if err != nil {
		return nil, err
	}
	if asset == nil || asset.SHA256 != current.SHA256 {
		return nil, domain.ErrAssetChecksumMismatch
	}
	return s.transition(
		ctx,
		packageID,
		packageVersion,
		domain.ActionPublish,
		actorAccountID,
		"",
	)
}

// Withdraw removes a published version from the active catalog.
func (s *Service) Withdraw(
	ctx context.Context,
	packageID string,
	packageVersion int,
	actorAccountID string,
	reason string,
) (*domain.PackageVersion, error) {
	return s.transition(
		ctx,
		packageID,
		packageVersion,
		domain.ActionWithdraw,
		actorAccountID,
		strings.TrimSpace(reason),
	)
}

// Archive retires a withdrawn version permanently.
func (s *Service) Archive(
	ctx context.Context,
	packageID string,
	packageVersion int,
	actorAccountID string,
) (*domain.PackageVersion, error) {
	return s.transition(
		ctx,
		packageID,
		packageVersion,
		domain.ActionArchive,
		actorAccountID,
		"",
	)
}

// Catalog returns packages changed after the caller's revision plus a
// stable, monotonic revision for the next incremental request.
func (s *Service) Catalog(
	ctx context.Context,
	query domain.CatalogQuery,
) (*domain.Catalog, error) {
	if query.SinceRevision < 0 {
		return nil, domain.ErrInvalidMetadata
	}
	if query.AgeTier != "" && !domain.IsAgeTier(query.AgeTier) {
		return nil, domain.ErrInvalidMetadata
	}
	if query.Category != "" && !domain.IsCategory(domain.Category(query.Category)) {
		return nil, domain.ErrInvalidMetadata
	}
	catalog, err := s.repository.Catalog(ctx, query)
	if err != nil {
		return nil, err
	}
	catalog.GeneratedAt = s.clock.Now().UTC()
	if catalog.Packages == nil {
		catalog.Packages = []domain.PackageVersion{}
	}
	if catalog.WithdrawnPackageIDs == nil {
		catalog.WithdrawnPackageIDs = []string{}
	}
	return catalog, nil
}

// Download resolves one published package and verifies its asset before
// returning the public download URL.
func (s *Service) Download(
	ctx context.Context,
	packageID string,
) (*domain.DownloadInfo, error) {
	version, err := s.repository.PublishedDownload(
		ctx,
		strings.TrimSpace(packageID),
	)
	if err != nil {
		return nil, err
	}
	if version.PublishedAt == nil {
		return nil, domain.ErrPackageNotFound
	}
	if s.assetReader == nil {
		return nil, domain.ErrAssetNotFound
	}
	asset, err := s.assetReader.FindContentAsset(ctx, version.AssetKey)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, domain.ErrAssetNotFound
	}
	if asset.SHA256 != version.SHA256 {
		return nil, domain.ErrAssetChecksumMismatch
	}
	return &domain.DownloadInfo{
		PackageID:      version.PackageID,
		PackageVersion: version.PackageVersion,
		Title:          version.Title,
		AssetKey:       version.AssetKey,
		SHA256:         version.SHA256,
		SizeBytes:      version.SizeBytes,
		DownloadURL:    asset.DownloadURL,
		PublishedAt:    version.PublishedAt.UTC(),
	}, nil
}

func (s *Service) transition(
	ctx context.Context,
	packageID string,
	packageVersion int,
	action domain.Action,
	actorAccountID string,
	reason string,
) (*domain.PackageVersion, error) {
	current, err := s.repository.GetVersion(ctx, packageID, packageVersion)
	if err != nil {
		return nil, err
	}
	toStatus, err := domain.NextStatus(current.Status, action)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(actorAccountID) == "" {
		return nil, domain.ErrInvalidMetadata
	}
	now := s.clock.Now().UTC()
	return s.repository.ApplyTransition(ctx, domain.Transition{
		PackageID:      packageID,
		PackageVersion: packageVersion,
		FromStatus:     current.Status,
		ToStatus:       toStatus,
		Action:         action,
		Reason:         reason,
		ActorAccountID: strings.TrimSpace(actorAccountID),
		Now:            now,
	})
}
