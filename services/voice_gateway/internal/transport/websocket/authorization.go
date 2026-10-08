package websocket

import (
	"context"
	"errors"
	"strings"
)

// ErrDeviceNotAuthorized means the guardian is disabled or has withdrawn the
// child-data-processing authorization that permits AI voice processing.
var ErrDeviceNotAuthorized = errors.New("device guardian is not authorized")

// DeviceAuthorizer verifies at connection time that the guardian who owns a
// device still permits AI voice processing.
//
// The signed device token proves device identity, but it cannot express a
// guardian's later consent withdrawal. Re-checking the authoritative platform
// state before the WebSocket upgrade closes that gap without putting raw audio
// or child data on the check path.
type DeviceAuthorizer interface {
	AuthorizeDevice(ctx context.Context, deviceID string) error
}

// authorizeDevice runs the optional guardian authorization check.
//
// A nil authorizer means the deployment has not wired the platform database;
// callers that require the check must reject that configuration at startup.
// Unknown devices and withdrawn consents fail closed.
func authorizeDevice(
	ctx context.Context,
	authorizer DeviceAuthorizer,
	deviceID string,
) error {
	if authorizer == nil {
		return nil
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ErrDeviceNotAuthorized
	}
	if err := authorizer.AuthorizeDevice(ctx, deviceID); err != nil {
		return ErrDeviceNotAuthorized
	}
	return nil
}
