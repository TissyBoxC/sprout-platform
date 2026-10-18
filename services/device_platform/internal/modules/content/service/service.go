// Package service owns the content-package lifecycle and catalog delivery.
package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/repository"
	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
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
	Repository   repository.Repository
	AssetReader  AssetReader
	PolicyReader ContentPolicyReader
	Clock        clock.Clock
}

// ContentPolicyReader resolves the effective family policy that constrains a
// device-facing catalog. Admin browsing does not use this reader.
type ContentPolicyReader interface {
	GetEffective(
		ctx context.Context,
		familyID string,
	) (*policydomain.EffectivePolicy, error)
}

// Service owns the content-package lifecycle.
type Service struct {
	repository   repository.Repository
	assetReader  AssetReader
	policyReader ContentPolicyReader
	clock        clock.Clock
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
		repository:   options.Repository,
		assetReader:  options.AssetReader,
		policyReader: options.PolicyReader,
		clock:        timeSource,
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
	detail, err := s.repository.GetDetail(ctx, strings.TrimSpace(packageID))
	if err != nil {
		return nil, err
	}
	// The repository applies SQL LIMIT; these copies keep in-memory callers
	// within the same response contract.
	if len(detail.Versions) > domain.PackageDetailVersionMax {
		detail.Versions = detail.Versions[:domain.PackageDetailVersionMax]
	}
	if len(detail.History) > domain.PackageReviewHistoryMax {
		detail.History = detail.History[:domain.PackageReviewHistoryMax]
	}
	return detail, nil
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
	if query.FamilyID != "" {
		if s.policyReader == nil {
			return nil, domain.ErrInvalidMetadata
		}
		effective, err := s.policyReader.GetEffective(ctx, query.FamilyID)
		if err != nil {
			return nil, err
		}
		query.AllowedCategories = intersectCategories(
			query.AllowedCategories,
			effective.AllowedCategories,
		)
		if len(query.AllowedCategories) == 0 {
			// No category survives the policy intersection. Return an empty
			// manifest, but keep the real delivery cursor so the device can
			// still advance and receive packages once the policy widens.
			revision, err := s.repository.CatalogRevision(ctx)
			if err != nil {
				return nil, err
			}
			return &domain.Catalog{
				Revision:            revision,
				Packages:            []domain.PackageVersion{},
				WithdrawnPackageIDs: []string{},
				GeneratedAt:         s.clock.Now().UTC(),
			}, nil
		}
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
	return s.downloadVersion(ctx, version)
}

// DownloadForFamily resolves a published package only when its category is
// allowed by the family's effective parent policy. The device path uses this
// method so a client cannot bypass catalog filtering by requesting a URL.
func (s *Service) DownloadForFamily(
	ctx context.Context,
	familyID string,
	packageID string,
) (*domain.DownloadInfo, error) {
	if strings.TrimSpace(familyID) == "" {
		return nil, domain.ErrInvalidMetadata
	}
	if s.policyReader == nil {
		return nil, domain.ErrInvalidMetadata
	}
	effective, err := s.policyReader.GetEffective(ctx, familyID)
	if err != nil {
		return nil, err
	}
	version, err := s.repository.PublishedDownload(
		ctx,
		strings.TrimSpace(packageID),
	)
	if err != nil {
		return nil, err
	}
	if !categoryAllowed(version.Category, effective.AllowedCategories) {
		return nil, domain.ErrPackageNotFound
	}
	return s.downloadVersion(ctx, version)
}

func (s *Service) downloadVersion(
	ctx context.Context,
	version *domain.PackageVersion,
) (*domain.DownloadInfo, error) {
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
	if asset.SizeBytes != version.SizeBytes {
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

func categoryAllowed(category domain.Category, allowed []string) bool {
	for _, allowedCategory := range allowed {
		if string(category) == allowedCategory {
			return true
		}
	}
	return false
}

// AddFavorite bookmarks one published package for a guardian.
//
// The package must exist in some version so a guardian cannot favourite an
// arbitrary id. A repeat add is reported as ErrPackageNotFound-free conflict
// via ErrFavoriteExists so the handler can answer 200 idempotently.
func (s *Service) AddFavorite(
	ctx context.Context,
	parentAccountID string,
	packageID string,
) (domain.Favorite, error) {
	parentAccountID = strings.TrimSpace(parentAccountID)
	packageID = strings.TrimSpace(packageID)
	if parentAccountID == "" {
		return domain.Favorite{}, domain.ErrInvalidMetadata
	}
	if _, err := s.repository.GetDetail(ctx, packageID); err != nil {
		return domain.Favorite{}, err
	}
	return s.repository.AddFavorite(ctx, parentAccountID, packageID, s.clock.Now())
}

// RemoveFavorite deletes one bookmark. A missing row is not an error so the
// console can call it without first checking existence.
func (s *Service) RemoveFavorite(
	ctx context.Context,
	parentAccountID string,
	packageID string,
) (bool, error) {
	parentAccountID = strings.TrimSpace(parentAccountID)
	packageID = strings.TrimSpace(packageID)
	if parentAccountID == "" || packageID == "" {
		return false, domain.ErrInvalidMetadata
	}
	return s.repository.RemoveFavorite(ctx, parentAccountID, packageID)
}

// ListFavorites returns the guardian's bookmarks with availability resolved
// against the currently published version.
func (s *Service) ListFavorites(
	ctx context.Context,
	parentAccountID string,
) ([]domain.FavoriteView, error) {
	parentAccountID = strings.TrimSpace(parentAccountID)
	if parentAccountID == "" {
		return nil, domain.ErrInvalidMetadata
	}
	views, err := s.repository.ListFavorites(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	if len(views) > domain.FavoriteListMax {
		views = views[:domain.FavoriteListMax]
	}
	for index := range views {
		if views[index].AgeTiers == nil {
			views[index].AgeTiers = []domain.AgeTier{}
		}
	}
	return views, nil
}

// RecordPlayback validates and stores one bounded device playback event.
// The retention limit keeps the per-device history table bounded.
func (s *Service) RecordPlayback(
	ctx context.Context,
	input domain.PlayRecordInput,
) (domain.PlayRecord, error) {
	input.FamilyID = strings.TrimSpace(input.FamilyID)
	if input.FamilyID == "" {
		return domain.PlayRecord{}, domain.ErrInvalidMetadata
	}
	normalized, err := domain.ValidatePlayRecord(input)
	if err != nil {
		return domain.PlayRecord{}, err
	}
	return s.repository.RecordPlayback(
		ctx,
		normalized,
		playHistoryRetentionLimit,
		s.clock.Now(),
	)
}

// ListPlayHistory returns the bounded, newest-first history for one device.
func (s *Service) ListPlayHistory(
	ctx context.Context,
	familyID string,
	deviceID string,
	limit int,
) ([]domain.PlayRecord, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidMetadata
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, domain.ErrInvalidMetadata
	}
	records, err := s.repository.ListPlayHistory(ctx, domain.PlayHistoryQuery{
		FamilyID: familyID,
		DeviceID: deviceID,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}
	if records == nil {
		records = []domain.PlayRecord{}
	}
	return records, nil
}

// playHistoryRetentionLimit is the number of history rows kept per device.
const playHistoryRetentionLimit = 50

// ListThemePacks returns theme packs for the admin console and, filtered to
// published, for the guardian and device surfaces.
func (s *Service) ListThemePackPage(
	ctx context.Context,
	query domain.ThemePackQuery,
) (domain.ThemePackPage, error) {
	query.Status = strings.TrimSpace(query.Status)
	query.Keyword = strings.TrimSpace(query.Keyword)
	switch domain.ThemePackStatus(query.Status) {
	case "", domain.ThemePackDraft, domain.ThemePackPublished, domain.ThemePackWithdrawn:
	default:
		return domain.ThemePackPage{}, domain.ErrInvalidMetadata
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = domain.ThemePackListDefaultPageSize
	}
	if query.PageSize > domain.ThemePackListMaxPageSize {
		query.PageSize = domain.ThemePackListMaxPageSize
	}
	result, err := s.listThemePackPage(ctx, query)
	if err != nil {
		return domain.ThemePackPage{}, err
	}
	if result.ThemePacks == nil {
		result.ThemePacks = []domain.ThemePack{}
	}
	result.Page = query.Page
	result.PageSize = query.PageSize
	for index := range result.ThemePacks {
		normalizeThemePack(&result.ThemePacks[index])
	}
	return result, nil
}

// themePackPageReader is the optional paged read implemented by the
// PostgreSQL repository. The core Repository contract stays on the original
// collection shape so existing adapters and test doubles remain compatible.
type themePackPageReader interface {
	ListThemePackPage(
		ctx context.Context,
		query domain.ThemePackQuery,
	) (domain.ThemePackPage, error)
}

func (s *Service) listThemePackPage(
	ctx context.Context,
	query domain.ThemePackQuery,
) (domain.ThemePackPage, error) {
	if reader, ok := s.repository.(themePackPageReader); ok {
		return reader.ListThemePackPage(ctx, query)
	}
	packs, err := s.repository.ListThemePacks(ctx, query)
	if err != nil {
		return domain.ThemePackPage{}, err
	}
	total := len(packs)
	start := (query.Page - 1) * query.PageSize
	if start > total {
		start = total
	}
	end := start + query.PageSize
	if end > total {
		end = total
	}
	return domain.ThemePackPage{
		ThemePacks: packs[start:end],
		Page:       query.Page,
		PageSize:   query.PageSize,
		Total:      total,
	}, nil
}

// ListThemePacks preserves the original collection return type for callers
// that already encode it as the theme_packs array. The bounded page is always
// the first page, so this remains API-compatible while still preventing an
// unbounded response.
func (s *Service) ListThemePacks(
	ctx context.Context,
	query domain.ThemePackQuery,
) ([]domain.ThemePack, error) {
	query.Page = 1
	query.PageSize = domain.ThemePackListMaxPageSize
	page, err := s.ListThemePackPage(ctx, query)
	if err != nil {
		return nil, err
	}
	return page.ThemePacks, nil
}

// GetThemePack returns one theme pack with normalized slices.
func (s *Service) GetThemePack(
	ctx context.Context,
	themeID string,
) (*domain.ThemePack, error) {
	themeID = strings.TrimSpace(themeID)
	if themeID == "" {
		return nil, domain.ErrInvalidMetadata
	}
	pack, err := s.repository.GetThemePack(ctx, themeID)
	if err != nil {
		return nil, err
	}
	normalizeThemePack(pack)
	return pack, nil
}

// UpsertThemePack validates and saves a draft theme pack.
//
// Every member package must exist in some version so an operator cannot curate
// a theme around an unknown package id. The check is presence-only: a theme
// may reference a package whose current version is withdrawn, because the pack
// is editorial intent rather than a published manifest.
func (s *Service) UpsertThemePack(
	ctx context.Context,
	input domain.ThemePackInput,
) (*domain.ThemePack, error) {
	normalized, err := domain.ValidateThemePack(input)
	if err != nil {
		return nil, err
	}
	allExist, err := s.packageIDsExist(ctx, normalized.PackageIDs)
	if err != nil {
		return nil, err
	}
	if !allExist {
		return nil, domain.ErrPackageNotFound
	}
	pack, err := s.repository.UpsertThemePack(
		ctx,
		normalized,
		s.clock.Now(),
	)
	if err != nil {
		return nil, err
	}
	normalizeThemePack(pack)
	return pack, nil
}

// PublishThemePack moves a draft pack to published.
//
// Draft editing is intentionally permissive, but publication is the last
// point where the platform can guarantee every member is downloadable. A
// member may have been withdrawn since the draft was saved, so re-read the
// current pack immediately before the lifecycle transition.
func (s *Service) PublishThemePack(
	ctx context.Context,
	themeID string,
) (*domain.ThemePack, error) {
	themeID = strings.TrimSpace(themeID)
	if themeID == "" {
		return nil, domain.ErrInvalidMetadata
	}
	pack, err := s.repository.GetThemePack(ctx, themeID)
	if err != nil {
		return nil, err
	}
	allPublished, err := s.packageIDsPublished(ctx, pack.PackageIDs)
	if err != nil {
		return nil, err
	}
	if !allPublished {
		return nil, domain.ErrThemeMembersInvalid
	}
	return s.transitionThemePack(ctx, themeID, domain.ActionPublish)
}

// WithdrawThemePack moves a published pack back out of delivery.
func (s *Service) WithdrawThemePack(
	ctx context.Context,
	themeID string,
) (*domain.ThemePack, error) {
	return s.transitionThemePack(ctx, themeID, domain.ActionWithdraw)
}

// DeleteThemePack removes a non-published pack.
func (s *Service) DeleteThemePack(
	ctx context.Context,
	themeID string,
) error {
	themeID = strings.TrimSpace(themeID)
	if themeID == "" {
		return domain.ErrInvalidMetadata
	}
	return s.repository.DeleteThemePack(ctx, themeID)
}

func (s *Service) transitionThemePack(
	ctx context.Context,
	themeID string,
	action domain.Action,
) (*domain.ThemePack, error) {
	themeID = strings.TrimSpace(themeID)
	if themeID == "" {
		return nil, domain.ErrInvalidMetadata
	}
	pack, err := s.repository.TransitionThemePack(
		ctx,
		themeID,
		action,
		s.clock.Now(),
	)
	if err != nil {
		return nil, err
	}
	normalizeThemePack(pack)
	return pack, nil
}

func normalizeThemePack(pack *domain.ThemePack) {
	if pack.AgeTiers == nil {
		pack.AgeTiers = []domain.AgeTier{}
	}
	if pack.PackageIDs == nil {
		pack.PackageIDs = []string{}
	}
}

// ListPlayHistoryForParent merges the playback history of the guardian's
// devices into a single newest-first list.
//
// The parent app holds only a guardian credential, so it cannot authenticate
// against the device-scoped history route. The caller resolves the guardian's
// device ids (already scoped to the account) and this method merges the bounded
// per-device history, capping the merged result at the requested limit.
func (s *Service) ListPlayHistoryForParent(
	ctx context.Context,
	familyID string,
	deviceIDs []string,
	limit int,
) ([]domain.PlayRecord, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidMetadata
	}
	if limit < 1 || limit > domain.PlayHistoryQueryMax {
		limit = 50
	}
	deviceIDs = normalizeDeviceIDs(deviceIDs)
	if len(deviceIDs) == 0 {
		return []domain.PlayRecord{}, nil
	}
	records, err := s.listPlayHistoryForDevices(ctx, familyID, deviceIDs, limit)
	if err != nil {
		return nil, err
	}
	if records == nil {
		records = []domain.PlayRecord{}
	}
	return records, nil
}

// batchPackageReader is implemented by the PostgreSQL repository. Keeping it
// optional preserves the existing Repository contract for lightweight test
// doubles and external adapters while production uses one query.
type batchPackageReader interface {
	ListExistingPackageIDs(
		ctx context.Context,
		packageIDs []string,
	) ([]string, error)
	ListPublishedPackageIDs(
		ctx context.Context,
		packageIDs []string,
	) ([]string, error)
}

// recentCategoryReader is an optional batch lookup. Callers that only expose
// the original Repository surface continue to compile and can use the
// per-package PublishedDownload fallback when needed.
type recentCategoryReader interface {
	ListRecentPublishedCategories(
		ctx context.Context,
		packageIDs []string,
	) (map[string]string, error)
}

func (s *Service) packageIDsExist(
	ctx context.Context,
	packageIDs []string,
) (bool, error) {
	if reader, ok := s.repository.(batchPackageReader); ok {
		existing, err := reader.ListExistingPackageIDs(ctx, packageIDs)
		if err != nil {
			return false, err
		}
		return len(existing) == len(packageIDs), nil
	}
	for _, packageID := range packageIDs {
		if _, err := s.repository.GetDetail(ctx, packageID); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) packageIDsPublished(
	ctx context.Context,
	packageIDs []string,
) (bool, error) {
	if reader, ok := s.repository.(batchPackageReader); ok {
		published, err := reader.ListPublishedPackageIDs(ctx, packageIDs)
		if err != nil {
			return false, err
		}
		return len(published) == len(packageIDs), nil
	}
	for _, packageID := range packageIDs {
		if _, err := s.repository.PublishedDownload(ctx, packageID); err != nil {
			if errors.Is(err, domain.ErrPackageNotFound) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}

// batchPlayHistoryReader is the optional cross-device read. The production
// repository implements it in one SQL statement.
type batchPlayHistoryReader interface {
	ListPlayHistoryForDevices(
		ctx context.Context,
		query domain.PlayHistoryQuery,
		deviceIDs []string,
	) ([]domain.PlayRecord, error)
}

func (s *Service) listPlayHistoryForDevices(
	ctx context.Context,
	familyID string,
	deviceIDs []string,
	limit int,
) ([]domain.PlayRecord, error) {
	if reader, ok := s.repository.(batchPlayHistoryReader); ok {
		return reader.ListPlayHistoryForDevices(
			ctx,
			domain.PlayHistoryQuery{
				FamilyID: familyID,
				Limit:    limit,
			},
			deviceIDs,
		)
	}
	// Compatibility fallback for implementations that predate the batch
	// reader. The production path above issues one query.
	merged := make([]domain.PlayRecord, 0, limit)
	for _, deviceID := range deviceIDs {
		records, err := s.repository.ListPlayHistory(
			ctx,
			domain.PlayHistoryQuery{
				FamilyID: familyID,
				DeviceID: deviceID,
				Limit:    limit,
			},
		)
		if err != nil {
			return nil, err
		}
		merged = append(merged, records...)
	}
	sort.SliceStable(merged, func(left int, right int) bool {
		if merged[left].PlayedAt.Equal(merged[right].PlayedAt) {
			return merged[left].ID > merged[right].ID
		}
		return merged[left].PlayedAt.After(merged[right].PlayedAt)
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func normalizeDeviceIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func intersectCategories(requested []string, allowed []string) []string {
	if len(allowed) == 0 {
		return []string{}
	}
	if len(requested) == 0 {
		return append([]string(nil), allowed...)
	}
	result := make([]string, 0, len(requested))
	for _, category := range requested {
		for _, allowedCategory := range allowed {
			if category == allowedCategory {
				result = append(result, category)
				break
			}
		}
	}
	return result
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
