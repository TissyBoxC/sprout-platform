package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// intercomDeviceLookup resolves which family owns a device and which child the
// device currently serves.
//
// It reads the binding tables directly instead of calling the device binding
// service, because intercom must answer even while that service is busy with a
// provisioning write.
type intercomDeviceLookup struct {
	pool *pgxpool.Pool
}

// DeviceFamily returns the family that owns the device, or an empty string when
// the device is unknown or unbound.
func (lookup intercomDeviceLookup) DeviceFamily(ctx context.Context, deviceID string) (string, error) {
	const query = `
		SELECT COALESCE(family_id::text, '')
		FROM device_bindings
		WHERE device_id = $1::uuid`
	var familyID string
	if err := lookup.pool.QueryRow(ctx, query, deviceID).Scan(&familyID); err != nil {
		// An unknown device is not an error: the caller answers "not found".
		return "", nil
	}
	return familyID, nil
}

// DeviceChild returns the device's active child, or an empty string when no
// binding exists. Intercom still works for a family that has not chosen a child.
func (lookup intercomDeviceLookup) DeviceChild(ctx context.Context, deviceID string) (string, error) {
	const query = `
		SELECT COALESCE(b.child_id::text, '')
		FROM device_child_bindings b
		JOIN child_profiles c ON c.id = b.child_id
		WHERE b.device_id = $1::uuid AND b.is_active`
	var childID string
	if err := lookup.pool.QueryRow(ctx, query, deviceID).Scan(&childID); err != nil {
		return "", nil
	}
	return childID, nil
}

// intercomFamilySettings reads and writes the family's intercom switch.
//
// A family with no row has never changed the setting, so it is enabled: the
// guardian still starts every call and the child still has to accept it.
type intercomFamilySettings struct {
	pool *pgxpool.Pool
}

// IntercomEnabled reports whether the family currently allows intercom.
func (settings intercomFamilySettings) IntercomEnabled(ctx context.Context, familyID string) (bool, error) {
	const query = `
		SELECT is_enabled
		FROM intercom_family_settings
		WHERE family_id = $1::uuid`
	var enabled bool
	if err := settings.pool.QueryRow(ctx, query, familyID).Scan(&enabled); err != nil {
		if isNoRows(err) {
			return true, nil
		}
		return false, err
	}
	return enabled, nil
}

// SetIntercomEnabled stores the family's choice.
func (settings intercomFamilySettings) SetIntercomEnabled(
	ctx context.Context,
	familyID string,
	enabled bool,
) error {
	const query = `
		INSERT INTO intercom_family_settings (family_id, is_enabled, updated_at)
		VALUES ($1::uuid, $2, now())
		ON CONFLICT (family_id)
		DO UPDATE SET is_enabled = EXCLUDED.is_enabled, updated_at = now()`
	_, err := settings.pool.Exec(ctx, query, familyID, enabled)
	return err
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
