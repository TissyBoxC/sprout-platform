// Package http exposes device-to-child binding to guardians and devices.
//
// Guardian routes are scoped to a family the caller belongs to; the device
// route answers with the device's own active child using a device session.
package http

import (
	"context"
	"errors"
	"net/http"

	runtimeDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
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

// bindingCommandSender queues a command for a device. A switch asks the device
// to reload its policy at once instead of waiting out the background refresh,
// which can be fifteen minutes away.
type bindingCommandSender interface {
	CreateCommand(
		ctx context.Context,
		deviceID string,
		commandType runtimeDomain.CommandType,
		requestedBy string,
		payload map[string]any,
	) (*runtimeDomain.Command, error)
}

// guardianBindingHandler serves binding management for one family's device.
type guardianBindingHandler struct {
	service  bindingService
	commands bindingCommandSender
}

// notifyConfigurationChanged asks the device to reload its configuration.
//
// A failure is not fatal: the change is already stored, and the device picks it
// up on its next background refresh. Reporting an error here would tell the
// guardian the switch failed when it did not.
func (handler guardianBindingHandler) notifyConfigurationChanged(
	ctx context.Context,
	deviceID string,
	childID string,
) {
	if handler.commands == nil {
		return
	}
	_, _ = handler.commands.CreateCommand(
		ctx,
		deviceID,
		runtimeDomain.CommandRefreshConfiguration,
		"system",
		map[string]any{"reason": "child_switched", "child_id": childID},
	)
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
	// The device keeps serving the previous child's level until it reloads, so
	// ask it to reload now rather than at its next background check.
	handler.notifyConfigurationChanged(
		request.Context(),
		binding.DeviceID,
		binding.ChildID,
	)
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
