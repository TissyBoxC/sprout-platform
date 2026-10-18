package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	alarmreminderdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/domain"
	alarmreminderservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/service"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
)

// alarmReminderService is the guardian-facing alarm reminder surface. The HTTP
// layer depends on this narrow interface so the module stays removable.
type alarmReminderService interface {
	CreateAlarm(
		ctx context.Context,
		familyID string,
		input alarmreminderdomain.AlarmInput,
	) (*alarmreminderdomain.Alarm, error)
	UpdateAlarm(
		ctx context.Context,
		familyID string,
		alarmID string,
		input alarmreminderdomain.AlarmInput,
	) (*alarmreminderdomain.Alarm, error)
	ListAlarms(ctx context.Context, familyID string) ([]alarmreminderdomain.Alarm, error)
	DeleteAlarm(ctx context.Context, familyID string, alarmID string) error
	DeviceSchedule(
		ctx context.Context,
		familyID string,
	) ([]alarmreminderdomain.DeviceAlarm, error)
	RecordEvent(
		ctx context.Context,
		familyID string,
		deviceID string,
		alarmID string,
		input alarmreminderdomain.AlarmEventInput,
	) (*alarmreminderdomain.AlarmEvent, error)
	ListEvents(
		ctx context.Context,
		familyID string,
		limit int,
	) ([]alarmreminderdomain.AlarmEvent, error)
}

type alarmReminderHandler struct {
	service alarmReminderService
}

// deviceAlarmReminderHandler serves the family alarm schedule and records the
// bounded firing state for a bound device that holds a device session token.
type deviceAlarmReminderHandler struct {
	handler        *alarmReminderHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type alarmReminderRequest struct {
	ChildID   string `json:"child_id"`
	Label     string `json:"label"`
	Prompt    string `json:"prompt"`
	AlarmTime string `json:"alarm_time"`
	Weekdays  []int  `json:"weekdays"`
	Enabled   *bool  `json:"enabled"`
	SnoozeMin *int   `json:"snooze_minutes"`
}

type alarmReminderEventRequest struct {
	AlarmDate string `json:"alarm_date"`
	State     string `json:"state"`
}

// listAlarms returns every alarm owned by the authenticated family.
func (handler alarmReminderHandler) listAlarms(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	alarms, err := handler.service.ListAlarms(request.Context(), familyID)
	if err != nil {
		writeAlarmReminderError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(alarms))
	for index := range alarms {
		result = append(result, alarmReminderResponse(&alarms[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"alarms": result,
	})
}

// createAlarm stores one new alarm.
func (handler alarmReminderHandler) createAlarm(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload alarmReminderRequest
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查提醒内容")
		return
	}
	alarm, err := handler.service.CreateAlarm(
		request.Context(),
		familyID,
		alarmReminderInput(payload),
	)
	if err != nil {
		writeAlarmReminderError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"alarm": alarmReminderResponse(alarm),
	})
}

// updateAlarm replaces the editable fields of one alarm.
func (handler alarmReminderHandler) updateAlarm(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload alarmReminderRequest
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查提醒内容")
		return
	}
	alarmID := strings.TrimSpace(request.PathValue("alarm_id"))
	alarm, err := handler.service.UpdateAlarm(
		request.Context(),
		familyID,
		alarmID,
		alarmReminderInput(payload),
	)
	if err != nil {
		writeAlarmReminderError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"alarm": alarmReminderResponse(alarm),
	})
}

// deleteAlarm removes one alarm.
func (handler alarmReminderHandler) deleteAlarm(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	alarmID := strings.TrimSpace(request.PathValue("alarm_id"))
	if err := handler.service.DeleteAlarm(request.Context(), familyID, alarmID); err != nil {
		writeAlarmReminderError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"alarm_id": alarmID,
	})
}

// listEvents returns a bounded firing history for the authenticated family.
func (handler alarmReminderHandler) listEvents(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	limit := parsePositiveQueryInt(request.URL.Query().Get("limit"), 0)
	events, err := handler.service.ListEvents(request.Context(), familyID, limit)
	if err != nil {
		writeAlarmReminderError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(events))
	for index := range events {
		result = append(result, alarmReminderEventResponse(&events[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"events": result,
	})
}

// schedule returns the enabled alarm schedule to a bound device.
func (handler deviceAlarmReminderHandler) schedule(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	schedule, err := handler.handler.service.DeviceSchedule(request.Context(), familyID)
	if err != nil {
		writeAlarmReminderError(response, request, err)
		return
	}
	if schedule == nil {
		schedule = []alarmreminderdomain.DeviceAlarm{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"alarms": schedule,
	})
}

// reportEvent records one alarm firing acknowledgement.
func (handler deviceAlarmReminderHandler) reportEvent(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload alarmReminderEventRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	alarmID := strings.TrimSpace(request.PathValue("alarm_id"))
	event, err := handler.handler.service.RecordEvent(
		request.Context(),
		familyID,
		deviceID,
		alarmID,
		alarmreminderdomain.AlarmEventInput{
			AlarmDate: payload.AlarmDate,
			State:     payload.State,
		},
	)
	if err != nil {
		writeAlarmReminderError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"alarm_id": event.AlarmID,
		"state":    event.State,
		"recorded": true,
	})
}

func (handler deviceAlarmReminderHandler) verifyDevice(
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
		writeAlarmReminderError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

// alarmReminderInput defaults the enabled flag to true and the snooze window to
// five minutes so a client that omits them creates an active alarm instead of a
// silent disabled or zero-snooze one.
func alarmReminderInput(payload alarmReminderRequest) alarmreminderdomain.AlarmInput {
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	snooze := 5
	if payload.SnoozeMin != nil {
		snooze = *payload.SnoozeMin
	}
	return alarmreminderdomain.AlarmInput{
		ChildID:   payload.ChildID,
		Label:     payload.Label,
		Prompt:    payload.Prompt,
		AlarmTime: payload.AlarmTime,
		Weekdays:  payload.Weekdays,
		Enabled:   enabled,
		SnoozeMin: snooze,
	}
}

func alarmReminderResponse(alarm *alarmreminderdomain.Alarm) map[string]any {
	weekdays := make([]int, len(alarm.Weekdays))
	copy(weekdays, alarm.Weekdays)
	return map[string]any{
		"id":             alarm.ID,
		"child_id":       alarm.ChildID,
		"label":          alarm.Label,
		"prompt":         alarm.Prompt,
		"alarm_time":     alarm.AlarmTime,
		"weekdays":       weekdays,
		"enabled":        alarm.Enabled,
		"snooze_minutes": alarm.SnoozeMin,
		"created_at":     alarm.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":     alarm.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func alarmReminderEventResponse(event *alarmreminderdomain.AlarmEvent) map[string]any {
	return map[string]any{
		"id":          event.ID,
		"alarm_id":    event.AlarmID,
		"device_id":   event.DeviceID,
		"alarm_date":  event.AlarmDate,
		"state":       event.State,
		"reported_at": event.ReportedAt.UTC().Format(time.RFC3339),
	}
}

func writeAlarmReminderError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, alarmreminderdomain.ErrAlarmNotFound):
		writeError(response, request, http.StatusNotFound, "alarm_not_found", "没有找到这个提醒")
	case errors.Is(err, alarmreminderdomain.ErrInvalidAlarmEvent):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_alarm_event", "请检查提醒上报内容")
	case errors.Is(err, alarmreminderdomain.ErrInvalidAlarm):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_alarm", "请检查提醒时间、星期和名称")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

var _ alarmReminderService = (*alarmreminderservice.Service)(nil)
