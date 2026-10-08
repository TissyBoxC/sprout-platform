package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
)

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

type memoryRepository struct {
	mu           sync.Mutex
	versions     map[string]*domain.PackageVersion
	logs         []domain.ReviewLog
	revision     int64
	revisionLock chan struct{}
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		versions:     make(map[string]*domain.PackageVersion),
		revisionLock: make(chan struct{}, 1),
	}
}

func (repository *memoryRepository) key(packageID string, packageVersion int) string {
	return fmt.Sprintf("%s/%d", packageID, packageVersion)
}

func (repository *memoryRepository) List(
	_ context.Context,
	filter domain.PackageListFilter,
) (domain.PackagePage, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	all := make([]domain.PackageVersion, 0, len(repository.versions))
	for _, version := range repository.versions {
		if filter.Category != "" && string(version.Category) != filter.Category {
			continue
		}
		if filter.Status != "" && string(version.Status) != filter.Status {
			continue
		}
		if filter.AgeTier != "" {
			matched := false
			for _, ageTier := range version.AgeTiers {
				if string(ageTier) == filter.AgeTier {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		all = append(all, *cloneVersion(version))
	}
	start := (filter.Page - 1) * filter.PageSize
	if start > len(all) {
		start = len(all)
	}
	end := start + filter.PageSize
	if end > len(all) {
		end = len(all)
	}
	return domain.PackagePage{
		Packages: all[start:end],
		Page:     filter.Page,
		PageSize: filter.PageSize,
		Total:    len(all),
	}, nil
}

func (repository *memoryRepository) GetDetail(
	_ context.Context,
	packageID string,
) (*domain.PackageDetail, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	versions := make([]domain.PackageVersion, 0)
	for _, version := range repository.versions {
		if version.PackageID != packageID {
			continue
		}
		versions = append(versions, *cloneVersion(version))
	}
	if len(versions) == 0 {
		return nil, domain.ErrPackageNotFound
	}
	history := make([]domain.ReviewLog, 0)
	for index := len(repository.logs) - 1; index >= 0; index-- {
		log := repository.logs[index]
		if log.PackageID == packageID {
			history = append(history, log)
		}
	}
	return &domain.PackageDetail{
		PackageID: packageID,
		Versions:  versions,
		History:   history,
	}, nil
}

func (repository *memoryRepository) GetVersion(
	_ context.Context,
	packageID string,
	packageVersion int,
) (*domain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	version := repository.versions[repository.key(packageID, packageVersion)]
	if version == nil {
		return nil, domain.ErrPackageNotFound
	}
	return cloneVersion(version), nil
}

func (repository *memoryRepository) CreateDraft(
	_ context.Context,
	input domain.MetadataInput,
	now time.Time,
) (*domain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := repository.key(input.PackageID, 1)
	if repository.versions[key] != nil {
		return nil, domain.ErrVersionExists
	}
	version := &domain.PackageVersion{
		PackageID:      input.PackageID,
		PackageVersion: 1,
		Title:          input.Title,
		Category:       input.Category,
		AgeTiers:       append([]domain.AgeTier(nil), input.AgeTiers...),
		AssetKey:       input.AssetKey,
		SHA256:         input.SHA256,
		SizeBytes:      input.SizeBytes,
		Status:         domain.StatusDraft,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	repository.versions[key] = version
	return cloneVersion(version), nil
}

func (repository *memoryRepository) CreateVersion(
	_ context.Context,
	input domain.MetadataInput,
	packageVersion int,
	now time.Time,
) (*domain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := repository.key(input.PackageID, packageVersion)
	if repository.versions[key] != nil {
		return nil, domain.ErrVersionExists
	}
	version := &domain.PackageVersion{
		PackageID:      input.PackageID,
		PackageVersion: packageVersion,
		Title:          input.Title,
		Category:       input.Category,
		AgeTiers:       append([]domain.AgeTier(nil), input.AgeTiers...),
		AssetKey:       input.AssetKey,
		SHA256:         input.SHA256,
		SizeBytes:      input.SizeBytes,
		Status:         domain.StatusDraft,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	repository.versions[key] = version
	return cloneVersion(version), nil
}

func (repository *memoryRepository) UpdateDraft(
	_ context.Context,
	packageID string,
	packageVersion int,
	input domain.MetadataInput,
	now time.Time,
) (*domain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	version := repository.versions[repository.key(packageID, packageVersion)]
	if version == nil {
		return nil, domain.ErrPackageNotFound
	}
	if version.Status != domain.StatusDraft {
		return nil, &domain.TransitionError{
			Current: cloneVersion(version),
			Err:     domain.ErrInvalidTransition,
		}
	}
	version.Title = input.Title
	version.Category = input.Category
	version.AgeTiers = append([]domain.AgeTier(nil), input.AgeTiers...)
	version.AssetKey = input.AssetKey
	version.SHA256 = input.SHA256
	version.SizeBytes = input.SizeBytes
	version.UpdatedAt = now
	return cloneVersion(version), nil
}

func (repository *memoryRepository) ApplyTransition(
	_ context.Context,
	transition domain.Transition,
) (*domain.PackageVersion, error) {
	repository.revisionLock <- struct{}{}
	defer func() {
		<-repository.revisionLock
	}()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	version := repository.versions[repository.key(
		transition.PackageID,
		transition.PackageVersion,
	)]
	if version == nil {
		return nil, domain.ErrPackageNotFound
	}
	if version.Status != transition.FromStatus {
		return nil, &domain.TransitionError{
			Current: cloneVersion(version),
			Err:     domain.ErrInvalidTransition,
		}
	}
	repository.revision++
	revision := repository.revision
	if transition.Action == domain.ActionPublish {
		for _, candidate := range repository.versions {
			if candidate.PackageID == transition.PackageID &&
				candidate.PackageVersion != transition.PackageVersion &&
				candidate.Status == domain.StatusPublished {
				candidate.Status = domain.StatusArchived
				candidate.CatalogRevision = revision
				candidate.UpdatedAt = transition.Now
				repository.logs = append(repository.logs, domain.ReviewLog{
					PackageID:      candidate.PackageID,
					PackageVersion: candidate.PackageVersion,
					Action:         domain.ActionArchive,
					FromStatus:     domain.StatusPublished,
					ToStatus:       domain.StatusArchived,
					ActorAccountID: transition.ActorAccountID,
					CreatedAt:      transition.Now,
				})
			}
		}
	}
	version.Status = transition.ToStatus
	version.UpdatedAt = transition.Now
	switch transition.Action {
	case domain.ActionSubmit:
		version.SubmittedAt = timePointer(transition.Now)
	case domain.ActionApprove:
		version.ApprovedAt = timePointer(transition.Now)
		version.ApprovedBy = transition.ActorAccountID
	case domain.ActionPublish:
		version.PublishedAt = timePointer(transition.Now)
		version.CatalogRevision = revision
	case domain.ActionWithdraw, domain.ActionArchive:
		version.CatalogRevision = revision
	}
	repository.logs = append(repository.logs, domain.ReviewLog{
		PackageID:      transition.PackageID,
		PackageVersion: transition.PackageVersion,
		Action:         transition.Action,
		FromStatus:     transition.FromStatus,
		ToStatus:       transition.ToStatus,
		Reason:         transition.Reason,
		ActorAccountID: transition.ActorAccountID,
		CreatedAt:      transition.Now,
	})
	return cloneVersion(version), nil
}

func (repository *memoryRepository) Catalog(
	_ context.Context,
	query domain.CatalogQuery,
) (*domain.Catalog, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if query.SinceRevision > repository.revision {
		return nil, domain.ErrRevisionAhead
	}
	packages := make([]domain.PackageVersion, 0)
	withdrawnSet := make(map[string]struct{})
	for _, version := range repository.versions {
		if version.CatalogRevision <= query.SinceRevision {
			continue
		}
		switch version.Status {
		case domain.StatusPublished:
			if query.Category != "" && string(version.Category) != query.Category {
				continue
			}
			if !categoryAllowedByQuery(version.Category, query.AllowedCategories) {
				continue
			}
			if query.AgeTier != "" && !containsAgeTier(version.AgeTiers, query.AgeTier) {
				continue
			}
			packages = append(packages, *cloneVersion(version))
		case domain.StatusWithdrawn, domain.StatusArchived:
			hasPublished := false
			for _, candidate := range repository.versions {
				if candidate.PackageID == version.PackageID &&
					candidate.Status == domain.StatusPublished {
					hasPublished = true
					break
				}
			}
			if !hasPublished {
				withdrawnSet[version.PackageID] = struct{}{}
			}
		}
	}
	withdrawn := make([]string, 0, len(withdrawnSet))
	for packageID := range withdrawnSet {
		withdrawn = append(withdrawn, packageID)
	}
	sort.Strings(withdrawn)
	return &domain.Catalog{
		Revision:            repository.revision,
		Packages:            packages,
		WithdrawnPackageIDs: withdrawn,
	}, nil
}

func (repository *memoryRepository) CatalogRevision(
	_ context.Context,
) (int64, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.revision, nil
}

func (repository *memoryRepository) PublishedDownload(
	_ context.Context,
	packageID string,
) (*domain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, version := range repository.versions {
		if version.PackageID == packageID &&
			version.Status == domain.StatusPublished {
			return cloneVersion(version), nil
		}
	}
	return nil, domain.ErrPackageNotFound
}

type fakeAssetReader struct {
	asset *domain.Asset
	err   error
}

func (reader fakeAssetReader) FindContentAsset(
	_ context.Context,
	_ string,
) (*domain.Asset, error) {
	if reader.err != nil {
		return nil, reader.err
	}
	return reader.asset, nil
}

type keyedAssetReader struct {
	assets map[string]*domain.Asset
}

func (reader keyedAssetReader) FindContentAsset(
	_ context.Context,
	assetKey string,
) (*domain.Asset, error) {
	asset := reader.assets[assetKey]
	if asset == nil {
		return nil, domain.ErrAssetNotFound
	}
	return asset, nil
}

func newContentService(
	t *testing.T,
	repository *memoryRepository,
	reader AssetReader,
) *Service {
	t.Helper()
	service, err := New(Options{
		Repository:  repository,
		AssetReader: reader,
		Clock: fixedClock{
			now: time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	return service
}

// policyReaderStub returns one fixed effective policy for the device path.
type policyReaderStub struct {
	effective *policydomain.EffectivePolicy
	err       error
}

func (reader policyReaderStub) GetEffective(
	_ context.Context,
	_ string,
) (*policydomain.EffectivePolicy, error) {
	if reader.err != nil {
		return nil, reader.err
	}
	return reader.effective, nil
}

func newContentServiceWithPolicy(
	t *testing.T,
	repository *memoryRepository,
	reader AssetReader,
	allowedCategories []string,
) *Service {
	t.Helper()
	service, err := New(Options{
		Repository:  repository,
		AssetReader: reader,
		PolicyReader: policyReaderStub{
			effective: &policydomain.EffectivePolicy{
				DailyLimitMinutes: 60,
				AllowedCategories: append(
					[]string(nil),
					allowedCategories...,
				),
			},
		},
		Clock: fixedClock{
			now: time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	return service
}

func validMetadata() domain.MetadataInput {
	return domain.MetadataInput{
		PackageID: "content_story_001",
		Title:     "月亮晚安故事",
		Category:  domain.CategoryStory,
		AgeTiers: []domain.AgeTier{
			domain.AgeTier3To4,
			domain.AgeTier5To6,
		},
		AssetKey:  "content/story/moon-night-v1.bin",
		SHA256:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SizeBytes: 1024,
	}
}

// publishPackage drives one package through the full review lifecycle up to
// published so the catalog and download paths can be exercised.
func publishPackage(
	t *testing.T,
	service *Service,
	input domain.MetadataInput,
) *domain.PackageVersion {
	t.Helper()
	created, err := service.CreateDraft(context.Background(), input)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.Submit(
		context.Background(),
		created.PackageID,
		created.PackageVersion,
		"admin-001",
	); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := service.Approve(
		context.Background(),
		created.PackageID,
		created.PackageVersion,
		"admin-001",
		"内容适合儿童",
	); err != nil {
		t.Fatalf("approve: %v", err)
	}
	published, err := service.Publish(
		context.Background(),
		created.PackageID,
		created.PackageVersion,
		"admin-001",
	)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return published
}

func TestLifecycleTransitionsAndReviewLog(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentService(t, repository, fakeAssetReader{
		asset: &domain.Asset{
			Key:         "content/story/moon-night-v1.bin",
			SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SizeBytes:   1024,
			DownloadURL: "https://download.example.test/content/story/moon-night-v1.bin",
		},
	})
	created, err := service.CreateDraft(context.Background(), validMetadata())
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if created.Status != domain.StatusDraft {
		t.Fatalf("new package status = %q, want draft", created.Status)
	}
	if _, err := service.Submit(
		context.Background(),
		created.PackageID,
		created.PackageVersion,
		"admin-001",
	); err != nil {
		t.Fatalf("submit: %v", err)
	}
	approved, err := service.Approve(
		context.Background(),
		created.PackageID,
		created.PackageVersion,
		"admin-001",
		"内容适合儿童",
	)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != domain.StatusInReview ||
		approved.ApprovedAt == nil ||
		approved.ApprovedBy != "admin-001" {
		t.Fatalf("approval metadata was not recorded: %+v", approved)
	}
	published, err := service.Publish(
		context.Background(),
		created.PackageID,
		created.PackageVersion,
		"admin-001",
	)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.Status != domain.StatusPublished ||
		published.PublishedAt == nil ||
		published.CatalogRevision < 1 {
		t.Fatalf("published package missing delivery metadata: %+v", published)
	}
	detail, err := service.GetDetail(context.Background(), created.PackageID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if len(detail.History) != 3 {
		t.Fatalf("review history length = %d, want 3", len(detail.History))
	}
	if detail.History[0].ActorAccountID != "admin-001" {
		t.Fatalf("review actor = %q, want admin-001", detail.History[0].ActorAccountID)
	}
}

func TestRejectRequiresReasonAndReturnsToDraft(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentService(t, repository, fakeAssetReader{})
	created, err := service.CreateDraft(context.Background(), validMetadata())
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.Submit(
		context.Background(),
		created.PackageID,
		created.PackageVersion,
		"admin-001",
	); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := service.Reject(
		context.Background(),
		created.PackageID,
		created.PackageVersion,
		"admin-001",
		" ",
	); !errors.Is(err, domain.ErrReasonRequired) {
		t.Fatalf("expected reason-required error, got %v", err)
	}
	rejected, err := service.Reject(
		context.Background(),
		created.PackageID,
		created.PackageVersion,
		"admin-001",
		"需要补充来源说明",
	)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != domain.StatusDraft {
		t.Fatalf("rejected status = %q, want draft", rejected.Status)
	}
	detail, err := service.GetDetail(context.Background(), created.PackageID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if detail.History[0].Reason != "需要补充来源说明" {
		t.Fatalf("reject reason = %q", detail.History[0].Reason)
	}
}

func TestPublishRejectsChecksumMismatch(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentService(t, repository, fakeAssetReader{
		asset: &domain.Asset{
			Key:    "content/story/moon-night-v1.bin",
			SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	})
	created, err := service.CreateDraft(context.Background(), validMetadata())
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.Submit(
		context.Background(),
		created.PackageID,
		1,
		"admin-001",
	); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := service.Approve(
		context.Background(),
		created.PackageID,
		1,
		"admin-001",
		"",
	); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := service.Publish(
		context.Background(),
		created.PackageID,
		1,
		"admin-001",
	); !errors.Is(err, domain.ErrAssetChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestPublishWithoutApprovalIsRejected(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentService(t, repository, fakeAssetReader{})
	created, err := service.CreateDraft(context.Background(), validMetadata())
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.Submit(
		context.Background(),
		created.PackageID,
		1,
		"admin-001",
	); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := service.Publish(
		context.Background(),
		created.PackageID,
		1,
		"admin-001",
	); !errors.Is(err, domain.ErrReviewRequired) {
		t.Fatalf("expected review-required error, got %v", err)
	}
}

func TestCatalogIncrementalRevisionAndDownload(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentService(t, repository, fakeAssetReader{
		asset: &domain.Asset{
			Key:         "content/story/moon-night-v1.bin",
			SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SizeBytes:   1024,
			DownloadURL: "https://download.example.test/content/story/moon-night-v1.bin",
		},
	})
	created, err := service.CreateDraft(context.Background(), validMetadata())
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.Submit(context.Background(), created.PackageID, 1, "admin-001"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := service.Approve(context.Background(), created.PackageID, 1, "admin-001", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := service.Publish(context.Background(), created.PackageID, 1, "admin-001"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	first, err := service.Catalog(context.Background(), domain.CatalogQuery{})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if first.Revision < 1 || len(first.Packages) != 1 {
		t.Fatalf("unexpected first catalog: %+v", first)
	}
	if first.Packages[0].PackageID != created.PackageID {
		t.Fatalf("catalog package = %q, want %q", first.Packages[0].PackageID, created.PackageID)
	}
	second, err := service.Catalog(context.Background(), domain.CatalogQuery{
		SinceRevision: first.Revision,
	})
	if err != nil {
		t.Fatalf("incremental catalog: %v", err)
	}
	if len(second.Packages) != 0 || len(second.WithdrawnPackageIDs) != 0 {
		t.Fatalf("expected empty incremental catalog, got %+v", second)
	}
	if _, err := service.Catalog(context.Background(), domain.CatalogQuery{
		SinceRevision: first.Revision + 10,
	}); !errors.Is(err, domain.ErrRevisionAhead) {
		t.Fatalf("expected revision-ahead error, got %v", err)
	}
	download, err := service.Download(context.Background(), created.PackageID)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if download.DownloadURL == "" || download.SHA256 != validMetadata().SHA256 {
		t.Fatalf("unexpected download: %+v", download)
	}
}

func TestCatalogReportsWithdrawnAndArchivedPackagesIncrementally(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentService(t, repository, fakeAssetReader{
		asset: &domain.Asset{
			Key:         "content/story/moon-night-v1.bin",
			SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SizeBytes:   1024,
			DownloadURL: "https://download.example.test/content/story/moon-night-v1.bin",
		},
	})
	created, err := service.CreateDraft(context.Background(), validMetadata())
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.Submit(context.Background(), created.PackageID, 1, "admin-001"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := service.Approve(context.Background(), created.PackageID, 1, "admin-001", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := service.Publish(context.Background(), created.PackageID, 1, "admin-001"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	afterPublish, err := service.Catalog(context.Background(), domain.CatalogQuery{})
	if err != nil {
		t.Fatalf("catalog after publish: %v", err)
	}
	if len(afterPublish.Packages) != 1 {
		t.Fatalf("packages after publish = %d, want 1", len(afterPublish.Packages))
	}
	if _, err := service.Withdraw(
		context.Background(),
		created.PackageID,
		1,
		"admin-001",
		"发现内容不适宜",
	); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	incremental, err := service.Catalog(context.Background(), domain.CatalogQuery{
		SinceRevision: afterPublish.Revision,
	})
	if err != nil {
		t.Fatalf("incremental catalog: %v", err)
	}
	if incremental.Revision <= afterPublish.Revision {
		t.Fatalf("revision did not advance: %d <= %d", incremental.Revision, afterPublish.Revision)
	}
	if len(incremental.Packages) != 0 {
		t.Fatalf("withdrawn package still delivered: %+v", incremental.Packages)
	}
	if len(incremental.WithdrawnPackageIDs) != 1 ||
		incremental.WithdrawnPackageIDs[0] != created.PackageID {
		t.Fatalf("withdrawn ids = %+v, want [%s]", incremental.WithdrawnPackageIDs, created.PackageID)
	}
	if _, err := service.Archive(
		context.Background(),
		created.PackageID,
		1,
		"admin-001",
	); err != nil {
		t.Fatalf("archive: %v", err)
	}
	archivedIncremental, err := service.Catalog(context.Background(), domain.CatalogQuery{
		SinceRevision: incremental.Revision,
	})
	if err != nil {
		t.Fatalf("archived incremental catalog: %v", err)
	}
	if len(archivedIncremental.WithdrawnPackageIDs) != 1 ||
		archivedIncremental.WithdrawnPackageIDs[0] != created.PackageID {
		t.Fatalf(
			"archived withdrawn ids = %+v, want [%s]",
			archivedIncremental.WithdrawnPackageIDs,
			created.PackageID,
		)
	}
	// A cursor after the archive must not keep re-delivering the retired
	// package, and the full catalog must stay empty on a fresh request.
	fresh, err := service.Catalog(context.Background(), domain.CatalogQuery{
		SinceRevision: archivedIncremental.Revision,
	})
	if err != nil {
		t.Fatalf("fresh catalog after archive: %v", err)
	}
	if len(fresh.Packages) != 0 || len(fresh.WithdrawnPackageIDs) != 0 {
		t.Fatalf("expected settled catalog, got %+v", fresh)
	}
}

func TestPublishingNewVersionArchivesPrevious(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentService(t, repository, keyedAssetReader{
		assets: map[string]*domain.Asset{
			"content/story/moon-night-v1.bin": {
				Key:         "content/story/moon-night-v1.bin",
				SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				SizeBytes:   1024,
				DownloadURL: "https://download.example.test/content/story/moon-night-v1.bin",
			},
			"content/story/moon-night-v2.bin": {
				Key:         "content/story/moon-night-v2.bin",
				SHA256:      "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				SizeBytes:   2048,
				DownloadURL: "https://download.example.test/content/story/moon-night-v2.bin",
			},
		},
	})
	first, err := service.CreateDraft(context.Background(), validMetadata())
	if err != nil {
		t.Fatalf("create first draft: %v", err)
	}
	for _, action := range []func() error{
		func() error {
			_, err := service.Submit(context.Background(), first.PackageID, 1, "admin-001")
			return err
		},
		func() error {
			_, err := service.Approve(context.Background(), first.PackageID, 1, "admin-001", "")
			return err
		},
		func() error {
			_, err := service.Publish(context.Background(), first.PackageID, 1, "admin-001")
			return err
		},
	} {
		if err := action(); err != nil {
			t.Fatalf("publish first version: %v", err)
		}
	}
	input := validMetadata()
	input.Title = "月亮晚安故事（新版）"
	input.AssetKey = "content/story/moon-night-v2.bin"
	input.SHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	second, err := service.CreateVersion(context.Background(), first.PackageID, input)
	if err != nil {
		t.Fatalf("create second version: %v", err)
	}
	if second.PackageVersion != 2 {
		t.Fatalf("second version = %d, want 2", second.PackageVersion)
	}
	if _, err := service.Submit(context.Background(), first.PackageID, 2, "admin-001"); err != nil {
		t.Fatalf("submit second version: %v", err)
	}
	if _, err := service.Approve(context.Background(), first.PackageID, 2, "admin-001", ""); err != nil {
		t.Fatalf("approve second version: %v", err)
	}
	if _, err := service.Publish(context.Background(), first.PackageID, 2, "admin-001"); err != nil {
		t.Fatalf("publish second version: %v", err)
	}
	detail, err := service.GetDetail(context.Background(), first.PackageID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	publishedCount := 0
	archivedCount := 0
	for _, version := range detail.Versions {
		switch version.Status {
		case domain.StatusPublished:
			publishedCount++
			if version.PackageVersion != 2 {
				t.Fatalf("published version = %d, want 2", version.PackageVersion)
			}
		case domain.StatusArchived:
			archivedCount++
		}
	}
	if publishedCount != 1 || archivedCount != 1 {
		t.Fatalf(
			"published=%d archived=%d, want one of each",
			publishedCount,
			archivedCount,
		)
	}
}

func TestConcurrentRevisionIsStrictlyMonotonic(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentService(t, repository, fakeAssetReader{})
	const packageCount = 8
	for index := 0; index < packageCount; index++ {
		input := validMetadata()
		input.PackageID = fmt.Sprintf("content_story_%03d", index+1)
		if _, err := service.CreateDraft(context.Background(), input); err != nil {
			t.Fatalf("create draft %d: %v", index, err)
		}
	}
	var waitGroup sync.WaitGroup
	for index := 0; index < packageCount; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			packageID := fmt.Sprintf("content_story_%03d", index+1)
			if _, err := service.Submit(
				context.Background(),
				packageID,
				1,
				"admin-001",
			); err != nil {
				t.Errorf("submit %s: %v", packageID, err)
			}
		}(index)
	}
	waitGroup.Wait()
	if repository.revision != packageCount {
		t.Fatalf("revision = %d, want %d", repository.revision, packageCount)
	}
}

// TestDeviceCatalogFiltersDisallowedCategory proves the device path cannot
// receive packages from a category the family policy excludes, while admin
// browsing keeps the full catalog.
func TestDeviceCatalogFiltersDisallowedCategory(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentServiceWithPolicy(
		t,
		repository,
		keyedAssetReader{assets: map[string]*domain.Asset{
			"content/story/moon-night-v1.bin": {
				Key:         "content/story/moon-night-v1.bin",
				SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				SizeBytes:   1024,
				DownloadURL: "https://download.example.test/content/story/moon-night-v1.bin",
			},
			"content/english/hello-v1.bin": {
				Key:         "content/english/hello-v1.bin",
				SHA256:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				SizeBytes:   2048,
				DownloadURL: "https://download.example.test/content/english/hello-v1.bin",
			},
		}},
		[]string{"story"},
	)
	storyMetadata := validMetadata()
	publishPackage(t, service, storyMetadata)
	englishMetadata := validMetadata()
	englishMetadata.PackageID = "content_english_001"
	englishMetadata.Title = "早安英语"
	englishMetadata.Category = domain.CategoryEnglish
	englishMetadata.AssetKey = "content/english/hello-v1.bin"
	englishMetadata.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	publishPackage(t, service, englishMetadata)

	adminCatalog, err := service.Catalog(context.Background(), domain.CatalogQuery{})
	if err != nil {
		t.Fatalf("admin catalog: %v", err)
	}
	if len(adminCatalog.Packages) != 2 {
		t.Fatalf("admin catalog packages = %d, want 2", len(adminCatalog.Packages))
	}

	deviceCatalog, err := service.Catalog(context.Background(), domain.CatalogQuery{
		FamilyID: "family_001",
	})
	if err != nil {
		t.Fatalf("device catalog: %v", err)
	}
	if len(deviceCatalog.Packages) != 1 {
		t.Fatalf("device catalog packages = %d, want 1", len(deviceCatalog.Packages))
	}
	if deviceCatalog.Packages[0].Category != domain.CategoryStory {
		t.Fatalf(
			"device catalog category = %q, want %q",
			deviceCatalog.Packages[0].Category,
			domain.CategoryStory,
		)
	}
}

// TestDeviceCatalogKeepsRevisionWhenPolicyDeniesAll proves the device still
// learns the current delivery cursor even when no category is allowed.
func TestDeviceCatalogKeepsRevisionWhenPolicyDeniesAll(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentServiceWithPolicy(
		t,
		repository,
		fakeAssetReader{
			asset: &domain.Asset{
				Key:         "content/story/moon-night-v1.bin",
				SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				SizeBytes:   1024,
				DownloadURL: "https://download.example.test/content/story/moon-night-v1.bin",
			},
		},
		// The family allows only a category that has no published package, so
		// the intersection is empty while storage still has a real revision.
		[]string{"bedtime"},
	)
	publishPackage(t, service, validMetadata())
	catalog, err := service.Catalog(context.Background(), domain.CatalogQuery{
		FamilyID: "family_001",
	})
	if err != nil {
		t.Fatalf("device catalog: %v", err)
	}
	if len(catalog.Packages) != 0 {
		t.Fatalf("expected empty catalog, got %+v", catalog.Packages)
	}
	if catalog.Revision < 1 {
		t.Fatalf("revision = %d, want >= 1", catalog.Revision)
	}
}

// TestDeviceDownloadDeniesDisallowedCategory proves a device cannot bypass the
// catalog filter by requesting the package download directly.
func TestDeviceDownloadDeniesDisallowedCategory(t *testing.T) {
	repository := newMemoryRepository()
	service := newContentServiceWithPolicy(
		t,
		repository,
		keyedAssetReader{assets: map[string]*domain.Asset{
			"content/english/hello-v1.bin": {
				Key:         "content/english/hello-v1.bin",
				SHA256:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				SizeBytes:   2048,
				DownloadURL: "https://download.example.test/content/english/hello-v1.bin",
			},
		}},
		[]string{"story"},
	)
	englishMetadata := validMetadata()
	englishMetadata.PackageID = "content_english_001"
	englishMetadata.Title = "早安英语"
	englishMetadata.Category = domain.CategoryEnglish
	englishMetadata.AssetKey = "content/english/hello-v1.bin"
	englishMetadata.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	publishPackage(t, service, englishMetadata)

	if _, err := service.DownloadForFamily(
		context.Background(),
		"family_001",
		"content_english_001",
	); !errors.Is(err, domain.ErrPackageNotFound) {
		t.Fatalf("expected package-not-found denial, got %v", err)
	}
}

func TestNextStatusRejectsIllegalMoves(t *testing.T) {
	for _, test := range []struct {
		current domain.Status
		action  domain.Action
		want    domain.Status
	}{
		{domain.StatusDraft, domain.ActionSubmit, domain.StatusInReview},
		{domain.StatusInReview, domain.ActionApprove, domain.StatusInReview},
		{domain.StatusInReview, domain.ActionReject, domain.StatusDraft},
		{domain.StatusInReview, domain.ActionPublish, domain.StatusPublished},
		{domain.StatusPublished, domain.ActionWithdraw, domain.StatusWithdrawn},
		{domain.StatusWithdrawn, domain.ActionArchive, domain.StatusArchived},
		{domain.StatusDraft, domain.ActionPublish, ""},
		{domain.StatusArchived, domain.ActionSubmit, ""},
		{domain.StatusPublished, domain.ActionArchive, ""},
	} {
		got, err := domain.NextStatus(test.current, test.action)
		if test.want == "" {
			if !errors.Is(err, domain.ErrInvalidTransition) {
				t.Fatalf(
					"NextStatus(%s, %s) error = %v, want invalid transition",
					test.current,
					test.action,
					err,
				)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Fatalf(
				"NextStatus(%s, %s) = %q, %v, want %q",
				test.current,
				test.action,
				got,
				err,
				test.want,
			)
		}
	}
}

func cloneVersion(value *domain.PackageVersion) *domain.PackageVersion {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.AgeTiers = append([]domain.AgeTier(nil), value.AgeTiers...)
	return &cloned
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func containsAgeTier(values []domain.AgeTier, want string) bool {
	for _, value := range values {
		if string(value) == want {
			return true
		}
	}
	return false
}

func categoryAllowedByQuery(category domain.Category, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, value := range allowed {
		if string(category) == value {
			return true
		}
	}
	return false
}
