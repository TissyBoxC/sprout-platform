package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	contentofflinepolicydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/domain"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
)

// contentOfflinePolicyService is the guardian-facing offline policy surface.
//
// The HTTP layer depends on this narrow interface so the module stays
// removable and tests can substitute a small fake.
type contentOfflinePolicyService interface {
	Current(
		ctx context.Context,
	) (contentofflinepolicydomain.Policy, error)
	Publish(
		ctx context.Context,
		input contentofflinepolicydomain.Input,
	) (contentofflinepolicydomain.Policy, error)
	Acknowledge(
		ctx context.Context,
		familyID string,
		deviceID string,
		revision int,
	) error
	Acknowledgements(
		ctx context.Context,
		familyID string,
	) ([]contentofflinepolicydomain.Acknowledgement, error)
}

type contentOfflinePolicyHandler struct {
	service contentOfflinePolicyService
}

// deviceContentOfflinePolicyHandler hands the fleet policy to a bound device
// behind a device session token rather than a guardian credential.
type deviceContentOfflinePolicyHandler struct {
	handler        *contentOfflinePolicyHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type contentOfflinePolicyPayload struct {
	OfflineEnabled       bool  `json:"offline_enabled"`
	MaxCachedPackages    int   `json:"max_cached_packages"`
	MaxCacheBytes        int64 `json:"max_cache_bytes"`
	CacheTTLHours        int   `json:"cache_ttl_hours"`
	DownloadOverWifiOnly bool  `json:"download_over_wifi_only"`
}

type acknowledgeOfflinePolicyRequest struct {
	Revision int `json:"revision"`
}

func (handler contentOfflinePolicyHandler) current(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, ok := authenticatedAccountID(request); !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	policy, err := handler.service.Current(request.Context())
	if err != nil {
		writeContentOfflinePolicyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, contentOfflinePolicyResponse(policy))
}

// acknowledgements lists the applied revision per family device.
func (handler contentOfflinePolicyHandler) acknowledgements(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	acks, err := handler.service.Acknowledgements(request.Context(), familyID)
	if err != nil {
		writeContentOfflinePolicyError(response, request, err)
		return
	}
	devices := make([]map[string]any, 0, len(acks))
	for index := range acks {
		devices = append(devices, map[string]any{
			"device_id":  acks[index].DeviceID,
			"revision":   acks[index].Revision,
			"up_to_date": acks[index].UpToDate,
		})
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

// adminCurrent is the admin-console variant of current.
func (handler contentOfflinePolicyHandler) adminCurrent(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.current(response, request)
}

// adminPublish is the admin-console variant of publish.
func (handler contentOfflinePolicyHandler) adminPublish(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.publish(response, request)
}

// acknowledgementsForAdmin lists applied revisions for one family. The admin
// console selects the family with a query parameter and the platform scopes
// the read to that family.
func (handler contentOfflinePolicyHandler) acknowledgementsForAdmin(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID := strings.TrimSpace(request.URL.Query().Get("family_id"))
	if familyID == "" {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请选择家庭")
		return
	}
	acks, err := handler.service.Acknowledgements(request.Context(), familyID)
	if err != nil {
		writeContentOfflinePolicyError(response, request, err)
		return
	}
	devices := make([]map[string]any, 0, len(acks))
	for index := range acks {
		devices = append(devices, map[string]any{
			"device_id":  acks[index].DeviceID,
			"revision":   acks[index].Revision,
			"up_to_date": acks[index].UpToDate,
		})
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"family_id": familyID,
		"devices":   devices,
	})
}

func (handler contentOfflinePolicyHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, ok := authenticatedAccountID(request); !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload contentOfflinePolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查离线下载策略")
		return
	}
	policy, err := handler.service.Publish(
		request.Context(),
		contentofflinepolicydomain.Input{
			OfflineEnabled:       payload.OfflineEnabled,
			MaxCachedPackages:    payload.MaxCachedPackages,
			MaxCacheBytes:        payload.MaxCacheBytes,
			CacheTTLHours:        payload.CacheTTLHours,
			DownloadOverWifiOnly: payload.DownloadOverWifiOnly,
		},
	)
	if err != nil {
		writeContentOfflinePolicyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, contentOfflinePolicyResponse(policy))
}

// currentForDevice returns the fleet policy to a bound device.
func (handler deviceContentOfflinePolicyHandler) currentForDevice(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, _, ok := handler.verifyDevice(response, request); !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context())
	if err != nil {
		writeContentOfflinePolicyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, contentOfflinePolicyResponse(policy))
}

// acknowledgeForDevice records the revision a device currently applies.
func (handler deviceContentOfflinePolicyHandler) acknowledgeForDevice(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload acknowledgeOfflinePolicyRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	if err := handler.handler.service.Acknowledge(
		request.Context(),
		familyID,
		deviceID,
		payload.Revision,
	); err != nil {
		writeContentOfflinePolicyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id": deviceID,
		"revision":  payload.Revision,
	})
}

func (handler deviceContentOfflinePolicyHandler) verifyDevice(
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
		writeContentOfflinePolicyError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func contentOfflinePolicyResponse(
	policy contentofflinepolicydomain.Policy,
) map[string]any {
	return map[string]any{
		"offline_enabled":         policy.OfflineEnabled,
		"max_cached_packages":     policy.MaxCachedPackages,
		"max_cache_bytes":         policy.MaxCacheBytes,
		"cache_ttl_hours":         policy.CacheTTLHours,
		"download_over_wifi_only": policy.DownloadOverWifiOnly,
		"revision":                policy.Revision,
	}
}

func writeContentOfflinePolicyError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, contentofflinepolicydomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "离线下载策略超出允许范围")
	case errors.Is(err, contentofflinepolicydomain.ErrInvalidAcceptedRevision):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_revision", "设备上报的版本号无效")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
