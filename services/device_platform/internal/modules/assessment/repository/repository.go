// Package repository persists learning-assessment sessions and their answers.
//
// A row here is an envelope plus the answers the child gave. It never holds
// audio or a transcript: target_text is the lesson content that was asked for,
// and the recognized speech belongs to the English practice module, which has
// its own retention rules.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/domain"
)

var (
	// ErrSessionNotFound means no session matches the identifier within the
	// caller's scope.
	ErrSessionNotFound = errors.New("assessment session not found")
	// ErrSessionConflict means the session changed underneath the caller: it
	// finished, expired, or another answer already took the position.
	ErrSessionConflict = errors.New("assessment session conflict")
	// ErrAlreadyActive means the family or child already has a live session,
	// which the partial unique index refuses.
	ErrAlreadyActive = errors.New("assessment session already active")
)

// Repository stores assessment sessions.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds a repository over the shared pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const sessionColumns = `id::text, family_id::text, COALESCE(child_id::text, ''),
	lesson_id, state, difficulty, item_index, correct_count, answered_count,
	started_at, expires_at, finished_at, finish_reason`

// Create stores a new active session.
//
// The partial unique index turns a second live session in the same scope into a
// unique violation, reported as ErrAlreadyActive so callers never inspect
// PostgreSQL error codes.
func (repository *Repository) Create(ctx context.Context, session domain.Session) error {
	_, err := repository.pool.Exec(ctx,
		`INSERT INTO assessment_sessions
			(id, family_id, child_id, lesson_id, state, difficulty, item_index,
			 correct_count, answered_count, started_at, expires_at)
		 VALUES ($1::uuid, $2::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8, $9, $10, $11)`,
		session.ID, session.FamilyID, session.ChildID, session.LessonID,
		string(session.State), session.Difficulty, session.ItemIndex,
		session.CorrectCount, session.AnsweredCount, session.StartedAt,
		session.ExpiresAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyActive
		}
		return fmt.Errorf("create assessment session: %w", err)
	}
	return nil
}

// Get returns one session owned by the family.
//
// The family predicate is part of the query rather than a check in Go, so a
// cross-family identifier can never load a row in the first place.
func (repository *Repository) Get(ctx context.Context, familyID, sessionID string) (domain.Session, error) {
	row := repository.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM assessment_sessions
		 WHERE id = $1::uuid AND family_id = $2::uuid`,
		sessionID, familyID,
	)
	return scanSession(row)
}

// GetForDevice returns one session for the device currently serving its child.
//
// Device identifiers are text in every binding table, so the join is textual and
// the parameter is passed as text too. A session with no child cannot be claimed
// by a device: without a child there is no device to show it to.
func (repository *Repository) GetForDevice(
	ctx context.Context,
	deviceID, sessionID string,
) (domain.Session, error) {
	row := repository.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM assessment_sessions
		 WHERE id = $1::uuid
		   AND child_id IS NOT NULL
		   AND EXISTS (
				SELECT 1 FROM device_child_bindings binding
				WHERE binding.child_id = assessment_sessions.child_id
				  AND binding.is_active
				  AND binding.device_id = $2
		   )`,
		sessionID, deviceID,
	)
	return scanSession(row)
}

// ListOpen returns every live session whose deadline already passed.
func (repository *Repository) ListExpired(ctx context.Context, now time.Time, limit int) ([]domain.Session, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT `+sessionColumns+` FROM assessment_sessions
		 WHERE state = 'active' AND expires_at <= $1
		 ORDER BY expires_at
		 LIMIT $2`,
		now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list expired assessment sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]domain.Session, 0, 8)
	for rows.Next() {
		session, scanErr := scanSession(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired assessment sessions: %w", err)
	}
	return sessions, nil
}

// History returns a family's past sessions, newest first.
func (repository *Repository) History(
	ctx context.Context,
	familyID, childID string,
	limit int,
) ([]domain.Session, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT `+sessionColumns+` FROM assessment_sessions
		 WHERE family_id = $1::uuid
		   AND ($2 = '' OR child_id = NULLIF($2, '')::uuid)
		 ORDER BY started_at DESC
		 LIMIT $3`,
		familyID, childID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list assessment history: %w", err)
	}
	defer rows.Close()

	sessions := make([]domain.Session, 0, limit)
	for rows.Next() {
		session, scanErr := scanSession(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate assessment history: %w", err)
	}
	return sessions, nil
}

// ListAnswers returns a session's answers in position order.
func (repository *Repository) ListAnswers(ctx context.Context, sessionID string) ([]domain.Answer, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT id::text, session_id::text, position, item_type, item_id,
			target_text, score, is_correct, answered_at
		 FROM assessment_answers
		 WHERE session_id = $1::uuid
		 ORDER BY position`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list assessment answers: %w", err)
	}
	defer rows.Close()

	answers := make([]domain.Answer, 0, 8)
	for rows.Next() {
		var answer domain.Answer
		if err := rows.Scan(
			&answer.ID, &answer.SessionID, &answer.Position, &answer.ItemType,
			&answer.ItemID, &answer.TargetText, &answer.Score,
			&answer.IsCorrect, &answer.AnsweredAt,
		); err != nil {
			return nil, fmt.Errorf("scan assessment answer: %w", err)
		}
		answers = append(answers, answer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate assessment answers: %w", err)
	}
	return answers, nil
}

// RecordAnswer stores one answer and the session counters in one transaction.
//
// The update is conditional on the exact counters the caller read, which is what
// makes a concurrent duplicate answer safe: the second writer matches no row and
// reports ErrSessionConflict instead of applying the difficulty twice.
func (repository *Repository) RecordAnswer(
	ctx context.Context,
	session domain.Session,
	answer domain.Answer,
	previousAnswered int,
) error {
	transaction, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin assessment answer: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	tag, err := transaction.Exec(ctx,
		`UPDATE assessment_sessions
		 SET difficulty = $2, item_index = $3, correct_count = $4,
		     answered_count = $5, state = $6, finished_at = $7,
		     finish_reason = $8, updated_at = NOW()
		 WHERE id = $1::uuid AND state = 'active' AND answered_count = $9`,
		session.ID, session.Difficulty, session.ItemIndex, session.CorrectCount,
		session.AnsweredCount, string(session.State), session.FinishedAt,
		session.FinishReason, previousAnswered,
	)
	if err != nil {
		return fmt.Errorf("update assessment session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionConflict
	}

	if _, err := transaction.Exec(ctx,
		`INSERT INTO assessment_answers
			(id, session_id, position, item_type, item_id, target_text,
			 score, is_correct, answered_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9)`,
		answer.ID, answer.SessionID, answer.Position, answer.ItemType,
		answer.ItemID, answer.TargetText, answer.Score, answer.IsCorrect,
		answer.AnsweredAt,
	); err != nil {
		if isUniqueViolation(err) {
			return ErrSessionConflict
		}
		return fmt.Errorf("insert assessment answer: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit assessment answer: %w", err)
	}
	return nil
}

// Transition writes the session only if it is still active with the counters the
// caller read. A no-op transition means another actor already closed it.
func (repository *Repository) Transition(
	ctx context.Context,
	session domain.Session,
	previousAnswered int,
) error {
	tag, err := repository.pool.Exec(ctx,
		`UPDATE assessment_sessions
		 SET state = $2, finished_at = $3, finish_reason = $4, updated_at = NOW()
		 WHERE id = $1::uuid AND state = 'active' AND answered_count = $5`,
		session.ID, string(session.State), session.FinishedAt,
		session.FinishReason, previousAnswered,
	)
	if err != nil {
		return fmt.Errorf("transition assessment session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionConflict
	}
	return nil
}

// CountActive reports how many sessions are live right now.
func (repository *Repository) CountActive(ctx context.Context) (int, error) {
	var count int
	err := repository.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM assessment_sessions WHERE state = 'active'`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active assessment sessions: %w", err)
	}
	return count, nil
}

// CountSessionsSince counts sessions a family started in a window.
func (repository *Repository) CountSessionsSince(
	ctx context.Context,
	familyID string,
	since time.Time,
) (int, error) {
	var count int
	err := repository.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM assessment_sessions
		 WHERE family_id = $1::uuid AND started_at >= $2`,
		familyID, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count assessment sessions: %w", err)
	}
	return count, nil
}

// CountStartedSince counts sessions started across every family in a window.
//
// It is the platform-wide activity number the feature center reports, so it is
// deliberately not family-scoped.
func (repository *Repository) CountStartedSince(ctx context.Context, since time.Time) (int, error) {
	var count int
	err := repository.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM assessment_sessions WHERE started_at >= $1`,
		since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count started assessment sessions: %w", err)
	}
	return count, nil
}

// ChildInFamily reports whether a child profile belongs to the family.
func (repository *Repository) ChildInFamily(ctx context.Context, familyID, childID string) (bool, error) {
	var exists bool
	err := repository.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM child_profiles
			WHERE id = $1::uuid AND family_id = $2::uuid
		 )`,
		childID, familyID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check assessment child: %w", err)
	}
	return exists, nil
}

// scanSession reads one session row.
func scanSession(row pgx.Row) (domain.Session, error) {
	var session domain.Session
	var state string
	err := row.Scan(
		&session.ID, &session.FamilyID, &session.ChildID, &session.LessonID,
		&state, &session.Difficulty, &session.ItemIndex, &session.CorrectCount,
		&session.AnsweredCount, &session.StartedAt, &session.ExpiresAt,
		&session.FinishedAt, &session.FinishReason,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Session{}, ErrSessionNotFound
		}
		return domain.Session{}, fmt.Errorf("scan assessment session: %w", err)
	}
	session.State = domain.SessionState(state)
	return session, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
