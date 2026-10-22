package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/domain"
	careviewservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/service"
	runtimeDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
)

// careViewSwitch reads the family's care-view setting.
//
// A family without a row is DISABLED. Unlike intercom, where a guardian still has
// to call and a child still has to accept, watching a camera needs an explicit
// decision recorded before anything can turn on.
type careViewSwitch struct {
	pool *pgxpool.Pool
}

// ViewEnabled reports whether the family turned live viewing on.
func (switcher careViewSwitch) ViewEnabled(ctx context.Context, familyID string) (bool, error) {
	var enabled bool
	err := switcher.pool.QueryRow(ctx,
		`SELECT is_enabled FROM care_view_settings WHERE family_id = $1::uuid`,
		familyID,
	).Scan(&enabled)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, err
	}
	return enabled, nil
}

// careViewConsentReader reads the guardian's image consent decision.
type careViewConsentReader struct {
	pool *pgxpool.Pool
}

// ImageConsentGranted reports whether the family consented to image processing.
//
// Care view reuses the existing image-privacy policy rather than inventing a
// second consent: two image consents that can disagree is how a child ends up
// watched after their guardian said no. Both the policy switch and the upload
// permission must be on, and a family without a policy row counts as no.
func (reader careViewConsentReader) ImageConsentGranted(ctx context.Context, familyID string) (bool, error) {
	var granted bool
	err := reader.pool.QueryRow(ctx,
		`SELECT COALESCE(
			(SELECT enabled AND upload_allowed
			 FROM image_privacy_policies WHERE family_id = $1::uuid), FALSE)`,
		familyID,
	).Scan(&granted)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, err
	}
	return granted, nil
}

// careViewNotifier pushes care-view control to the device.
type careViewNotifier struct {
	commands interface {
		CreateCommand(
			ctx context.Context,
			deviceID string,
			commandType runtimeDomain.CommandType,
			requestedBy string,
			payload map[string]any,
		) (*runtimeDomain.Command, error)
	}
}

// NotifyCareViewStart asks the device to show its indication and begin capture.
func (notifier careViewNotifier) NotifyCareViewStart(ctx context.Context, session domain.Session) error {
	if notifier.commands == nil {
		return nil
	}
	payload := map[string]any{
		"session_id": session.ID,
		// The device enforces this deadline itself, so a lost stop command still
		// cannot leave a camera running.
		"expires_at": session.ExpiresAt.Format(time.RFC3339),
	}
	if session.ChildID != "" {
		payload["child_id"] = session.ChildID
	}
	_, err := notifier.commands.CreateCommand(
		ctx,
		session.DeviceID,
		runtimeDomain.CommandCareViewStart,
		"guardian",
		payload,
	)
	return err
}

// NotifyCareViewStop tells the device to stop capturing and clear its indication.
func (notifier careViewNotifier) NotifyCareViewStop(ctx context.Context, session domain.Session) error {
	if notifier.commands == nil {
		return nil
	}
	_, err := notifier.commands.CreateCommand(
		ctx,
		session.DeviceID,
		runtimeDomain.CommandCareViewStop,
		"guardian",
		map[string]any{"session_id": session.ID},
	)
	return err
}

// Compile-time proof the adapters satisfy the service contracts.
var (
	_ careviewservice.ViewSwitch     = careViewSwitch{}
	_ careviewservice.ConsentReader  = careViewConsentReader{}
	_ careviewservice.DeviceNotifier = careViewNotifier{}
)
