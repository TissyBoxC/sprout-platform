// Package repository persists platform-side AI account projections.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const upsertProvisioningQuery = `
	INSERT INTO ai_accounts (
		id,
		parent_account_id,
		provider_account_id,
		provider_account_email,
		credential_ciphertext,
		credential_nonce,
		provider_api_key_id,
		credential_key_version,
		status,
		balance_usd,
		concurrency_limit,
		available_models,
		selected_models,
		allowed_models,
		created_at,
		updated_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	ON CONFLICT (parent_account_id) DO UPDATE
	SET provider_account_id = EXCLUDED.provider_account_id,
	    provider_account_email = EXCLUDED.provider_account_email,
	    credential_ciphertext = EXCLUDED.credential_ciphertext,
	    credential_nonce = EXCLUDED.credential_nonce,
	    provider_api_key_id = EXCLUDED.provider_api_key_id,
	    credential_key_version = EXCLUDED.credential_key_version,
	    status = EXCLUDED.status,
	    balance_usd = EXCLUDED.balance_usd,
	    concurrency_limit = EXCLUDED.concurrency_limit,
	    available_models = EXCLUDED.available_models,
	    selected_models = EXCLUDED.selected_models,
	    allowed_models = EXCLUDED.allowed_models,
	    updated_at = EXCLUDED.updated_at
	WHERE ai_accounts.credential_ciphertext = ''::bytea
	   OR ai_accounts.credential_nonce = ''::bytea
	   OR ai_accounts.provider_api_key_id = 0
	RETURNING
		id,
		parent_account_id,
		provider_account_id,
		provider_account_email,
		credential_ciphertext,
		credential_nonce,
		provider_api_key_id,
		credential_key_version,
		status,
		balance_usd,
		concurrency_limit,
		available_models,
		selected_models,
		allowed_models,
		created_at,
		updated_at
`

// Repository stores parent AI account state and encrypted credentials.
type Repository interface {
	GetByParentAccountID(
		ctx context.Context,
		parentAccountID string,
	) (*domain.Account, error)
	GetByProviderAccountID(
		ctx context.Context,
		providerAccountID string,
	) (*domain.Account, error)
	List(ctx context.Context) ([]domain.Account, error)
	DeleteByParentAccountID(ctx context.Context, parentAccountID string) error
	// UpsertProvisioning atomically creates or repairs one account projection.
	// It returns the stored account so callers can detect a concurrent winner
	// without issuing a second credential.
	UpsertProvisioning(ctx context.Context, account *domain.Account) (*domain.Account, error)
	UpdateFromProvider(ctx context.Context, account *domain.Account) error
	UpdateStatus(
		ctx context.Context,
		parentAccountID string,
		status string,
	) error
}

// PostgresRepository is the PostgreSQL-backed AI account repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates an AI account repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// GetByParentAccountID loads one account by the platform parent id.
func (r *PostgresRepository) GetByParentAccountID(
	ctx context.Context,
	parentAccountID string,
) (*domain.Account, error) {
	return r.getOne(ctx, `
		SELECT
			id,
			parent_account_id,
			provider_account_id,
			provider_account_email,
			credential_ciphertext,
			credential_nonce,
			provider_api_key_id,
			credential_key_version,
			status,
			balance_usd,
			concurrency_limit,
			available_models,
			selected_models,
			allowed_models,
			created_at,
			updated_at
		FROM ai_accounts
		WHERE parent_account_id = $1
	`, parentAccountID)
}

// GetByProviderAccountID loads one account by the provider namespace id.
func (r *PostgresRepository) GetByProviderAccountID(
	ctx context.Context,
	providerAccountID string,
) (*domain.Account, error) {
	return r.getOne(ctx, `
		SELECT
			id,
			parent_account_id,
			provider_account_id,
			provider_account_email,
			credential_ciphertext,
			credential_nonce,
			provider_api_key_id,
			credential_key_version,
			status,
			balance_usd,
			concurrency_limit,
			available_models,
			selected_models,
			allowed_models,
			created_at,
			updated_at
		FROM ai_accounts
		WHERE provider_account_id = $1
	`, providerAccountID)
}

// List returns all platform AI account projections for the operations API.
func (r *PostgresRepository) List(ctx context.Context) ([]domain.Account, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			ai_accounts.id,
			ai_accounts.parent_account_id,
			ai_accounts.provider_account_id,
			ai_accounts.provider_account_email,
			ai_accounts.credential_ciphertext,
			ai_accounts.credential_nonce,
			ai_accounts.provider_api_key_id,
			ai_accounts.credential_key_version,
			ai_accounts.status,
			ai_accounts.balance_usd,
			ai_accounts.concurrency_limit,
			ai_accounts.available_models,
			ai_accounts.selected_models,
			ai_accounts.allowed_models,
			ai_accounts.created_at,
			ai_accounts.updated_at,
			COALESCE(parent_accounts.email, ''),
			parent_accounts.display_name
		FROM ai_accounts
		JOIN parent_accounts ON parent_accounts.id = ai_accounts.parent_account_id
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list AI accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]domain.Account, 0)
	for rows.Next() {
		account, err := scanAccountWithParent(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI accounts: %w", err)
	}
	return accounts, nil
}

// DeleteByParentAccountID removes the local provider projection after the
// provider has accepted account deletion.
func (r *PostgresRepository) DeleteByParentAccountID(
	ctx context.Context,
	parentAccountID string,
) error {
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM ai_accounts
		WHERE parent_account_id = $1
	`, parentAccountID); err != nil {
		return fmt.Errorf("delete AI account projection: %w", err)
	}
	return nil
}

// UpsertProvisioning stores the encrypted credential and provider projection
// in one statement. The unique parent id makes retries safe: the latest
// provider key replaces only an incomplete platform credential, while an
// existing caller that already completed provisioning wins the conflict.
func (r *PostgresRepository) UpsertProvisioning(
	ctx context.Context,
	account *domain.Account,
) (*domain.Account, error) {
	if !accountHasCredential(account) {
		return nil, errors.New("AI account credential is incomplete")
	}
	row := r.pool.QueryRow(ctx, upsertProvisioningQuery,
		account.ID,
		account.ParentAccountID,
		account.ProviderAccountID,
		account.ProviderAccountEmail,
		account.APIKeyCiphertext,
		account.APIKeyNonce,
		account.ProviderAPIKeyID,
		account.CredentialKeyVersion,
		account.Status,
		account.BalanceUSD,
		account.ConcurrencyLimit,
		nonNilStrings(account.AvailableModels),
		nonNilStrings(account.SelectedModels),
		nonNilStrings(account.AllowedModels),
		account.CreatedAt,
		account.UpdatedAt,
	)
	stored, err := scanAccount(row)
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, domain.ErrAccountNotFound) {
		return nil, fmt.Errorf("upsert AI account provisioning: %w", err)
	}

	// A concurrent writer already stored a complete credential. Read and
	// return that winner so the caller can delete the provider key it no
	// longer needs.
	current, currentErr := r.GetByParentAccountID(ctx, account.ParentAccountID)
	if currentErr != nil {
		return nil, currentErr
	}
	if !accountHasCredential(current) {
		return nil, errors.New("AI account credential is incomplete")
	}
	return current, nil
}

func accountHasCredential(account *domain.Account) bool {
	return account != nil &&
		len(account.APIKeyCiphertext) > 0 &&
		len(account.APIKeyNonce) > 0 &&
		account.ProviderAPIKeyID > 0
}

// UpdateFromProvider updates the non-secret provider projection without
// touching credential material.
func (r *PostgresRepository) UpdateFromProvider(
	ctx context.Context,
	account *domain.Account,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_accounts
		SET status = $2,
		    balance_usd = $3,
		    concurrency_limit = $4,
		    available_models = $5,
		    selected_models = $6,
		    allowed_models = $7,
		    credential_ciphertext = $8,
		    credential_nonce = $9,
		    provider_api_key_id = $10,
		    credential_key_version = $11,
		    updated_at = $12
		WHERE id = $1
	`,
		account.ID,
		account.Status,
		account.BalanceUSD,
		account.ConcurrencyLimit,
		nonNilStrings(account.AvailableModels),
		nonNilStrings(account.SelectedModels),
		nonNilStrings(account.AllowedModels),
		account.APIKeyCiphertext,
		account.APIKeyNonce,
		account.ProviderAPIKeyID,
		account.CredentialKeyVersion,
		account.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update AI account: %w", err)
	}
	return nil
}

// nonNilStrings preserves "no models" as an empty database array. pgx binds a
// nil slice as SQL NULL, which violates the NOT NULL model-pool columns.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// UpdateStatus changes only the platform-side lifecycle state.
func (r *PostgresRepository) UpdateStatus(
	ctx context.Context,
	parentAccountID string,
	status string,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_accounts
		SET status = $2,
		    updated_at = NOW()
		WHERE parent_account_id = $1
	`, parentAccountID, status)
	if err != nil {
		return fmt.Errorf("update AI account status: %w", err)
	}
	return nil
}

func (r *PostgresRepository) getOne(
	ctx context.Context,
	query string,
	argument any,
) (*domain.Account, error) {
	row := r.pool.QueryRow(ctx, query, argument)
	return scanAccount(row)
}

type accountScanner interface {
	Scan(dest ...any) error
}

func scanAccount(row accountScanner) (*domain.Account, error) {
	var account domain.Account
	err := row.Scan(
		&account.ID,
		&account.ParentAccountID,
		&account.ProviderAccountID,
		&account.ProviderAccountEmail,
		&account.APIKeyCiphertext,
		&account.APIKeyNonce,
		&account.ProviderAPIKeyID,
		&account.CredentialKeyVersion,
		&account.Status,
		&account.BalanceUSD,
		&account.ConcurrencyLimit,
		&account.AvailableModels,
		&account.SelectedModels,
		&account.AllowedModels,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get AI account: %w", err)
	}
	return &account, nil
}

// scanAccountWithParent reads the operations list projection, which joins the
// owning guardian so operators can identify an account without internal ids.
func scanAccountWithParent(row accountScanner) (*domain.Account, error) {
	var account domain.Account
	err := row.Scan(
		&account.ID,
		&account.ParentAccountID,
		&account.ProviderAccountID,
		&account.ProviderAccountEmail,
		&account.APIKeyCiphertext,
		&account.APIKeyNonce,
		&account.ProviderAPIKeyID,
		&account.CredentialKeyVersion,
		&account.Status,
		&account.BalanceUSD,
		&account.ConcurrencyLimit,
		&account.AvailableModels,
		&account.SelectedModels,
		&account.AllowedModels,
		&account.CreatedAt,
		&account.UpdatedAt,
		&account.ParentEmail,
		&account.ParentDisplayName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get AI account: %w", err)
	}
	return &account, nil
}
