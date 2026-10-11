package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/domain"
)

// deviceVisionService is the narrow device-facing vision surface. It exposes
// only the operations a bound device performs: reading its pending tasks and
// reporting a bounded result or failure.
type deviceVisionService interface {
	ListTasks(
		ctx context.Context,
		parentAccountID string,
		deviceID string,
		limit int,
	) ([]domain.Task, error)
	ListPendingTasksForDevice(
		ctx context.Context,
		deviceID string,
		limit int,
	) ([]domain.Task, error)
	MarkDispatched(ctx context.Context, taskID string) (*domain.Task, error)
	CompleteTask(
		ctx context.Context,
		taskID string,
		width int,
		height int,
		byteSize int,
		labels []domain.ResultLabel,
		summary string,
	) (*domain.Result, error)
	FailTask(
		ctx context.Context,
		taskID string,
		failureCode string,
	) (*domain.Task, error)
}

type deviceSessionVerifier interface {
	VerifyDeviceSession(ctx context.Context, deviceSessionToken string) (string, error)
}

type deviceFamilyResolver interface {
	ResolveDeviceFamily(
		ctx context.Context,
		deviceSessionToken string,
		deviceID string,
	) (string, error)
}

type deviceVisionHandler struct {
	service        deviceVisionService
	bindingService deviceSessionVerifier
	runtimeService deviceFamilyResolver
}

type visionResultRequest struct {
	TaskID   string `json:"task_id"`
	Width    int    `json:"frame_width"`
	Height   int    `json:"frame_height"`
	ByteSize int    `json:"frame_bytes"`
	Summary  string `json:"summary"`
	Labels   []struct {
		Label      string `json:"label"`
		Confidence int    `json:"confidence"`
	} `json:"labels"`
}

type visionTaskStatusRequest struct {
	Status      string `json:"status"`
	FailureCode string `json:"failure_code"`
}

// listPendingTasks handles GET
// /api/v1/devices/{device_id}/vision/tasks/pending.
func (handler deviceVisionHandler) listPendingTasks(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, _, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	tasks, err := handler.service.ListPendingTasksForDevice(
		request.Context(),
		deviceID,
		20,
	)
	if err != nil {
		writeDeviceVisionError(response, request, err)
		return
	}
	pending := make([]map[string]any, 0, len(tasks))
	for index := range tasks {
		pending = append(pending, deviceVisionTaskResponse(&tasks[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"tasks": pending,
	})
}

// acknowledgeTask handles POST
// /api/v1/devices/{device_id}/vision/tasks/{task_id}/acknowledge.
func (handler deviceVisionHandler) acknowledgeTask(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, _, ok := handler.verifyDevice(response, request); !ok {
		return
	}
	task, err := handler.service.MarkDispatched(
		request.Context(),
		strings.TrimSpace(request.PathValue("task_id")),
	)
	if err != nil {
		writeDeviceVisionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, deviceVisionTaskResponse(task))
}

// reportResult handles POST /api/v1/devices/{device_id}/vision/results. Only
// bounded symbolic labels and metadata are accepted; image bytes are rejected
// by construction because the payload has no field for them.
func (handler deviceVisionHandler) reportResult(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, _, ok := handler.verifyDevice(response, request); !ok {
		return
	}
	var payload visionResultRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求内容不正确")
		return
	}
	labels := make([]domain.ResultLabel, 0, len(payload.Labels))
	for _, label := range payload.Labels {
		labels = append(labels, domain.ResultLabel{
			Label:      label.Label,
			Confidence: label.Confidence,
		})
	}
	result, err := handler.service.CompleteTask(
		request.Context(),
		strings.TrimSpace(payload.TaskID),
		payload.Width,
		payload.Height,
		payload.ByteSize,
		labels,
		payload.Summary,
	)
	if err != nil {
		writeDeviceVisionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, result)
}

// reportStatus handles POST
// /api/v1/devices/{device_id}/vision/tasks/{task_id}/status for failures.
func (handler deviceVisionHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, _, ok := handler.verifyDevice(response, request); !ok {
		return
	}
	var payload visionTaskStatusRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求内容不正确")
		return
	}
	if strings.TrimSpace(payload.Status) != string(domain.TaskStatusFailed) {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请求内容不正确")
		return
	}
	task, err := handler.service.FailTask(
		request.Context(),
		strings.TrimSpace(request.PathValue("task_id")),
		payload.FailureCode,
	)
	if err != nil {
		writeDeviceVisionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, deviceVisionTaskResponse(task))
}

func (handler deviceVisionHandler) verifyDevice(
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
		writeDeviceVisionError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func deviceVisionTaskResponse(task *domain.Task) map[string]any {
	if task == nil {
		return nil
	}
	return map[string]any{
		"task_id":      task.ID,
		"device_id":    task.DeviceID,
		"task_type":    task.TaskType,
		"status":       task.Status,
		"prompt":       task.Prompt,
		"failure_code": task.FailureCode,
		"created_at":   task.CreatedAt,
		"expires_at":   task.ExpiresAt,
	}
}

func writeDeviceVisionError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, domain.ErrTaskNotFound),
		errors.Is(err, domain.ErrResultNotFound):
		writeError(response, request, http.StatusNotFound, "not_found", "没有找到对应的任务")
	case errors.Is(err, domain.ErrTaskNotPending),
		errors.Is(err, domain.ErrInvalidTask):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请求内容不正确")
	case errors.Is(err, domain.ErrPrivacyNotGranted):
		writeError(response, request, http.StatusForbidden, "guardian_consent_required", "需要先获得监护人授权")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
