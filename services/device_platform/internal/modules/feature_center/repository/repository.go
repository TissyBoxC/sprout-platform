// Package repository persists feature-center configuration overlays.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines the persistence contract used by the feature service.
type Repository interface {
	Get(ctx context.Context, featureID string) (*domain.FeatureConfig, error)
	List(ctx context.Context) ([]domain.FeatureConfig, error)
	ListHealth(ctx context.Context) ([]domain.HealthSnapshot, error)
	GetHealth(ctx context.Context, featureID string) (*domain.HealthSnapshot, error)
	SaveHealth(ctx context.Context, snapshot *domain.HealthSnapshot) error
	Save(
		ctx context.Context,
		featureID string,
		values map[string]any,
		actorID string,
		expectedVersion int64,
	) (*domain.FeatureConfig, error)
}

// ListHealth returns every durable health snapshot ordered by feature id.
func (r *PostgresRepository) ListHealth(
	ctx context.Context,
) ([]domain.HealthSnapshot, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			feature_id,
			status,
			checked_at,
			message,
			latency_ms,
			trigger
		FROM platform_feature_health_snapshots
		ORDER BY feature_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list feature health snapshots: %w", err)
	}
	defer rows.Close()

	snapshots := make([]domain.HealthSnapshot, 0)
	for rows.Next() {
		snapshot, err := scanHealth(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, *snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feature health snapshots: %w", err)
	}
	return snapshots, nil
}

// GetHealth returns the latest durable snapshot for one feature.
func (r *PostgresRepository) GetHealth(
	ctx context.Context,
	featureID string,
) (*domain.HealthSnapshot, error) {
	snapshot, err := scanHealth(r.pool.QueryRow(ctx, `
		SELECT
			feature_id,
			status,
			checked_at,
			message,
			latency_ms,
			trigger
		FROM platform_feature_health_snapshots
		WHERE feature_id = $1
	`, featureID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrFeatureConfigNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load feature health snapshot: %w", err)
	}
	return snapshot, nil
}

// SaveHealth atomically replaces the last-known result only when the new
// observation is at least as recent as the stored result.
func (r *PostgresRepository) SaveHealth(
	ctx context.Context,
	snapshot *domain.HealthSnapshot,
) error {
	if snapshot == nil {
		return errors.New("feature health snapshot is required")
	}
	if !snapshot.Status.Valid() {
		return domain.ErrHealthCheckFailed
	}
	return saveHealthRows(ctx, r.pool, snapshot)
}

type healthExecer interface {
	Exec(
		ctx context.Context,
		sql string,
		arguments ...any,
	) (pgconn.CommandTag, error)
}

func saveHealthRows(
	ctx context.Context,
	execer healthExecer,
	snapshot *domain.HealthSnapshot,
) error {
	if execer == nil {
		return errors.New("feature health database is unavailable")
	}
	tag, err := execer.Exec(ctx, `
		INSERT INTO platform_feature_health_snapshots (
			feature_id,
			status,
			checked_at,
			message,
			latency_ms,
			trigger,
			updated_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			NOW()
		)
		ON CONFLICT (feature_id) DO UPDATE
		SET status = EXCLUDED.status,
		    checked_at = EXCLUDED.checked_at,
		    message = EXCLUDED.message,
		    latency_ms = EXCLUDED.latency_ms,
		    trigger = EXCLUDED.trigger,
		    updated_at = NOW()
		WHERE platform_feature_health_snapshots.checked_at <=
		      EXCLUDED.checked_at
	`,
		snapshot.FeatureID,
		snapshot.Status,
		snapshot.CheckedAt.UTC(),
		snapshot.Message,
		snapshot.LatencyMS,
		snapshot.Trigger,
	)
	if err != nil {
		return fmt.Errorf("save feature health snapshot: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf(
			"%w: feature=%s checked_at=%s",
			domain.ErrHealthSnapshotStale,
			snapshot.FeatureID,
			snapshot.CheckedAt.UTC().Format(time.RFC3339Nano),
		)
	}
	return nil
}

// schedulerLeaseLockID is a stable, application-owned PostgreSQL advisory
// lock key. It coordinates feature health scheduling across service replicas
// without coupling the scheduler to Redis or to process-local state.
const schedulerLeaseLockID int64 = 0x5350524f55544643 // "SPROUTFC"

// SchedulerLease runs work under a cross-replica leader lock.
//
// Implementations must execute the callback only while the shared lock is
// held. Returning domain.ErrSchedulerLeaseUnavailable means another replica
// currently owns the lease and the work was intentionally skipped.
type SchedulerLease interface {
	WithLeaderLease(
		ctx context.Context,
		work func(context.Context) error,
	) error
}

var _ SchedulerLease = (*PostgresRepository)(nil)

// SchedulerLeaseReleaseError identifies a callback outcome joined with an
// explicit advisory-lock release failure. Callers can preserve the callback
// error while still exposing the release failure.
type SchedulerLeaseReleaseError struct {
	Err error
}

func (err *SchedulerLeaseReleaseError) Error() string {
	if err == nil || err.Err == nil {
		return domain.ErrSchedulerLeaseReleaseFailed.Error()
	}
	return err.Err.Error()
}

func (err *SchedulerLeaseReleaseError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func (err *SchedulerLeaseReleaseError) Is(target error) bool {
	return target == domain.ErrSchedulerLeaseReleaseFailed
}

// leaderLeaseSession is the narrow PostgreSQL session surface used by the
// scheduler lease. Keeping it separate from pgxpool.Conn makes the release and
// failure paths deterministic to test without changing application wiring.
type leaderLeaseSession interface {
	TryLock(ctx context.Context, lockID int64) (bool, error)
	Unlock(ctx context.Context, lockID int64) (bool, error)
	Release()
	Destroy(ctx context.Context) error
	Discard(ctx context.Context) error
}

type leaderLeaseAcquirer interface {
	Acquire(ctx context.Context) (leaderLeaseSession, error)
}

type poolLeaderLeaseAcquirer struct {
	pool *pgxpool.Pool
}

func (acquirer poolLeaderLeaseAcquirer) Acquire(
	ctx context.Context,
) (leaderLeaseSession, error) {
	conn, err := acquirer.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return newPoolLeaderLeaseSession(conn), nil
}

type poolLeaderLeaseSession struct {
	conn      *pgxpool.Conn
	discardMu *sync.Mutex
	discarded bool
	released  bool
}

func (session *poolLeaderLeaseSession) TryLock(
	ctx context.Context,
	lockID int64,
) (bool, error) {
	var acquired bool
	err := session.conn.QueryRow(
		ctx,
		"SELECT pg_try_advisory_lock($1)",
		lockID,
	).Scan(&acquired)
	return acquired, err
}

func (session *poolLeaderLeaseSession) Unlock(
	ctx context.Context,
	lockID int64,
) (bool, error) {
	var unlocked bool
	err := session.conn.QueryRow(
		ctx,
		"SELECT pg_advisory_unlock($1)",
		lockID,
	).Scan(&unlocked)
	return unlocked, err
}

// Destroy ends the checked-out connection lifecycle without returning it to
// the pool. This is required after an unlock failure because PostgreSQL
// releases session advisory locks only when the underlying socket closes.
func (session *poolLeaderLeaseSession) Destroy(ctx context.Context) error {
	session.discardMu.Lock()
	defer session.discardMu.Unlock()
	if session.discarded || session.released {
		return nil
	}
	// Mark the session first. Even a failed socket close must never lead to a
	// second Release that could return a lock-holding connection to the pool.
	session.discarded = true
	conn := session.conn.Hijack()
	if err := conn.Close(ctx); err != nil {
		return err
	}
	return nil
}

// Discard is the non-lock cleanup path. A pgxpool release is safe after a
// failed TryLock because no advisory lock was acquired.
func (session *poolLeaderLeaseSession) Discard(ctx context.Context) error {
	session.discardMu.Lock()
	defer session.discardMu.Unlock()
	if session.discarded || session.released {
		return nil
	}
	session.discarded = true
	session.conn.Release()
	return nil
}

func (session *poolLeaderLeaseSession) Release() {
	session.discardMu.Lock()
	defer session.discardMu.Unlock()
	if session.discarded || session.released {
		return
	}
	session.released = true
	session.conn.Release()
}

func newPoolLeaderLeaseSession(conn *pgxpool.Conn) leaderLeaseSession {
	return &poolLeaderLeaseSession{
		conn:      conn,
		discardMu: &sync.Mutex{},
	}
}

// WithLeaderLease executes one scheduled cycle while holding a PostgreSQL
// session advisory lock.
//
// The lock is bound to the acquired connection rather than pgxpool.Exec, so
// another replica cannot accidentally unlock or release it. Advisory locks are
// released automatically if the connection is lost, which gives the scheduler
// a crash-safe lease without a separate heartbeat table.
func (r *PostgresRepository) WithLeaderLease(
	ctx context.Context,
	work func(context.Context) error,
) error {
	if r == nil || r.pool == nil {
		return errors.New("feature scheduler database is unavailable")
	}
	if work == nil {
		return errors.New("feature scheduler work is required")
	}
	return withLeaderLease(
		ctx,
		poolLeaderLeaseAcquirer{pool: r.pool},
		work,
	)
}

func withLeaderLease(
	ctx context.Context,
	acquirer leaderLeaseAcquirer,
	work func(context.Context) error,
) error {
	if acquirer == nil {
		return errors.New("feature scheduler lease acquirer is required")
	}
	if work == nil {
		return errors.New("feature scheduler work is required")
	}
	session, err := acquirer.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire feature scheduler lease connection: %w", err)
	}

	acquired, err := tryLeaderLease(ctx, session)
	if err != nil {
		_ = discardLeaderLease(session)
		return err
	}
	if !acquired {
		session.Release()
		return domain.ErrSchedulerLeaseUnavailable
	}
	released := false
	// This guard covers callback panics and unexpected returns. On panic the
	// lease is released or the database session is destroyed before the panic
	// continues, preventing a poisoned connection from returning to the pool.
	defer func() {
		if released {
			return
		}
		releaseCtx, cancel := context.WithTimeout(
			context.Background(),
			schedulerLeaseTimeout,
		)
		defer cancel()
		if err := releaseLeaderLease(releaseCtx, session); err != nil {
			_ = destroyLeaderLeaseSession(session)
			return
		}
		session.Release()
	}()

	workErr := work(ctx)
	releaseErr := releaseLeaderLease(ctx, session)
	if releaseErr != nil {
		discardErr := destroyLeaderLeaseSession(session)
		released = true
		return &SchedulerLeaseReleaseError{
			Err: errors.Join(
				releaseErr,
				discardErr,
				workErr,
			),
		}
	}
	released = true
	session.Release()
	return workErr
}

const schedulerLeaseTimeout = 2 * time.Second

func tryLeaderLease(
	ctx context.Context,
	session leaderLeaseSession,
) (bool, error) {
	acquired, err := session.TryLock(ctx, schedulerLeaseLockID)
	if err != nil {
		return false, fmt.Errorf("acquire feature scheduler lease: %w", err)
	}
	return acquired, nil
}

func releaseLeaderLease(
	ctx context.Context,
	session leaderLeaseSession,
) error {
	unlocked, err := session.Unlock(ctx, schedulerLeaseLockID)
	if err == nil && unlocked {
		return nil
	}

	// A cancelled callback or request context must not strand the advisory lock
	// until the pooled session is recycled.
	fallbackCtx, cancel := context.WithTimeout(
		context.Background(),
		schedulerLeaseTimeout,
	)
	defer cancel()
	fallbackUnlocked, fallbackErr := session.Unlock(
		fallbackCtx,
		schedulerLeaseLockID,
	)
	if fallbackErr == nil && fallbackUnlocked {
		return nil
	}
	return errors.Join(
		domain.ErrSchedulerLeaseReleaseFailed,
		err,
		fallbackErr,
	)
}

func discardLeaderLease(session leaderLeaseSession) error {
	if session == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		schedulerLeaseTimeout,
	)
	defer cancel()
	if err := session.Discard(ctx); err != nil {
		return fmt.Errorf("discard feature scheduler lease session: %w", err)
	}
	return nil
}

func destroyLeaderLeaseSession(session leaderLeaseSession) error {
	if session == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		schedulerLeaseTimeout,
	)
	defer cancel()
	if err := session.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy feature scheduler lease session: %w", err)
	}
	return nil
}

// PostgresRepository stores feature configuration overlays in PostgreSQL.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a PostgreSQL feature configuration repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Get returns one persisted configuration overlay.
func (r *PostgresRepository) Get(
	ctx context.Context,
	featureID string,
) (*domain.FeatureConfig, error) {
	config, err := scanConfig(r.pool.QueryRow(ctx, `
		SELECT
			feature_id,
			"values",
			version,
			COALESCE(updated_by::text, ''),
			updated_at
		FROM platform_feature_configs
		WHERE feature_id = $1
	`, featureID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrFeatureConfigNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load feature configuration: %w", err)
	}
	return config, nil
}

// List returns every persisted configuration overlay ordered by feature id.
func (r *PostgresRepository) List(
	ctx context.Context,
) ([]domain.FeatureConfig, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			feature_id,
			"values",
			version,
			COALESCE(updated_by::text, ''),
			updated_at
		FROM platform_feature_configs
		ORDER BY feature_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list feature configurations: %w", err)
	}
	defer rows.Close()

	configs := make([]domain.FeatureConfig, 0)
	for rows.Next() {
		config, err := scanConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, *config)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feature configurations: %w", err)
	}
	return configs, nil
}

// Save writes one configuration overlay using optimistic concurrency.
//
// An expected version of zero inserts the first row. Any existing row with a
// different version is rejected with VersionConflictError.
func (r *PostgresRepository) Save(
	ctx context.Context,
	featureID string,
	values map[string]any,
	actorID string,
	expectedVersion int64,
) (*domain.FeatureConfig, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode feature configuration: %w", err)
	}
	config, err := scanConfig(r.pool.QueryRow(ctx, `
		INSERT INTO platform_feature_configs (
			feature_id,
			"values",
			version,
			updated_by,
			updated_at
		)
		SELECT
			$1,
			$2::jsonb,
			1,
			NULLIF($3, '')::uuid,
			NOW()
		WHERE $4 = 0
		ON CONFLICT (feature_id) DO UPDATE
		SET "values" = EXCLUDED."values",
		    version = platform_feature_configs.version + 1,
		    updated_by = EXCLUDED.updated_by,
		    updated_at = NOW()
		WHERE platform_feature_configs.version = $4
		RETURNING
			feature_id,
			"values",
			version,
			COALESCE(updated_by::text, ''),
			updated_at
	`, featureID, encoded, actorID, expectedVersion))
	if err == nil {
		return config, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("save feature configuration: %w", err)
	}

	currentVersion := int64(0)
	current, currentErr := r.Get(ctx, featureID)
	switch {
	case currentErr == nil:
		currentVersion = current.Version
	case errors.Is(currentErr, domain.ErrFeatureConfigNotFound):
	default:
		return nil, currentErr
	}
	return nil, &domain.VersionConflictError{
		ExpectedVersion: expectedVersion,
		CurrentVersion:  currentVersion,
	}
}

type configScanner interface {
	Scan(destinations ...any) error
}

func scanConfig(row configScanner) (*domain.FeatureConfig, error) {
	var config domain.FeatureConfig
	var raw []byte
	if err := row.Scan(
		&config.FeatureID,
		&raw,
		&config.Version,
		&config.UpdatedBy,
		&config.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &config.Values); err != nil {
		return nil, fmt.Errorf("decode feature configuration: %w", err)
	}
	if config.Values == nil {
		config.Values = map[string]any{}
	}
	return &config, nil
}

func scanHealth(row configScanner) (*domain.HealthSnapshot, error) {
	var snapshot domain.HealthSnapshot
	if err := row.Scan(
		&snapshot.FeatureID,
		&snapshot.Status,
		&snapshot.CheckedAt,
		&snapshot.Message,
		&snapshot.LatencyMS,
		&snapshot.Trigger,
	); err != nil {
		return nil, err
	}
	snapshot.CheckedAt = snapshot.CheckedAt.UTC()
	return &snapshot, nil
}

var _ Repository = (*PostgresRepository)(nil)
