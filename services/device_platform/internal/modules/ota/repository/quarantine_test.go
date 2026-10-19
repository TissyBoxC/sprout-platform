package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
)

func TestPublishedReleaseCatalogExcludesQuarantineAndOversizedArtifacts(
	t *testing.T,
) {
	normalized := strings.Join(strings.Fields(publishedReleaseCatalogSQL), " ")
	for _, required := range []string{
		"release.status = 'published'",
		"release.size_bytes > 0",
		"release.size_bytes <= $1",
		"ota_legacy_quarantined_releases AS quarantine",
		"quarantine.release_id = release.id",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf(
				"published release query is missing %q: %s",
				required,
				normalized,
			)
		}
	}
	if maxOTAArtifactSizeBytes != 4*1024*1024 {
		t.Fatalf(
			"catalog size limit = %d, want 4 MiB",
			maxOTAArtifactSizeBytes,
		)
	}
}

func TestOTAArtifactSizeMigrationQuarantinesLegacyRows(t *testing.T) {
	migrationPath := filepath.Join(
		"..", "..", "..", "..", "migrations",
		"0068_ota_artifact_size_limit.sql",
	)
	contents, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	migration := strings.Join(strings.Fields(string(contents)), " ")
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS ota_legacy_quarantined_releases",
		"release_id UUID PRIMARY KEY",
		"REFERENCES ota_releases(id) ON DELETE RESTRICT",
		"to_regclass('public.ota_releases') IS NULL",
		"OTA release tables are missing before migration 0068",
		"BEFORE UPDATE OR DELETE ON ota_legacy_quarantined_releases",
		"INSERT INTO ota_legacy_quarantined_releases (release_id, reason)",
		"WHERE release.size_bytes > 4194304",
		"AND release.status <> 'draft'",
		"ON CONFLICT (release_id) DO NOTHING",
		"oversized OTA drafts must be corrected or withdrawn",
		"CHECK (size_bytes > 0 AND size_bytes <= 4194304) NOT VALID",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("migration is missing %q", required)
		}
	}
	if strings.Contains(migration, "VALIDATE CONSTRAINT ota_releases_size_valid") {
		t.Fatal(
			"migration must retain legacy rows with an unvalidated size constraint",
		)
	}
}

func TestPublishedCatalogFiltersHistoricalOversizedReleases(t *testing.T) {
	legacyOversized := releaseFixture(
		"10000000-0000-4000-8000-000000000001",
		"2.0.0",
		4194305,
	)
	valid := releaseFixture(
		"10000000-0000-4000-8000-000000000002",
		"1.9.0",
		4194304,
	)

	releases := filterPublishedCatalog(
		[]domain.Release{legacyOversized, valid},
		nil,
	)
	if len(releases) != 1 || releases[0].ID != valid.ID {
		t.Fatalf(
			"historical oversized release must be excluded, got %+v",
			releases,
		)
	}
}

func TestCatalogArtifactSizeBoundaryMatchesDatabaseConstraint(t *testing.T) {
	release := releaseFixture(
		"11000000-0000-4000-8000-000000000001",
		"2.1.0",
		4194305,
	)
	if validCatalogArtifactSize(release.SizeBytes) {
		t.Fatal("oversized release must be outside the catalog contract")
	}
	if !validCatalogArtifactSize(4194304) {
		t.Fatal("4 MiB boundary must be accepted")
	}
}

func TestHighestQuarantinedReleaseFallsBackToNextValidVersion(t *testing.T) {
	highest := releaseFixture(
		"12000000-0000-4000-8000-000000000001",
		"3.0.0",
		1024,
	)
	fallback := releaseFixture(
		"12000000-0000-4000-8000-000000000002",
		"2.9.0",
		1024,
	)
	quarantined := map[string]struct{}{highest.ID: {}}

	releases := filterPublishedCatalog(
		[]domain.Release{highest, fallback},
		quarantined,
	)
	selected, err := domain.SelectRelease(
		releases,
		domain.DeviceContext{
			DeviceID:         "ota-quarantine-device",
			HardwareRevision: "sprout-v1",
			CurrentVersion:   "1.0.0",
		},
		domain.ChannelStable,
	)
	if err != nil {
		t.Fatalf("select fallback release: %v", err)
	}
	if selected.ID != fallback.ID || selected.Version != fallback.Version {
		t.Fatalf(
			"selected release = %s %s, want fallback %s %s",
			selected.ID,
			selected.Version,
			fallback.ID,
			fallback.Version,
		)
	}
}

func filterPublishedCatalog(
	releases []domain.Release,
	quarantined map[string]struct{},
) []domain.Release {
	filtered := make([]domain.Release, 0, len(releases))
	for index := range releases {
		release := &releases[index]
		if !eligiblePublishedCatalogRelease(release) {
			continue
		}
		if _, isQuarantined := quarantined[release.ID]; isQuarantined {
			continue
		}
		filtered = append(filtered, *release)
	}
	return filtered
}

func releaseFixture(
	id string,
	version string,
	sizeBytes int64,
) domain.Release {
	publishedAt := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	return domain.Release{
		ID:               id,
		Version:          version,
		Channel:          domain.ChannelStable,
		HardwareRevision: "sprout-v1",
		MinSourceVersion: "1.0.0",
		ArtifactURL: "https://download.clarkhub.cn/firmware/" +
			version + ".bin",
		ArtifactKey: "firmware/" + version + ".bin",
		SizeBytes:   sizeBytes,
		Status:      domain.ReleaseStatusPublished,
		Target: domain.Target{
			Scope: domain.TargetScopeAll,
		},
		RecordVersion: 1,
		CreatedBy:     "integration-test",
		UpdatedBy:     "integration-test",
		PublishedAt:   &publishedAt,
	}
}
