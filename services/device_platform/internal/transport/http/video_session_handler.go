package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	videosessiondomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_session/domain"
)

// videoSessionService is the guardian-facing video-session surface. The HTTP
// layer depends on this narrow interface so the module stays removable.
type videoSessionService interface {
	Catalog() ([]string, []string)
	Current(
		ctx context.Context,
		familyID string,
	) (videosessiondomain.Policy, error)
	Publish(
		ctx context.Context,
		familyID string,
		input videosessiondomain.Input,
	) (videosessiondomain.Policy, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (videosessiondomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input videosessiondomain.StatusInput,
	) (videosessiondomain.Status, error)
	DeviceStatuses(
		ctx context.Context,
		familyID string,
	) ([]videosessiondomain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (videosessiondomain.AdminStats, error)
}

type videoSessionHandler struct {
	service videoSessionService
}

// deviceVideoSessionHandler serves the family media policy and records bounded
// session status for a bound device that holds a device session token.
type deviceVideoSessionHandler struct {
	handler        *videoSessionHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type videoSessionPolicyPayload struct {
	Enabled             bool   `json:"enabled"`
	CameraAllowed       bool   `json:"camera_allowed"`
	MicrophoneAllowed   bool   `json:"microphone_allowed"`
	RequireChildConsent bool   `json:"require_child_consent"`
	QualityFloor        string `json:"quality_floor"`
	RingTimeoutSeconds  int    `json:"ring_timeout_seconds"`
	MaxSessionSeconds   int    `json:"max_session_seconds"`
}

type videoSessionStatusPayload struct {
	SessionSupported  bool   `json:"session_supported"`
	CameraAllowed     bool   `json:"camera_allowed"`
	MicrophoneAllowed bool   `json:"microphone_allowed"`
	CallState         string `json:"call_state"`
	Quality           string `json:"quality"`
	LastEndReason     string `json:"last_end_reason"`
	PolicyRevision    int    `json:"policy_revision"`
}

// current returns the guardian's video-session policy for the authenticated
// family together with the fixed quality and state catalogs.
func (handler videoSessionHandler) current(
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
		writeVideoSessionError(response, request, err)
		return
	}
	qualities, states := handler.service.Catalog()
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"policy":    videoSessionPolicyResponse(policy),
		"qualities": qualities,
		"states":    states,
	})
}

// publish validates and stores a new guardian media policy.
func (handler videoSessionHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload videoSessionPolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查视频通话设置")
		return
	}
	policy, err := handler.service.Publish(
		request.Context(),
		familyID,
		videosessiondomain.Input{
			Enabled:             payload.Enabled,
			CameraAllowed:       payload.CameraAllowed,
			MicrophoneAllowed:   payload.MicrophoneAllowed,
			RequireChildConsent: payload.RequireChildConsent,
			QualityFloor:        payload.QualityFloor,
			RingTimeoutSeconds:  payload.RingTimeoutSeconds,
			MaxSessionSeconds:   payload.MaxSessionSeconds,
		},
	)
	if err != nil {
		writeVideoSessionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, videoSessionPolicyResponse(policy))
}

// statuses returns each family device's latest bounded session status.
func (handler videoSessionHandler) statuses(
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
		writeVideoSessionError(response, request, err)
		return
	}
	if devices == nil {
		devices = []videosessiondomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": devices})
}

// capability returns the video-session capability for one family device.
func (handler videoSessionHandler) capability(
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
		writeVideoSessionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// adminStats returns the bounded fleet video-session summary.
func (handler videoSessionHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseVideoSessionWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeVideoSessionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

// devicePolicy returns the family media policy to a bound device over its
// device session token so the firmware can clamp and apply it.
func (handler deviceVideoSessionHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeVideoSessionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK,
		videoSessionPolicyResponse(policy))
}

// reportStatus records one bound device's session status.
func (handler deviceVideoSessionHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload videoSessionStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		videosessiondomain.StatusInput{
			SessionSupported:  payload.SessionSupported,
			CameraAllowed:     payload.CameraAllowed,
			MicrophoneAllowed: payload.MicrophoneAllowed,
			CallState:         payload.CallState,
			Quality:           payload.Quality,
			LastEndReason:     payload.LastEndReason,
			PolicyRevision:    payload.PolicyRevision,
		},
	)
	if err != nil {
		writeVideoSessionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

func (handler deviceVideoSessionHandler) verifyDevice(
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
		writeVideoSessionError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func videoSessionPolicyResponse(
	policy videosessiondomain.Policy,
) map[string]any {
	return map[string]any{
		"schema_version":        policy.SchemaVersion,
		"enabled":               policy.Enabled,
		"camera_allowed":        policy.CameraAllowed,
		"microphone_allowed":    policy.MicrophoneAllowed,
		"require_child_consent": policy.RequireChildConsent,
		"quality_floor":         policy.QualityFloor,
		"ring_timeout_seconds":  policy.RingTimeoutSeconds,
		"max_session_seconds":   policy.MaxSessionSeconds,
		"revision":              policy.Revision,
	}
}

func parseVideoSessionWindowDays(raw string) int {
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

func writeVideoSessionError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, videosessiondomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "视频通话设置超出允许范围")
	case errors.Is(err, videosessiondomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
