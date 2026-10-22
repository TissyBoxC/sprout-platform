package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	runtimeDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/domain"
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

// intercomCallNotifier pushes a ring to the device through the existing device
// command channel.
//
// The command is stored before it is published, so a device that is offline
// still receives the ring on its next poll. A silent, never-stored push would
// make a call disappear whenever the network hiccuped.
type intercomCallNotifier struct {
	commands interface {
		CreateCommand(
			ctx context.Context,
			deviceID string,
			commandType runtimeDomain.CommandType,
			requestedBy string,
			payload map[string]any,
		) (*runtimeDomain.Command, error)
	}
	// cancelCommand ends a call from the platform side.
	clock func() time.Time
}

// NotifyIntercomCall sends the ring command for one call.
func (notifier intercomCallNotifier) NotifyIntercomCall(ctx context.Context, call domain.Call) error {
	if notifier.commands == nil {
		return nil
	}
	payload := map[string]any{
		"call_id": call.ID,
		// The device stops ringing at this instant even if it stops hearing from
		// the platform, so a lost cancel cannot ring forever.
		"ring_deadline": call.Deadline().Format(time.RFC3339),
	}
	if call.ChildID != "" {
		payload["child_id"] = call.ChildID
	}
	_, err := notifier.commands.CreateCommand(
		ctx,
		call.DeviceID,
		runtimeDomain.CommandVoiceIntercomCall,
		"guardian",
		payload,
	)
	return err
}

// NotifyIntercomCallEnded tells the device to stop ringing or to drop an active
// call.
//
// A call that was never accepted ends by ringing out on the device, so this is
// sent for every terminal state.
func (notifier intercomCallNotifier) NotifyIntercomCallEnded(ctx context.Context, call domain.Call) error {
	if notifier.commands == nil {
		return nil
	}
	action := "hangup"
	if call.State == domain.StateCancelled || call.State == domain.StateMissed {
		action = "decline"
	}
	payload := map[string]any{
		"call_id": call.ID,
		"action":  action,
	}
	_, err := notifier.commands.CreateCommand(
		ctx,
		call.DeviceID,
		runtimeDomain.CommandVoiceIntercomCallControl,
		"guardian",
		payload,
	)
	return err
}
