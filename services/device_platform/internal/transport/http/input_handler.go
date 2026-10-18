package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	inputdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/input/domain"
)

// inputService is the guardian-facing touch configuration surface. The HTTP
// layer depends on this narrow interface so the module stays removable.
type inputService interface {
	Current(ctx context.Context, familyID string) (inputdomain.Config, error)
	Publish(
		ctx context.Context,
		familyID string,
		input inputdomain.Input,
	) (inputdomain.Config, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (inputdomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input inputdomain.StatusInput,
	) (inputdomain.Status, error)
	DeviceStatuses(ctx context.Context, familyID string) ([]inputdomain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (inputdomain.AdminStats, error)
}

type inputHandler struct {
	service inputService
}

// deviceInputHandler records bounded touch status from a bound device.
type deviceInputHandler struct {
	handler        *inputHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type touchConfigPayload struct {
	Enabled          bool `json:"enabled"`
	TapEnabled       bool `json:"tap_enabled"`
	SwipeEnabled     bool `json:"swipe_enabled"`
	LongPressEnabled bool `json:"long_press_enabled"`
	HapticEnabled    bool `json:"haptic_enabled"`
	Sensitivity      int  `json:"sensitivity"`
	RotationDegrees  int  `json:"rotation_degrees"`
	LongPressMS      int  `json:"long_press_ms"`
	DoubleTapMS      int  `json:"double_tap_ms"`
	SwipeDistancePX  int  `json:"swipe_distance_px"`
}

type touchStatusPayload struct {
	TouchSupported  bool `json:"touch_supported"`
	HapticSupported bool `json:"haptic_supported"`
	GestureCount    int  `json:"gesture_count"`
	PolicyRevision  int  `json:"policy_revision"`
}

// current returns the guardian's touch configuration for the authenticated
// family.
func (handler inputHandler) current(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	config, err := handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeInputError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, touchConfigResponse(config))
}

// publish validates and stores a new guardian touch configuration.
func (handler inputHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload touchConfigPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查触摸设置")
		return
	}
	config, err := handler.service.Publish(request.Context(), familyID, inputdomain.Input{
		Enabled:          payload.Enabled,
		TapEnabled:       payload.TapEnabled,
		SwipeEnabled:     payload.SwipeEnabled,
		LongPressEnabled: payload.LongPressEnabled,
		HapticEnabled:    payload.HapticEnabled,
		Sensitivity:      payload.Sensitivity,
		RotationDegrees:  payload.RotationDegrees,
		LongPressMS:      payload.LongPressMS,
		DoubleTapMS:      payload.DoubleTapMS,
		SwipeDistancePX:  payload.SwipeDistancePX,
	})
	if err != nil {
		writeInputError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, touchConfigResponse(config))
}

// capability returns the touch capability for one family device.
func (handler inputHandler) capability(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	capability, err := handler.service.Capability(
		request.Context(),
		familyID,
		deviceID,
	)
	if err != nil {
		writeInputError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// statuses returns each family device's latest bounded touch status.
func (handler inputHandler) statuses(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	devices, err := handler.service.DeviceStatuses(request.Context(), familyID)
	if err != nil {
		writeInputError(response, request, err)
		return
	}
	if devices == nil {
		devices = []inputdomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

// reportStatus records one bound device's touch status.
func (handler deviceInputHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload touchStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		inputdomain.StatusInput{
			TouchSupported:  payload.TouchSupported,
			HapticSupported: payload.HapticSupported,
			GestureCount:    payload.GestureCount,
			PolicyRevision:  payload.PolicyRevision,
		},
	)
	if err != nil {
		writeInputError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

// devicePolicy returns the family touch configuration to a bound device over
// its device session token so the firmware can clamp and apply it, mirroring
// the other device policy endpoints.
func (handler deviceInputHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	config, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeInputError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, touchConfigResponse(config))
}

// adminStats returns the bounded fleet touch summary.
func (handler inputHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseInputWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeInputError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func (handler deviceInputHandler) verifyDevice(
	response http.ResponseWriter,
	request *http.Request,
) (string, string, bool) {
	if handler.bindingService == nil || handler.runtimeService == nil {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	pathDeviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionDeviceID, err := handler.bindingService.VerifyDeviceSession(
		request.Context(),
		deviceSessionToken,
	)
	if err != nil || sessionDeviceID != pathDeviceID {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	familyID, err := handler.runtimeService.ResolveDeviceFamily(
		request.Context(),
		deviceSessionToken,
		pathDeviceID,
	)
	if err != nil {
		writeInputError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func touchConfigResponse(config inputdomain.Config) map[string]any {
	return map[string]any{
		"schema_version":     config.SchemaVersion,
		"enabled":            config.Enabled,
		"tap_enabled":        config.TapEnabled,
		"swipe_enabled":      config.SwipeEnabled,
		"long_press_enabled": config.LongPressEnabled,
		"haptic_enabled":     config.HapticEnabled,
		"sensitivity":        config.Sensitivity,
		"rotation_degrees":   config.RotationDegrees,
		"long_press_ms":      config.LongPressMS,
		"double_tap_ms":      config.DoubleTapMS,
		"swipe_distance_px":  config.SwipeDistancePX,
		"revision":           config.Revision,
	}
}

func parseInputWindowDays(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 7
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > 90 {
		return 7
	}
	return value
}

func writeInputError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, inputdomain.ErrInvalidConfig):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_config", "触摸设置超出允许范围")
	case errors.Is(err, inputdomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
