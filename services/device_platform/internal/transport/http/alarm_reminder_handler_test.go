package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	alarmreminderdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/domain"
)

// alarmReminderTestService is an in-memory double so the handler tests exercise
// the real validation and lifecycle without a database.
type alarmReminderTestService struct {
	alarms map[string]alarmreminderdomain.Alarm
	events []alarmreminderdomain.AlarmEvent
}

func newAlarmReminderTestService() *alarmReminderTestService {
	return &alarmReminderTestService{
		alarms: map[string]alarmreminderdomain.Alarm{},
	}
}

func (s *alarmReminderTestService) CreateAlarm(
	_ context.Context,
	familyID string,
	input alarmreminderdomain.AlarmInput,
) (*alarmreminderdomain.Alarm, error) {
	normalized, err := alarmreminderdomain.ValidateAlarmInput(input)
	if err != nil {
		return nil, err
	}
	alarm := alarmreminderdomain.Alarm{
		ID:        "alarm-1",
		FamilyID:  familyID,
		ChildID:   normalized.ChildID,
		Label:     normalized.Label,
		Prompt:    normalized.Prompt,
		AlarmTime: normalized.AlarmTime,
		Weekdays:  normalized.Weekdays,
		Enabled:   normalized.Enabled,
		SnoozeMin: normalized.SnoozeMin,
	}
	if len(alarm.Weekdays) == 0 {
		alarm.Weekdays = []int{1, 2, 3, 4, 5, 6, 7}
	}
	s.alarms[alarm.ID] = alarm
	return &alarm, nil
}

func (s *alarmReminderTestService) UpdateAlarm(
	_ context.Context,
	_ string,
	alarmID string,
	input alarmreminderdomain.AlarmInput,
) (*alarmreminderdomain.Alarm, error) {
	if _, ok := s.alarms[alarmID]; !ok {
		return nil, alarmreminderdomain.ErrAlarmNotFound
	}
	normalized, err := alarmreminderdomain.ValidateAlarmInput(input)
	if err != nil {
		return nil, err
	}
	alarm := alarmreminderdomain.Alarm{
		ID:        alarmID,
		Label:     normalized.Label,
		AlarmTime: normalized.AlarmTime,
		Weekdays:  normalized.Weekdays,
		Enabled:   normalized.Enabled,
		SnoozeMin: normalized.SnoozeMin,
	}
	s.alarms[alarmID] = alarm
	return &alarm, nil
}

func (s *alarmReminderTestService) ListAlarms(
	_ context.Context,
	_ string,
) ([]alarmreminderdomain.Alarm, error) {
	alarms := make([]alarmreminderdomain.Alarm, 0, len(s.alarms))
	for _, alarm := range s.alarms {
		alarms = append(alarms, alarm)
	}
	return alarms, nil
}

func (s *alarmReminderTestService) DeleteAlarm(
	_ context.Context,
	_ string,
	alarmID string,
) error {
	if _, ok := s.alarms[alarmID]; !ok {
		return alarmreminderdomain.ErrAlarmNotFound
	}
	delete(s.alarms, alarmID)
	return nil
}

func (s *alarmReminderTestService) DeviceSchedule(
	_ context.Context,
	_ string,
) ([]alarmreminderdomain.DeviceAlarm, error) {
	schedule := make([]alarmreminderdomain.DeviceAlarm, 0, len(s.alarms))
	for _, alarm := range s.alarms {
		if !alarm.Enabled {
			continue
		}
		schedule = append(schedule, alarmreminderdomain.DeviceAlarm{
			ID:        alarm.ID,
			Label:     alarm.Label,
			AlarmTime: alarm.AlarmTime,
			Weekdays:  alarm.Weekdays,
			SnoozeMin: alarm.SnoozeMin,
		})
	}
	return schedule, nil
}

func (s *alarmReminderTestService) RecordEvent(
	_ context.Context,
	familyID string,
	deviceID string,
	alarmID string,
	input alarmreminderdomain.AlarmEventInput,
) (*alarmreminderdomain.AlarmEvent, error) {
	if _, ok := s.alarms[alarmID]; !ok {
		return nil, alarmreminderdomain.ErrAlarmNotFound
	}
	normalized, err := alarmreminderdomain.ValidateAlarmEvent(input)
	if err != nil {
		return nil, err
	}
	event := alarmreminderdomain.AlarmEvent{
		ID:        "event-1",
		AlarmID:   alarmID,
		FamilyID:  familyID,
		DeviceID:  deviceID,
		AlarmDate: normalized.AlarmDate,
		State:     normalized.State,
	}
	s.events = append(s.events, event)
	return &event, nil
}

func (s *alarmReminderTestService) ListEvents(
	_ context.Context,
	_ string,
	_ int,
) ([]alarmreminderdomain.AlarmEvent, error) {
	return s.events, nil
}

func TestAlarmReminderListRequiresAuthentication(t *testing.T) {
	handler := alarmReminderHandler{service: newAlarmReminderTestService()}
	request := englishRequest(http.MethodGet, "/api/v1/alarm-reminders", "", "")
	recorder := httptest.NewRecorder()
	handler.listAlarms(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestAlarmReminderCreateDefaultsSnoozeAndEnabled(t *testing.T) {
	handler := alarmReminderHandler{service: newAlarmReminderTestService()}
	body := `{"label":"起床","alarm_time":"07:30","weekdays":[1,2,3,4,5]}`
	request := englishRequest(
		http.MethodPost,
		"/api/v1/alarm-reminders",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.createAlarm(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Alarm struct {
				Enabled   bool `json:"enabled"`
				SnoozeMin int  `json:"snooze_minutes"`
			} `json:"alarm"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Alarm.Enabled || envelope.Data.Alarm.SnoozeMin != 5 {
		t.Fatalf("defaults not applied: %+v", envelope.Data.Alarm)
	}
}

func TestAlarmReminderCreateRejectsBadTime(t *testing.T) {
	handler := alarmReminderHandler{service: newAlarmReminderTestService()}
	body := `{"label":"起床","alarm_time":"25:00","weekdays":[1]}`
	request := englishRequest(
		http.MethodPost,
		"/api/v1/alarm-reminders",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.createAlarm(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestAlarmReminderUpdateMissingIsNotFound(t *testing.T) {
	handler := alarmReminderHandler{service: newAlarmReminderTestService()}
	body := `{"label":"起床","alarm_time":"07:30","weekdays":[1]}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/alarm-reminders/missing",
		body,
		"family-1",
	)
	request.SetPathValue("alarm_id", "missing")
	recorder := httptest.NewRecorder()
	handler.updateAlarm(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestAlarmReminderDeleteRoundTrip(t *testing.T) {
	service := newAlarmReminderTestService()
	service.alarms["alarm-1"] = alarmreminderdomain.Alarm{ID: "alarm-1", Enabled: true}
	handler := alarmReminderHandler{service: service}
	request := englishRequest(
		http.MethodDelete,
		"/api/v1/alarm-reminders/alarm-1",
		"",
		"family-1",
	)
	request.SetPathValue("alarm_id", "alarm-1")
	recorder := httptest.NewRecorder()
	handler.deleteAlarm(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	missing := englishRequest(
		http.MethodDelete,
		"/api/v1/alarm-reminders/alarm-1",
		"",
		"family-1",
	)
	missing.SetPathValue("alarm_id", "alarm-1")
	recorder = httptest.NewRecorder()
	handler.deleteAlarm(recorder, missing)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestAlarmReminderDeviceScheduleRefusedWithoutSession(t *testing.T) {
	handler := deviceAlarmReminderHandler{
		handler:        &alarmReminderHandler{service: newAlarmReminderTestService()},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/alarm-reminders",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.schedule(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestAlarmReminderEventRefusedWithoutSession(t *testing.T) {
	handler := deviceAlarmReminderHandler{
		handler:        &alarmReminderHandler{service: newAlarmReminderTestService()},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodPost,
		"/api/v1/devices/device-1/alarm-reminders/alarm-1/events",
		`{"alarm_date":"2026-03-04","state":"ringing"}`,
		"",
	)
	request.SetPathValue("alarm_id", "alarm-1")
	recorder := httptest.NewRecorder()
	handler.reportEvent(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
