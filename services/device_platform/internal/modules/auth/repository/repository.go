// Package repository persists parent accounts and refresh sessions.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for parent authentication.
type Repository interface {
	CreateParentAccount(ctx context.Context, account *domain.ParentAccount) error
	DeleteParentAccount(ctx context.Context, accountID string) error
	GetParentAccountByEmail(ctx context.Context, email string) (*domain.ParentAccount, error)
	GetParentAccountByPhone(ctx context.Context, phone string) (*domain.ParentAccount, error)
	GetParentAccountByID(ctx context.Context, accountID string) (*domain.ParentAccount, error)
	UpdatePasswordHash(ctx context.Context, accountID string, passwordHash string) error
	UpdateEmail(ctx context.Context, accountID string, email string) error
	UpdateProfile(ctx context.Context, account *domain.ParentAccount) error
	UpdateLastLogin(ctx context.Context, accountID string) error
	ListAdminAccounts(ctx context.Context) ([]domain.ParentAccount, error)
	UpdateAdminPermissions(
		ctx context.Context,
		accountID string,
		permissions []string,
	) error
	CreateSession(ctx context.Context, session *domain.Session) error
	GetSessionByRefreshTokenHash(
		ctx context.Context,
		refreshTokenHash string,
	) (*domain.Session, error)
	GetActiveSessionByID(
		ctx context.Context,
		sessionID string,
		parentAccountID string,
	) (*domain.Session, error)
	RotateSession(
		ctx context.Context,
		sessionID string,
		currentRefreshTokenHash string,
		refreshTokenHash string,
		expiresAt time.Time,
	) error
	RevokeSession(ctx context.Context, sessionID string) error
	RevokeAllSessions(ctx context.Context, parentAccountID string) error
	CreateMFAChallenge(ctx context.Context, challenge *domain.MFAChallenge) error
	DeleteMFAChallenge(ctx context.Context, challengeID string) error
	GetMFAChallengeByHash(
		ctx context.Context,
		challengeHash string,
	) (*domain.MFAChallenge, error)
	GetMFAAccountFailureState(
		ctx context.Context,
		parentAccountID string,
	) (*domain.MFAAccountFailureState, error)
	ClearExpiredMFAAccountLock(
		ctx context.Context,
		parentAccountID string,
		now time.Time,
	) (*domain.MFAAccountFailureState, error)
	ConsumeMFAChallenge(
		ctx context.Context,
		challengeID string,
		now time.Time,
	) error
	RecordMFAChallengeFailure(
		ctx context.Context,
		challengeID string,
		maxAttempts int,
		lockDuration time.Duration,
		now time.Time,
	) (
		*domain.MFAChallenge,
		*domain.MFAAccountFailureState,
		MFAFailureOutcome,
		error,
	)
	GetTOTPCredential(
		ctx context.Context,
		parentAccountID string,
	) (*domain.TOTPCredential, error)
	UpsertTOTPCredential(ctx context.Context, credential *domain.TOTPCredential) error
	CreatePhoneVerificationCode(
		ctx context.Context,
		verification *domain.PhoneVerificationCode,
	) error
	ConsumePhoneVerificationCode(
		ctx context.Context,
		phone string,
		purpose string,
		codeHash string,
	) error
}

// MFAFailureOutcome distinguishes the account-state transition performed by
// RecordMFAChallengeFailure from a request that observed another request's
// terminal state.
type MFAFailureOutcome string

const (
	MFAFailureIncremented   MFAFailureOutcome = "incremented"
	MFAFailureLockedNow     MFAFailureOutcome = "locked_now"
	MFAFailureAlreadyLocked MFAFailureOutcome = "already_locked"
	MFAFailureLockExpired   MFAFailureOutcome = "lock_expired"
)

// PostgresRepository is the PostgreSQL-backed authentication repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates an authentication repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// CreateParentAccount inserts one guardian account.
func (r *PostgresRepository) CreateParentAccount(
	ctx context.Context,
	account *domain.ParentAccount,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO parent_accounts (
			id,
			email,
			phone,
			password_hash,
			display_name,
			guardian_family_name,
			child_nickname,
			child_birthday,
			status,
			role,
			guardian_consent_version,
			guardian_consented_at,
			phone_verified_at,
			email_verified_at,
			admin_permissions,
			last_login_at,
			created_at,
			updated_at
		)
		VALUES (
			$1, NULLIF($2, ''), $3, $4, $5, $6, $7, NULLIF($8, '')::date, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, COALESCE($18, ARRAY[]::TEXT[])
		)
	`,
		account.ID,
		account.Email,
		account.Phone,
		account.PasswordHash,
		account.DisplayName,
		account.GuardianFamilyName,
		account.ChildNickname,
		account.ChildBirthday,
		account.Status,
		account.Role,
		account.GuardianConsentVersion,
		account.GuardianConsentedAt,
		account.PhoneVerifiedAt,
		account.EmailVerifiedAt,
		account.AdminPermissions,
		account.LastLoginAt,
		account.CreatedAt,
		account.UpdatedAt,
	)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "23505" {
			if strings.Contains(pgError.ConstraintName, "phone") {
				return domain.ErrPhoneExists
			}
			return domain.ErrEmailExists
		}
		return fmt.Errorf("insert parent account: %w", err)
	}
	return nil
}

// DeleteParentAccount removes an account created by a failed registration
// transaction. The caller must only use this immediately after creation.
func (r *PostgresRepository) DeleteParentAccount(
	ctx context.Context,
	accountID string,
) error {
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM parent_accounts
		WHERE id = $1
	`, accountID); err != nil {
		return fmt.Errorf("delete parent account after failed registration: %w", err)
	}
	return nil
}

// UpdateEmail binds a recovery email and marks it verified because the caller
// proved control of the existing account with its password.
func (r *PostgresRepository) UpdateEmail(
	ctx context.Context,
	accountID string,
	email string,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE parent_accounts
		SET email = $2,
		    email_verified_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, accountID, email)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "23505" {
			return domain.ErrEmailExists
		}
		return fmt.Errorf("update parent email: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAccountNotFound
	}
	return nil
}

// UpdateProfile persists guardian-editable fields and leaves identity,
// authorization, and verification state untouched.
func (r *PostgresRepository) UpdateProfile(
	ctx context.Context,
	account *domain.ParentAccount,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE parent_accounts
		SET display_name = $2,
		    guardian_family_name = $3,
		    child_nickname = NULLIF($4, ''),
		    child_birthday = NULLIF($5, '')::date,
		    updated_at = $6
		WHERE id = $1
		  AND status = 'active'
	`,
		account.ID,
		account.DisplayName,
		account.GuardianFamilyName,
		account.ChildNickname,
		account.ChildBirthday,
		account.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update parent profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAccountNotFound
	}
	return nil
}

// GetParentAccountByEmail loads one account by normalized email.
func (r *PostgresRepository) GetParentAccountByEmail(
	ctx context.Context,
	email string,
) (*domain.ParentAccount, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			COALESCE(email, ''),
			COALESCE(phone, ''),
			password_hash,
			display_name,
			guardian_family_name,
			COALESCE(child_nickname, ''),
			COALESCE(child_birthday::text, ''),
			status,
			role,
			guardian_consent_version,
			guardian_consented_at,
			phone_verified_at,
			email_verified_at,
			admin_permissions,
			last_login_at,
			created_at,
			updated_at
		FROM parent_accounts
		WHERE email IS NOT NULL
		  AND email = $1
	`, email)
	return scanParentAccount(row)
}

// GetParentAccountByPhone loads one account by normalized mobile number.
func (r *PostgresRepository) GetParentAccountByPhone(
	ctx context.Context,
	phone string,
) (*domain.ParentAccount, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			COALESCE(email, ''),
			COALESCE(phone, ''),
			password_hash,
			display_name,
			guardian_family_name,
			COALESCE(child_nickname, ''),
			COALESCE(child_birthday::text, ''),
			status,
			role,
			guardian_consent_version,
			guardian_consented_at,
			phone_verified_at,
			email_verified_at,
			admin_permissions,
			last_login_at,
			created_at,
			updated_at
		FROM parent_accounts
		WHERE phone = $1
	`, phone)
	return scanParentAccount(row)
}

// GetParentAccountByID loads one account by stable identifier.
func (r *PostgresRepository) GetParentAccountByID(
	ctx context.Context,
	accountID string,
) (*domain.ParentAccount, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			COALESCE(email, ''),
			COALESCE(phone, ''),
			password_hash,
			display_name,
			guardian_family_name,
			COALESCE(child_nickname, ''),
			COALESCE(child_birthday::text, ''),
			status,
			role,
			guardian_consent_version,
			guardian_consented_at,
			phone_verified_at,
			email_verified_at,
			admin_permissions,
			last_login_at,
			created_at,
			updated_at
		FROM parent_accounts
		WHERE id = $1
	`, accountID)
	return scanParentAccount(row)
}

// UpdateLastLogin records a successful parent login and clears the shared MFA
// failure budget in the same transaction. A successful password login resets
// the account's pre-MFA failure state without issuing an administrator session.
func (r *PostgresRepository) UpdateLastLogin(
	ctx context.Context,
	accountID string,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin parent last login: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	tag, err := transaction.Exec(ctx, `
		UPDATE parent_accounts
		SET last_login_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
		  AND status = 'active'
	`, accountID)
	if err != nil {
		return fmt.Errorf("update parent last login: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAccountNotFound
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE admin_mfa_challenges
		SET failed_attempts = 0,
		    locked_until = NULL,
		    last_failed_at = NULL,
		    updated_at = NOW()
		WHERE parent_account_id = $1
		  AND challenge_hash IS NULL
	`, accountID); err != nil {
		return fmt.Errorf("clear parent MFA failures after login: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit parent last login: %w", err)
	}
	return nil
}

// ListAdminAccounts returns every administrator with its permission set for
// the management console. Guardians are excluded because they never hold an
// administrator grant.
func (r *PostgresRepository) ListAdminAccounts(
	ctx context.Context,
) ([]domain.ParentAccount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			COALESCE(email, ''),
			COALESCE(phone, ''),
			password_hash,
			display_name,
			guardian_family_name,
			COALESCE(child_nickname, ''),
			COALESCE(child_birthday::text, ''),
			status,
			role,
			guardian_consent_version,
			guardian_consented_at,
			phone_verified_at,
			email_verified_at,
			admin_permissions,
			last_login_at,
			created_at,
			updated_at
		FROM parent_accounts
		WHERE role = 'admin'
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list administrator accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]domain.ParentAccount, 0)
	for rows.Next() {
		account, scanErr := scanParentAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		accounts = append(accounts, *account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate administrator accounts: %w", err)
	}
	return accounts, nil
}

// UpdateAdminPermissions replaces one administrator's permission set. The
// role guard keeps a guardian id from being turned into an administrator
// through this path, and it is the single write point for the grant.
func (r *PostgresRepository) UpdateAdminPermissions(
	ctx context.Context,
	accountID string,
	permissions []string,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE parent_accounts
		SET admin_permissions = $2,
		    updated_at = NOW()
		WHERE id = $1
		  AND role = 'admin'
	`, accountID, permissions)
	if err != nil {
		return fmt.Errorf("update administrator permissions: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAccountNotFound
	}
	return nil
}

// UpdatePasswordHash replaces one account's password hash and revokes every
// active session so a reset cannot leave an old login usable.
func (r *PostgresRepository) UpdatePasswordHash(
	ctx context.Context,
	accountID string,
	passwordHash string,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin password reset: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	tag, err := transaction.Exec(ctx, `
		UPDATE parent_accounts
		SET password_hash = $2,
		    updated_at = NOW()
		WHERE id = $1
	`, accountID, passwordHash)
	if err != nil {
		return fmt.Errorf("update parent password hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAccountNotFound
	}

	if _, err := transaction.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at = COALESCE(revoked_at, NOW())
		WHERE parent_account_id = $1
		  AND revoked_at IS NULL
	`, accountID); err != nil {
		return fmt.Errorf("revoke sessions after password reset: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit password reset: %w", err)
	}
	return nil
}

// CreateSession stores a refresh session.
func (r *PostgresRepository) CreateSession(
	ctx context.Context,
	session *domain.Session,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO auth_sessions (
			id,
			parent_account_id,
			refresh_token_hash,
			expires_at,
			created_at,
			last_used_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		session.ID,
		session.ParentAccountID,
		session.RefreshTokenHash,
		session.ExpiresAt,
		session.CreatedAt,
		session.LastUsedAt,
	)
	if err != nil {
		return fmt.Errorf("insert auth session: %w", err)
	}
	return nil
}

// GetSessionByRefreshTokenHash loads an unrevoked session.
func (r *PostgresRepository) GetSessionByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
) (*domain.Session, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			parent_account_id,
			refresh_token_hash,
			expires_at,
			revoked_at,
			created_at,
			last_used_at
		FROM auth_sessions
		WHERE refresh_token_hash = $1
		  AND revoked_at IS NULL
	`, refreshTokenHash)

	var session domain.Session
	err := row.Scan(
		&session.ID,
		&session.ParentAccountID,
		&session.RefreshTokenHash,
		&session.ExpiresAt,
		&session.RevokedAt,
		&session.CreatedAt,
		&session.LastUsedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get auth session: %w", err)
	}
	return &session, nil
}

// GetActiveSessionByID loads an unrevoked, unexpired session for access-token
// validation. The account id is part of the predicate so a token cannot pair
// one account's subject with another account's session id.
func (r *PostgresRepository) GetActiveSessionByID(
	ctx context.Context,
	sessionID string,
	parentAccountID string,
) (*domain.Session, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			parent_account_id,
			refresh_token_hash,
			expires_at,
			revoked_at,
			created_at,
			last_used_at
		FROM auth_sessions
		WHERE id = $1
		  AND parent_account_id = $2
		  AND revoked_at IS NULL
		  AND expires_at > NOW()
	`, sessionID, parentAccountID)

	var session domain.Session
	err := row.Scan(
		&session.ID,
		&session.ParentAccountID,
		&session.RefreshTokenHash,
		&session.ExpiresAt,
		&session.RevokedAt,
		&session.CreatedAt,
		&session.LastUsedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get active auth session: %w", err)
	}
	return &session, nil
}

// RotateSession replaces the refresh hash and expiry after a successful
// refresh. The WHERE clause makes replay of a consumed token fail.
func (r *PostgresRepository) RotateSession(
	ctx context.Context,
	sessionID string,
	currentRefreshTokenHash string,
	refreshTokenHash string,
	expiresAt time.Time,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE auth_sessions
		SET refresh_token_hash = $3,
		    expires_at = $4,
		    last_used_at = NOW()
		WHERE id = $1
		  AND refresh_token_hash = $2
		  AND revoked_at IS NULL
	`, sessionID, currentRefreshTokenHash, refreshTokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("rotate auth session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrSessionNotFound
	}
	return nil
}

// RevokeSession revokes one session idempotently.
func (r *PostgresRepository) RevokeSession(ctx context.Context, sessionID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at = COALESCE(revoked_at, NOW())
		WHERE id = $1
	`, sessionID)
	if err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
	}
	return nil
}

// RevokeAllSessions revokes every active session for one parent account.
func (r *PostgresRepository) RevokeAllSessions(
	ctx context.Context,
	parentAccountID string,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at = COALESCE(revoked_at, NOW())
		WHERE parent_account_id = $1
		  AND revoked_at IS NULL
	`, parentAccountID)
	if err != nil {
		return fmt.Errorf("revoke parent sessions: %w", err)
	}
	return nil
}

// CreateMFAChallenge stores one hashed, short-lived administrator challenge.
// The account state is checked in the same transaction so a lock committed by a
// concurrent failure cannot be bypassed by an in-flight password login.
func (r *PostgresRepository) CreateMFAChallenge(
	ctx context.Context,
	challenge *domain.MFAChallenge,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin administrator MFA challenge: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	state, err := lockMFAAccountFailureState(
		ctx,
		transaction,
		challenge.ParentAccountID,
		challenge.CreatedAt,
	)
	if err != nil {
		return err
	}
	if state.LockedUntil != nil &&
		!state.LockedUntil.After(challenge.CreatedAt) {
		state.FailedAttempts = 0
		state.LockedUntil = nil
		state.LastFailedAt = nil
		state.UpdatedAt = challenge.CreatedAt
		if _, err := transaction.Exec(ctx, `
			UPDATE admin_mfa_challenges
			SET failed_attempts = 0,
			    locked_until = NULL,
			    last_failed_at = NULL,
			    updated_at = $2
			WHERE parent_account_id = $1
			  AND challenge_hash IS NULL
		`, challenge.ParentAccountID, challenge.CreatedAt); err != nil {
			return fmt.Errorf("clear expired administrator MFA account state: %w", err)
		}
	}
	if state.LockedUntil != nil &&
		state.LockedUntil.After(challenge.CreatedAt) {
		return domain.ErrMFAAccountLocked
	}
	challenge.FailedAttempts = state.FailedAttempts
	if _, err := transaction.Exec(ctx, `
		INSERT INTO admin_mfa_challenges (
			id,
			parent_account_id,
			challenge_hash,
			expires_at,
			failed_attempts,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		challenge.ID,
		challenge.ParentAccountID,
		challenge.ChallengeHash,
		challenge.ExpiresAt,
		challenge.FailedAttempts,
		challenge.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert administrator MFA challenge: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit administrator MFA challenge: %w", err)
	}
	return nil
}

// DeleteMFAChallenge removes a challenge that could not be audited before its
// token was returned. It is intentionally limited to that issuance failure
// path; normal challenge lifecycle changes use consume or lock.
func (r *PostgresRepository) DeleteMFAChallenge(
	ctx context.Context,
	challengeID string,
) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM admin_mfa_challenges
		WHERE id = $1
		  AND consumed_at IS NULL
		  AND locked_at IS NULL
	`, challengeID)
	if err != nil {
		return fmt.Errorf("delete unaudited administrator MFA challenge: %w", err)
	}
	return nil
}

// GetMFAChallengeByHash loads one administrator MFA challenge by token hash.
func (r *PostgresRepository) GetMFAChallengeByHash(
	ctx context.Context,
	challengeHash string,
) (*domain.MFAChallenge, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			parent_account_id,
			challenge_hash,
			expires_at,
			consumed_at,
			failed_attempts,
			locked_at,
			created_at
		FROM admin_mfa_challenges
		WHERE challenge_hash = $1
	`, challengeHash)

	var challenge domain.MFAChallenge
	err := row.Scan(
		&challenge.ID,
		&challenge.ParentAccountID,
		&challenge.ChallengeHash,
		&challenge.ExpiresAt,
		&challenge.ConsumedAt,
		&challenge.FailedAttempts,
		&challenge.LockedAt,
		&challenge.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMFAChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get administrator MFA challenge: %w", err)
	}
	return &challenge, nil
}

// GetMFAAccountFailureState reads the account-wide MFA failure budget. A
// missing row is a clean state rather than an error.
func (r *PostgresRepository) GetMFAAccountFailureState(
	ctx context.Context,
	parentAccountID string,
) (*domain.MFAAccountFailureState, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			parent_account_id,
			failed_attempts,
			locked_until,
			last_failed_at,
			updated_at
		FROM admin_mfa_challenges
		WHERE parent_account_id = $1
		  AND challenge_hash IS NULL
	`, parentAccountID)

	state, err := scanMFAAccountFailureState(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.MFAAccountFailureState{
			ParentAccountID: parentAccountID,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get administrator MFA account state: %w", err)
	}
	return state, nil
}

// ClearExpiredMFAAccountLock atomically resets a lock whose deadline passed.
// The reset happens before a new challenge is inserted so its 5-minute lifetime
// does not shorten a 15-minute account lockout.
func (r *PostgresRepository) ClearExpiredMFAAccountLock(
	ctx context.Context,
	parentAccountID string,
	now time.Time,
) (*domain.MFAAccountFailureState, error) {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin administrator MFA lock recovery: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	state, err := lockMFAAccountFailureState(
		ctx,
		transaction,
		parentAccountID,
		now,
	)
	if err != nil {
		return nil, err
	}
	recovered := false
	if state.LockedUntil != nil && !state.LockedUntil.After(now) {
		recovered = true
		state.FailedAttempts = 0
		state.LockedUntil = nil
		state.UpdatedAt = now
		state.LastFailedAt = nil
		if _, err := transaction.Exec(ctx, `
			UPDATE admin_mfa_challenges
			SET failed_attempts = 0,
			    locked_until = NULL,
			    last_failed_at = NULL,
			    updated_at = $2
			WHERE parent_account_id = $1
			  AND challenge_hash IS NULL
		`, parentAccountID, now); err != nil {
			return nil, fmt.Errorf("clear expired administrator MFA lock: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit administrator MFA lock recovery: %w", err)
	}
	state.Recovered = recovered
	return state, nil
}

// ConsumeMFAChallenge atomically prevents replay and clears the account's
// shared failure budget. The challenge and account state are locked in one
// transaction so a concurrent failure cannot repopulate a just-cleared budget.
func (r *PostgresRepository) ConsumeMFAChallenge(
	ctx context.Context,
	challengeID string,
	now time.Time,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin administrator MFA consumption: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var parentAccountID string
	if err := transaction.QueryRow(ctx, `
		SELECT parent_account_id
		FROM admin_mfa_challenges
		WHERE id = $1
	`, challengeID).Scan(&parentAccountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrMFAChallengeNotFound
		}
		return fmt.Errorf("find administrator MFA challenge account: %w", err)
	}
	if _, err := lockMFAAccountFailureState(
		ctx,
		transaction,
		parentAccountID,
		now,
	); err != nil {
		return err
	}

	var challenge domain.MFAChallenge
	if err := transaction.QueryRow(ctx, `
		SELECT
			id,
			parent_account_id,
			challenge_hash,
			expires_at,
			consumed_at,
			failed_attempts,
			locked_at,
			created_at
		FROM admin_mfa_challenges
		WHERE id = $1
		FOR UPDATE
	`, challengeID).Scan(
		&challenge.ID,
		&challenge.ParentAccountID,
		&challenge.ChallengeHash,
		&challenge.ExpiresAt,
		&challenge.ConsumedAt,
		&challenge.FailedAttempts,
		&challenge.LockedAt,
		&challenge.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrMFAChallengeNotFound
		}
		return fmt.Errorf("lock administrator MFA challenge: %w", err)
	}
	if state, stateErr := scanMFAAccountFailureState(
		transaction.QueryRow(ctx, `
			SELECT
				parent_account_id,
				failed_attempts,
				locked_until,
				last_failed_at,
				updated_at
			FROM admin_mfa_challenges
			WHERE parent_account_id = $1
			  AND challenge_hash IS NULL
		`, parentAccountID),
	); stateErr == nil {
		if state.LockedUntil != nil && state.LockedUntil.After(now) {
			return domain.ErrMFAAccountLocked
		}
	} else if !errors.Is(stateErr, pgx.ErrNoRows) {
		return fmt.Errorf("read administrator MFA account state: %w", stateErr)
	}
	if challenge.ConsumedAt != nil {
		return domain.ErrMFAChallengeConsumed
	}
	if !challenge.ExpiresAt.After(now) {
		return domain.ErrMFAChallengeExpired
	}
	if challenge.LockedAt != nil {
		return domain.ErrMFAAccountLocked
	}
	if strings.TrimSpace(challenge.ChallengeHash) != "" {
		if _, err := transaction.Exec(ctx, `
			UPDATE admin_mfa_challenges
			SET consumed_at = $2
			WHERE id = $1
		`, challengeID, now); err != nil {
			return fmt.Errorf("consume administrator MFA challenge: %w", err)
		}
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE admin_mfa_challenges
		SET failed_attempts = 0,
		    locked_until = NULL,
		    last_failed_at = NULL,
		    updated_at = $2
		WHERE parent_account_id = $1
		  AND challenge_hash IS NULL
	`, parentAccountID, now); err != nil {
		return fmt.Errorf("clear administrator MFA account failures: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit administrator MFA consumption: %w", err)
	}
	return nil
}

// RecordMFAChallengeFailure increments the account-wide counter and applies
// the terminal lock in one transaction. The account row, not the challenge, is
// the lockout authority, so starting a new challenge cannot reset the budget.
func (r *PostgresRepository) RecordMFAChallengeFailure(
	ctx context.Context,
	challengeID string,
	maxAttempts int,
	lockDuration time.Duration,
	now time.Time,
) (
	*domain.MFAChallenge,
	*domain.MFAAccountFailureState,
	MFAFailureOutcome,
	error,
) {
	if maxAttempts <= 0 {
		return nil, nil, "", errors.New("MFA maximum attempts must be positive")
	}
	if lockDuration <= 0 {
		return nil, nil, "", errors.New("MFA lock duration must be positive")
	}
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, "", fmt.Errorf("begin administrator MFA failure: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var parentAccountID string
	if err := transaction.QueryRow(ctx, `
		SELECT parent_account_id
		FROM admin_mfa_challenges
		WHERE id = $1
	`, challengeID).Scan(&parentAccountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, "", domain.ErrMFAChallengeNotFound
		}
		return nil, nil, "", fmt.Errorf("find administrator MFA challenge account: %w", err)
	}
	state, err := lockMFAAccountFailureState(
		ctx,
		transaction,
		parentAccountID,
		now,
	)
	if err != nil {
		return nil, nil, "", err
	}

	var challenge domain.MFAChallenge
	if err := transaction.QueryRow(ctx, `
		SELECT
			id,
			parent_account_id,
			challenge_hash,
			expires_at,
			consumed_at,
			failed_attempts,
			locked_at,
			created_at
		FROM admin_mfa_challenges
		WHERE id = $1
		FOR UPDATE
	`, challengeID).Scan(
		&challenge.ID,
		&challenge.ParentAccountID,
		&challenge.ChallengeHash,
		&challenge.ExpiresAt,
		&challenge.ConsumedAt,
		&challenge.FailedAttempts,
		&challenge.LockedAt,
		&challenge.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, "", domain.ErrMFAChallengeNotFound
		}
		return nil, nil, "", fmt.Errorf("lock administrator MFA challenge: %w", err)
	}
	if challenge.ConsumedAt != nil {
		return &challenge, state, "", domain.ErrMFAChallengeConsumed
	}
	if !challenge.ExpiresAt.After(now) {
		return &challenge, state, "", domain.ErrMFAChallengeExpired
	}
	if challenge.LockedAt != nil {
		return &challenge, state, MFAFailureAlreadyLocked, domain.ErrMFAAccountLocked
	}
	if state.LockedUntil != nil && state.LockedUntil.After(now) {
		return &challenge, state, MFAFailureAlreadyLocked, domain.ErrMFAAccountLocked
	}

	outcome := MFAFailureIncremented
	if state.LockedUntil != nil {
		state.FailedAttempts = 0
		state.LockedUntil = nil
		state.LastFailedAt = nil
		outcome = MFAFailureLockExpired
	}
	state.FailedAttempts++
	state.LastFailedAt = &now
	state.UpdatedAt = now
	if state.FailedAttempts >= maxAttempts {
		lockedUntil := now.Add(lockDuration)
		state.LockedUntil = &lockedUntil
		challenge.LockedAt = &now
		outcome = MFAFailureLockedNow
	}

	if _, err := transaction.Exec(ctx, `
		UPDATE admin_mfa_challenges
		SET failed_attempts = $2,
		    locked_until = $3,
		    last_failed_at = $4,
		    updated_at = $5
		WHERE parent_account_id = $1
		  AND challenge_hash IS NULL
	`, state.ParentAccountID, state.FailedAttempts, state.LockedUntil,
		state.LastFailedAt, state.UpdatedAt); err != nil {
		return nil, nil, "", fmt.Errorf("update administrator MFA account failures: %w", err)
	}
	challenge.FailedAttempts = state.FailedAttempts
	if _, err := transaction.Exec(ctx, `
		UPDATE admin_mfa_challenges
		SET failed_attempts = $2,
		    locked_at = $3
		WHERE id = $1
		  AND locked_at IS NULL
	`, challenge.ID, challenge.FailedAttempts, challenge.LockedAt); err != nil {
		return nil, nil, "", fmt.Errorf("record administrator MFA challenge failures: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, nil, "", fmt.Errorf("commit administrator MFA failure: %w", err)
	}
	return &challenge, state, outcome, nil
}

func lockMFAAccountFailureState(
	ctx context.Context,
	transaction pgx.Tx,
	parentAccountID string,
	now time.Time,
) (*domain.MFAAccountFailureState, error) {
	if _, err := transaction.Exec(ctx, `
		INSERT INTO admin_mfa_challenges (
			id,
			parent_account_id,
			challenge_hash,
			expires_at,
			failed_attempts,
			consumed_at,
			created_at,
			updated_at
		)
		VALUES ($1, $2, NULL, 'infinity', 0, $3, $3, $3)
		ON CONFLICT (parent_account_id)
		WHERE challenge_hash IS NULL
		DO NOTHING
	`, uuid.NewString(), parentAccountID, now); err != nil {
		return nil, fmt.Errorf("ensure administrator MFA account state: %w", err)
	}
	state, err := scanMFAAccountFailureState(transaction.QueryRow(ctx, `
		SELECT
			parent_account_id,
			failed_attempts,
			locked_until,
			last_failed_at,
			updated_at
		FROM admin_mfa_challenges
		WHERE parent_account_id = $1
		  AND challenge_hash IS NULL
		FOR UPDATE
	`, parentAccountID))
	if err != nil {
		return nil, fmt.Errorf("lock administrator MFA account state: %w", err)
	}
	return state, nil
}

func scanMFAAccountFailureState(
	row rowScanner,
) (*domain.MFAAccountFailureState, error) {
	var state domain.MFAAccountFailureState
	if err := row.Scan(
		&state.ParentAccountID,
		&state.FailedAttempts,
		&state.LockedUntil,
		&state.LastFailedAt,
		&state.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &state, nil
}

// GetTOTPCredential loads the encrypted TOTP enrollment for one administrator.
func (r *PostgresRepository) GetTOTPCredential(
	ctx context.Context,
	parentAccountID string,
) (*domain.TOTPCredential, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			parent_account_id,
			encrypted_secret,
			secret_nonce,
			key_version,
			enabled_at,
			created_at,
			updated_at
		FROM admin_totp_credentials
		WHERE parent_account_id = $1
	`, parentAccountID)

	var credential domain.TOTPCredential
	err := row.Scan(
		&credential.ParentAccountID,
		&credential.EncryptedSecret,
		&credential.SecretNonce,
		&credential.KeyVersion,
		&credential.EnabledAt,
		&credential.CreatedAt,
		&credential.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMFANotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("get administrator TOTP credential: %w", err)
	}
	return &credential, nil
}

// UpsertTOTPCredential stores or replaces an administrator TOTP enrollment.
func (r *PostgresRepository) UpsertTOTPCredential(
	ctx context.Context,
	credential *domain.TOTPCredential,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO admin_totp_credentials (
			parent_account_id,
			encrypted_secret,
			secret_nonce,
			key_version,
			enabled_at,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (parent_account_id) DO UPDATE
		SET encrypted_secret = EXCLUDED.encrypted_secret,
		    secret_nonce = EXCLUDED.secret_nonce,
		    key_version = EXCLUDED.key_version,
		    enabled_at = EXCLUDED.enabled_at,
		    updated_at = EXCLUDED.updated_at
	`,
		credential.ParentAccountID,
		credential.EncryptedSecret,
		credential.SecretNonce,
		credential.KeyVersion,
		credential.EnabledAt,
		credential.CreatedAt,
		credential.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert administrator TOTP credential: %w", err)
	}
	return nil
}

// CreatePhoneVerificationCode stores one hashed, short-lived SMS code.
func (r *PostgresRepository) CreatePhoneVerificationCode(
	ctx context.Context,
	verification *domain.PhoneVerificationCode,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO phone_verification_codes (
			id,
			phone,
			purpose,
			code_hash,
			expires_at,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		verification.ID,
		verification.Phone,
		verification.Purpose,
		verification.CodeHash,
		verification.ExpiresAt,
		verification.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert phone verification code: %w", err)
	}
	return nil
}

// ConsumePhoneVerificationCode atomically validates and consumes one SMS code.
func (r *PostgresRepository) ConsumePhoneVerificationCode(
	ctx context.Context,
	phone string,
	purpose string,
	codeHash string,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE phone_verification_codes
		SET consumed_at = NOW()
		WHERE id = (
			SELECT id
			FROM phone_verification_codes
			WHERE phone = $1
			  AND purpose = $2
			  AND code_hash = $3
			  AND consumed_at IS NULL
			  AND expires_at > NOW()
			ORDER BY created_at DESC
			LIMIT 1
		)
	`, phone, purpose, codeHash)
	if err != nil {
		return fmt.Errorf("consume phone verification code: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInvalidVerification
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanParentAccount(row rowScanner) (*domain.ParentAccount, error) {
	var account domain.ParentAccount
	err := row.Scan(
		&account.ID,
		&account.Email,
		&account.Phone,
		&account.PasswordHash,
		&account.DisplayName,
		&account.GuardianFamilyName,
		&account.ChildNickname,
		&account.ChildBirthday,
		&account.Status,
		&account.Role,
		&account.GuardianConsentVersion,
		&account.GuardianConsentedAt,
		&account.PhoneVerifiedAt,
		&account.EmailVerifiedAt,
		&account.AdminPermissions,
		&account.LastLoginAt,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan parent account: %w", err)
	}
	return &account, nil
}
