package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

// GrantSpendingConsent records guardian spending consent for one terms version.
//
// The partial unique index allows one live row per (family, version), so an
// existing live grant is returned instead of duplicated: the evidence of the
// first agreement is what matters, and re-tapping the consent screen must not
// manufacture a second record.
func (r *PostgresRepository) GrantSpendingConsent(
	ctx context.Context,
	consent domain.SpendingConsent,
) (*domain.SpendingConsent, error) {
	_, err := r.database.Exec(ctx, `
		INSERT INTO billing_spending_consents (
			id, parent_account_id, consent_version, granted_at, revoked_at,
			ip_address, method, created_at, updated_at
		) VALUES ($1, $2, $3, $4, NULL, $5, $6, $7, $8)
		ON CONFLICT (parent_account_id, consent_version) WHERE revoked_at IS NULL
		DO NOTHING
	`,
		consent.ID,
		consent.ParentAccountID,
		consent.ConsentVersion,
		consent.GrantedAt,
		consent.IPAddress,
		consent.Method,
		consent.CreatedAt,
		consent.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("grant billing spending consent: %w", err)
	}
	return r.GetActiveSpendingConsent(ctx, consent.ParentAccountID, consent.ConsentVersion)
}

// RevokeSpendingConsent withdraws a live consent and returns when it happened.
//
// Revocation stamps revoked_at instead of deleting the row: the consent is
// legal evidence of a prior agreement and of when it ended.
func (r *PostgresRepository) RevokeSpendingConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
	revokedAt time.Time,
) (bool, error) {
	tag, err := r.database.Exec(ctx, `
		UPDATE billing_spending_consents
		SET revoked_at = $3,
		    updated_at = $3
		WHERE parent_account_id = $1
		  AND consent_version = $2
		  AND revoked_at IS NULL
	`, parentAccountID, consentVersion, revokedAt)
	if err != nil {
		return false, fmt.Errorf("revoke billing spending consent: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// GetActiveSpendingConsent returns the live consent for one terms version, or
// nil when the guardian has not agreed to it (or has withdrawn it).
func (r *PostgresRepository) GetActiveSpendingConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
) (*domain.SpendingConsent, error) {
	var consent domain.SpendingConsent
	err := r.database.QueryRow(ctx, `
		SELECT id, parent_account_id, consent_version, granted_at, revoked_at,
		       ip_address, method, created_at, updated_at
		FROM billing_spending_consents
		WHERE parent_account_id = $1
		  AND consent_version = $2
		  AND revoked_at IS NULL
	`, parentAccountID, consentVersion).Scan(
		&consent.ID,
		&consent.ParentAccountID,
		&consent.ConsentVersion,
		&consent.GrantedAt,
		&consent.RevokedAt,
		&consent.IPAddress,
		&consent.Method,
		&consent.CreatedAt,
		&consent.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active billing spending consent: %w", err)
	}
	return &consent, nil
}

// GetLatestSpendingConsent returns the newest consent of any version for a
// family, so a guardian who agreed under an older terms version is not asked
// again for the same thing when the current version has no live row.
func (r *PostgresRepository) GetLatestSpendingConsent(
	ctx context.Context,
	parentAccountID string,
) (*domain.SpendingConsent, error) {
	var consent domain.SpendingConsent
	err := r.database.QueryRow(ctx, `
		SELECT id, parent_account_id, consent_version, granted_at, revoked_at,
		       ip_address, method, created_at, updated_at
		FROM billing_spending_consents
		WHERE parent_account_id = $1
		  AND revoked_at IS NULL
		ORDER BY granted_at DESC
		LIMIT 1
	`, parentAccountID).Scan(
		&consent.ID,
		&consent.ParentAccountID,
		&consent.ConsentVersion,
		&consent.GrantedAt,
		&consent.RevokedAt,
		&consent.IPAddress,
		&consent.Method,
		&consent.CreatedAt,
		&consent.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest billing spending consent: %w", err)
	}
	return &consent, nil
}
