// This file projects the worker-owned release catalogue for the management
// console. Device platform only reads the shared snapshot and never receives
// the GitHub token used by the worker.
package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/repository"
)

const (
	// ReleaseCatalogLimit matches the worker's default release catalogue
	// capacity. Keeping it exported prevents the transport layer from drifting
	// to a smaller page size than the catalogue can contain.
	ReleaseCatalogLimit = 100

	releasePageSizeDefault = ReleaseCatalogLimit
	releasePageSizeMax     = ReleaseCatalogLimit
)

// ReleaseSource lists published versions from the worker-generated catalogue.
// Implementations are read-only and must never expose credentials.
type ReleaseSource interface {
	ListReleases(
		ctx context.Context,
		repositoryID string,
		page int,
		pageSize int,
	) ([]domain.Release, bool, error)
	LatestRelease(
		ctx context.Context,
		repositoryID string,
	) (domain.Release, error)
	FindRelease(
		ctx context.Context,
		repositoryID string,
		version string,
	) (domain.Release, error)
}

// RepositoryReleaseSource reads the worker-owned release catalogue.
type RepositoryReleaseSource struct {
	repository repository.Repository
}

// NewRepositoryReleaseSource creates the read-only release source.
func NewRepositoryReleaseSource(
	catalogueRepository repository.Repository,
) *RepositoryReleaseSource {
	return &RepositoryReleaseSource{repository: catalogueRepository}
}

func (source *RepositoryReleaseSource) ListReleases(
	ctx context.Context,
	repositoryID string,
	page int,
	pageSize int,
) ([]domain.Release, bool, error) {
	catalog, err := source.repository.ReleaseCatalog(ctx)
	if err != nil {
		return nil, false, mapReleaseCatalogError(err)
	}
	releases := publishedReleases(catalog.Services[repositoryID])
	page, pageSize = normalizeReleasePage(page, pageSize)
	start := (page - 1) * pageSize
	if start >= len(releases) {
		return []domain.Release{}, false, nil
	}
	end := start + pageSize
	if end > len(releases) {
		end = len(releases)
	}
	return append([]domain.Release(nil), releases[start:end]...), end < len(releases), nil
}

func (source *RepositoryReleaseSource) LatestRelease(
	ctx context.Context,
	repositoryID string,
) (domain.Release, error) {
	releases, _, err := source.ListReleases(ctx, repositoryID, 1, releasePageSizeMax)
	if err != nil {
		return domain.Release{}, err
	}
	if len(releases) == 0 {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	return releases[0], nil
}

func (source *RepositoryReleaseSource) FindRelease(
	ctx context.Context,
	repositoryID string,
	version string,
) (domain.Release, error) {
	version = normalizeReleaseVersion(version)
	if !isReleaseVersion(version) {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	catalog, err := source.repository.ReleaseCatalog(ctx)
	if err != nil {
		return domain.Release{}, mapReleaseCatalogError(err)
	}
	for _, release := range publishedReleases(catalog.Services[repositoryID]) {
		if release.Version == version {
			return release, nil
		}
	}
	return domain.Release{}, domain.ErrReleaseNotFound
}

func mapReleaseCatalogError(err error) error {
	if errors.Is(err, domain.ErrStateUnavailable) {
		return domain.ErrReleaseCatalogNotReady
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return domain.ErrReleaseSourceFailed
}

func publishedReleases(releases []domain.Release) []domain.Release {
	result := make([]domain.Release, 0, len(releases))
	for _, release := range releases {
		release.Version = normalizeReleaseVersion(release.Version)
		if !isReleaseVersion(release.Version) {
			continue
		}
		result = append(result, release)
	}
	sort.SliceStable(result, func(left int, right int) bool {
		return domain.CompareVersions(result[left].Version, result[right].Version) > 0
	})
	for index := range result {
		result[index].IsCurrent = false
		result[index].IsLatest = index == 0
	}
	return result
}

func normalizeReleasePage(page int, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = releasePageSizeDefault
	}
	if pageSize > releasePageSizeMax {
		pageSize = releasePageSizeMax
	}
	return page, pageSize
}

func normalizeReleaseVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	return value
}

func isReleaseVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}
