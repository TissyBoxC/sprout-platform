package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	playbackdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/domain"
)

// playbackService is the guardian-facing on-demand playback surface.
//
// The HTTP layer depends on this narrow interface so the module stays
// removable and tests can substitute a small fake.
type playbackService interface {
	Create(
		ctx context.Context,
		familyID string,
		input playbackdomain.RequestInput,
	) (*playbackdomain.Request, error)
	List(
		ctx context.Context,
		familyID string,
		query playbackdomain.RequestQuery,
	) ([]playbackdomain.Request, error)
	Get(
		ctx context.Context,
		familyID string,
		requestID string,
	) (*playbackdomain.Request, error)
	Cancel(
		ctx context.Context,
		familyID string,
		requestID string,
	) (*playbackdomain.Request, error)
	ListPendingForDevice(
		ctx context.Context,
		deviceID string,
	) ([]playbackdomain.Request, error)
	Acknowledge(
		ctx context.Context,
		deviceID string,
		requestID string,
	) (*playbackdomain.Request, error)
	Complete(
		ctx context.Context,
		deviceID string,
		requestID string,
		outcome string,
	) (*playbackdomain.Request, error)
}

type playbackHandler struct {
	service playbackService
}

// devicePlaybackHandler wraps the device-facing reads and writes with device
// session verification so firmware can drain and report requests.
type devicePlaybackHandler struct {
	playbackHandler *playbackHandler
	bindingService  englishSessionResolver
	runtimeService  *runtimeservice.Service
}

type playbackRequestPayload struct {
	DeviceID  string `json:"device_id"`
	PackageID string `json:"package_id"`
	Note      string `json:"note"`
}

type playbackOutcomePayload struct {
	Outcome string `json:"outcome"`
}

func (handler playbackHandler) create(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload playbackRequestPayload
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查点播内容")
		return
	}
	created, err := handler.service.Create(
		request.Context(),
		familyID,
		playbackdomain.RequestInput{
			DeviceID:  payload.DeviceID,
			PackageID: payload.PackageID,
			Note:      payload.Note,
		},
	)
	if err != nil {
		writePlaybackError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"request": playbackRequestResponse(created),
	})
}

func (handler playbackHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	query := playbackdomain.RequestQuery{
		DeviceID: strings.TrimSpace(request.URL.Query().Get("device_id")),
		Status:   strings.TrimSpace(request.URL.Query().Get("status")),
		Limit:    parsePositiveQueryInt(request.URL.Query().Get("limit"), 0),
	}
	requests, err := handler.service.List(request.Context(), familyID, query)
	if err != nil {
		writePlaybackError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"requests": playbackRequestListResponse(requests),
	})
}

func (handler playbackHandler) get(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	requestID := strings.TrimSpace(request.PathValue("request_id"))
	found, err := handler.service.Get(request.Context(), familyID, requestID)
	if err != nil {
		writePlaybackError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"request": playbackRequestResponse(found),
	})
}

func (handler playbackHandler) cancel(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	requestID := strings.TrimSpace(request.PathValue("request_id"))
	cancelled, err := handler.service.Cancel(request.Context(), familyID, requestID)
	if err != nil {
		writePlaybackError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"request": playbackRequestResponse(cancelled),
	})
}

// listPending returns the requests a device should act on, oldest first.
func (handler devicePlaybackHandler) listPending(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	requests, err := handler.playbackHandler.service.ListPendingForDevice(
		request.Context(),
		deviceID,
	)
	if err != nil {
		writePlaybackError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"requests": playbackRequestListResponse(requests),
	})
}

// acknowledge marks a device-owned request acknowledged.
func (handler devicePlaybackHandler) acknowledge(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	requestID := strings.TrimSpace(request.PathValue("request_id"))
	acknowledged, err := handler.playbackHandler.service.Acknowledge(
		request.Context(),
		deviceID,
		requestID,
	)
	if err != nil {
		writePlaybackError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"request": playbackRequestResponse(acknowledged),
	})
}

// complete records the terminal outcome for a device-owned request.
func (handler devicePlaybackHandler) complete(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	requestID := strings.TrimSpace(request.PathValue("request_id"))
	var payload playbackOutcomePayload
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查播放结果")
		return
	}
	completed, err := handler.playbackHandler.service.Complete(
		request.Context(),
		deviceID,
		requestID,
		payload.Outcome,
	)
	if err != nil {
		writePlaybackError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"request": playbackRequestResponse(completed),
	})
}

func (handler devicePlaybackHandler) verifyDevice(
	response http.ResponseWriter,
	request *http.Request,
) (string, bool) {
	if handler.bindingService == nil || handler.runtimeService == nil {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", false
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", false
	}
	pathDeviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionDeviceID, err := handler.bindingService.VerifyDeviceSession(
		request.Context(),
		deviceSessionToken,
	)
	if err != nil || sessionDeviceID != pathDeviceID {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", false
	}
	return pathDeviceID, true
}

func playbackRequestResponse(item *playbackdomain.Request) map[string]any {
	response := map[string]any{
		"id":           item.ID,
		"device_id":    item.DeviceID,
		"package_id":   item.PackageID,
		"source":       item.Source,
		"status":       item.Status,
		"note":         item.Note,
		"requested_at": item.RequestedAt.UTC().Format(time.RFC3339),
	}
	if item.AcknowledgedAt != nil {
		response["acknowledged_at"] = item.AcknowledgedAt.UTC().Format(time.RFC3339)
	}
	if item.CompletedAt != nil {
		response["completed_at"] = item.CompletedAt.UTC().Format(time.RFC3339)
	}
	if item.Outcome != "" {
		response["outcome"] = item.Outcome
	}
	return response
}

func playbackRequestListResponse(items []playbackdomain.Request) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for index := range items {
		result = append(result, playbackRequestResponse(&items[index]))
	}
	return result
}

func writePlaybackError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, playbackdomain.ErrRequestNotFound):
		writeError(response, request, http.StatusNotFound, "request_not_found", "没有找到这条点播记录")
	case errors.Is(err, playbackdomain.ErrInvalidPackageID):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_package", "这个内容暂时不能点播")
	case errors.Is(err, playbackdomain.ErrInvalidDeviceID):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_device", "请选择已绑定的设备")
	case errors.Is(err, playbackdomain.ErrInvalidNote):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_note", "留言不能超过 200 个字")
	case errors.Is(err, playbackdomain.ErrInvalidOutcome):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_outcome", "请检查播放结果")
	case errors.Is(err, playbackdomain.ErrDuplicateRequest):
		writeError(response, request, http.StatusConflict, "duplicate_request", "这台设备已经在播放这条内容了")
	case errors.Is(err, playbackdomain.ErrInvalidRequestState):
		writeError(response, request, http.StatusConflict, "invalid_state", "这条点播已经结束，无法再修改")
	case errors.Is(err, playbackdomain.ErrInvalidRequest):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查点播内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
