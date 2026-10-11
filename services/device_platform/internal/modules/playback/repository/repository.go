// Package repository persists guardian on-demand playback requests.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for playback requests.
type Repository interface {
	CreateRequest(ctx context.Context, request *domain.Request) error
	ListRequests(
		ctx context.Context,
		familyID string,
		query domain.RequestQuery,
	) ([]domain.Request, error)
	GetRequest(
		ctx context.Context,
		familyID string,
		requestID string,
	) (*domain.Request, error)
	// CancelRequest moves a still-pending family request to cancelled. It
	// returns ErrRequestNotFound for an unknown id and ErrInvalidRequestState
	// when the request already reached a terminal state.
	CancelRequest(ctx context.Context, familyID string, requestID string) (*domain.Request, error)
	// ListPendingForDevice returns the requests a bound device should act on,
	// oldest first so the device drains them in the order they were queued.
	ListPendingForDevice(
		ctx context.Context,
		deviceID string,
	) ([]domain.Request, error)
	// AcknowledgeRequest marks one device-owned request acknowledged. It is
	// idempotent: re-acknowledging an acknowledged request is a no-op.
	AcknowledgeRequest(
		ctx context.Context,
		deviceID string,
		requestID string,
	) (*domain.Request, error)
	// CompleteRequest records the terminal outcome for one device-owned
	// request. A request that is already terminal is not changed.
	CompleteRequest(
		ctx context.Context,
		deviceID string,
		requestID string,
		outcome string,
	) (*domain.Request, error)
	// HasPendingRequest reports whether a pending request already exists for
	// the device and package, so the service can reject a duplicate tap.
	HasPendingRequest(
		ctx context.Context,
		familyID string,
		deviceID string,
		packageID string,
	) (bool, error)
}

// PostgresRepository is the PostgreSQL-backed playback repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a playback repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const requestColumns = `
	id,
	family_id,
	device_id,
	package_id,
	source,
	status,
	note,
	requested_at,
	acknowledged_at,
	completed_at,
	COALESCE(outcome, ''),
	created_at,
	updated_at
`

// CreateRequest inserts one pending request.
func (r *PostgresRepository) CreateRequest(
	ctx context.Context,
	request *domain.Request,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO playback_requests (
			id,
			family_id,
			device_id,
			package_id,
			source,
			status,
			note,
			requested_at,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		request.ID,
		request.FamilyID,
		request.DeviceID,
		request.PackageID,
		request.Source,
		request.Status,
		request.Note,
		request.RequestedAt,
		request.CreatedAt,
		request.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create playback request: %w", err)
	}
	return nil
}

// ListRequests returns a family-scoped, newest-first page of requests.
func (r *PostgresRepository) ListRequests(
	ctx context.Context,
	familyID string,
	query domain.RequestQuery,
) ([]domain.Request, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+requestColumns+`
		FROM playback_requests
		WHERE family_id = $1
			AND ($2 = '' OR device_id = $2)
			AND ($3 = '' OR status = $3)
		ORDER BY requested_at DESC, id DESC
		LIMIT $4
	`, familyID, query.DeviceID, query.Status, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("list playback requests: %w", err)
	}
	defer rows.Close()
	return scanRequests(rows)
}

// GetRequest returns one family-owned request.
func (r *PostgresRepository) GetRequest(
	ctx context.Context,
	familyID string,
	requestID string,
) (*domain.Request, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+requestColumns+`
		FROM playback_requests
		WHERE family_id = $1 AND id = $2
	`, familyID, requestID)
	request, err := scanRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrRequestNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get playback request: %w", err)
	}
	return request, nil
}

// CancelRequest moves a pending request to cancelled.
func (r *PostgresRepository) CancelRequest(
	ctx context.Context,
	familyID string,
	requestID string,
) (*domain.Request, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE playback_requests
		SET status = $3, completed_at = NOW(), updated_at = NOW()
		WHERE family_id = $1
			AND id = $2
			AND status = $4
		RETURNING `+requestColumns+`
	`, familyID, requestID, domain.StatusCancelled, domain.StatusPending)
	request, err := scanRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the request does not exist or it is no longer pending; the
		// service distinguishes the two with a follow-up read.
		existing, getErr := r.GetRequest(ctx, familyID, requestID)
		if getErr != nil {
			return nil, getErr
		}
		return existing, domain.ErrInvalidRequestState
	}
	if err != nil {
		return nil, fmt.Errorf("cancel playback request: %w", err)
	}
	return request, nil
}

// ListPendingForDevice returns a device's pending and acknowledged requests,
// oldest first.
func (r *PostgresRepository) ListPendingForDevice(
	ctx context.Context,
	deviceID string,
) ([]domain.Request, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+requestColumns+`
		FROM playback_requests
		WHERE device_id = $1
			AND status IN ($2, $3)
		ORDER BY requested_at, id
	`, deviceID, domain.StatusPending, domain.StatusAcknowledged)
	if err != nil {
		return nil, fmt.Errorf("list pending playback requests: %w", err)
	}
	defer rows.Close()
	return scanRequests(rows)
}

// AcknowledgeRequest marks one device-owned request acknowledged.
func (r *PostgresRepository) AcknowledgeRequest(
	ctx context.Context,
	deviceID string,
	requestID string,
) (*domain.Request, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE playback_requests
		SET status = $3,
			acknowledged_at = COALESCE(acknowledged_at, NOW()),
			updated_at = NOW()
		WHERE device_id = $1
			AND id = $2
			AND status = $4
		RETURNING `+requestColumns+`
	`, deviceID, requestID, domain.StatusAcknowledged, domain.StatusPending)
	request, err := scanRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrRequestNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("acknowledge playback request: %w", err)
	}
	return request, nil
}

// CompleteRequest records the terminal outcome for one device-owned request.
func (r *PostgresRepository) CompleteRequest(
	ctx context.Context,
	deviceID string,
	requestID string,
	outcome string,
) (*domain.Request, error) {
	status := domain.StatusCompleted
	if outcome == domain.OutcomeError {
		status = domain.StatusFailed
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE playback_requests
		SET status = $3,
			outcome = $4,
			completed_at = NOW(),
			updated_at = NOW()
		WHERE device_id = $1
			AND id = $2
			AND status IN ($5, $6)
		RETURNING `+requestColumns+`
	`,
		deviceID,
		requestID,
		status,
		outcome,
		domain.StatusPending,
		domain.StatusAcknowledged,
	)
	request, err := scanRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrRequestNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("complete playback request: %w", err)
	}
	return request, nil
}

// HasPendingRequest reports whether a pending request already exists.
func (r *PostgresRepository) HasPendingRequest(
	ctx context.Context,
	familyID string,
	deviceID string,
	packageID string,
) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM playback_requests
			WHERE family_id = $1
				AND device_id = $2
				AND package_id = $3
				AND status IN ($4, $5)
		)
	`, familyID, deviceID, packageID, domain.StatusPending,
		domain.StatusAcknowledged).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check pending playback request: %w", err)
	}
	return exists, nil
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRequest(row rowScanner) (*domain.Request, error) {
	var request domain.Request
	err := row.Scan(
		&request.ID,
		&request.FamilyID,
		&request.DeviceID,
		&request.PackageID,
		&request.Source,
		&request.Status,
		&request.Note,
		&request.RequestedAt,
		&request.AcknowledgedAt,
		&request.CompletedAt,
		&request.Outcome,
		&request.CreatedAt,
		&request.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func scanRequests(rows pgx.Rows) ([]domain.Request, error) {
	requests := make([]domain.Request, 0, 16)
	for rows.Next() {
		request, err := scanRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("scan playback request: %w", err)
		}
		requests = append(requests, *request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate playback requests: %w", err)
	}
	return requests, nil
}
