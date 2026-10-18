package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	connectivitydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/connectivity/domain"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
)

// connectivityService is the guardian-facing cellular policy surface. The HTTP
// layer depends on this narrow interface so the module stays removable.
type connectivityService interface {
	Current(ctx context.Context, familyID string) (connectivitydomain.Policy, error)
	Publish(
		ctx context.Context,
		familyID string,
		input connectivitydomain.Input,
	) (connectivitydomain.Policy, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (connectivitydomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input connectivitydomain.StatusInput,
	) (connectivitydomain.Status, error)
	DeviceStatuses(ctx context.Context, familyID string) ([]connectivitydomain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (connectivitydomain.AdminStats, error)
}

type connectivityHandler struct {
	service connectivityService
}

// deviceConnectivityHandler records bounded cellular status from a bound device.
type deviceConnectivityHandler struct {
	handler        *connectivityHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type cellularPolicyPayload struct {
	Enabled             bool   `json:"enabled"`
	RoamingAllowed      bool   `json:"roaming_allowed"`
	RemoteWakeEnabled   bool   `json:"remote_wake_enabled"`
	MeteredUploadLimit  bool   `json:"metered_upload_limit"`
	Preference          string `json:"preference"`
	MonthlyQuotaMB      int    `json:"monthly_quota_mb"`
	LowQuotaWarnPercent int    `json:"low_quota_warn_percent"`
	KeepaliveSeconds    int    `json:"keepalive_seconds"`
}

type connectivityStatusPayload struct {
	Bearer         string `json:"bearer"`
	Roaming        bool   `json:"roaming"`
	ESIMState      string `json:"esim_state"`
	SignalPercent  int    `json:"signal_percent"`
	UsedMB         int    `json:"used_mb"`
	PolicyRevision int    `json:"policy_revision"`
}

type deviceCellularPolicyResponse struct {
	SchemaVersion       int    `json:"schema_version"`
	Enabled             bool   `json:"enabled"`
	RoamingAllowed      bool   `json:"roaming_allowed"`
	RemoteWakeEnabled   bool   `json:"remote_wake_enabled"`
	MeteredUploadLimit  bool   `json:"metered_upload_limit"`
	Preference          string `json:"preference"`
	MonthlyQuotaMB      int    `json:"monthly_quota_mb"`
	LowQuotaWarnPercent int    `json:"low_quota_warn_percent"`
	KeepaliveSeconds    int    `json:"keepalive_seconds"`
	PolicyRevision      int    `json:"policy_revision"`
}

// current returns the guardian's cellular policy for the authenticated family.
func (handler connectivityHandler) current(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	policy, err := handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeConnectivityError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, cellularPolicyResponse(policy))
}

// publish validates and stores a new guardian cellular policy.
func (handler connectivityHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload cellularPolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查蜂窝网络设置")
		return
	}
	policy, err := handler.service.Publish(request.Context(), familyID, connectivitydomain.Input{
		Enabled:             payload.Enabled,
		RoamingAllowed:      payload.RoamingAllowed,
		RemoteWakeEnabled:   payload.RemoteWakeEnabled,
		MeteredUploadLimit:  payload.MeteredUploadLimit,
		Preference:          connectivitydomain.Preference(payload.Preference),
		MonthlyQuotaMB:      payload.MonthlyQuotaMB,
		LowQuotaWarnPercent: payload.LowQuotaWarnPercent,
		KeepaliveSeconds:    payload.KeepaliveSeconds,
	})
	if err != nil {
		writeConnectivityError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, cellularPolicyResponse(policy))
}

// capability returns the cellular capability for one family device.
func (handler connectivityHandler) capability(
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
		writeConnectivityError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// statuses returns each family device's latest bounded connectivity status.
func (handler connectivityHandler) statuses(
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
		writeConnectivityError(response, request, err)
		return
	}
	if devices == nil {
		devices = []connectivitydomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

// reportStatus records one bound device's connectivity status.
func (handler deviceConnectivityHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload connectivityStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		connectivitydomain.StatusInput{
			Bearer:         connectivitydomain.Bearer(payload.Bearer),
			Roaming:        payload.Roaming,
			ESIMState:      connectivitydomain.ESIMState(payload.ESIMState),
			SignalPercent:  payload.SignalPercent,
			UsedMB:         payload.UsedMB,
			PolicyRevision: payload.PolicyRevision,
		},
	)
	if err != nil {
		writeConnectivityError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

// devicePolicy returns the family cellular policy to a bound device that holds
// a device session token, so the modem applies the guardian preference and
// quota without an authenticated guardian present.
func (handler deviceConnectivityHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeConnectivityError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, deviceCellularPolicyResponse{
		SchemaVersion:       policy.SchemaVersion,
		Enabled:             policy.Enabled,
		RoamingAllowed:      policy.RoamingAllowed,
		RemoteWakeEnabled:   policy.RemoteWakeEnabled,
		MeteredUploadLimit:  policy.MeteredUploadLimit,
		Preference:          string(policy.Preference),
		MonthlyQuotaMB:      policy.MonthlyQuotaMB,
		LowQuotaWarnPercent: policy.LowQuotaWarnPercent,
		KeepaliveSeconds:    policy.KeepaliveSeconds,
		PolicyRevision:      policy.Revision,
	})
}

// adminStats returns the bounded fleet connectivity summary.
func (handler connectivityHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseConnectivityWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeConnectivityError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func (handler deviceConnectivityHandler) verifyDevice(
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
		writeConnectivityError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func cellularPolicyResponse(policy connectivitydomain.Policy) map[string]any {
	return map[string]any{
		"schema_version":         policy.SchemaVersion,
		"enabled":                policy.Enabled,
		"roaming_allowed":        policy.RoamingAllowed,
		"remote_wake_enabled":    policy.RemoteWakeEnabled,
		"metered_upload_limit":   policy.MeteredUploadLimit,
		"preference":             string(policy.Preference),
		"monthly_quota_mb":       policy.MonthlyQuotaMB,
		"low_quota_warn_percent": policy.LowQuotaWarnPercent,
		"keepalive_seconds":      policy.KeepaliveSeconds,
		"revision":               policy.Revision,
	}
}

func parseConnectivityWindowDays(raw string) int {
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

func writeConnectivityError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, connectivitydomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "蜂窝网络设置超出允许范围")
	case errors.Is(err, connectivitydomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
