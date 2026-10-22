// Package repository persists care-view session envelopes.
//
// No frame is ever written here. The table holds who looked, at which device,
// for how long, and why it ended, and nothing that could reconstruct an image.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/domain"
)

var (
	// ErrSessionNotFound means no session matches the identifier.
	ErrSessionNotFound = errors.New("care view session not found")
	// ErrDeviceBusy means the device already serves an open session.
	ErrDeviceBusy = errors.New("care view device already has an open session")
)

// Repository stores care-view sessions.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds a repository over the shared pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const sessionColumns = `id::text, family_id::text, device_id, COALESCE(child_id::text, ''),
	guardian_id, state, started_at, expires_at, finished_at,
	finish_reason, frames_delivered`

// Create stores a new session.
//
// The partial unique index turns a second open session on the same device into a
// unique violation, which is reported as ErrDeviceBusy so callers never have to
// inspect PostgreSQL error codes.
func (repository *Repository) Create(ctx context.Context, session domain.Session) error {
	_, err := repository.pool.Exec(ctx,
		`INSERT INTO care_view_sessions
			(id, family_id, device_id, child_id, guardian_id, state, started_at, expires_at)
		 VALUES ($1::uuid, $2::uuid, $3, NULLIF($4, '')::uuid, $5, $6, $7, $8)`,
		session.ID, session.FamilyID, session.DeviceID, session.ChildID,
		session.GuardianID, string(session.State), session.StartedAt, session.ExpiresAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDeviceBusy
		}
		return fmt.Errorf("create care view session: %w", err)
	}
	return nil
}

// Get returns one session owned by the family.
func (repository *Repository) Get(ctx context.Context, familyID, sessionID string) (domain.Session, error) {
	row := repository.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM care_view_sessions
		 WHERE id = $1::uuid AND family_id = $2::uuid`,
		sessionID, familyID,
	)
	return scanSession(row)
}

// GetForDevice returns one session addressed to the device.
//
// It answers for the owning device only: a device must not learn that another
// household's session exists.
func (repository *Repository) GetForDevice(ctx context.Context, deviceID, sessionID string) (domain.Session, error) {
	row := repository.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM care_view_sessions
		 WHERE id = $1::uuid AND device_id = $2`,
		sessionID, deviceID,
	)
	return scanSession(row)
}

// Transition writes the session only if it is still in expectedState.
//
// The conditional update is what makes concurrent transitions safe: the first
// writer wins and a later one sees ErrDeviceBusy instead of overwriting a state
// another actor already advanced.
func (repository *Repository) Transition(ctx context.Context, session domain.Session, expectedState domain.ViewState) error {
	tag, err := repository.pool.Exec(ctx,
		`UPDATE care_view_sessions
		 SET state = $2, finished_at = $3, finish_reason = $4, frames_delivered = $5, updated_at = NOW()
		 WHERE id = $1::uuid AND state = $6`,
		session.ID, string(session.State), session.FinishedAt,
		session.FinishReason, session.FramesDelivered, string(expectedState),
	)
	if err != nil {
		return fmt.Errorf("transition care view session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDeviceBusy
	}
	return nil
}

// ListOpen returns sessions that still hold a device.
func (repository *Repository) ListOpen(ctx context.Context) ([]domain.Session, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT `+sessionColumns+` FROM care_view_sessions
		 WHERE state IN ('requested', 'streaming')
		 ORDER BY started_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list open care view sessions: %w", err)
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
	return sessions, rows.Err()
}

// History returns a family's past sessions, newest first.
func (repository *Repository) History(ctx context.Context, familyID string, limit int) ([]domain.Session, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT `+sessionColumns+` FROM care_view_sessions
		 WHERE family_id = $1::uuid
		 ORDER BY started_at DESC
		 LIMIT $2`,
		familyID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list care view history: %w", err)
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
	return sessions, rows.Err()
}

// CountOpen reports how many sessions currently hold a device.
func (repository *Repository) CountOpen(ctx context.Context) (int, error) {
	var count int
	err := repository.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM care_view_sessions WHERE state IN ('requested', 'streaming')`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count open care view sessions: %w", err)
	}
	return count, nil
}

// CountSessionsSince counts sessions a family opened in a window.
func (repository *Repository) CountSessionsSince(ctx context.Context, familyID string, since time.Time) (int, error) {
	var count int
	err := repository.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM care_view_sessions
		 WHERE family_id = $1::uuid AND started_at >= $2`,
		familyID, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count care view sessions: %w", err)
	}
	return count, nil
}

// scanSession reads one session row.
func scanSession(row pgx.Row) (domain.Session, error) {
	var session domain.Session
	var state string
	err := row.Scan(
		&session.ID, &session.FamilyID, &session.DeviceID, &session.ChildID,
		&session.GuardianID, &state, &session.StartedAt, &session.ExpiresAt,
		&session.FinishedAt, &session.FinishReason, &session.FramesDelivered,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Session{}, ErrSessionNotFound
		}
		return domain.Session{}, fmt.Errorf("scan care view session: %w", err)
	}
	session.State = domain.ViewState(state)
	return session, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
