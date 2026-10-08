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
// withdrawn the child-data-processing consent.
var ErrNotAuthorized = errors.New("guardian is not authorized for voice processing")

// PostgresDeviceAuthorizer resolves the owning guardian and checks both the
// account status and the newest consent decision.
//
// The voice gateway already reads the platform database for policy and usage.
// Reusing that connection keeps authorization on the same source of truth the
// guardian controls in the parent application.
type PostgresDeviceAuthorizer struct {
	pool *pgxpool.Pool
}

// NewPostgresDeviceAuthorizer creates a database-backed device authorizer.
func NewPostgresDeviceAuthorizer(pool *pgxpool.Pool) *PostgresDeviceAuthorizer {
	return &PostgresDeviceAuthorizer{pool: pool}
}

// AuthorizeDevice fails closed unless the device is bound to an active parent
// account whose latest child-data-processing consent is still granted.
func (a *PostgresDeviceAuthorizer) AuthorizeDevice(
	ctx context.Context,
	deviceID string,
) error {
	if a == nil || a.pool == nil {
		return fmt.Errorf("%w: authorization database is unavailable", ErrNotAuthorized)
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ErrNotAuthorized
	}

	var accountStatus string
	var consentGranted bool
	err := a.pool.QueryRow(ctx, `
		SELECT
			account.status,
			COALESCE(consent.granted, TRUE)
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
		) AS consent ON TRUE
		WHERE binding.device_id = $1
	`, deviceID).Scan(&accountStatus, &consentGranted)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotAuthorized
	}
	if err != nil {
		return fmt.Errorf("%w: read guardian authorization", ErrNotAuthorized)
	}
	if accountStatus != "active" || !consentGranted {
		return ErrNotAuthorized
	}
	return nil
}
