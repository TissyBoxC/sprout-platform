// Package http exposes device-to-child binding to guardians and devices.
//
// Guardian routes are scoped to a family the caller belongs to; the device
// route answers with the device's own active child using a device session.
package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/devicechild/domain"
)

// ErrUnknownChild is re-exported for transport-level mapping.
var ErrUnknownChild = errors.New("child does not belong to the device family")

// ErrNothingToUnbind is re-exported for transport-level mapping.
var ErrNothingToUnbind = errors.New("device has no active child")

// bindingService is the slice the routes need.
type bindingService interface {
	Bind(ctx context.Context, request domain.Request) (domain.Binding, error)
	Resolve(ctx context.Context, deviceID string) (domain.Resolution, error)
	Unbind(ctx context.Context, deviceID string) error
	History(ctx context.Context, deviceID string) ([]domain.History, error)
}

// guardianBindingHandler serves binding management for one family's device.
type guardianBindingHandler struct {
	service bindingService
}

// history answers with the children this device has served.
func (handler guardianBindingHandler) history(
	response http.ResponseWriter,
	request *http.Request,
) {
	entries, err := handler.service.History(
		request.Context(),
		request.PathValue("device_id"),
	)
	if err != nil {
		writeBindingError(response, request, err)
		return
	}
	history := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		item := map[string]any{
			"child_id":     entry.ChildID,
			"is_active":    entry.IsActive,
			"activated_at": entry.ActivatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if entry.DeactivatedAt != nil {
			item["deactivated_at"] = entry.DeactivatedAt.UTC().Format(
				"2006-01-02T15:04:05Z",
			)
		}
		history = append(history, item)
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"history": history,
	})
}

// activate makes a child the device's current child.
func (handler guardianBindingHandler) activate(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		ChildID string `json:"child_id"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		writeError(
			response,
			request,
			http.StatusBadRequest,
			"invalid_request",
			"请选择要使用的儿童",
		)
		return
	}
	binding, err := handler.service.Bind(request.Context(), domain.Request{
		FamilyID: request.PathValue("family_id"),
		DeviceID: request.PathValue("device_id"),
		ChildID:  payload.ChildID,
	})
	if err != nil {
		writeBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"binding": map[string]any{
			"device_id":    binding.DeviceID,
			"child_id":     binding.ChildID,
			"activated_at": binding.ActivatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		},
	})
}

// clear removes the device's current child.
func (handler guardianBindingHandler) clear(
	response http.ResponseWriter,
	request *http.Request,
) {
	if err := handler.service.Unbind(
		request.Context(),
		request.PathValue("device_id"),
	); err != nil {
		writeBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id": request.PathValue("device_id"),
	})
}

// deviceChildBindingHandler answers a device session with its active child.
type deviceChildBindingHandler struct {
	service bindingService
}

// current reports which child the device is currently serving.
//
// The three non-assigned states are distinct on purpose: the device shows
// "add a child" for no_child and "choose a child" for unassigned.
func (handler deviceChildBindingHandler) current(
	response http.ResponseWriter,
	request *http.Request,
) {
	resolution, err := handler.service.Resolve(
		request.Context(),
		request.PathValue("device_id"),
	)
	if err != nil {
		writeBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"state":       string(resolution.State),
		"child_id":    resolution.ChildID,
		"child_count": resolution.ChildCount,
	})
}

// writeBindingError maps a service failure to a stable code and safe message.
func writeBindingError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, ErrUnknownChild), errors.Is(err, domain.ErrChildNotInFamily):
		writeError(
			response,
			request,
			http.StatusNotFound,
			"child_not_found",
			"这个儿童不在当前家庭里",
		)
	case errors.Is(err, ErrNothingToUnbind), errors.Is(err, domain.ErrNoActiveChild):
		writeError(
			response,
			request,
			http.StatusConflict,
			"no_active_child",
			"这台设备当前没有正在使用的儿童",
		)
	case errors.Is(err, domain.ErrDeviceMissing):
		writeError(
			response,
			request,
			http.StatusNotFound,
			"device_not_found",
			"找不到这台设备",
		)
	case errors.Is(err, domain.ErrInvalidBinding):
		writeError(
			response,
			request,
			http.StatusBadRequest,
			"invalid_request",
			"请选择要使用的儿童",
		)
	default:
		writeError(
			response,
			request,
			http.StatusServiceUnavailable,
			"service_unavailable",
			"暂时无法更新设备与儿童的绑定，请稍后重试",
		)
	}
}
