package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/domain"
)

// fakeRepository is an in-memory double so the service tests exercise the real
// validation and lifecycle without a database.
type fakeRepository struct {
	alarms map[string]domain.Alarm
	events map[string]domain.AlarmEvent
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		alarms: map[string]domain.Alarm{},
		events: map[string]domain.AlarmEvent{},
	}
}

func (r *fakeRepository) CreateAlarm(_ context.Context, alarm *domain.Alarm) error {
	r.alarms[alarm.ID] = *alarm
	return nil
}

func (r *fakeRepository) UpdateAlarm(_ context.Context, alarm *domain.Alarm) error {
	if _, ok := r.alarms[alarm.ID]; !ok {
		return domain.ErrAlarmNotFound
	}
	r.alarms[alarm.ID] = *alarm
	return nil
}

func (r *fakeRepository) GetAlarm(
	_ context.Context,
	_ string,
	alarmID string,
) (*domain.Alarm, error) {
	alarm, ok := r.alarms[alarmID]
	if !ok {
		return nil, domain.ErrAlarmNotFound
	}
	return &alarm, nil
}

func (r *fakeRepository) ListAlarms(_ context.Context, _ string) ([]domain.Alarm, error) {
	alarms := make([]domain.Alarm, 0, len(r.alarms))
	for _, alarm := range r.alarms {
		alarms = append(alarms, alarm)
	}
	return alarms, nil
}

func (r *fakeRepository) ListEnabledAlarms(
	_ context.Context,
	_ string,
) ([]domain.Alarm, error) {
	alarms := make([]domain.Alarm, 0, len(r.alarms))
	for _, alarm := range r.alarms {
		if alarm.Enabled {
			alarms = append(alarms, alarm)
		}
	}
	return alarms, nil
}

func (r *fakeRepository) DeleteAlarm(_ context.Context, _ string, alarmID string) error {
	if _, ok := r.alarms[alarmID]; !ok {
		return domain.ErrAlarmNotFound
	}
	delete(r.alarms, alarmID)
	return nil
}

func (r *fakeRepository) AlarmCount(_ context.Context, _ string) (int, error) {
	return len(r.alarms), nil
}

func (r *fakeRepository) RecordEvent(_ context.Context, event *domain.AlarmEvent) error {
	r.events[event.AlarmID+"|"+event.AlarmDate] = *event
	return nil
}

func (r *fakeRepository) ListEvents(
	_ context.Context,
	_ string,
	_ int,
) ([]domain.AlarmEvent, error) {
	events := make([]domain.AlarmEvent, 0, len(r.events))
	for _, event := range r.events {
		events = append(events, event)
	}
	return events, nil
}

func newService(t *testing.T) (*Service, *fakeRepository) {
	t.Helper()
	repository := newFakeRepository()
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return service, repository
}

func validInput() domain.AlarmInput {
	return domain.AlarmInput{
		ChildID:   "child-1",
		Label:     "上学",
		Prompt:    "背好书包，出发啦。",
		AlarmTime: "07:00",
		Weekdays:  []int{1, 2, 3, 4, 5},
		Enabled:   true,
		SnoozeMin: 5,
	}
}

func TestNewRequiresRepository(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatalf("New without repository should fail")
	}
}

func TestCreateAlarmExpandsEmptyWeekdays(t *testing.T) {
	service, _ := newService(t)
	input := validInput()
	input.Weekdays = nil
	alarm, err := service.CreateAlarm(context.Background(), "family-1", input)
	if err != nil {
		t.Fatalf("CreateAlarm: %v", err)
	}
	if len(alarm.Weekdays) != 7 {
		t.Fatalf("weekdays = %v, want every day", alarm.Weekdays)
	}
}

func TestCreateAlarmRejectsOverCapacity(t *testing.T) {
	service, repository := newService(t)
	for index := 0; index < domain.AlarmCapacity; index++ {
		alarm := domain.Alarm{
			ID:        "alarm-" + strconv.Itoa(index),
			FamilyID:  "family-1",
			AlarmTime: "07:00",
			Weekdays:  []int{1},
			SnoozeMin: 5,
		}
		repository.alarms[alarm.ID] = alarm
	}
	if _, err := service.CreateAlarm(
		context.Background(),
		"family-1",
		validInput(),
	); !errors.Is(err, domain.ErrInvalidAlarm) {
		t.Fatalf("err = %v, want ErrInvalidAlarm", err)
	}
}

func TestUpdateAlarmReplacesFields(t *testing.T) {
	service, _ := newService(t)
	created, err := service.CreateAlarm(context.Background(), "family-1", validInput())
	if err != nil {
		t.Fatalf("CreateAlarm: %v", err)
	}
	update := validInput()
	update.Label = "放学"
	update.AlarmTime = "16:30"
	update.Enabled = false
	update.SnoozeMin = 15
	updated, err := service.UpdateAlarm(
		context.Background(),
		"family-1",
		created.ID,
		update,
	)
	if err != nil {
		t.Fatalf("UpdateAlarm: %v", err)
	}
	if updated.Label != "放学" || updated.AlarmTime != "16:30" {
		t.Fatalf("update not applied: %+v", updated)
	}
	if updated.Enabled || updated.SnoozeMin != 15 {
		t.Fatalf("update not applied: %+v", updated)
	}
}

func TestUpdateMissingAlarmNotFound(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.UpdateAlarm(
		context.Background(),
		"family-1",
		"missing",
		validInput(),
	); !errors.Is(err, domain.ErrAlarmNotFound) {
		t.Fatalf("err = %v, want ErrAlarmNotFound", err)
	}
}

func TestDeviceScheduleOnlyEnabled(t *testing.T) {
	service, _ := newService(t)
	enabled := validInput()
	if _, err := service.CreateAlarm(context.Background(), "family-1", enabled); err != nil {
		t.Fatalf("CreateAlarm enabled: %v", err)
	}
	disabled := validInput()
	disabled.Enabled = false
	if _, err := service.CreateAlarm(context.Background(), "family-1", disabled); err != nil {
		t.Fatalf("CreateAlarm disabled: %v", err)
	}
	schedule, err := service.DeviceSchedule(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("DeviceSchedule: %v", err)
	}
	if len(schedule) != 1 {
		t.Fatalf("schedule length = %d, want 1", len(schedule))
	}
}

func TestRecordEventRejectsForeignAlarm(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.RecordEvent(
		context.Background(),
		"family-1",
		"device-1",
		"missing",
		domain.AlarmEventInput{AlarmDate: "2026-03-04", State: domain.StateRinging},
	); !errors.Is(err, domain.ErrAlarmNotFound) {
		t.Fatalf("err = %v, want ErrAlarmNotFound", err)
	}
}

func TestRecordEventStoresBoundedState(t *testing.T) {
	service, _ := newService(t)
	created, err := service.CreateAlarm(context.Background(), "family-1", validInput())
	if err != nil {
		t.Fatalf("CreateAlarm: %v", err)
	}
	event, err := service.RecordEvent(
		context.Background(),
		"family-1",
		"device-1",
		created.ID,
		domain.AlarmEventInput{AlarmDate: "2026-03-04", State: domain.StateDismissed},
	)
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if event.State != domain.StateDismissed || event.AlarmID != created.ID {
		t.Fatalf("event = %+v", event)
	}
	if _, err := service.RecordEvent(
		context.Background(),
		"family-1",
		"device-1",
		created.ID,
		domain.AlarmEventInput{AlarmDate: "2026-03-04", State: "exploded"},
	); !errors.Is(err, domain.ErrInvalidAlarmEvent) {
		t.Fatalf("err = %v, want ErrInvalidAlarmEvent", err)
	}
}

func TestListEventsClampsLimit(t *testing.T) {
	service, _ := newService(t)
	// A huge limit and a non-positive limit are both clamped; neither may error.
	for _, limit := range []int{100000, 0, -3} {
		if _, err := service.ListEvents(context.Background(), "family-1", limit); err != nil {
			t.Fatalf("ListEvents(%d): %v", limit, err)
		}
	}
}
