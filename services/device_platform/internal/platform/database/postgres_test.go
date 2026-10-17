package database

import (
	"io/fs"
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
