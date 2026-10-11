package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/vision"
)

// VisionService is the narrow vision surface required by HTTP transport.
type VisionService interface {
	Analyze(ctx context.Context, request vision.Request) (vision.Result, error)
	GetResult(ctx context.Context, taskID string, deviceID string) (vision.Result, error)
}

type visionAnalyzeRequest struct {
	TaskID      string `json:"task_id"`
	DeviceID    string `json:"device_id"`
	TaskType    string `json:"task_type"`
	Prompt      string `json:"prompt"`
	FrameWidth  int    `json:"frame_width"`
	FrameHeight int    `json:"frame_height"`
	FrameBytes  int    `json:"frame_bytes"`
}

type visionHandler struct {
	service VisionService
}

// analyzeTask handles POST /internal/v1/vision/tasks. The caller is a trusted
// server-side component authenticated with the internal service token; the
// request carries bounded symbolic metadata and never image bytes.
func (handler visionHandler) analyzeTask(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload visionAnalyzeRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		httpapi.WriteError(
			response,
			request,
			http.StatusBadRequest,
			"invalid_vision_task",
			"请检查视觉任务",
			false,
		)
		return
	}
	result, err := handler.service.Analyze(request.Context(), vision.Request{
		TaskID:      strings.TrimSpace(payload.TaskID),
		DeviceID:    strings.TrimSpace(payload.DeviceID),
		TaskType:    vision.TaskType(strings.TrimSpace(payload.TaskType)),
		Prompt:      payload.Prompt,
		FrameWidth:  payload.FrameWidth,
		FrameHeight: payload.FrameHeight,
		FrameBytes:  payload.FrameBytes,
	})
	if err != nil {
		writeVisionError(response, request, err)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, result)
}

// getResult handles GET /internal/v1/vision/results/{task_id}?device_id=...
// The device_id query parameter scopes the lookup so one device cannot read
// another device's result.
func (handler visionHandler) getResult(
	response http.ResponseWriter,
	request *http.Request,
) {
	taskID, err := url.PathUnescape(strings.TrimSpace(request.PathValue("task_id")))
	if err != nil {
		httpapi.WriteError(
			response,
			request,
			http.StatusBadRequest,
			"invalid_vision_task",
			"请检查视觉任务",
			false,
		)
		return
	}
	deviceID := strings.TrimSpace(request.URL.Query().Get("device_id"))
	result, err := handler.service.GetResult(request.Context(), taskID, deviceID)
	if err != nil {
		writeVisionError(response, request, err)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, result)
}

func writeVisionError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, vision.ErrInvalidRequest),
		errors.Is(err, vision.ErrUnsupportedTask):
		httpapi.WriteError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_vision_task",
			"请检查视觉任务",
			false,
		)
	case errors.Is(err, vision.ErrContentRejected):
		httpapi.WriteError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"vision_content_rejected",
			"该内容不适合儿童，请更换任务",
			false,
		)
	case errors.Is(err, vision.ErrModelUnavailable):
		httpapi.WriteError(
			response,
			request,
			http.StatusServiceUnavailable,
			"vision_unavailable",
			"视觉服务暂时不可用，请稍后重试",
			true,
		)
	case errors.Is(err, vision.ErrDeviceNotAuthorized):
		httpapi.WriteError(
			response,
			request,
			http.StatusForbidden,
			"vision_not_authorized",
			"该设备未获得视觉处理授权，请让家长重新确认",
			false,
		)
	case errors.Is(err, vision.ErrResultNotFound):
		httpapi.WriteError(
			response,
			request,
			http.StatusNotFound,
			"vision_result_not_found",
			"视觉结果不存在或已过期",
			false,
		)
	default:
		httpapi.WriteError(
			response,
			request,
			http.StatusInternalServerError,
			"vision_unavailable",
			"视觉服务暂时不可用，请稍后重试",
			true,
		)
	}
}
