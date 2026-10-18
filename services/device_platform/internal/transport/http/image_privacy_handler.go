package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	imageprivacydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/image_privacy/domain"
)

// imagePrivacyService is the guardian-facing image privacy surface. The HTTP
// layer depends on this narrow interface so the module stays removable.
type imagePrivacyService interface {
	Catalog() ([]string, []string, []string, []string)
	Current(
		ctx context.Context,
		familyID string,
	) (imageprivacydomain.Policy, error)
	Publish(
		ctx context.Context,
		familyID string,
		input imageprivacydomain.Input,
	) (imageprivacydomain.Policy, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (imageprivacydomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input imageprivacydomain.StatusInput,
	) (imageprivacydomain.Status, error)
	DeviceStatuses(
		ctx context.Context,
		familyID string,
	) ([]imageprivacydomain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (imageprivacydomain.AdminStats, error)
}

type imagePrivacyHandler struct {
	service imagePrivacyService
}

// deviceImagePrivacyHandler serves the family policy and records bounded camera
// pipeline status for a bound device that holds a device session token.
type deviceImagePrivacyHandler struct {
	handler        *imagePrivacyHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type imagePrivacyPolicyPayload struct {
	Enabled             bool   `json:"enabled"`
	UploadAllowed       bool   `json:"upload_allowed"`
	LocalProcessingOnly bool   `json:"local_processing_only"`
	ProcessingMode      string `json:"processing_mode"`
	UploadScope         string `json:"upload_scope"`
	RetentionDays       int    `json:"retention_days"`
	RedactionLevel      string `json:"redaction_level"`
	RequireChildConsent bool   `json:"require_child_consent"`
}

type imagePrivacyStatusPayload struct {
	CameraSupported bool   `json:"camera_supported"`
	LocalProcessing bool   `json:"local_processing"`
	UploadAllowed   bool   `json:"upload_allowed"`
	ProcessingMode  string `json:"processing_mode"`
	UploadScope     string `json:"upload_scope"`
	RedactionLevel  string `json:"redaction_level"`
	ProcessedCount  int    `json:"processed_count"`
	UploadedCount   int    `json:"uploaded_count"`
	BlockedCount    int    `json:"blocked_count"`
	LastBlockReason string `json:"last_block_reason"`
	PolicyRevision  int    `json:"policy_revision"`
}

// current returns the guardian's image privacy policy for the authenticated
// family together with the fixed mode, scope, redaction and reason catalogs.
func (handler imagePrivacyHandler) current(
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
		writeImagePrivacyError(response, request, err)
		return
	}
	processingModes, uploadScopes, redactionLevels, blockReasons := handler.service.Catalog()
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"policy":           imagePrivacyPolicyResponse(policy),
		"processing_modes": processingModes,
		"upload_scopes":    uploadScopes,
		"redaction_levels": redactionLevels,
		"block_reasons":    blockReasons,
	})
}

// publish validates and stores a new guardian image privacy policy.
func (handler imagePrivacyHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload imagePrivacyPolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查图片隐私设置")
		return
	}
	policy, err := handler.service.Publish(
		request.Context(),
		familyID,
		imageprivacydomain.Input{
			Enabled:             payload.Enabled,
			UploadAllowed:       payload.UploadAllowed,
			LocalProcessingOnly: payload.LocalProcessingOnly,
			ProcessingMode:      payload.ProcessingMode,
			UploadScope:         payload.UploadScope,
			RetentionDays:       payload.RetentionDays,
			RedactionLevel:      payload.RedactionLevel,
			RequireChildConsent: payload.RequireChildConsent,
		},
	)
	if err != nil {
		writeImagePrivacyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, imagePrivacyPolicyResponse(policy))
}

// statuses returns each family device's latest bounded camera pipeline status.
func (handler imagePrivacyHandler) statuses(
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
		writeImagePrivacyError(response, request, err)
		return
	}
	if devices == nil {
		devices = []imageprivacydomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": devices})
}

// capability returns the image pipeline capability for one family device.
func (handler imagePrivacyHandler) capability(
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
		writeImagePrivacyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// adminStats returns the bounded fleet image privacy summary.
func (handler imagePrivacyHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseImagePrivacyWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeImagePrivacyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

// devicePolicy returns the family image privacy policy to a bound device over
// its device session token so the firmware can clamp and apply it.
func (handler deviceImagePrivacyHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeImagePrivacyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK,
		imagePrivacyPolicyResponse(policy))
}

// reportStatus records one bound device's camera pipeline status.
func (handler deviceImagePrivacyHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload imagePrivacyStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		imageprivacydomain.StatusInput{
			CameraSupported: payload.CameraSupported,
			LocalProcessing: payload.LocalProcessing,
			UploadAllowed:   payload.UploadAllowed,
			ProcessingMode:  payload.ProcessingMode,
			UploadScope:     payload.UploadScope,
			RedactionLevel:  payload.RedactionLevel,
			ProcessedCount:  payload.ProcessedCount,
			UploadedCount:   payload.UploadedCount,
			BlockedCount:    payload.BlockedCount,
			LastBlockReason: payload.LastBlockReason,
			PolicyRevision:  payload.PolicyRevision,
		},
	)
	if err != nil {
		writeImagePrivacyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"processing_mode": status.ProcessingMode,
		"upload_scope":    status.UploadScope,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

func (handler deviceImagePrivacyHandler) verifyDevice(
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
		writeImagePrivacyError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func imagePrivacyPolicyResponse(
	policy imageprivacydomain.Policy,
) map[string]any {
	return map[string]any{
		"schema_version":        policy.SchemaVersion,
		"enabled":               policy.Enabled,
		"upload_allowed":        policy.UploadAllowed,
		"local_processing_only": policy.LocalProcessingOnly,
		"processing_mode":       policy.ProcessingMode,
		"upload_scope":          policy.UploadScope,
		"retention_days":        policy.RetentionDays,
		"redaction_level":       policy.RedactionLevel,
		"require_child_consent": policy.RequireChildConsent,
		"revision":              policy.Revision,
	}
}

func parseImagePrivacyWindowDays(raw string) int {
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

func writeImagePrivacyError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, imageprivacydomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "图片隐私设置超出允许范围")
	case errors.Is(err, imageprivacydomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
