package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	expiresAtColumnPattern = regexp.MustCompile(
		`(?im)^\s*expires_at\s`)
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

// retentionExemptTables are tables whose `expires_at` rows are reclaimed by
// another scheduled job (not the privacy retention worker) or which must be
// retained by design. They are exempt from the privacy retention coverage scan.
var retentionExemptTables = map[string]string{
	"vision_tasks":   "pruned by the vision module's PruneExpired job",
	"vision_results": "pruned by the vision module's PruneExpired job",
	// notifications.expires_at bounds inbox visibility only. Operator-authored
	// broadcast history is intentionally retained for the admin console, so the
	// row is not deleted when the notice stops being visible to guardians.
	"notifications": "expiry hides the notice from inboxes; admin history is retained",
	// billing_orders.expires_at is the payment deadline, not a retention
	// window. A settled order is a financial record the guardian and the
	// reconciliation trail both depend on, so the row outlives the deadline and
	// only its status changes.
	"billing_orders": "expires_at is the payment deadline; the order is retained as a financial record",
}

// TestPurgeExpiredRecordsCoversEphemeralTables guards the retention job that
// migration 0034 promises. It scans every migration for a table with an
// `expires_at` deadline and requires PurgeExpiredRecords to delete from that
// table (or an explicit exemption for a table reclaimed elsewhere). Without
// this, a new short-lived table silently accumulates expired rows forever.
func TestPurgeExpiredRecordsCoversEphemeralTables(t *testing.T) {
	source, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}

	migrationDir := filepath.Join("..", "..", "..", "..", "migrations")
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
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
			if !expiresAtColumnPattern.MatchString(table[2]) {
				continue
			}
			checked++
			if bytes.Contains(source, []byte("DELETE FROM "+name)) {
				continue
			}
			if _, exempt := retentionExemptTables[name]; exempt {
				continue
			}
			t.Errorf(
				"table %q has an expires_at column but no DELETE in "+
					"PurgeExpiredRecords; add a delete or a documented exemption",
				name,
			)
		}
	}
	if checked == 0 {
		t.Fatal("no expires_at tables were discovered; migration scan is broken")
	}
}

type purgeTestDatabase struct {
	results []pgconn.CommandTag
	errs    []error
	args    [][]any
	calls   int
}

func (d *purgeTestDatabase) Exec(
	_ context.Context,
	_ string,
	args ...any,
) (pgconn.CommandTag, error) {
	d.calls++
	d.args = append(d.args, append([]any(nil), args...))
	if len(d.errs) > 0 {
		err := d.errs[0]
		d.errs = d.errs[1:]
		if err != nil {
			return pgconn.CommandTag{}, err
		}
	}
	if len(d.results) == 0 {
		return pgconn.NewCommandTag("DELETE 0"), nil
	}
	result := d.results[0]
	d.results = d.results[1:]
	return result, nil
}

func (d *purgeTestDatabase) Query(
	context.Context,
	string,
	...any,
) (pgx.Rows, error) {
	return nil, errors.New("unexpected query in retention test")
}

func (d *purgeTestDatabase) QueryRow(
	context.Context,
	string,
	...any,
) pgx.Row {
	return purgeFailingRow{err: errors.New("unexpected query row")}
}

func (d *purgeTestDatabase) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected transaction in retention test")
}

type purgeFailingRow struct {
	err error
}

func (row purgeFailingRow) Scan(...any) error {
	return row.err
}

func TestPurgeRetainedRecordsOnlyRunsEnabledWindows(t *testing.T) {
	database := &purgeTestDatabase{
		results: []pgconn.CommandTag{
			pgconn.NewCommandTag("DELETE 2"),
			pgconn.NewCommandTag("DELETE 3"),
			pgconn.NewCommandTag("DELETE 1"),
			pgconn.NewCommandTag("DELETE 4"),
		},
	}
	repository := &PostgresRepository{pool: database}
	imageBefore := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	conversationBefore := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)

	purge, err := repository.PurgeRetainedRecords(
		context.Background(),
		imageBefore,
		conversationBefore,
	)
	if err != nil {
		t.Fatalf("purge retained records: %v", err)
	}
	if purge.VisionResults != 2 ||
		purge.VisionTasks != 3 ||
		purge.VideoCalls != 1 ||
		purge.DeviceVideoCalls != 4 ||
		purge.Total() != 10 {
		t.Fatalf("unexpected purge result: %+v", purge)
	}
	if database.calls != 4 {
		t.Fatalf("delete calls = %d, want 4", database.calls)
	}
	if len(database.args) != 4 ||
		!database.args[0][0].(time.Time).Equal(imageBefore) ||
		!database.args[1][0].(time.Time).Equal(imageBefore) ||
		!database.args[2][0].(time.Time).Equal(conversationBefore) ||
		len(database.args[3]) != 0 {
		t.Fatalf("unexpected retention arguments: %#v", database.args)
	}
}

func TestPurgeRetainedRecordsSkipsDisabledWindow(t *testing.T) {
	database := &purgeTestDatabase{
		results: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1")},
	}
	repository := &PostgresRepository{pool: database}
	imageBefore := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	purge, err := repository.PurgeRetainedRecords(
		context.Background(),
		imageBefore,
		time.Time{},
	)
	if err != nil {
		t.Fatalf("purge retained records: %v", err)
	}
	if database.calls != 2 || purge.Total() != 1 {
		t.Fatalf("calls=%d purge=%+v, want only image statements", database.calls, purge)
	}
	for _, args := range database.args {
		if len(args) != 1 || !args[0].(time.Time).Equal(imageBefore) {
			t.Fatalf("disabled conversation window received args: %#v", database.args)
		}
	}
}

func TestPurgeRetainedRecordsFailsVisibleWithoutContinuing(t *testing.T) {
	database := &purgeTestDatabase{
		results: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1")},
		errs:    []error{nil, errors.New("database unavailable")},
	}
	repository := &PostgresRepository{pool: database}
	_, err := repository.PurgeRetainedRecords(
		context.Background(),
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("expected the failed delete to be reported")
	}
	if database.calls != 2 {
		t.Fatalf("statements after failure = %d, want 2", database.calls)
	}
}
