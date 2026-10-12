package repository

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// purgeExemptTables are tables that intentionally survive an account purge.
// They are either global catalogs/releases whose rows are not owned by a
// guardian or device, audit projections that must keep a stable account
// identifier for the anonymized tombstone, or child tables whose only parent
// reference is a device credential deleted earlier in the same transaction.
var purgeExemptTables = map[string]string{
	"platform_settings":           "global operational settings",
	"platform_releases":           "global release manifest",
	"platform_parent_audit":       "retained platform audit projection",
	"platform_feature_configs":    "global feature configuration",
	"parent_deletion_requests":    "retained deletion audit record",
	"content_package_versions":    "global content catalog",
	"content_review_logs":         "retained content review audit",
	"ota_releases":                "global immutable release manifest",
	"content_offline_policy_acks": "cascades from deleted device_credentials",
	"ui_text_package_versions":    "global UI text catalog",
}

var (
	migrationTablePattern = regexp.MustCompile(
		`(?is)CREATE TABLE IF NOT EXISTS\s+([a-z_]+)\s*\((.*?)\n\);`)
	ownedColumnPattern = regexp.MustCompile(
		`(?im)^\s*(family_id|parent_account_id)\s`)
)

// TestPurgeParentDataCoversOwnedTables fails when a migration introduces a
// table keyed by family_id or parent_account_id that PurgeParentData neither
// deletes nor explicitly exempts. Because the guardian row is anonymized
// rather than removed, ON DELETE CASCADE from parent_accounts never fires
// during a purge, so an omitted table silently retains guardian data.
func TestPurgeParentDataCoversOwnedTables(t *testing.T) {
	source, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}

	migrationDir := filepath.Join("..", "..", "..", "..", "migrations")
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}

	purged := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?i)DELETE FROM\s+([a-z_]+)`).
		FindAllStringSubmatch(string(source), -1) {
		purged[match[1]] = true
	}

	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(migrationDir, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		for _, table := range migrationTablePattern.FindAllStringSubmatch(
			string(contents), -1) {
			name := table[1]
			if !ownedColumnPattern.MatchString(table[2]) {
				continue
			}
			checked++
			if purged[name] {
				continue
			}
			if reason, exempt := purgeExemptTables[name]; exempt {
				_ = reason
				continue
			}
			t.Errorf(
				"table %q has a family_id/parent_account_id column but no "+
					"DELETE in PurgeParentData; add an explicit delete or a "+
					"documented exemption",
				name,
			)
		}
	}
	if checked == 0 {
		t.Fatal("no owned tables were discovered; migration scan is broken")
	}
}

// TestPurgeExpiredRecordsCoversEphemeralTables guards the retention job that
// migration 0034 promises. Every table below stores an `expires_at` deadline;
// if the job stops deleting one, expired credential material accumulates in
// the database indefinitely even though the schema says it is short-lived.
func TestPurgeExpiredRecordsCoversEphemeralTables(t *testing.T) {
	source, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	required := []string{
		"DELETE FROM auth_sessions",
		"DELETE FROM admin_mfa_challenges",
		"DELETE FROM device_sessions",
		"DELETE FROM device_binding_tokens",
		"DELETE FROM device_registration_tokens",
		"DELETE FROM device_challenges",
		"DELETE FROM phone_verification_codes",
		"DELETE FROM parent_data_export_receipts",
	}
	for _, fragment := range required {
		if !bytes.Contains(source, []byte(fragment)) {
			t.Errorf("PurgeExpiredRecords is missing %q", fragment)
		}
	}
}
