package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	videodomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_companion/domain"
)

// videoCompanionService is the guardian-facing video companionship surface. The
// HTTP layer depends on this narrow interface so the module stays removable.
type videoCompanionService interface {
	CreateContact(
		ctx context.Context,
		familyID string,
		input videodomain.ContactInput,
	) (videodomain.Contact, error)
	UpdateContact(
		ctx context.Context,
		familyID string,
		contactID string,
		input videodomain.ContactInput,
	) (videodomain.Contact, error)
	DeleteContact(ctx context.Context, familyID string, contactID string) error
	Contacts(ctx context.Context, familyID string) ([]videodomain.Contact, error)
	StartCall(
		ctx context.Context,
		familyID string,
		input videodomain.CallInput,
	) (videodomain.Call, error)
	UpdateCallState(
		ctx context.Context,
		familyID string,
		callID string,
		state string,
		quality string,
		endReason string,
	) (videodomain.Call, error)
	CallHistory(ctx context.Context, familyID string, limit int) ([]videodomain.Call, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (videodomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input videodomain.CallStatusInput,
	) (videodomain.DeviceStatus, error)
	DeviceStatuses(ctx context.Context, familyID string) ([]videodomain.DeviceCallStatus, error)
	AdminStats(ctx context.Context, windowDays int) (videodomain.AdminStats, error)
}

type videoCompanionHandler struct {
	service videoCompanionService
}

// deviceVideoHandler records bounded call status from a bound device.
type deviceVideoHandler struct {
	handler        *videoCompanionHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type videoContactPayload struct {
	ChildID     string `json:"child_id"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Enabled     bool   `json:"enabled"`
}

type videoCallPayload struct {
	DeviceID  string `json:"device_id"`
	ContactID string `json:"contact_id"`
	Initiator string `json:"initiator"`
}

type videoCallStatePayload struct {
	State     string `json:"state"`
	Quality   string `json:"quality"`
	EndReason string `json:"end_reason"`
}

type videoStatusPayload struct {
	CallState         string `json:"call_state"`
	Quality           string `json:"quality"`
	CameraAllowed     bool   `json:"camera_allowed"`
	MicrophoneAllowed bool   `json:"microphone_allowed"`
	LastCallID        string `json:"last_call_id"`
}

// listContacts returns every contact in the authenticated family.
func (handler videoCompanionHandler) listContacts(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	contacts, err := handler.service.Contacts(request.Context(), familyID)
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	if contacts == nil {
		contacts = []videodomain.Contact{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"contacts": videoContactResponses(contacts),
	})
}

// createContact stores a new guardian-managed contact.
func (handler videoCompanionHandler) createContact(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload videoContactPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查联系人信息")
		return
	}
	contact, err := handler.service.CreateContact(request.Context(), familyID, videodomain.ContactInput{
		ChildID:     payload.ChildID,
		DisplayName: payload.DisplayName,
		Role:        payload.Role,
		Enabled:     payload.Enabled,
	})
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, videoContactResponse(contact))
}

// updateContact stores one contact edit.
func (handler videoCompanionHandler) updateContact(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	contactID := strings.TrimSpace(request.PathValue("contact_id"))
	var payload videoContactPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查联系人信息")
		return
	}
	contact, err := handler.service.UpdateContact(
		request.Context(),
		familyID,
		contactID,
		videodomain.ContactInput{
			ChildID:     payload.ChildID,
			DisplayName: payload.DisplayName,
			Role:        payload.Role,
			Enabled:     payload.Enabled,
		},
	)
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, videoContactResponse(contact))
}

// deleteContact removes one contact.
func (handler videoCompanionHandler) deleteContact(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	contactID := strings.TrimSpace(request.PathValue("contact_id"))
	if err := handler.service.DeleteContact(request.Context(), familyID, contactID); err != nil {
		writeVideoError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"deleted": true})
}

// startCall creates a new ringing call.
func (handler videoCompanionHandler) startCall(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload videoCallPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查通话请求")
		return
	}
	call, err := handler.service.StartCall(request.Context(), familyID, videodomain.CallInput{
		DeviceID:  payload.DeviceID,
		ContactID: payload.ContactID,
		Initiator: payload.Initiator,
	})
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, videoCallResponse(call))
}

// updateCall stores one call lifecycle transition.
func (handler videoCompanionHandler) updateCall(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	callID := strings.TrimSpace(request.PathValue("call_id"))
	var payload videoCallStatePayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查通话状态")
		return
	}
	call, err := handler.service.UpdateCallState(
		request.Context(),
		familyID,
		callID,
		payload.State,
		payload.Quality,
		payload.EndReason,
	)
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, videoCallResponse(call))
}

// history returns the recent call history in the family.
func (handler videoCompanionHandler) history(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	limit := parseVideoLimit(request.URL.Query().Get("limit"))
	calls, err := handler.service.CallHistory(request.Context(), familyID, limit)
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	if calls == nil {
		calls = []videodomain.Call{}
	}
	results := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		results = append(results, videoCallResponse(call))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"calls": results})
}

// statuses returns each family device's latest video status.
func (handler videoCompanionHandler) statuses(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	devices, err := handler.service.DeviceStatuses(request.Context(), familyID)
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	if devices == nil {
		devices = []videodomain.DeviceCallStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": devices})
}

// capability returns the video capability for one family device.
func (handler videoCompanionHandler) capability(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	capability, err := handler.service.Capability(request.Context(), familyID, deviceID)
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// reportStatus records one bound device's video status.
func (handler deviceVideoHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload videoStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		videodomain.CallStatusInput{
			CallState:         payload.CallState,
			Quality:           payload.Quality,
			CameraAllowed:     payload.CameraAllowed,
			MicrophoneAllowed: payload.MicrophoneAllowed,
			LastCallID:        payload.LastCallID,
		},
	)
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id": status.DeviceID,
		"recorded":  true,
	})
}

// adminStats returns the bounded fleet video summary.
func (handler videoCompanionHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseVideoWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeVideoError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func (handler deviceVideoHandler) verifyDevice(
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
		writeVideoError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func videoContactResponse(contact videodomain.Contact) map[string]any {
	return map[string]any{
		"id":           contact.ID,
		"child_id":     contact.ChildID,
		"display_name": contact.DisplayName,
		"role":         contact.Role,
		"enabled":      contact.Enabled,
		"created_at":   contact.CreatedAt,
	}
}

func videoContactResponses(contacts []videodomain.Contact) []map[string]any {
	results := make([]map[string]any, 0, len(contacts))
	for _, contact := range contacts {
		results = append(results, videoContactResponse(contact))
	}
	return results
}

func videoCallResponse(call videodomain.Call) map[string]any {
	response := map[string]any{
		"id":         call.ID,
		"device_id":  call.DeviceID,
		"contact_id": call.ContactID,
		"initiator":  call.Initiator,
		"state":      call.State,
		"quality":    call.Quality,
		"started_at": call.StartedAt,
	}
	if call.EndReason != "" {
		response["end_reason"] = call.EndReason
	}
	if call.AnsweredAt != nil {
		response["answered_at"] = call.AnsweredAt
	}
	if call.EndedAt != nil {
		response["ended_at"] = call.EndedAt
	}
	return response
}

func parseVideoLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > videodomain.CallHistoryMaxLimit {
		return 0
	}
	return value
}

func parseVideoWindowDays(raw string) int {
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

func writeVideoError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, videodomain.ErrInvalidContact):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_contact", "请检查联系人信息")
	case errors.Is(err, videodomain.ErrContactNotFound):
		writeError(response, request, http.StatusNotFound, "contact_not_found", "联系人不存在")
	case errors.Is(err, videodomain.ErrInvalidCall):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_call", "请检查通话请求")
	case errors.Is(err, videodomain.ErrCallNotFound):
		writeError(response, request, http.StatusNotFound, "call_not_found", "通话不存在")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
