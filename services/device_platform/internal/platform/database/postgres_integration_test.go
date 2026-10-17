package database

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMigrateIsRepeatableOnPostgres(t *testing.T) {
	dsn := os.Getenv("DEVICE_PLATFORM_DATABASE_DSN")
	if dsn == "" {
		t.Skip("DEVICE_PLATFORM_DATABASE_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open and migrate database: %v", err)
	}
	defer store.Close(context.Background())

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("repeat migrations: %v", err)
	}
	var applied int
	if err := store.Pool().QueryRow(
		ctx,
		"SELECT COUNT(*) FROM schema_migrations",
	).Scan(&applied); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if applied == 0 {
		t.Fatal("expected the migration ledger to contain applied versions")
	}
}
