// Package service contains companionship use cases.
//
// Every operation is family-scoped: the caller passes the authenticated family
// id and the service never trusts a client-provided owner. Growth records form
// the guardian-visible timeline; habit reminders are the editable schedule.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

// DefaultGrowthLimit and MaxGrowthLimit bound a timeline read so a long-lived
// family cannot force an unbounded response.
const (
	DefaultGrowthLimit = 50
	MaxGrowthLimit     = 200
)

// Service owns companionship validation and persistence.
type Service struct {
	repository      repository.Repository
	readinessReader ReadinessReader
	timeSource      clock.Clock
}

// Options contains companionship dependencies.
type Options struct {
	Repository      repository.Repository
	ReadinessReader ReadinessReader
	Clock           clock.Clock
}

// New creates the companionship service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("companionship repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	readinessReader := options.ReadinessReader
	if readinessReader == nil {
		if reader, ok := options.Repository.(ReadinessReader); ok {
			readinessReader = reader
		}
	}
	return &Service{
		repository:      options.Repository,
		readinessReader: readinessReader,
		timeSource:      timeSource,
	}, nil
}

// CreateGrowthRecord validates and stores one timeline entry.
func (s *Service) CreateGrowthRecord(
	ctx context.Context,
	familyID string,
	input domain.GrowthRecordInput,
) (*domain.GrowthRecord, error) {
	normalized, err := domain.ValidateGrowthRecordInput(input)
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	record := &domain.GrowthRecord{
		ID:         uuid.NewString(),
		FamilyID:   familyID,
		ChildID:    normalized.ChildID,
		Category:   normalized.Category,
		Title:      normalized.Title,
		Note:       normalized.Note,
		RecordedAt: now,
		CreatedAt:  now,
	}
	if err := s.repository.CreateGrowthRecord(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}

// ListGrowthRecords returns a bounded, family-scoped timeline.
func (s *Service) ListGrowthRecords(
	ctx context.Context,
	familyID string,
	query domain.GrowthRecordQuery,
) ([]domain.GrowthRecord, error) {
	if query.Category != "" && !domain.IsGrowthCategory(query.Category) {
		return nil, domain.ErrInvalidGrowthCategory
	}
	query.ChildID = strings.TrimSpace(query.ChildID)
	query.Category = strings.TrimSpace(query.Category)
	switch {
	case query.Limit <= 0:
		query.Limit = DefaultGrowthLimit
	case query.Limit > MaxGrowthLimit:
		query.Limit = MaxGrowthLimit
	}
	return s.repository.ListGrowthRecords(ctx, familyID, query)
}

// DeleteGrowthRecord removes one family-owned entry.
func (s *Service) DeleteGrowthRecord(
	ctx context.Context,
	familyID string,
	recordID string,
) error {
	if strings.TrimSpace(recordID) == "" {
		return domain.ErrInvalidGrowthRecord
	}
	return s.repository.DeleteGrowthRecord(ctx, familyID, recordID)
}

// CreateHabitReminder validates and stores one reminder.
func (s *Service) CreateHabitReminder(
	ctx context.Context,
	familyID string,
	input domain.HabitReminderInput,
) (*domain.HabitReminder, error) {
	normalized, err := domain.ValidateHabitReminderInput(input)
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	reminder := &domain.HabitReminder{
		ID:           uuid.NewString(),
		FamilyID:     familyID,
		ChildID:      normalized.ChildID,
		Label:        normalized.Label,
		Prompt:       normalized.Prompt,
		ReminderTime: normalized.ReminderTime,
		Weekdays:     expandWeekdays(normalized.Weekdays),
		Enabled:      normalized.Enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repository.CreateHabitReminder(ctx, reminder); err != nil {
		return nil, err
	}
	return reminder, nil
}

// UpdateHabitReminder validates and replaces the editable fields.
func (s *Service) UpdateHabitReminder(
	ctx context.Context,
	familyID string,
	reminderID string,
	input domain.HabitReminderInput,
) (*domain.HabitReminder, error) {
	if strings.TrimSpace(reminderID) == "" {
		return nil, domain.ErrInvalidHabitReminder
	}
	existing, err := s.repository.GetHabitReminder(ctx, familyID, reminderID)
	if err != nil {
		return nil, err
	}
	normalized, err := domain.ValidateHabitReminderInput(input)
	if err != nil {
		return nil, err
	}
	existing.ChildID = normalized.ChildID
	existing.Label = normalized.Label
	existing.Prompt = normalized.Prompt
	existing.ReminderTime = normalized.ReminderTime
	existing.Weekdays = expandWeekdays(normalized.Weekdays)
	existing.Enabled = normalized.Enabled
	existing.UpdatedAt = s.timeSource.Now().UTC()
	if err := s.repository.UpdateHabitReminder(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// ListHabitReminders returns every reminder owned by one family.
func (s *Service) ListHabitReminders(
	ctx context.Context,
	familyID string,
) ([]domain.HabitReminder, error) {
	return s.repository.ListHabitReminders(ctx, familyID)
}

// DeleteHabitReminder removes one family-owned reminder.
func (s *Service) DeleteHabitReminder(
	ctx context.Context,
	familyID string,
	reminderID string,
) error {
	if strings.TrimSpace(reminderID) == "" {
		return domain.ErrInvalidHabitReminder
	}
	return s.repository.DeleteHabitReminder(ctx, familyID, reminderID)
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
