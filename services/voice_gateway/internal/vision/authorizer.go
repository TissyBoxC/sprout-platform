package vision

import "context"

// DeviceAuthorizer verifies that the guardian who owns a device still permits
// camera/vision processing.
//
// The internal vision endpoints are called by a trusted server-side component
// behind the service token, but that token does not express a specific
// guardian's consent. The device identifier in the request is therefore
// attacker-controlled from the endpoint's perspective, so the authorizer is
// the sole authority on whether that device may be processed. It shares the
// same platform-table source of truth the realtime transport uses.
type DeviceAuthorizer interface {
	AuthorizeDevice(ctx context.Context, deviceID string) error
}
