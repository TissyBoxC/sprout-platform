// Package authorization verifies that a device's guardian still permits AI
// voice processing at the moment a realtime connection is established.
package authorization

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotAuthorized is returned when the guardian is disabled, unbound, or has
// withdrawn a consent that AI voice processing depends on.
var ErrNotAuthorized = errors.New("guardian is not authorized for voice processing")

// voiceConsentTypes are the consent decisions that must all be granted for AI
// voice processing. Child-data processing is the baseline privacy consent; AI
// interaction is the separate decision that permits sending a child's speech
// to a model, so both are required and a withdrawal of either must fail closed.
var voiceConsentTypes = []string{"child_data_processing", "ai_interaction"}

// rowQuerier is the narrow database surface the authorizer needs. It is
// satisfied by *pgxpool.Pool and lets tests substitute a stub row.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresDeviceAuthorizer resolves the owning guardian and checks both the
// account status and the newest decisions for every consent AI voice
// processing depends on.
//
// The voice gateway already reads the platform database for policy and usage.
// Reusing that connection keeps authorization on the same source of truth the
// guardian controls in the parent application.
type PostgresDeviceAuthorizer struct {
	queryer rowQuerier
}

// NewPostgresDeviceAuthorizer creates a database-backed device authorizer.
func NewPostgresDeviceAuthorizer(pool *pgxpool.Pool) *PostgresDeviceAuthorizer {
	if pool == nil {
		return &PostgresDeviceAuthorizer{}
	}
	return &PostgresDeviceAuthorizer{queryer: pool}
}

// AuthorizeDevice fails closed unless the device is bound to an active parent
// account whose latest child-data-processing and AI-interaction consents are
// both still granted.
func (a *PostgresDeviceAuthorizer) AuthorizeDevice(
	ctx context.Context,
	deviceID string,
) error {
	if a == nil || a.queryer == nil {
		return fmt.Errorf("%w: authorization database is unavailable", ErrNotAuthorized)
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ErrNotAuthorized
	}

	var accountStatus string
	var childDataGranted bool
	var aiInteractionGranted bool
	err := a.queryer.QueryRow(ctx, `
		SELECT
			account.status,
			COALESCE(child_data.granted, TRUE),
			COALESCE(ai_interaction.granted, TRUE)
		FROM device_bindings AS binding
		JOIN parent_accounts AS account
		  ON account.id = binding.parent_account_id
		LEFT JOIN LATERAL (
			SELECT event.granted
			FROM parent_consent_events AS event
			WHERE event.parent_account_id = binding.parent_account_id
			  AND event.consent_type = 'child_data_processing'
			ORDER BY event.created_at DESC, event.id DESC
			LIMIT 1
		) AS child_data ON TRUE
		LEFT JOIN LATERAL (
			SELECT event.granted
			FROM parent_consent_events AS event
			WHERE event.parent_account_id = binding.parent_account_id
			  AND event.consent_type = 'ai_interaction'
			ORDER BY event.created_at DESC, event.id DESC
			LIMIT 1
		) AS ai_interaction ON TRUE
		WHERE binding.device_id = $1
	`, deviceID).Scan(&accountStatus, &childDataGranted, &aiInteractionGranted)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotAuthorized
	}
	if err != nil {
		return fmt.Errorf("%w: read guardian authorization", ErrNotAuthorized)
	}
	if accountStatus != "active" || !childDataGranted || !aiInteractionGranted {
		return ErrNotAuthorized
	}
	return nil
}

// ensure the pool satisfies the narrowed interface at compile time.
var _ rowQuerier = (*pgxpool.Pool)(nil)
