package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	powerdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/power/domain"
)

// powerService is the guardian-facing battery policy surface. The HTTP layer
// depends on this narrow interface so the module stays removable.
type powerService interface {
	Current(ctx context.Context, familyID string) (powerdomain.Policy, error)
	Publish(
		ctx context.Context,
		familyID string,
		input powerdomain.Input,
	) (powerdomain.Policy, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (powerdomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input powerdomain.StatusInput,
	) (powerdomain.Status, error)
	DeviceStatuses(ctx context.Context, familyID string) ([]powerdomain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (powerdomain.AdminStats, error)
}

type powerHandler struct {
	service powerService
}

// devicePowerHandler records bounded battery status from a bound device.
type devicePowerHandler struct {
	handler        *powerHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type powerPolicyPayload struct {
	Enabled               bool `json:"enabled"`
	LowPowerEnabled       bool `json:"low_power_enabled"`
	DeepSleepEnabled      bool `json:"deep_sleep_enabled"`
	ChargePriority        bool `json:"charge_priority"`
	LowBatteryWarnPercent int  `json:"low_battery_warn_percent"`
	LowPowerEnterPercent  int  `json:"low_power_enter_percent"`
	SleepAfterSeconds     int  `json:"sleep_after_seconds"`
}

type powerStatusPayload struct {
	ChargeState    string `json:"charge_state"`
	PowerMode      string `json:"power_mode"`
	BatteryPercent int    `json:"battery_percent"`
	Millivolts     int    `json:"millivolts"`
	Charging       bool   `json:"charging"`
	PolicyRevision int    `json:"policy_revision"`
}

// current returns the guardian's power policy for the authenticated family.
func (handler powerHandler) current(
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
		writePowerError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, powerPolicyResponse(policy))
}

// publish validates and stores a new guardian power policy.
func (handler powerHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload powerPolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查电源设置")
		return
	}
	policy, err := handler.service.Publish(request.Context(), familyID, powerdomain.Input{
		Enabled:               payload.Enabled,
		LowPowerEnabled:       payload.LowPowerEnabled,
		DeepSleepEnabled:      payload.DeepSleepEnabled,
		ChargePriority:        payload.ChargePriority,
		LowBatteryWarnPercent: payload.LowBatteryWarnPercent,
		LowPowerEnterPercent:  payload.LowPowerEnterPercent,
		SleepAfterSeconds:     payload.SleepAfterSeconds,
	})
	if err != nil {
		writePowerError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, powerPolicyResponse(policy))
}

// capability returns the battery capability for one family device.
func (handler powerHandler) capability(
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
		writePowerError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// statuses returns each family device's latest bounded power status.
func (handler powerHandler) statuses(
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
		writePowerError(response, request, err)
		return
	}
	if devices == nil {
		devices = []powerdomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

// reportStatus records one bound device's power status.
func (handler devicePowerHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload powerStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		powerdomain.StatusInput{
			ChargeState:    powerdomain.ChargeState(payload.ChargeState),
			PowerMode:      powerdomain.PowerMode(payload.PowerMode),
			BatteryPercent: payload.BatteryPercent,
			Millivolts:     payload.Millivolts,
			Charging:       payload.Charging,
			PolicyRevision: payload.PolicyRevision,
		},
	)
	if err != nil {
		writePowerError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

// devicePolicy returns the family power policy to a bound device over its
// device session token so the firmware can clamp and apply it, mirroring the
// other device policy endpoints.
func (handler devicePowerHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writePowerError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, powerPolicyResponse(policy))
}

// adminStats returns the bounded fleet power summary.
func (handler powerHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parsePowerWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writePowerError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func (handler devicePowerHandler) verifyDevice(
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
		writePowerError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func powerPolicyResponse(policy powerdomain.Policy) map[string]any {
	return map[string]any{
		"schema_version":           policy.SchemaVersion,
		"enabled":                  policy.Enabled,
		"low_power_enabled":        policy.LowPowerEnabled,
		"deep_sleep_enabled":       policy.DeepSleepEnabled,
		"charge_priority":          policy.ChargePriority,
		"low_battery_warn_percent": policy.LowBatteryWarnPercent,
		"low_power_enter_percent":  policy.LowPowerEnterPercent,
		"sleep_after_seconds":      policy.SleepAfterSeconds,
		"revision":                 policy.Revision,
	}
}

func parsePowerWindowDays(raw string) int {
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

func writePowerError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, powerdomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "电源设置超出允许范围")
	case errors.Is(err, powerdomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
