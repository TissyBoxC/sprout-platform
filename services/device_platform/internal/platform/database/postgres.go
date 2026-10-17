// Package database owns PostgreSQL connectivity.
package database

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store owns the PostgreSQL connection pool shared by database-backed
// repositories. Callers must not close the pool themselves.
type Store struct {
	pool *pgxpool.Pool
}

// Open creates and verifies a PostgreSQL connection pool.
//
// The DSN must be supplied by secret-backed runtime configuration. Open never
// falls back to a shared database account.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		return nil, fmt.Errorf("database DSN is required")
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database DSN: %w", err)
	}
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 15 * time.Minute
	poolConfig.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{pool: pool}
	if err := store.Migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return store, nil
}

// Pool returns the initialized PostgreSQL pool.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// Close releases database resources.
func (s *Store) Close(_ context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	s.pool.Close()
	s.pool = nil
	return nil
}

// Migrate applies ordered, idempotent schema migrations owned by
// device_platform. The advisory lock prevents two replicas from racing during
// a rollout.
func (s *Store) Migrate(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("database pool is not initialized")
	}

	connection, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Release()

	const migrationLockID int64 = 0x5350524f55544d31 // "SPROUTM1"
	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = connection.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	if _, err := connection.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})
	if err := validateMigrationVersions(entries); err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		applied, err := migrationApplied(ctx, connection, entry.Name())
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		contents, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		transaction, err := connection.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", entry.Name(), err)
		}
		if _, err := transaction.Exec(ctx, string(contents)); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if _, err := transaction.Exec(
			ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			entry.Name(),
		); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func validateMigrationVersions(entries []fs.DirEntry) error {
	versions := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		separator := strings.IndexByte(entry.Name(), '_')
		if separator < 1 {
			return fmt.Errorf("migration %s must start with a numeric version", entry.Name())
		}
		version := entry.Name()[:separator]
		if existing, ok := versions[version]; ok {
			return fmt.Errorf(
				"migration version %s is used by both %s and %s",
				version,
				existing,
				entry.Name(),
			)
		}
		versions[version] = entry.Name()
	}
	return nil
}

func migrationApplied(
	ctx context.Context,
	connection *pgxpool.Conn,
	version string,
) (bool, error) {
	var exists bool
	if err := connection.QueryRow(
		ctx,
		"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)",
		version,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	return exists, nil
}
