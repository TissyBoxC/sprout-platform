// Package service contains alarm reminder use cases.
//
// Every operation is family-scoped: the caller passes the authenticated family
// id and the service never trusts a client-provided owner. A device reads the
// enabled schedule over its device session and reports the bounded firing state
// back; the platform stores no audio.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

// DefaultEventLimit and MaxEventLimit bound a firing history read.
const (
	DefaultEventLimit = 50
	MaxEventLimit     = 200
)

// Service owns alarm reminder validation and persistence.
type Service struct {
	repository repository.Repository
	timeSource clock.Clock
}

// Options contains alarm reminder dependencies.
type Options struct {
	Repository repository.Repository
	Clock      clock.Clock
}

// New creates the alarm reminder service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("alarm reminder repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{repository: options.Repository, timeSource: timeSource}, nil
}

// CreateAlarm validates and stores one alarm, refusing a family already at its
// alarm bound.
func (s *Service) CreateAlarm(
	ctx context.Context,
	familyID string,
	input domain.AlarmInput,
) (*domain.Alarm, error) {
	normalized, err := domain.ValidateAlarmInput(input)
	if err != nil {
		return nil, err
	}
	count, err := s.repository.AlarmCount(ctx, familyID)
	if err != nil {
		return nil, err
	}
	if count >= domain.AlarmCapacity {
		return nil, domain.ErrInvalidAlarm
	}
	now := s.timeSource.Now().UTC()
	alarm := &domain.Alarm{
		ID:        uuid.NewString(),
		FamilyID:  familyID,
		ChildID:   normalized.ChildID,
		Label:     normalized.Label,
		Prompt:    normalized.Prompt,
		AlarmTime: normalized.AlarmTime,
		Weekdays:  expandWeekdays(normalized.Weekdays),
		Enabled:   normalized.Enabled,
		SnoozeMin: normalized.SnoozeMin,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repository.CreateAlarm(ctx, alarm); err != nil {
		return nil, err
	}
	return alarm, nil
}

// UpdateAlarm validates and replaces the editable fields of one alarm.
func (s *Service) UpdateAlarm(
	ctx context.Context,
	familyID string,
	alarmID string,
	input domain.AlarmInput,
) (*domain.Alarm, error) {
	if strings.TrimSpace(alarmID) == "" {
		return nil, domain.ErrInvalidAlarm
	}
	existing, err := s.repository.GetAlarm(ctx, familyID, alarmID)
	if err != nil {
		return nil, err
	}
	normalized, err := domain.ValidateAlarmInput(input)
	if err != nil {
		return nil, err
	}
	existing.ChildID = normalized.ChildID
	existing.Label = normalized.Label
	existing.Prompt = normalized.Prompt
	existing.AlarmTime = normalized.AlarmTime
	existing.Weekdays = expandWeekdays(normalized.Weekdays)
	existing.Enabled = normalized.Enabled
	existing.SnoozeMin = normalized.SnoozeMin
	existing.UpdatedAt = s.timeSource.Now().UTC()
	if err := s.repository.UpdateAlarm(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// ListAlarms returns every alarm owned by one family.
func (s *Service) ListAlarms(
	ctx context.Context,
	familyID string,
) ([]domain.Alarm, error) {
	return s.repository.ListAlarms(ctx, familyID)
}

// DeleteAlarm removes one family-owned alarm.
func (s *Service) DeleteAlarm(
	ctx context.Context,
	familyID string,
	alarmID string,
) error {
	if strings.TrimSpace(alarmID) == "" {
		return domain.ErrInvalidAlarm
	}
	return s.repository.DeleteAlarm(ctx, familyID, alarmID)
}

// DeviceSchedule returns the enabled alarms a bound device matches locally.
func (s *Service) DeviceSchedule(
	ctx context.Context,
	familyID string,
) ([]domain.DeviceAlarm, error) {
	alarms, err := s.repository.ListEnabledAlarms(ctx, familyID)
	if err != nil {
		return nil, err
	}
	schedule := make([]domain.DeviceAlarm, 0, len(alarms))
	for _, alarm := range alarms {
		schedule = append(schedule, domain.DeviceAlarm{
			ID:        alarm.ID,
			ChildID:   alarm.ChildID,
			Label:     alarm.Label,
			Prompt:    alarm.Prompt,
			AlarmTime: alarm.AlarmTime,
			Weekdays:  alarm.Weekdays,
			SnoozeMin: alarm.SnoozeMin,
			UpdatedAt: alarm.UpdatedAt,
		})
	}
	return schedule, nil
}

// RecordEvent stores one bounded firing acknowledgement. The alarm must belong
// to the reporting family so a device cannot acknowledge another family's
// alarm.
func (s *Service) RecordEvent(
	ctx context.Context,
	familyID string,
	deviceID string,
	alarmID string,
	input domain.AlarmEventInput,
) (*domain.AlarmEvent, error) {
	alarmID = strings.TrimSpace(alarmID)
	if alarmID == "" {
		return nil, domain.ErrInvalidAlarmEvent
	}
	normalized, err := domain.ValidateAlarmEvent(input)
	if err != nil {
		return nil, err
	}
	if _, err := s.repository.GetAlarm(ctx, familyID, alarmID); err != nil {
		return nil, err
	}
	event := &domain.AlarmEvent{
		ID:        uuid.NewString(),
		AlarmID:   alarmID,
		FamilyID:  familyID,
		DeviceID:  strings.TrimSpace(deviceID),
		AlarmDate: normalized.AlarmDate,
		State:     normalized.State,
	}
	if err := s.repository.RecordEvent(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

// ListEvents returns a bounded, family-scoped firing history.
func (s *Service) ListEvents(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.AlarmEvent, error) {
	switch {
	case limit <= 0:
		limit = DefaultEventLimit
	case limit > MaxEventLimit:
		limit = MaxEventLimit
	}
	return s.repository.ListEvents(ctx, familyID, limit)
}

// expandWeekdays turns an empty set into "every day" so callers and the
// database always observe a concrete weekly schedule.
func expandWeekdays(weekdays []int) []int {
	if len(weekdays) == 0 {
		return []int{1, 2, 3, 4, 5, 6, 7}
	}
	return weekdays
}

// Now exposes the service clock for handlers that need the same time source.
func (s *Service) Now() time.Time {
	return s.timeSource.Now().UTC()
}
