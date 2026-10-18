// Package handler maps guardian camera/vision requests onto the vision service.
// It never exposes raw provider errors or image bytes.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/domain"
)

// Service is the narrow vision surface required by HTTP transport.
type Service interface {
	Capability(
		ctx context.Context,
		parentAccountID string,
		deviceID string,
	) (*domain.CapabilityState, error)
	CreateTask(
		ctx context.Context,
		parentAccountID string,
		deviceID string,
		taskType domain.TaskType,
		prompt string,
	) (*domain.Task, error)
	ListTasks(
		ctx context.Context,
		parentAccountID string,
		deviceID string,
		limit int,
	) ([]domain.Task, error)
	ListResults(
		ctx context.Context,
		parentAccountID string,
		deviceID string,
		limit int,
	) ([]domain.Result, error)
	GetResult(
		ctx context.Context,
		parentAccountID string,
		taskID string,
	) (*domain.Result, error)
	CancelTask(
		ctx context.Context,
		parentAccountID string,
		taskID string,
	) (*domain.Task, error)
	AdminStats(
		ctx context.Context,
		windowDays int,
	) (*domain.AdminStats, error)
}

// Handler exposes guardian camera/vision endpoints.
type Handler struct {
	service Service
}

// New creates a vision HTTP handler.
func New(service Service) *Handler {
	return &Handler{service: service}
}

type createTaskRequest struct {
	TaskType string `json:"task_type"`
	Prompt   string `json:"prompt"`
}

// GetCapability handles GET /api/v1/devices/{device_id}/vision/capability.
func (handler *Handler) GetCapability(
	response http.ResponseWriter,
	request *http.Request,
	accountID string,
) {
	if strings.TrimSpace(accountID) == "" {
		httpapi.WriteError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录", false)
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	state, err := handler.service.Capability(request.Context(), accountID, deviceID)
	if err != nil {
		writeVisionError(response, request, err)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, state)
}

// CreateTask handles POST /api/v1/devices/{device_id}/vision/tasks. The
// calling transport resolves and passes the authenticated parent account.
func (handler *Handler) CreateTask(
	response http.ResponseWriter,
	request *http.Request,
	accountID string,
) {
	if strings.TrimSpace(accountID) == "" {
		httpapi.WriteError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录", false)
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	var payload createTaskRequest
	if err := decodeRequestJSON(request, &payload); err != nil {
		httpapi.WriteError(response, request, http.StatusBadRequest, "invalid_request", "请求内容不正确", false)
		return
	}
	task, err := handler.service.CreateTask(
		request.Context(),
		accountID,
		deviceID,
		domain.TaskType(strings.TrimSpace(payload.TaskType)),
		payload.Prompt,
	)
	if err != nil {
		writeVisionError(response, request, err)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusCreated, task)
}

// ListTasks handles GET /api/v1/devices/{device_id}/vision/tasks.
func (handler *Handler) ListTasks(
	response http.ResponseWriter,
	request *http.Request,
	accountID string,
) {
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	limit := parseVisionLimit(request.URL.Query().Get("limit"))
	tasks, err := handler.service.ListTasks(
		request.Context(),
		accountID,
		deviceID,
		limit,
	)
	if err != nil {
		writeVisionError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(tasks))
	for index := range tasks {
		result = append(result, taskResponse(&tasks[index]))
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, map[string]any{"tasks": result})
}

// CancelTask handles POST /api/v1/devices/{device_id}/vision/tasks/{task_id}/cancel.
func (handler *Handler) CancelTask(
	response http.ResponseWriter,
	request *http.Request,
	accountID string,
) {
	if strings.TrimSpace(accountID) == "" {
		httpapi.WriteError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录", false)
		return
	}
	taskID := strings.TrimSpace(request.PathValue("task_id"))
	task, err := handler.service.CancelTask(request.Context(), accountID, taskID)
	if err != nil {
		writeVisionError(response, request, err)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, taskResponse(task))
}

// ListResults handles GET /api/v1/devices/{device_id}/vision/results.
func (handler *Handler) ListResults(
	response http.ResponseWriter,
	request *http.Request,
	accountID string,
) {
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	limit := parseVisionLimit(request.URL.Query().Get("limit"))
	results, err := handler.service.ListResults(
		request.Context(),
		accountID,
		deviceID,
		limit,
	)
	if err != nil {
		writeVisionError(response, request, err)
		return
	}
	httpapi.WriteSuccess(
		response,
		request,
		http.StatusOK,
		map[string]any{"results": results},
	)
}

// GetResult handles GET /api/v1/devices/{device_id}/vision/results/{task_id}.
func (handler *Handler) GetResult(
	response http.ResponseWriter,
	request *http.Request,
	accountID string,
) {
	taskID := strings.TrimSpace(request.PathValue("task_id"))
	result, err := handler.service.GetResult(request.Context(), accountID, taskID)
	if err != nil {
		writeVisionError(response, request, err)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, result)
}

func decodeRequestJSON(request *http.Request, destination any) error {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

// GetAdminStats handles GET /api/v1/admin/vision/stats. The route is mounted
// behind requireAdmin.
func (handler *Handler) GetAdminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseSummaryWindowDays(
		request.URL.Query().Get("window_days"),
	)
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeVisionError(response, request, err)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, stats)
}

func parseSummaryWindowDays(raw string) int {
	const (
		defaultWindowDays = 7
		maxWindowDays     = 90
	)
	if raw == "" {
		return defaultWindowDays
	}
	value := 0
	for _, character := range raw {
		if character < '0' || character > '9' {
			return defaultWindowDays
		}
		value = value*10 + int(character-'0')
		if value > maxWindowDays {
			return maxWindowDays
		}
	}
	if value <= 0 {
		return defaultWindowDays
	}
	return value
}

func taskResponse(task *domain.Task) map[string]any {
	if task == nil {
		return nil
	}
	return map[string]any{
		"task_id":      task.ID,
		"device_id":    task.DeviceID,
		"task_type":    task.TaskType,
		"status":       task.Status,
		"prompt":       task.Prompt,
		"frame_width":  task.FrameWidth,
		"frame_height": task.FrameHeight,
		"frame_bytes":  task.FrameBytes,
		"failure_code": task.FailureCode,
		"created_at":   task.CreatedAt,
		"completed_at": task.CompletedAt,
		"expires_at":   task.ExpiresAt,
	}
}

func parseVisionLimit(raw string) int {
	const (
		defaultLimit = 20
		maxLimit     = 100
	)
	if raw == "" {
		return defaultLimit
	}
	value := 0
	for _, character := range raw {
		if character < '0' || character > '9' {
			return defaultLimit
		}
		value = value*10 + int(character-'0')
		if value > maxLimit {
			return maxLimit
		}
	}
	if value <= 0 {
		return defaultLimit
	}
	return value
}

func writeVisionError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, domain.ErrPrivacyNotGranted):
		httpapi.WriteError(
			response,
			request,
			http.StatusForbidden,
			"guardian_consent_required",
			"需要先获得监护人授权",
			false,
		)
	case errors.Is(err, domain.ErrTaskNotFound),
		errors.Is(err, domain.ErrResultNotFound):
		httpapi.WriteError(
			response,
			request,
			http.StatusNotFound,
			"not_found",
			"没有找到对应的记录",
			false,
		)
	case errors.Is(err, domain.ErrInvalidTask),
		errors.Is(err, domain.ErrTaskNotPending),
		errors.Is(err, domain.ErrUnsupportedTask):
		httpapi.WriteError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_request",
			"请求内容不正确",
			false,
		)
	default:
		httpapi.WriteError(
			response,
			request,
			http.StatusInternalServerError,
			"service_error",
			"操作没有完成，请稍后重试",
			false,
		)
	}
}
