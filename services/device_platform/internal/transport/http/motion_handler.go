package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	motiondomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/motion/domain"
)

// motionService is the guardian-facing motion policy surface. The HTTP layer
// depends on this narrow interface so the module stays removable.
type motionService interface {
	Current(ctx context.Context, familyID string) (motiondomain.Policy, error)
	Publish(
		ctx context.Context,
		familyID string,
		input motiondomain.Input,
	) (motiondomain.Policy, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (motiondomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input motiondomain.StatusInput,
	) (motiondomain.Status, error)
	DeviceStatuses(ctx context.Context, familyID string) ([]motiondomain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (motiondomain.AdminStats, error)
}

type motionHandler struct {
	service motionService
}

// deviceMotionHandler records bounded motion status from a bound device.
type deviceMotionHandler struct {
	handler        *motionHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type motionPolicyPayload struct {
	Enabled         bool `json:"enabled"`
	RequireStopAck  bool `json:"require_stop_ack"`
	AllowLocomotion bool `json:"allow_locomotion"`
	MaxSpeedPercent int  `json:"max_speed_percent"`
	MaxRuntimeMS    int  `json:"max_runtime_ms"`
	StopGraceMS     int  `json:"stop_grace_ms"`
}

type motionStatusPayload struct {
	State          string `json:"state"`
	Action         string `json:"action"`
	ActuatorCount  int    `json:"actuator_count"`
	Faulted        bool   `json:"faulted"`
	PolicyRevision int    `json:"policy_revision"`
}

// current returns the guardian's motion policy for the authenticated family.
func (handler motionHandler) current(
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
		writeMotionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, motionPolicyResponse(policy))
}

// publish validates and stores a new guardian motion policy.
func (handler motionHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload motionPolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查动作设置")
		return
	}
	policy, err := handler.service.Publish(request.Context(), familyID, motiondomain.Input{
		Enabled:         payload.Enabled,
		RequireStopAck:  payload.RequireStopAck,
		AllowLocomotion: payload.AllowLocomotion,
		MaxSpeedPercent: payload.MaxSpeedPercent,
		MaxRuntimeMS:    payload.MaxRuntimeMS,
		StopGraceMS:     payload.StopGraceMS,
	})
	if err != nil {
		writeMotionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, motionPolicyResponse(policy))
}

// capability returns the motion capability for one family device.
func (handler motionHandler) capability(
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
		writeMotionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// statuses returns each family device's latest bounded motion status.
func (handler motionHandler) statuses(
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
		writeMotionError(response, request, err)
		return
	}
	if devices == nil {
		devices = []motiondomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

// reportStatus records one bound device's motion status.
func (handler deviceMotionHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload motionStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		motiondomain.StatusInput{
			State:          motiondomain.State(payload.State),
			Action:         motiondomain.Action(payload.Action),
			ActuatorCount:  payload.ActuatorCount,
			Faulted:        payload.Faulted,
			PolicyRevision: payload.PolicyRevision,
		},
	)
	if err != nil {
		writeMotionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

// devicePolicy returns the family motion policy to a bound device over its
// device session token so the firmware can clamp and apply it, mirroring the
// other device policy endpoints.
func (handler deviceMotionHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeMotionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, motionPolicyResponse(policy))
}

// adminStats returns the bounded fleet motion summary.
func (handler motionHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseMotionWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeMotionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func (handler deviceMotionHandler) verifyDevice(
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
		writeMotionError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func motionPolicyResponse(policy motiondomain.Policy) map[string]any {
	return map[string]any{
		"schema_version":    policy.SchemaVersion,
		"enabled":           policy.Enabled,
		"require_stop_ack":  policy.RequireStopAck,
		"allow_locomotion":  policy.AllowLocomotion,
		"max_speed_percent": policy.MaxSpeedPercent,
		"max_runtime_ms":    policy.MaxRuntimeMS,
		"stop_grace_ms":     policy.StopGraceMS,
		"revision":          policy.Revision,
	}
}

func parseMotionWindowDays(raw string) int {
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

func writeMotionError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, motiondomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "动作设置超出允许范围")
	case errors.Is(err, motiondomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
