// Package repository persists device binding tokens and durable bindings.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sqlExecutor is the subset shared by the pool and an open transaction.
type sqlExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// Repository defines device binding persistence operations.
type Repository interface {
	CreateToken(ctx context.Context, token *domain.BindingToken) error
	GetTokenByHash(ctx context.Context, tokenHash string) (*domain.BindingToken, error)
	UpsertBinding(ctx context.Context, binding *domain.Binding) error
	ListByParentAccountID(
		ctx context.Context,
		parentAccountID string,
	) ([]domain.Binding, error)
	ListAllBindings(ctx context.Context) ([]domain.Binding, error)
	GetByDeviceID(ctx context.Context, deviceID string) (*domain.Binding, error)
	Delete(ctx context.Context, parentAccountID string, deviceID string) error
	RevokeDeviceSessionsAndDeleteBinding(
		ctx context.Context,
		parentAccountID string,
		deviceID string,
	) error
	CreateRegistrationToken(ctx context.Context, token *domain.RegistrationToken) error
	GetRegistrationTokenByHash(
		ctx context.Context,
		tokenHash string,
	) (*domain.RegistrationToken, error)
	ConsumeRegistrationTokenAndUpsertCredential(
		ctx context.Context,
		tokenID string,
		credential *domain.DeviceCredential,
	) error
	GetDeviceCredential(
		ctx context.Context,
		deviceID string,
	) (*domain.DeviceCredential, error)
	CreateDeviceChallenge(ctx context.Context, challenge *domain.DeviceChallenge) error
	GetDeviceChallengeByNonceHash(
		ctx context.Context,
		nonceHash string,
	) (*domain.DeviceChallenge, error)
	CompleteDeviceAuthentication(
		ctx context.Context,
		challengeID string,
		deviceID string,
		session *domain.DeviceSession,
	) error
	GetDeviceSessionByTokenHash(
		ctx context.Context,
		tokenHash string,
	) (*domain.DeviceSession, error)
	ConsumeTokenAndUpsertBinding(
		ctx context.Context,
		tokenID string,
		binding *domain.Binding,
	) error
}

// PostgresRepository is the PostgreSQL-backed device binding repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a device binding repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// CreateToken inserts a hashed, single-use provisioning token.
func (r *PostgresRepository) CreateToken(
	ctx context.Context,
	token *domain.BindingToken,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_binding_tokens (
			id,
			device_id,
			token_hash,
			expires_at,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5)
	`,
		token.ID,
		token.DeviceID,
		token.TokenHash,
		token.ExpiresAt,
		token.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert device binding token: %w", err)
	}
	return nil
}

// GetTokenByHash loads one unconsumed token by its hash.
func (r *PostgresRepository) GetTokenByHash(
	ctx context.Context,
	tokenHash string,
) (*domain.BindingToken, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			device_id,
			token_hash,
			expires_at,
			consumed_at,
			created_at
		FROM device_binding_tokens
		WHERE token_hash = $1
	`, tokenHash)

	var token domain.BindingToken
	err := row.Scan(
		&token.ID,
		&token.DeviceID,
		&token.TokenHash,
		&token.ExpiresAt,
		&token.ConsumedAt,
		&token.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device binding token: %w", err)
	}
	return &token, nil
}

// UpsertBinding binds or transfers one device to a parent account.
func (r *PostgresRepository) UpsertBinding(
	ctx context.Context,
	binding *domain.Binding,
) error {
	return upsertBinding(ctx, r.pool, binding)
}

func upsertBinding(
	ctx context.Context,
	executor sqlExecutor,
	binding *domain.Binding,
) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO device_bindings (
			id,
			parent_account_id,
			device_id,
			device_name,
			hardware_model,
			firmware_version,
			capability_set,
			bound_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (device_id) DO UPDATE
		SET parent_account_id = EXCLUDED.parent_account_id,
		    device_name = EXCLUDED.device_name,
		    hardware_model = EXCLUDED.hardware_model,
		    firmware_version = EXCLUDED.firmware_version,
		    capability_set = EXCLUDED.capability_set,
		    updated_at = EXCLUDED.updated_at
	`,
		binding.ID,
		binding.ParentAccountID,
		binding.DeviceID,
		binding.DeviceName,
		binding.HardwareModel,
		binding.FirmwareVersion,
		binding.Capabilities,
		binding.BoundAt,
		binding.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert device binding: %w", err)
	}
	return nil
}

// ConsumeTokenAndUpsertBinding atomically consumes the provisioning token and
// writes the durable parent-device binding. A unique or connectivity failure
// must not leave the parent with a burned binding code.
func (r *PostgresRepository) ConsumeTokenAndUpsertBinding(
	ctx context.Context,
	tokenID string,
	binding *domain.Binding,
) error {
	return withTransaction(ctx, r.pool, func(transaction pgx.Tx) error {
		tag, err := transaction.Exec(ctx, `
			UPDATE device_binding_tokens
			SET consumed_at = NOW()
			WHERE id = $1
			  AND consumed_at IS NULL
			  AND expires_at > NOW()
		`, tokenID)
		if err != nil {
			return fmt.Errorf("consume device binding token: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrTokenConsumed
		}
		return upsertBinding(ctx, transaction, binding)
	})
}

// ListByParentAccountID returns all devices owned by one parent.
func (r *PostgresRepository) ListByParentAccountID(
	ctx context.Context,
	parentAccountID string,
) ([]domain.Binding, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			parent_account_id,
			device_id,
			device_name,
			hardware_model,
			firmware_version,
			capability_set,
			bound_at,
			updated_at
		FROM device_bindings
		WHERE parent_account_id = $1
		ORDER BY updated_at DESC
	`, parentAccountID)
	if err != nil {
		return nil, fmt.Errorf("list device bindings: %w", err)
	}
	defer rows.Close()

	bindings := make([]domain.Binding, 0)
	for rows.Next() {
		binding, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, *binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device bindings: %w", err)
	}
	return bindings, nil
}

// ListAllBindings returns every durable device binding for operations support.
//
// This method is intentionally separate from the guardian-scoped lookup so
// callers must choose the privileged service surface explicitly.
func (r *PostgresRepository) ListAllBindings(
	ctx context.Context,
) ([]domain.Binding, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			parent_account_id,
			device_id,
			device_name,
			hardware_model,
			firmware_version,
			capability_set,
			bound_at,
			updated_at
		FROM device_bindings
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list all device bindings: %w", err)
	}
	defer rows.Close()

	bindings := make([]domain.Binding, 0)
	for rows.Next() {
		binding, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, *binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate all device bindings: %w", err)
	}
	return bindings, nil
}

// GetByDeviceID loads the current owner of one device.
func (r *PostgresRepository) GetByDeviceID(
	ctx context.Context,
	deviceID string,
) (*domain.Binding, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			parent_account_id,
			device_id,
			device_name,
			hardware_model,
			firmware_version,
			capability_set,
			bound_at,
			updated_at
		FROM device_bindings
		WHERE device_id = $1
	`, deviceID)
	return scanBinding(row)
}

// Delete removes one binding only when it belongs to the requesting parent.
func (r *PostgresRepository) Delete(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM device_bindings
		WHERE parent_account_id = $1
		  AND device_id = $2
	`, parentAccountID, deviceID)
	if err != nil {
		return fmt.Errorf("delete device binding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrDeviceNotFound
	}
	return nil
}

// RevokeDeviceSessionsAndDeleteBinding removes one guardian-owned binding and
// revokes every session for that device in one transaction. Revoking first
// closes the race where an in-flight device request could observe the old
// binding after the guardian has already removed it.
func (r *PostgresRepository) RevokeDeviceSessionsAndDeleteBinding(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
) error {
	return withTransaction(ctx, r.pool, func(transaction pgx.Tx) error {
		if _, err := transaction.Exec(ctx, `
			UPDATE device_sessions
			SET revoked_at = NOW()
			WHERE device_id = $1
			  AND revoked_at IS NULL
		`, deviceID); err != nil {
			return fmt.Errorf("revoke device sessions: %w", err)
		}
		tag, err := transaction.Exec(ctx, `
			DELETE FROM device_bindings
			WHERE parent_account_id = $1
			  AND device_id = $2
		`, parentAccountID, deviceID)
		if err != nil {
			return fmt.Errorf("delete device binding: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrDeviceNotFound
		}
		return nil
	})
}

// CreateRegistrationToken stores one hashed, single-use device registration token.
func (r *PostgresRepository) CreateRegistrationToken(
	ctx context.Context,
	token *domain.RegistrationToken,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_registration_tokens (
			id,
			device_id,
			token_hash,
			expires_at,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5)
	`,
		token.ID,
		token.DeviceID,
		token.TokenHash,
		token.ExpiresAt,
		token.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert device registration token: %w", err)
	}
	return nil
}

// GetRegistrationTokenByHash loads one device registration token by hash.
func (r *PostgresRepository) GetRegistrationTokenByHash(
	ctx context.Context,
	tokenHash string,
) (*domain.RegistrationToken, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			device_id,
			token_hash,
			expires_at,
			consumed_at,
			created_at
		FROM device_registration_tokens
		WHERE token_hash = $1
	`, tokenHash)

	var token domain.RegistrationToken
	err := row.Scan(
		&token.ID,
		&token.DeviceID,
		&token.TokenHash,
		&token.ExpiresAt,
		&token.ConsumedAt,
		&token.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrRegistrationTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device registration token: %w", err)
	}
	return &token, nil
}

// ConsumeRegistrationTokenAndUpsertCredential atomically consumes a
// registration grant and stores the device public identity. A failed identity
// write must not burn the grant, so both operations share one transaction.
func (r *PostgresRepository) ConsumeRegistrationTokenAndUpsertCredential(
	ctx context.Context,
	tokenID string,
	credential *domain.DeviceCredential,
) error {
	return withTransaction(ctx, r.pool, func(transaction pgx.Tx) error {
		tag, err := transaction.Exec(ctx, `
			UPDATE device_registration_tokens
			SET consumed_at = NOW()
			WHERE id = $1
			  AND consumed_at IS NULL
			  AND expires_at > NOW()
		`, tokenID)
		if err != nil {
			return fmt.Errorf("consume device registration token: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrRegistrationTokenConsumed
		}
		if err := upsertDeviceCredential(ctx, transaction, credential); err != nil {
			return err
		}
		return nil
	})
}

// GetDeviceCredential loads the registered public identity for one device.
func (r *PostgresRepository) GetDeviceCredential(
	ctx context.Context,
	deviceID string,
) (*domain.DeviceCredential, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			device_id,
			hardware_model,
			firmware_version,
			capability_set,
			public_key,
			status,
			registered_at,
			last_authenticated_at,
			updated_at
		FROM device_credentials
		WHERE device_id = $1
	`, deviceID)

	var credential domain.DeviceCredential
	err := row.Scan(
		&credential.DeviceID,
		&credential.HardwareModel,
		&credential.FirmwareVersion,
		&credential.CapabilitySet,
		&credential.PublicKey,
		&credential.Status,
		&credential.RegisteredAt,
		&credential.LastAuthenticatedAt,
		&credential.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device credential: %w", err)
	}
	return &credential, nil
}

func upsertDeviceCredential(
	ctx context.Context,
	executor sqlExecutor,
	credential *domain.DeviceCredential,
) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO device_credentials (
			device_id,
			hardware_model,
			firmware_version,
			capability_set,
			public_key,
			status,
			registered_at,
			last_authenticated_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (device_id) DO UPDATE
		SET hardware_model = EXCLUDED.hardware_model,
		    firmware_version = EXCLUDED.firmware_version,
		    capability_set = EXCLUDED.capability_set,
		    public_key = EXCLUDED.public_key,
		    status = EXCLUDED.status,
		    updated_at = EXCLUDED.updated_at
	`,
		credential.DeviceID,
		credential.HardwareModel,
		credential.FirmwareVersion,
		credential.CapabilitySet,
		credential.PublicKey,
		credential.Status,
		credential.RegisteredAt,
		credential.LastAuthenticatedAt,
		credential.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert device credential: %w", err)
	}
	return nil
}

// CreateDeviceChallenge stores one short-lived, single-use device nonce.
func (r *PostgresRepository) CreateDeviceChallenge(
	ctx context.Context,
	challenge *domain.DeviceChallenge,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_challenges (
			id,
			device_id,
			nonce_hash,
			expires_at,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5)
	`,
		challenge.ID,
		challenge.DeviceID,
		challenge.NonceHash,
		challenge.ExpiresAt,
		challenge.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert device challenge: %w", err)
	}
	return nil
}

// GetDeviceChallengeByNonceHash loads one device challenge by nonce hash.
func (r *PostgresRepository) GetDeviceChallengeByNonceHash(
	ctx context.Context,
	nonceHash string,
) (*domain.DeviceChallenge, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			device_id,
			nonce_hash,
			expires_at,
			consumed_at,
			created_at
		FROM device_challenges
		WHERE nonce_hash = $1
	`, nonceHash)

	var challenge domain.DeviceChallenge
	err := row.Scan(
		&challenge.ID,
		&challenge.DeviceID,
		&challenge.NonceHash,
		&challenge.ExpiresAt,
		&challenge.ConsumedAt,
		&challenge.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrDeviceChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device challenge: %w", err)
	}
	return &challenge, nil
}

// CompleteDeviceAuthentication atomically consumes a challenge, records the
// authentication time, and stores the new session. A partial failure must not
// consume the nonce without issuing a usable session.
func (r *PostgresRepository) CompleteDeviceAuthentication(
	ctx context.Context,
	challengeID string,
	deviceID string,
	session *domain.DeviceSession,
) error {
	return withTransaction(ctx, r.pool, func(transaction pgx.Tx) error {
		tag, err := transaction.Exec(ctx, `
			UPDATE device_challenges
			SET consumed_at = NOW()
			WHERE id = $1
			  AND consumed_at IS NULL
			  AND expires_at > NOW()
		`, challengeID)
		if err != nil {
			return fmt.Errorf("consume device challenge: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrDeviceChallengeConsumed
		}
		if _, err := transaction.Exec(ctx, `
			UPDATE device_credentials
			SET last_authenticated_at = NOW(),
			    updated_at = NOW()
			WHERE device_id = $1
			  AND status = 'active'
		`, deviceID); err != nil {
			return fmt.Errorf("update device authentication time: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO device_sessions (
				id,
				device_id,
				token_hash,
				expires_at,
				created_at,
				last_used_at
			)
			VALUES ($1, $2, $3, $4, $5, $6)
		`,
			session.ID,
			session.DeviceID,
			session.TokenHash,
			session.ExpiresAt,
			session.CreatedAt,
			session.LastUsedAt,
		); err != nil {
			return fmt.Errorf("insert device session: %w", err)
		}
		return nil
	})
}

// GetDeviceSessionByTokenHash loads one unrevoked device session.
func (r *PostgresRepository) GetDeviceSessionByTokenHash(
	ctx context.Context,
	tokenHash string,
) (*domain.DeviceSession, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			device_id,
			token_hash,
			expires_at,
			revoked_at,
			created_at,
			last_used_at
		FROM device_sessions
		WHERE token_hash = $1
		  AND revoked_at IS NULL
	`, tokenHash)

	var session domain.DeviceSession
	err := row.Scan(
		&session.ID,
		&session.DeviceID,
		&session.TokenHash,
		&session.ExpiresAt,
		&session.RevokedAt,
		&session.CreatedAt,
		&session.LastUsedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrDeviceSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device session: %w", err)
	}
	return &session, nil
}

type bindingScanner interface {
	Scan(dest ...any) error
}

func scanBinding(row bindingScanner) (*domain.Binding, error) {
	var binding domain.Binding
	err := row.Scan(
		&binding.ID,
		&binding.ParentAccountID,
		&binding.DeviceID,
		&binding.DeviceName,
		&binding.HardwareModel,
		&binding.FirmwareVersion,
		&binding.Capabilities,
		&binding.BoundAt,
		&binding.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan device binding: %w", err)
	}
	return &binding, nil
}

func withTransaction(
	ctx context.Context,
	pool *pgxpool.Pool,
	operation func(transaction pgx.Tx) error,
) error {
	transaction, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin device binding transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	if err := operation(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit device binding transaction: %w", err)
	}
	return nil
}
