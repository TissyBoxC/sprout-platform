package http

import (
	"context"
	"net/http"
	"strings"

	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
)

// deviceSessionContextKey carries the authenticated device identifier.
type deviceSessionContextKey struct{}

// requireDeviceSession authenticates a device request before the handler runs.
//
// Every device route is wrapped so a handler cannot accidentally expose an
// unauthenticated endpoint: the token is verified here, and the resulting
// device identifier must match the {device_id} path segment so one device can
// never act on another. Handlers keep their own verification for the paths that
// are not registered through this wrapper, but this is the enforced boundary.
func requireDeviceSession(
	verifier deviceSessionVerifier,
	next http.HandlerFunc,
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if verifier == nil {
			writeError(
				response,
				request,
				http.StatusServiceUnavailable,
				"service_unavailable",
				"服务暂时不可用，请稍后重试",
			)
			return
		}
		sessionToken, ok := bearerToken(request)
		if !ok {
			writeError(
				response,
				request,
				http.StatusUnauthorized,
				"device_session_expired",
				"设备登录已过期，请重新连接",
			)
			return
		}
		sessionDeviceID, err := verifier.VerifyDeviceSession(request.Context(), sessionToken)
		if err != nil {
			writeDeviceBindingError(response, request, err)
			return
		}
		pathDeviceID := strings.TrimSpace(request.PathValue("device_id"))
		if pathDeviceID != "" && pathDeviceID != sessionDeviceID {
			writeDeviceBindingError(response, request, bindingdomain.ErrInvalidDeviceProof)
			return
		}
		next(response, request.WithContext(
			context.WithValue(request.Context(), deviceSessionContextKey{}, sessionDeviceID),
		))
	}
}

// deviceSessionFromContext returns the device identifier authenticated by
// requireDeviceSession. It is empty for routes that authenticate differently.
func deviceSessionFromContext(ctx context.Context) string {
	deviceID, _ := ctx.Value(deviceSessionContextKey{}).(string)
	return deviceID
}
