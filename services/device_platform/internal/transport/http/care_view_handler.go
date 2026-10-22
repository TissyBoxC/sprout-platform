package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/domain"
	careviewservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/service"
)

// guardianCareViewHandler serves the guardian side of a live view.
type guardianCareViewHandler struct {
	service *careviewservice.Service
}

// deviceCareViewHandler serves the device side of a live view.
type deviceCareViewHandler struct {
	service *careviewservice.Service
}

// start opens a viewing session on one of the guardian's devices.
func (handler guardianCareViewHandler) start(response http.ResponseWriter, request *http.Request) {
	guardianID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请先登录")
		return
	}
	session, err := handler.service.Start(request.Context(), domain.Request{
		FamilyID: request.PathValue("family_id"),
		DeviceID: request.PathValue("device_id"),
		GuardianID: guardianID,
		// Consent is re-read from the stored decision inside the service; this
		// flag carries the caller's affirmation and the service cross-checks it.
		ConsentGranted: true,
	})
	if err != nil {
		writeCareViewError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, careViewResponse(session))
}

// get answers with one session owned by the family.
func (handler guardianCareViewHandler) get(response http.ResponseWriter, request *http.Request) {
	session, err := handler.service.Get(
		request.Context(),
		request.PathValue("family_id"),
		request.PathValue("session_id"),
	)
	if err != nil {
		writeCareViewError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, careViewResponse(session))
}

// end closes a session from the guardian side.
func (handler guardianCareViewHandler) end(response http.ResponseWriter, request *http.Request) {
	session, err := handler.service.End(
		request.Context(),
		request.PathValue("family_id"),
		request.PathValue("session_id"),
		"guardian_closed",
	)
	if err != nil {
		writeCareViewError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, careViewResponse(session))
}

// history lists a family's past sessions.
func (handler guardianCareViewHandler) history(response http.ResponseWriter, request *http.Request) {
	limit := 0
	if raw := request.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	sessions, err := handler.service.History(
		request.Context(),
		request.PathValue("family_id"),
		limit,
	)
	if err != nil {
		writeCareViewError(response, request, err)
		return
	}
	payload := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		payload = append(payload, careViewResponse(session))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"sessions": payload})
}

// current answers the device with the session it was asked to serve.
func (handler deviceCareViewHandler) current(response http.ResponseWriter, request *http.Request) {
	session, err := handler.service.GetForDevice(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("session_id"),
	)
	if err != nil {
		writeCareViewError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, careViewResponse(session))
}

// accept records that the device started showing its indication and may stream.
func (handler deviceCareViewHandler) accept(response http.ResponseWriter, request *http.Request) {
	session, err := handler.service.Accept(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("session_id"),
	)
	if err != nil {
		writeCareViewError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, careViewResponse(session))
}

// decline records that the child or the camera refused the view.
func (handler deviceCareViewHandler) decline(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	session, err := handler.service.Decline(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("session_id"),
		payload.Reason,
	)
	if err != nil {
		writeCareViewError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, careViewResponse(session))
}

// end lets the device close a session, for example when the child presses the
// button to stop being watched.
func (handler deviceCareViewHandler) end(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	session, err := handler.service.EndForDevice(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("session_id"),
		payload.Reason,
	)
	if err != nil {
		writeCareViewError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, careViewResponse(session))
}

// internalCareViewHandler answers the voice gateway's state re-read.
type internalCareViewHandler struct {
	service *careviewservice.Service
}

// session returns the authoritative state of one session.
func (handler internalCareViewHandler) session(response http.ResponseWriter, request *http.Request) {
	session, err := handler.service.GetForDevice(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("session_id"),
	)
	if err != nil {
		writeCareViewError(response, request, err)
		return
	}
	payload := careViewResponse(session)
	// The relay bounds its stream by this instant, so a session that already ran
	// past its limit cannot keep sending frames.
	payload["expires_at"] = session.Deadline()
	writeSuccess(response, request, http.StatusOK, payload)
}

// careViewResponse exposes lifecycle only.
//
// No frame, image, or token is ever part of a care-view response. A guardian sees
// how long a view ran and why it stopped, never what it showed.
func careViewResponse(session domain.Session) map[string]any {
	payload := map[string]any{
		"session_id":       session.ID,
		"device_id":        session.DeviceID,
		"child_id":         session.ChildID,
		"state":            string(session.State),
		"started_at":       session.StartedAt,
		"deadline":         session.Deadline(),
		"frames_delivered": session.FramesDelivered,
		"finish_reason":    session.FinishReason,
		// AllowsFrames tells the client whether frames may flow, so a reconnecting
		// client never renders a stream the platform has already stopped.
		"allows_frames": session.State.AllowsFrames(),
	}
	if session.FinishedAt != nil {
		payload["finished_at"] = session.FinishedAt
		payload["duration_seconds"] = int(session.Duration(*session.FinishedAt).Seconds())
	}
	return payload
}

// writeCareViewError maps care-view failures to stable codes.
func writeCareViewError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, careviewservice.ErrDeviceNotFound):
		writeError(response, request, http.StatusNotFound, "care_view_device_not_found", "没有找到这台设备")
	case errors.Is(err, careviewservice.ErrSessionNotFound):
		writeError(response, request, http.StatusNotFound, "care_view_not_found", "没有找到这次查看")
	case errors.Is(err, domain.ErrDisabled):
		writeError(response, request, http.StatusForbidden, "care_view_disabled", "还没有开启查看功能，请先在设置里开启")
	case errors.Is(err, domain.ErrNoConsent):
		writeError(response, request, http.StatusForbidden, "care_view_consent_required", "需要先同意图片授权才能查看")
	case errors.Is(err, domain.ErrRateLimited):
		writeError(response, request, http.StatusTooManyRequests, "care_view_rate_limited", "查看太频繁了，请稍后再试")
	case errors.Is(err, careviewservice.ErrBusy):
		writeError(response, request, http.StatusConflict, "care_view_device_busy", "这台设备正在被查看，请稍后再试")
	case errors.Is(err, careviewservice.ErrConflict):
		writeError(response, request, http.StatusConflict, "care_view_state_changed", "这次查看的状态已经变了，请刷新后再试")
	case errors.Is(err, domain.ErrInvalidRequest):
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求格式不正确")
	default:
		writeError(response, request, http.StatusInternalServerError, "care_view_failed", "暂时无法查看，请稍后再试")
	}
}
