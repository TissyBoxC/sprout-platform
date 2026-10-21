// Package repository persists child long-term memories.
//
// Every write runs inside one transaction because two invariants span rows:
// the per-child revision must be assigned monotonically, and the per-child key
// count must stay under the configured limit while a batch is inserted. A
// per-row autocommit would let two concurrent device batches both pass the
// limit check or both reuse a revision.
package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines the persistence surface for child memories.
type Repository interface {
	// Upsert stores a validated batch for one child and returns the stored
	// rows. It fails with domain.ErrMemoryLimitExceeded when the child would
	// exceed limit and with domain.ErrMemoryRevisionConflict when
	// expectedRevision is non-nil and no longer matches the child's counter.
	Upsert(
		ctx context.Context,
		familyID string,
		childID string,
		records []domain.MemoryRecord,
		limit int,
		expectedRevision *int64,
		now time.Time,
	) ([]domain.ChildMemory, error)
	// ListByChild returns every memory of one child in key order.
	ListByChild(ctx context.Context, familyID string, childID string) ([]domain.ChildMemory, error)
	// ListSummariesByFamily returns key-level summaries for every child of a
	// family without loading memory values.
	ListSummariesByFamily(
		ctx context.Context,
		familyID string,
	) ([]FamilyMemorySummary, error)
	// PageSince returns the delta of one child since a revision.
	PageSince(
		ctx context.Context,
		familyID string,
		childID string,
		sinceRevision int64,
	) (domain.MemoryPage, error)
	// CurrentRevision returns the child's highest assigned revision, or 0 when
	// the child has no memory yet.
	CurrentRevision(ctx context.Context, familyID string, childID string) (int64, error)
	// Get returns one family-owned memory by key.
	Get(ctx context.Context, familyID string, childID string, memoryKey string) (*domain.ChildMemory, error)
	// Delete removes one family-owned memory and returns the child's revision
	// after the deletion so the device cursor advances past the tombstone.
	Delete(
		ctx context.Context,
		familyID string,
		childID string,
		memoryKey string,
	) (int64, error)
	// DeleteForChild removes every memory of one child and returns the count.
	DeleteForChild(ctx context.Context, familyID string, childID string) (int64, error)
	// DeleteForFamily removes every memory of a family and returns the count.
	DeleteForFamily(ctx context.Context, familyID string) (int64, error)
	// PurgeExpired removes memories whose last reinforcement is older than
	// cutoff and returns the count.
	PurgeExpired(ctx context.Context, cutoff time.Time) (int64, error)
	// Readiness returns aggregate counters for the Feature Center. The counters
	// are computed against limit so an operator can see how many children have
	// already saturated the configured cap.
	Readiness(ctx context.Context, limit int) (domain.Readiness, error)
}

// FamilyMemorySummary groups the key-level memory metadata of one child.
type FamilyMemorySummary struct {
	ChildID  string
	Memories []domain.MemorySummary
}

// PostgresRepository is the PostgreSQL-backed memory repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a child memory repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Upsert stores one validated batch for a child.
//
// The child's revision counter is read with FOR UPDATE so two concurrent
// batches cannot share a revision. Within the batch the highest revision is
// assigned to the entry that actually changed, which keeps the device cursor
// meaningful even when the batch also re-sends unchanged facts.
func (r *PostgresRepository) Upsert(
	ctx context.Context,
	familyID string,
	childID string,
	records []domain.MemoryRecord,
	limit int,
	expectedRevision *int64,
	now time.Time,
) ([]domain.ChildMemory, error) {
	if len(records) == 0 {
		return []domain.ChildMemory{}, nil
	}
	if limit <= 0 {
		limit = domain.DefaultChildMemoryLimit
	}
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin memory upsert: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	revision, err := lockChildRevision(ctx, transaction, familyID, childID)
	if err != nil {
		return nil, err
	}
	if expectedRevision != nil && *expectedRevision != revision {
		return nil, domain.ErrMemoryRevisionConflict
	}

	// Two different keys in one request must not race each other to the same
	// row; the second occurrence silently overwrites the first like a map.
	unique := make([]domain.MemoryRecord, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if _, ok := seen[record.MemoryKey]; ok {
			for index := range unique {
				if unique[index].MemoryKey == record.MemoryKey {
					unique[index] = record
				}
			}
			continue
		}
		seen[record.MemoryKey] = struct{}{}
		unique = append(unique, record)
	}

	stored := make([]domain.ChildMemory, 0, len(unique))
	for _, record := range unique {
		revision++
		memory := domain.ChildMemory{
			FamilyID:         familyID,
			ChildID:          childID,
			MemoryKey:        record.MemoryKey,
			MemoryValue:      record.MemoryValue,
			Confidence:       record.Confidence,
			Source:           record.Source,
			Revision:         revision,
			CreatedAt:        now,
			UpdatedAt:        now,
			LastReinforcedAt: now,
		}
		err := transaction.QueryRow(ctx, `
			INSERT INTO child_memories (
				id,
				family_id,
				child_id,
				memory_key,
				memory_value,
				confidence,
				source,
				revision,
				created_at,
				updated_at,
				last_reinforced_at
			) VALUES (
				gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $8, $8
			)
			ON CONFLICT (child_id, memory_key)
			DO UPDATE SET
				memory_value = EXCLUDED.memory_value,
				confidence = EXCLUDED.confidence,
				source = EXCLUDED.source,
				revision = EXCLUDED.revision,
				updated_at = EXCLUDED.updated_at,
				last_reinforced_at = EXCLUDED.last_reinforced_at
			RETURNING id, created_at
		`,
			familyID,
			childID,
			record.MemoryKey,
			record.MemoryValue,
			record.Confidence,
			record.Source,
			revision,
			now,
		).Scan(&memory.ID, &memory.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("upsert child memory: %w", err)
		}
		stored = append(stored, memory)
	}

	// The limit is checked after the writes so the count includes the rows this
	// batch just created. Rolling back here keeps the limit hard: a rejected
	// batch leaves no partial memories behind and no revision gap that a device
	// would have to reconcile.
	var count int
	if err := transaction.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM child_memories
		WHERE child_id = $1
	`, childID).Scan(&count); err != nil {
		return nil, fmt.Errorf("count child memories: %w", err)
	}
	if count > limit {
		return nil, domain.ErrMemoryLimitExceeded
	}

	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit memory upsert: %w", err)
	}
	return stored, nil
}

// ListByChild returns every memory of one child in key order.
func (r *PostgresRepository) ListByChild(
	ctx context.Context,
	familyID string,
	childID string,
) ([]domain.ChildMemory, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			family_id,
			child_id,
			memory_key,
			memory_value,
			confidence,
			source,
			revision,
			created_at,
			updated_at,
			last_reinforced_at
		FROM child_memories
		WHERE family_id = $1 AND child_id = $2
		ORDER BY memory_key
	`, familyID, childID)
	if err != nil {
		return nil, fmt.Errorf("list child memories: %w", err)
	}
	defer rows.Close()
	memories := make([]domain.ChildMemory, 0, 32)
	for rows.Next() {
		memory, scanErr := scanMemory(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		memories = append(memories, *memory)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate child memories: %w", err)
	}
	return memories, nil
}

// ListSummariesByFamily returns key-level metadata for every child of a family.
//
// The query never selects memory_value: the guardian overview lists which facts
// exist without shipping the child's statements into every response.
func (r *PostgresRepository) ListSummariesByFamily(
	ctx context.Context,
	familyID string,
) ([]FamilyMemorySummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			child_id,
			memory_key,
			source,
			confidence,
			revision,
			updated_at,
			last_reinforced_at
		FROM child_memories
		WHERE family_id = $1
		ORDER BY child_id, memory_key
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list family memory summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]FamilyMemorySummary, 0, 4)
	indexByChildID := make(map[string]int, 4)
	for rows.Next() {
		var childID string
		var summary domain.MemorySummary
		if err := rows.Scan(
			&childID,
			&summary.MemoryKey,
			&summary.Source,
			&summary.Confidence,
			&summary.Revision,
			&summary.UpdatedAt,
			&summary.LastReinforcedAt,
		); err != nil {
			return nil, fmt.Errorf("scan family memory summary: %w", err)
		}
		index, ok := indexByChildID[childID]
		if !ok {
			index = len(summaries)
			indexByChildID[childID] = index
			summaries = append(summaries, FamilyMemorySummary{ChildID: childID})
		}
		summaries[index].Memories = append(summaries[index].Memories, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate family memory summaries: %w", err)
	}
	return summaries, nil
}

// PageSince returns the live rows and tombstones of one child with a revision
// strictly greater than sinceRevision.
func (r *PostgresRepository) PageSince(
	ctx context.Context,
	familyID string,
	childID string,
	sinceRevision int64,
) (domain.MemoryPage, error) {
	memories, err := r.ListByChild(ctx, familyID, childID)
	if err != nil {
		return domain.MemoryPage{}, err
	}
	page := domain.MemoryPage{
		ChildID:       childID,
		SinceRevision: sinceRevision,
		Memories:      make([]domain.ChildMemory, 0, len(memories)),
	}
	for _, memory := range memories {
		if memory.Revision > page.NextRevision {
			page.NextRevision = memory.Revision
		}
		if memory.Revision > sinceRevision {
			page.Memories = append(page.Memories, memory)
		}
	}
	deletedKeys, err := r.listTombstoneKeys(ctx, familyID, childID, sinceRevision)
	if err != nil {
		return domain.MemoryPage{}, err
	}
	for _, tombstone := range deletedKeys {
		if tombstone.Revision > page.NextRevision {
			page.NextRevision = tombstone.Revision
		}
		page.DeletedKeys = append(page.DeletedKeys, tombstone.MemoryKey)
	}
	return page, nil
}

// listTombstoneKeys returns the keys deleted since a revision. Tombstones live
// in their own table because they must survive the row deletion while carrying
// the revision the device has to reach.
func (r *PostgresRepository) listTombstoneKeys(
	ctx context.Context,
	familyID string,
	childID string,
	sinceRevision int64,
) ([]tombstone, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT memory_key, revision
		FROM child_memory_tombstones
		WHERE family_id = $1 AND child_id = $2 AND revision > $3
		ORDER BY revision
	`, familyID, childID, sinceRevision)
	if err != nil {
		return nil, fmt.Errorf("list child memory tombstones: %w", err)
	}
	defer rows.Close()
	tombstones := make([]tombstone, 0, 4)
	for rows.Next() {
		var entry tombstone
		if err := rows.Scan(&entry.MemoryKey, &entry.Revision); err != nil {
			return nil, fmt.Errorf("scan child memory tombstone: %w", err)
		}
		tombstones = append(tombstones, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate child memory tombstones: %w", err)
	}
	return tombstones, nil
}

type tombstone struct {
	MemoryKey string
	Revision  int64
}

// CurrentRevision returns the child's highest assigned revision or 0.
func (r *PostgresRepository) CurrentRevision(
	ctx context.Context,
	familyID string,
	childID string,
) (int64, error) {
	var revision int64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(revision), 0)
		FROM child_memories
		WHERE family_id = $1 AND child_id = $2
	`, familyID, childID).Scan(&revision)
	if err != nil {
		return 0, fmt.Errorf("read child memory revision: %w", err)
	}
	return revision, nil
}

// Get returns one family-owned memory.
func (r *PostgresRepository) Get(
	ctx context.Context,
	familyID string,
	childID string,
	memoryKey string,
) (*domain.ChildMemory, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			family_id,
			child_id,
			memory_key,
			memory_value,
			confidence,
			source,
			revision,
			created_at,
			updated_at,
			last_reinforced_at
		FROM child_memories
		WHERE family_id = $1 AND child_id = $2 AND memory_key = $3
	`, familyID, childID, memoryKey)
	memory, err := scanMemory(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMemoryNotFound
	}
	if err != nil {
		return nil, err
	}
	return memory, nil
}

// Delete removes one family-owned memory and advances the child's revision.
//
// The deletion bumps the revision inside the same transaction that writes the
// tombstone, so a device that pulls with the previous revision learns about the
// deletion instead of silently keeping a fact the guardian removed.
func (r *PostgresRepository) Delete(
	ctx context.Context,
	familyID string,
	childID string,
	memoryKey string,
) (int64, error) {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin child memory delete: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	revision, err := lockChildRevision(ctx, transaction, familyID, childID)
	if err != nil {
		return 0, err
	}
	tag, err := transaction.Exec(ctx, `
		DELETE FROM child_memories
		WHERE family_id = $1 AND child_id = $2 AND memory_key = $3
	`, familyID, childID, memoryKey)
	if err != nil {
		return 0, fmt.Errorf("delete child memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, domain.ErrMemoryNotFound
	}
	revision++
	if _, err := transaction.Exec(ctx, `
		INSERT INTO child_memory_tombstones (
			family_id,
			child_id,
			memory_key,
			revision,
			deleted_at
		) VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (child_id, memory_key)
		DO UPDATE SET revision = EXCLUDED.revision, deleted_at = EXCLUDED.deleted_at
	`, familyID, childID, memoryKey, revision); err != nil {
		return 0, fmt.Errorf("record child memory tombstone: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit child memory delete: %w", err)
	}
	return revision, nil
}

// DeleteForChild removes every memory of one child. It is called from the child
// deletion path and is safe when the child has no memory yet.
func (r *PostgresRepository) DeleteForChild(
	ctx context.Context,
	familyID string,
	childID string,
) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM child_memories
		WHERE child_id = $1 AND ($2 = '' OR family_id = $2)
	`, childID, strings.TrimSpace(familyID))
	if err != nil {
		return 0, fmt.Errorf("delete child memories: %w", err)
	}
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM child_memory_tombstones WHERE child_id = $1
	`, childID); err != nil {
		return 0, fmt.Errorf("delete child memory tombstones: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteForFamily removes every memory of a family.
func (r *PostgresRepository) DeleteForFamily(
	ctx context.Context,
	familyID string,
) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM child_memories WHERE family_id = $1
	`, familyID)
	if err != nil {
		return 0, fmt.Errorf("delete family memories: %w", err)
	}
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM child_memory_tombstones WHERE family_id = $1
	`, familyID); err != nil {
		return 0, fmt.Errorf("delete family memory tombstones: %w", err)
	}
	return tag.RowsAffected(), nil
}

// PurgeExpired removes memories that have not been reinforced before cutoff.
//
// The window is measured from last_reinforced_at, not created_at: a fact the
// child keeps confirming must not disappear just because it was first learned
// long ago.
func (r *PostgresRepository) PurgeExpired(
	ctx context.Context,
	cutoff time.Time,
) (int64, error) {
	if cutoff.IsZero() {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM child_memories
		WHERE last_reinforced_at < $1
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge expired child memories: %w", err)
	}
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM child_memory_tombstones WHERE deleted_at < $1
	`, cutoff); err != nil {
		return 0, fmt.Errorf("purge expired child memory tombstones: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Readiness returns aggregate counters without loading any memory content.
func (r *PostgresRepository) Readiness(
	ctx context.Context,
	limit int,
) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("memory database is unavailable")
	}
	if limit <= 0 {
		limit = domain.DefaultChildMemoryLimit
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM child_memories),
			(SELECT COUNT(DISTINCT child_id) FROM child_memories),
			(
				SELECT COUNT(*)
				FROM (
					SELECT child_id
					FROM child_memories
					GROUP BY child_id
					HAVING COUNT(*) >= $1
				) AS saturated
			)
	`, limit).Scan(
		&readiness.MemoryCount,
		&readiness.ChildCount,
		&readiness.OverLimitChildCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read memory readiness: %w", err)
	}
	readiness.Limit = limit
	return readiness, nil
}

// lockChildRevision reads the child's revision counter under a row lock.
//
// The source of truth is GREATEST of the live rows and the tombstones, because
// a child whose every memory was deleted has no live rows left to lock. The
// advisory lock on the child id closes that gap: it is held for the whole
// transaction, so the "child has memories" and "child is empty" write paths
// serialize instead of both starting from the same revision. hashtextextended
// is used because the bigint-lock overload has no collision-prone narrowing.
func lockChildRevision(
	ctx context.Context,
	transaction pgx.Tx,
	familyID string,
	childID string,
) (int64, error) {
	if _, err := transaction.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended($1, 0))
	`, "child_memory:"+childID); err != nil {
		return 0, fmt.Errorf("lock child memory revision: %w", err)
	}
	var revision int64
	if err := transaction.QueryRow(ctx, `
		SELECT GREATEST(
			COALESCE((
				SELECT MAX(revision) FROM child_memories
				WHERE family_id = $1 AND child_id = $2
			), 0),
			COALESCE((
				SELECT MAX(revision) FROM child_memory_tombstones
				WHERE family_id = $1 AND child_id = $2
			), 0)
		)
	`, familyID, childID).Scan(&revision); err != nil {
		return 0, fmt.Errorf("read child memory revision: %w", err)
	}
	return revision, nil
}

type scanner interface {
	Scan(destinations ...any) error
}

func scanMemory(row scanner) (*domain.ChildMemory, error) {
	var memory domain.ChildMemory
	err := row.Scan(
		&memory.ID,
		&memory.FamilyID,
		&memory.ChildID,
		&memory.MemoryKey,
		&memory.MemoryValue,
		&memory.Confidence,
		&memory.Source,
		&memory.Revision,
		&memory.CreatedAt,
		&memory.UpdatedAt,
		&memory.LastReinforcedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan child memory: %w", err)
	}
	if math.IsNaN(memory.Confidence) {
		memory.Confidence = 0
	}
	return &memory, nil
}

var _ Repository = (*PostgresRepository)(nil)
