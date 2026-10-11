package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	formfactordomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/form_factor/domain"
)

// formFactorService is the form-factor surface. The HTTP layer depends on this
// narrow interface so the module stays removable.
type formFactorService interface {
	Report(
		ctx context.Context,
		familyID string,
		deviceID string,
		input formfactordomain.Input,
	) (formfactordomain.Profile, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (formfactordomain.CapabilitySummary, error)
	DeviceProfiles(
		ctx context.Context,
		familyID string,
	) ([]formfactordomain.DeviceProfile, error)
	AdminStats(ctx context.Context, windowDays int) (formfactordomain.AdminStats, error)
}

type formFactorHandler struct {
	service formFactorService
}

// deviceFormFactorHandler records the composed form-factor profile from a bound
// device.
type deviceFormFactorHandler struct {
	handler        *formFactorHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type formFactorPayload struct {
	TechnicalName  string `json:"technical_name"`
	CapabilityMask uint32 `json:"capability_mask"`
	SchemaVersion  int    `json:"schema_version"`
}

// capability returns the composed form-factor capability for one family device.
func (handler formFactorHandler) capability(
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
		writeFormFactorError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// statuses returns each family device's composed form-factor profile.
func (handler formFactorHandler) statuses(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	devices, err := handler.service.DeviceProfiles(request.Context(), familyID)
	if err != nil {
		writeFormFactorError(response, request, err)
		return
	}
	if devices == nil {
		devices = []formfactordomain.DeviceProfile{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

// report records one bound device's form-factor profile.
func (handler deviceFormFactorHandler) report(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload formFactorPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	profile, err := handler.handler.service.Report(
		request.Context(),
		familyID,
		deviceID,
		formfactordomain.Input{
			TechnicalName:  payload.TechnicalName,
			CapabilityMask: payload.CapabilityMask,
			SchemaVersion:  payload.SchemaVersion,
		},
	)
	if err != nil {
		writeFormFactorError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":      profile.DeviceID,
		"technical_name": profile.TechnicalName,
		"action_mask":    profile.ActionMask,
		"recorded":       true,
	})
}

// adminStats returns the bounded fleet form-factor summary.
func (handler formFactorHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseFormFactorWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeFormFactorError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func (handler deviceFormFactorHandler) verifyDevice(
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
		writeFormFactorError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func parseFormFactorWindowDays(raw string) int {
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

func writeFormFactorError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, formfactordomain.ErrInvalidProfile):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_profile", "形态能力上报无效")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
