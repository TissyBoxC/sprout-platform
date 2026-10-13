package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	displaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/display/domain"
)

// displayService is the guardian-facing display policy surface. The HTTP layer
// depends on this narrow interface so the module stays removable.
type displayService interface {
	Current(ctx context.Context, familyID string) (displaydomain.Policy, error)
	Publish(
		ctx context.Context,
		familyID string,
		input displaydomain.Input,
	) (displaydomain.Policy, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (displaydomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input displaydomain.StatusInput,
	) (displaydomain.Status, error)
	DeviceStatuses(ctx context.Context, familyID string) ([]displaydomain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (displaydomain.AdminStats, error)
}

type displayHandler struct {
	service displayService
}

// deviceDisplayHandler records bounded screen status from a bound device.
type deviceDisplayHandler struct {
	handler        *displayHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type displayPolicyPayload struct {
	BrightnessPercent      int  `json:"brightness_percent"`
	ColorTemperatureKelvin int  `json:"color_temperature_kelvin"`
	MinViewDistanceCM      int  `json:"min_view_distance_cm"`
	DailyLimitMinutes      int  `json:"daily_limit_minutes"`
	BreakIntervalMinutes   int  `json:"break_interval_minutes"`
	DimAfterSeconds        int  `json:"dim_after_seconds"`
	AutoBrightness         bool `json:"auto_brightness"`
	NightLight             bool `json:"night_light"`
	DistanceReminder       bool `json:"distance_reminder"`
}

type displayStatusPayload struct {
	BrightnessPercent int  `json:"brightness_percent"`
	ScreenWidth       int  `json:"screen_width"`
	ScreenHeight      int  `json:"screen_height"`
	TouchSupported    bool `json:"touch_supported"`
	PolicyRevision    int  `json:"policy_revision"`
}

// current returns the guardian's display policy for the authenticated family.
func (handler displayHandler) current(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	policy, err := handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeDisplayError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, displayPolicyResponse(policy))
}

// publish validates and stores a new guardian display policy.
func (handler displayHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload displayPolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查护眼屏幕设置")
		return
	}
	policy, err := handler.service.Publish(request.Context(), familyID, displaydomain.Input{
		BrightnessPercent:      payload.BrightnessPercent,
		ColorTemperatureKelvin: payload.ColorTemperatureKelvin,
		MinViewDistanceCM:      payload.MinViewDistanceCM,
		DailyLimitMinutes:      payload.DailyLimitMinutes,
		BreakIntervalMinutes:   payload.BreakIntervalMinutes,
		DimAfterSeconds:        payload.DimAfterSeconds,
		AutoBrightness:         payload.AutoBrightness,
		NightLight:             payload.NightLight,
		DistanceReminder:       payload.DistanceReminder,
	})
	if err != nil {
		writeDisplayError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, displayPolicyResponse(policy))
}

// capability returns the display capability for one family device.
func (handler displayHandler) capability(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
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
		writeDisplayError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// statuses returns each family device's latest bounded screen status.
func (handler displayHandler) statuses(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	devices, err := handler.service.DeviceStatuses(request.Context(), familyID)
	if err != nil {
		writeDisplayError(response, request, err)
		return
	}
	if devices == nil {
		devices = []displaydomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

// reportStatus records one bound device's screen status.
func (handler deviceDisplayHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload displayStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		displaydomain.StatusInput{
			BrightnessPercent: payload.BrightnessPercent,
			ScreenWidth:       payload.ScreenWidth,
			ScreenHeight:      payload.ScreenHeight,
			TouchSupported:    payload.TouchSupported,
			PolicyRevision:    payload.PolicyRevision,
		},
	)
	if err != nil {
		writeDisplayError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

// devicePolicy returns the family eye-care policy to a bound device over its
// device session token so the firmware can clamp and apply it, mirroring the
// other device policy endpoints.
func (handler deviceDisplayHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeDisplayError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, displayPolicyResponse(policy))
}

// adminStats returns the bounded fleet display summary.
func (handler displayHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseDisplayWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeDisplayError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func (handler deviceDisplayHandler) verifyDevice(
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
		writeDisplayError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func displayPolicyResponse(policy displaydomain.Policy) map[string]any {
	return map[string]any{
		"schema_version":           policy.SchemaVersion,
		"brightness_percent":       policy.BrightnessPercent,
		"color_temperature_kelvin": policy.ColorTemperatureKelvin,
		"min_view_distance_cm":     policy.MinViewDistanceCM,
		"daily_limit_minutes":      policy.DailyLimitMinutes,
		"break_interval_minutes":   policy.BreakIntervalMinutes,
		"dim_after_seconds":        policy.DimAfterSeconds,
		"auto_brightness":          policy.AutoBrightness,
		"night_light":              policy.NightLight,
		"distance_reminder":        policy.DistanceReminder,
		"revision":                 policy.Revision,
	}
}

func parseDisplayWindowDays(raw string) int {
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

func writeDisplayError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, displaydomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "护眼屏幕设置超出允许范围")
	case errors.Is(err, displaydomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
