package database

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestValidateMigrationVersionsRejectsDuplicatePrefix(t *testing.T) {
	t.Parallel()
	entries, err := fs.ReadDir(fstest.MapFS{
		"0056_notification.sql": {},
		"0056_ota.sql":          {},
	}, ".")
	if err != nil {
		t.Fatalf("read test migrations: %v", err)
	}
	err = validateMigrationVersions(entries)
	if err == nil {
		t.Fatal("expected duplicate migration version rejection")
	}
}

func TestValidateMigrationVersionsAcceptsOrderedPrefixes(t *testing.T) {
	t.Parallel()
	entries, err := fs.ReadDir(fstest.MapFS{
		"0056_notification.sql": {},
		"0057_ota.sql":          {},
	}, ".")
	if err != nil {
		t.Fatalf("read test migrations: %v", err)
	}
	if err := validateMigrationVersions(entries); err != nil {
		t.Fatalf("valid migration versions rejected: %v", err)
	}
}

func TestFamilyMembershipRevisionMigrationHasAtomicConcurrencyShape(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "0070_family_membership_revision.sql")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read revision migration: %v", err)
	}
	sql := string(body)
	for _, expected := range []string{
		"CREATE TABLE IF NOT EXISTS family_membership_revisions",
		"family_id UUID PRIMARY KEY",
		"REFERENCES parent_accounts(id) ON DELETE CASCADE",
		"revision BIGINT NOT NULL DEFAULT 1",
		"CHECK (revision >= 1)",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("revision migration missing %q", expected)
		}
	}
}

func TestReleaseArtifactIdentityMigrationRemovesVersionOnlyUniqueness(t *testing.T) {
	path := filepath.Join(
		"..",
		"..",
		"..",
		"migrations",
		"0071_platform_release_artifact_identity.sql",
	)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read release identity migration: %v", err)
	}
	sql := strings.ToLower(string(body))
	for _, expected := range []string{
		"drop constraint if exists platform_releases_version_unique",
		"unique (version, channel, kind, platform)",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("release identity migration missing %q", expected)
		}
	}
	if strings.Contains(sql, "begin;") || strings.Contains(sql, "commit;") {
		t.Fatal("release identity migration must rely on the migration runner transaction")
	}
}

func TestPlatformReleaseIdentityMigrationReplacesVersionConstraint(t *testing.T) {
	path := filepath.Join(
		"..",
		"..",
		"..",
		"migrations",
		"0071_platform_release_artifact_identity.sql",
	)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read platform release identity migration: %v", err)
	}
	sql := string(body)
	for _, expected := range []string{
		"DROP CONSTRAINT IF EXISTS platform_releases_version_unique",
		"platform_releases_artifact_identity_unique",
		"UNIQUE (version, channel, kind, platform)",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("platform release identity migration missing %q", expected)
		}
	}
}
