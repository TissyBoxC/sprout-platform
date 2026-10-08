package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/repository"
)

func TestRepositoryReleaseSourceListsPastLegacyPageLimits(t *testing.T) {
	releases := make([]domain.Release, 75)
	for index := range releases {
		releases[index] = domain.Release{
			Version: fmt.Sprintf("1.0.%d", index),
		}
	}
	source := newTestReleaseRepository(t, releases)

	page, hasMore, err := source.ListReleases(
		context.Background(),
		"device_platform",
		1,
		ReleaseCatalogLimit,
	)
	if err != nil {
		t.Fatalf("ListReleases() returned unexpected error: %v", err)
	}
	if len(page) != 75 {
		t.Fatalf("expected all 75 releases, got %d", len(page))
	}
	if hasMore {
		t.Fatal("expected no additional pages for a 75-entry catalogue")
	}
	if page[0].Version != "1.0.74" || page[len(page)-1].Version != "1.0.0" {
		t.Fatalf(
			"expected newest-first catalogue, got first %q and last %q",
			page[0].Version,
			page[len(page)-1].Version,
		)
	}
}

func TestRepositoryReleaseSourceFindsReleaseBeyondFirstPage(t *testing.T) {
	releases := make([]domain.Release, 125)
	for index := range releases {
		releases[index] = domain.Release{
			Version: fmt.Sprintf("1.0.%d", index),
		}
	}
	source := newTestReleaseRepository(t, releases)

	release, err := source.FindRelease(
		context.Background(),
		"device_platform",
		"1.0.1",
	)
	if err != nil {
		t.Fatalf("FindRelease() returned unexpected error: %v", err)
	}
	if release.Version != "1.0.1" {
		t.Fatalf("expected release 1.0.1, got %q", release.Version)
	}
}

func TestRepositoryReleaseSourceReportsMissingCatalogAsNotReady(t *testing.T) {
	source := NewRepositoryReleaseSource(repository.NewFileRepository(t.TempDir()))

	_, _, err := source.ListReleases(
		context.Background(),
		"device_platform",
		1,
		ReleaseCatalogLimit,
	)
	if !errors.Is(err, domain.ErrReleaseCatalogNotReady) {
		t.Fatalf("expected ErrReleaseCatalogNotReady, got %v", err)
	}
	if errors.Is(err, domain.ErrReleaseSourceFailed) {
		t.Fatal("a missing catalog must not be reported as a source failure")
	}
}

func TestRepositoryReleaseSourceReportsCorruptCatalogAsSourceFailure(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(stateDir, "releases.json"),
		[]byte("{not-json"),
		0o600,
	); err != nil {
		t.Fatalf("write corrupt release catalog: %v", err)
	}
	source := NewRepositoryReleaseSource(repository.NewFileRepository(stateDir))

	_, _, err := source.ListReleases(
		context.Background(),
		"device_platform",
		1,
		ReleaseCatalogLimit,
	)
	if !errors.Is(err, domain.ErrReleaseSourceFailed) {
		t.Fatalf("expected ErrReleaseSourceFailed, got %v", err)
	}
}

func newTestReleaseRepository(
	t *testing.T,
	releases []domain.Release,
) *RepositoryReleaseSource {
	t.Helper()
	stateDir := t.TempDir()
	payload, err := json.Marshal(domain.ReleaseCatalog{
		GeneratedAt: time.Unix(0, 0).UTC(),
		Services: map[string][]domain.Release{
			"device_platform": releases,
		},
	})
	if err != nil {
		t.Fatalf("encode release catalog: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(stateDir, "releases.json"),
		payload,
		0o600,
	); err != nil {
		t.Fatalf("write release catalog: %v", err)
	}
	return NewRepositoryReleaseSource(repository.NewFileRepository(stateDir))
}

func TestNormalizeReleasePageUsesCatalogueLimitByDefault(t *testing.T) {
	page, pageSize := normalizeReleasePage(0, 0)
	if page != 1 || pageSize != ReleaseCatalogLimit {
		t.Fatalf(
			"expected page 1 with size %d, got page %d with size %d",
			ReleaseCatalogLimit,
			page,
			pageSize,
		)
	}

	_, pageSize = normalizeReleasePage(1, ReleaseCatalogLimit+1)
	if pageSize != ReleaseCatalogLimit {
		t.Fatalf(
			"expected oversized page size to clamp to %d, got %d",
			ReleaseCatalogLimit,
			pageSize,
		)
	}
}
