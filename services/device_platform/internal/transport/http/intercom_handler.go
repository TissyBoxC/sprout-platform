package http

import (
	"errors"
	"net/http"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/domain"
	intercomservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/service"
)

// guardianIntercomHandler serves the guardian side of a call.
type guardianIntercomHandler struct {
	service *intercomservice.Service
}

// deviceIntercomHandler serves the device side of a call.
type deviceIntercomHandler struct {
	service *intercomservice.Service
}

// start opens a new call to one of the guardian's devices.
func (handler guardianIntercomHandler) start(response http.ResponseWriter, request *http.Request) {
	call, err := handler.service.Start(request.Context(), domain.Request{
		FamilyID: request.PathValue("family_id"),
		DeviceID: request.PathValue("device_id"),
	})
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, callResponse(call))
}

// get answers with one call owned by the family.
func (handler guardianIntercomHandler) get(response http.ResponseWriter, request *http.Request) {
	call, err := handler.service.Get(
		request.Context(),
		request.PathValue("family_id"),
		request.PathValue("call_id"),
	)
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, callResponse(call))
}

// cancel stops a call the child has not answered yet.
func (handler guardianIntercomHandler) cancel(response http.ResponseWriter, request *http.Request) {
	call, err := handler.service.Cancel(
		request.Context(),
		request.PathValue("family_id"),
		request.PathValue("call_id"),
	)
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, callResponse(call))
}

// hangUp ends an active call from the guardian side.
func (handler guardianIntercomHandler) hangUp(response http.ResponseWriter, request *http.Request) {
	call, err := handler.service.HangUp(
		request.Context(),
		request.PathValue("family_id"),
		request.PathValue("call_id"),
		"guardian_hangup",
	)
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, callResponse(call))
}

// history lists the family's recent calls.
func (handler guardianIntercomHandler) history(response http.ResponseWriter, request *http.Request) {
	calls, err := handler.service.History(
		request.Context(),
		request.PathValue("family_id"),
		parseLimit(request.URL.Query().Get("limit")),
	)
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	entries := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		entries = append(entries, callResponse(call))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"calls": entries})
}

// current answers with the device's unfinished call, if any.
//
// The device polls or subscribes to learn that a guardian is calling; an empty
// state means nobody is calling right now.
func (handler deviceIntercomHandler) current(response http.ResponseWriter, request *http.Request) {
	call, err := handler.service.GetForDevice(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("call_id"),
	)
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, callResponse(call))
}

// accept lets the device report that the child pressed accept.
func (handler deviceIntercomHandler) accept(response http.ResponseWriter, request *http.Request) {
	call, err := handler.service.Accept(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("call_id"),
	)
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, callResponse(call))
}

// decline lets the device report that the child refused the call.
func (handler deviceIntercomHandler) decline(response http.ResponseWriter, request *http.Request) {
	call, err := handler.service.Decline(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("call_id"),
	)
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, callResponse(call))
}

// hangUp ends an active call from the device side.
func (handler deviceIntercomHandler) hangUp(response http.ResponseWriter, request *http.Request) {
	call, err := handler.service.HangUpForDevice(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("call_id"),
		"device_hangup",
	)
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, callResponse(call))
}

// internalIntercomHandler answers the voice gateway's state re-read.
//
// The gateway must not trust a state carried from an earlier authorize call, so
// it asks the service that owns the call before relaying any audio.
type internalIntercomHandler struct {
	service *intercomservice.Service
}

// session returns the authoritative state of one call.
func (handler internalIntercomHandler) session(response http.ResponseWriter, request *http.Request) {
	call, err := handler.service.GetForDevice(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("call_id"),
	)
	if err != nil {
		writeIntercomError(response, request, err)
		return
	}
	payload := callResponse(call)
	// The gateway bounds its relay by this instant, so a call that already ran
	// past its limit cannot keep carrying audio.
	payload["expires_at"] = call.Deadline()
	writeSuccess(response, request, http.StatusOK, payload)
}

// callResponse exposes lifecycle only. No audio, transcript, or token is ever
// part of a call response.
func callResponse(call domain.Call) map[string]any {
	payload := map[string]any{
		"call_id":   call.ID,
		"device_id": call.DeviceID,
		"child_id":  call.ChildID,
		"state":     string(call.State),
		"started_at": call.StartedAt,
		// The client uses this to render a countdown instead of guessing the
		// remaining time from its own clock.
		"deadline": call.Deadline(),
	}
	if call.AcceptedAt != nil {
		payload["accepted_at"] = *call.AcceptedAt
	}
	if call.FinishedAt != nil {
		payload["finished_at"] = *call.FinishedAt
		payload["duration_seconds"] = int(call.Duration(*call.FinishedAt).Seconds())
	}
	if call.DeclineReason != "" {
		payload["finish_reason"] = call.DeclineReason
	}
	return payload
}

func writeIntercomError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, intercomservice.ErrDeviceNotFound),
		errors.Is(err, intercomservice.ErrCallNotFound):
		// A device in another family and a missing device answer identically so
		// the endpoint cannot be used to enumerate other families' devices.
		writeError(response, request, http.StatusNotFound, "intercom_not_found",
			"没有找到这台设备或这次通话")
	case errors.Is(err, domain.ErrIntercomDisabled):
		writeError(response, request, http.StatusForbidden, "intercom_disabled",
			"对讲功能已关闭，请先在设置中开启")
	case errors.Is(err, domain.ErrRateLimited):
		writeError(response, request, http.StatusTooManyRequests, "intercom_rate_limited",
			"呼叫次数太多，请稍后再试")
	case errors.Is(err, intercomservice.ErrBusy):
		writeError(response, request, http.StatusConflict, "intercom_device_busy",
			"这台设备正在通话中")
	case errors.Is(err, intercomservice.ErrConflict):
		writeError(response, request, http.StatusConflict, "intercom_state_changed",
			"通话状态已变化，请刷新后重试")
	default:
		writeError(response, request, http.StatusInternalServerError, "intercom_failed",
			"通话暂时无法继续，请稍后再试")
	}
}
