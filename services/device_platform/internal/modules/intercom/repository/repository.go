package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/domain"
)

// ErrCallNotFound is returned when no call has the requested identifier.
var ErrCallNotFound = errors.New("intercom call not found")

// ErrDeviceBusy is returned when the device is already in an unfinished call.
var ErrDeviceBusy = errors.New("device is already in a call")

// Repository stores intercom call lifecycle.
//
// No audio, transcript, or token is written here; the table holds timing and
// state only.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds a repository over the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const callColumns = `
	id::text, family_id::text, device_id::text, COALESCE(child_id::text, ''),
	state, started_at, accepted_at, finished_at, finish_reason`

// CreateRinging inserts a new ringing call for the device.
//
// The partial unique index on open calls makes "one call per device" atomic, so
// two guardians calling at the same instant cannot both start a ring.
func (repository *Repository) CreateRinging(
	ctx context.Context,
	callID string,
	familyID string,
	deviceID string,
	childID string,
	startedAt time.Time,
) (domain.Call, error) {
	const query = `
		INSERT INTO intercom_calls (id, family_id, device_id, child_id, state, started_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, NULLIF($4, '')::uuid, 'ringing', $5)
		RETURNING ` + callColumns
	row := repository.pool.QueryRow(ctx, query, callID, familyID, deviceID, childID, startedAt)
	call, err := scanCall(row)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Call{}, ErrDeviceBusy
		}
		return domain.Call{}, fmt.Errorf("create intercom call: %w", err)
	}
	return call, nil
}

// Get returns one call scoped to its family.
//
// The family predicate is part of the lookup rather than a separate check so a
// caller can never read another family's call by guessing an identifier.
func (repository *Repository) Get(
	ctx context.Context,
	familyID string,
	callID string,
) (domain.Call, error) {
	const query = `
		SELECT ` + callColumns + `
		FROM intercom_calls
		WHERE id = $1::uuid AND family_id = $2::uuid`
	row := repository.pool.QueryRow(ctx, query, callID, familyID)
	call, err := scanCall(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Call{}, ErrCallNotFound
		}
		return domain.Call{}, fmt.Errorf("get intercom call: %w", err)
	}
	return call, nil
}

// GetForDevice returns one call scoped to the device that must answer it.
func (repository *Repository) GetForDevice(
	ctx context.Context,
	deviceID string,
	callID string,
) (domain.Call, error) {
	const query = `
		SELECT ` + callColumns + `
		FROM intercom_calls
		WHERE id = $1::uuid AND device_id = $2::uuid`
	row := repository.pool.QueryRow(ctx, query, callID, deviceID)
	call, err := scanCall(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Call{}, ErrCallNotFound
		}
		return domain.Call{}, fmt.Errorf("get device intercom call: %w", err)
	}
	return call, nil
}

// Transition writes the new lifecycle position of a call.
//
// The update is conditional on the state the caller read, so a decline racing an
// accept cannot overwrite an already connected call.
func (repository *Repository) Transition(
	ctx context.Context,
	call domain.Call,
	expected domain.CallState,
) (domain.Call, error) {
	const query = `
		UPDATE intercom_calls
		SET state = $3, accepted_at = $4, finished_at = $5,
			finish_reason = $6, updated_at = now()
		WHERE id = $1::uuid AND state = $2
		RETURNING ` + callColumns
	row := repository.pool.QueryRow(
		ctx,
		query,
		call.ID,
		string(expected),
		string(call.State),
		call.AcceptedAt,
		call.FinishedAt,
		call.DeclineReason,
	)
	updated, err := scanCall(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Someone else changed the call first; report it as a conflict.
			return domain.Call{}, ErrDeviceBusy
		}
		return domain.Call{}, fmt.Errorf("transition intercom call: %w", err)
	}
	return updated, nil
}

// CountStartedSince counts calls a family started inside the rate window.
func (repository *Repository) CountStartedSince(
	ctx context.Context,
	familyID string,
	since time.Time,
) (int, error) {
	const query = `
		SELECT COUNT(*)::int
		FROM intercom_calls
		WHERE family_id = $1::uuid AND started_at >= $2`
	var count int
	if err := repository.pool.QueryRow(ctx, query, familyID, since).Scan(&count); err != nil {
		return 0, fmt.Errorf("count intercom calls: %w", err)
	}
	return count, nil
}

// ListOpen returns unfinished calls whose deadline has passed.
//
// The caller supplies the deadline arithmetic per state so this stays a plain
// query; expired ringing calls become missed and expired active calls end.
func (repository *Repository) ListOpen(ctx context.Context) ([]domain.Call, error) {
	const query = `
		SELECT ` + callColumns + `
		FROM intercom_calls
		WHERE state IN ('ringing', 'active')`
	rows, err := repository.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list open intercom calls: %w", err)
	}
	defer rows.Close()

	calls := make([]domain.Call, 0, 8)
	for rows.Next() {
		call, err := scanCall(rows)
		if err != nil {
			return nil, fmt.Errorf("scan open intercom call: %w", err)
		}
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open intercom calls: %w", err)
	}
	return calls, nil
}

// History returns a family's most recent calls, newest first.
func (repository *Repository) History(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.Call, error) {
	const query = `
		SELECT ` + callColumns + `
		FROM intercom_calls
		WHERE family_id = $1::uuid
		ORDER BY started_at DESC
		LIMIT $2`
	rows, err := repository.pool.Query(ctx, query, familyID, limit)
	if err != nil {
		return nil, fmt.Errorf("list intercom history: %w", err)
	}
	defer rows.Close()

	calls := make([]domain.Call, 0, limit)
	for rows.Next() {
		call, err := scanCall(rows)
		if err != nil {
			return nil, fmt.Errorf("scan intercom history: %w", err)
		}
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate intercom history: %w", err)
	}
	return calls, nil
}

// CountOpen returns how many calls are ringing or active right now.
func (repository *Repository) CountOpen(ctx context.Context) (int, error) {
	const query = `
		SELECT COUNT(*)::int
		FROM intercom_calls
		WHERE state IN ('ringing', 'active')`
	var count int
	if err := repository.pool.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count open intercom calls: %w", err)
	}
	return count, nil
}

// CountCallsSince returns finished calls in the window, used for metrics.
func (repository *Repository) CountCallsSince(ctx context.Context, since time.Time) (int, error) {
	const query = `
		SELECT COUNT(*)::int
		FROM intercom_calls
		WHERE started_at >= $1`
	var count int
	if err := repository.pool.QueryRow(ctx, query, since).Scan(&count); err != nil {
		return 0, fmt.Errorf("count recent intercom calls: %w", err)
	}
	return count, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCall(row rowScanner) (domain.Call, error) {
	var call domain.Call
	var state string
	if err := row.Scan(
		&call.ID,
		&call.FamilyID,
		&call.DeviceID,
		&call.ChildID,
		&state,
		&call.StartedAt,
		&call.AcceptedAt,
		&call.FinishedAt,
		&call.DeclineReason,
	); err != nil {
		return domain.Call{}, err
	}
	call.State = domain.CallState(state)
	return call, nil
}

func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}
