package service

import (
	"context"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/domain"
)

type memoryRepository struct {
	records   map[string]*domain.GrowthRecord
	reminders map[string]*domain.HabitReminder
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		records:   map[string]*domain.GrowthRecord{},
		reminders: map[string]*domain.HabitReminder{},
	}
}

func (r *memoryRepository) CreateGrowthRecord(
	_ context.Context,
	record *domain.GrowthRecord,
) error {
	copyRecord := *record
	r.records[record.ID] = &copyRecord
	return nil
}

func (r *memoryRepository) ListGrowthRecords(
	_ context.Context,
	familyID string,
	query domain.GrowthRecordQuery,
) ([]domain.GrowthRecord, error) {
	result := make([]domain.GrowthRecord, 0, len(r.records))
	for _, record := range r.records {
		if record.FamilyID != familyID {
			continue
		}
		if query.ChildID != "" && record.ChildID != query.ChildID {
			continue
		}
		if query.Category != "" && record.Category != query.Category {
			continue
		}
		result = append(result, *record)
	}
	if len(result) > query.Limit {
		result = result[:query.Limit]
	}
	return result, nil
}

func (r *memoryRepository) GetGrowthRecord(
	_ context.Context,
	familyID string,
	recordID string,
) (*domain.GrowthRecord, error) {
	record, ok := r.records[recordID]
	if !ok || record.FamilyID != familyID {
		return nil, domain.ErrGrowthRecordNotFound
	}
	copyRecord := *record
	return &copyRecord, nil
}

func (r *memoryRepository) DeleteGrowthRecord(
	_ context.Context,
	familyID string,
	recordID string,
) error {
	record, ok := r.records[recordID]
	if !ok || record.FamilyID != familyID {
		return domain.ErrGrowthRecordNotFound
	}
	delete(r.records, recordID)
	return nil
}

func (r *memoryRepository) CreateHabitReminder(
	_ context.Context,
	reminder *domain.HabitReminder,
) error {
	copyReminder := *reminder
	r.reminders[reminder.ID] = &copyReminder
	return nil
}

func (r *memoryRepository) UpdateHabitReminder(
	_ context.Context,
	reminder *domain.HabitReminder,
) error {
	existing, ok := r.reminders[reminder.ID]
	if !ok || existing.FamilyID != reminder.FamilyID {
		return domain.ErrHabitReminderNotFound
	}
	copyReminder := *reminder
	r.reminders[reminder.ID] = &copyReminder
	return nil
}

func (r *memoryRepository) ListHabitReminders(
	_ context.Context,
	familyID string,
) ([]domain.HabitReminder, error) {
	result := make([]domain.HabitReminder, 0, len(r.reminders))
	for _, reminder := range r.reminders {
		if reminder.FamilyID == familyID {
			result = append(result, *reminder)
		}
	}
	return result, nil
}

func (r *memoryRepository) GetHabitReminder(
	_ context.Context,
	familyID string,
	reminderID string,
) (*domain.HabitReminder, error) {
	reminder, ok := r.reminders[reminderID]
	if !ok || reminder.FamilyID != familyID {
		return nil, domain.ErrHabitReminderNotFound
	}
	copyReminder := *reminder
	return &copyReminder, nil
}

func (r *memoryRepository) DeleteHabitReminder(
	_ context.Context,
	familyID string,
	reminderID string,
) error {
	reminder, ok := r.reminders[reminderID]
	if !ok || reminder.FamilyID != familyID {
		return domain.ErrHabitReminderNotFound
	}
	delete(r.reminders, reminderID)
	return nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func newTestService(t *testing.T) (*Service, *memoryRepository) {
	t.Helper()
	repo := newMemoryRepository()
	service, err := New(Options{
		Repository: repo,
		Clock:      fixedClock{now: time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	return service, repo
}

func TestCreateGrowthRecordNormalizesAndScopesFamily(t *testing.T) {
	service, _ := newTestService(t)

	record, err := service.CreateGrowthRecord(context.Background(), "family-1", domain.GrowthRecordInput{
		ChildID:  " child-1 ",
		Category: domain.GrowthCategoryMilestone,
		Title:    "  第一次完整背出《静夜思》  ",
		Note:     "  睡前故事时间  ",
	})
	if err != nil {
		t.Fatalf("CreateGrowthRecord() returned unexpected error: %v", err)
	}
	if record.FamilyID != "family-1" {
		t.Fatalf("expected family scope, got %q", record.FamilyID)
	}
	if record.ChildID != "child-1" {
		t.Fatalf("expected trimmed child id, got %q", record.ChildID)
	}
	if record.Title != "第一次完整背出《静夜思》" {
		t.Fatalf("expected trimmed title, got %q", record.Title)
	}
	if record.RecordedAt.IsZero() {
		t.Fatal("expected a recorded timestamp")
	}
}

func TestCreateGrowthRecordRejectsUnknownCategory(t *testing.T) {
	service, _ := newTestService(t)

	_, err := service.CreateGrowthRecord(context.Background(), "family-1", domain.GrowthRecordInput{
		Category: "made_up",
		Title:    "标题",
	})
	if err != domain.ErrInvalidGrowthCategory {
		t.Fatalf("expected ErrInvalidGrowthCategory, got %v", err)
	}
}

func TestCreateGrowthRecordRejectsBlankTitle(t *testing.T) {
	service, _ := newTestService(t)

	_, err := service.CreateGrowthRecord(context.Background(), "family-1", domain.GrowthRecordInput{
		Category: domain.GrowthCategoryMilestone,
		Title:    "   ",
	})
	if err != domain.ErrInvalidGrowthRecord {
		t.Fatalf("expected ErrInvalidGrowthRecord, got %v", err)
	}
}

func TestListGrowthRecordsClampsLimit(t *testing.T) {
	service, _ := newTestService(t)

	records, err := service.ListGrowthRecords(context.Background(), "family-1", domain.GrowthRecordQuery{
		Limit: 9999,
	})
	if err != nil {
		t.Fatalf("ListGrowthRecords() returned unexpected error: %v", err)
	}
	if records == nil {
		t.Fatal("expected a non-nil slice")
	}
}

func TestListGrowthRecordsRejectsUnknownCategory(t *testing.T) {
	service, _ := newTestService(t)

	_, err := service.ListGrowthRecords(context.Background(), "family-1", domain.GrowthRecordQuery{
		Category: "nope",
	})
	if err != domain.ErrInvalidGrowthCategory {
		t.Fatalf("expected ErrInvalidGrowthCategory, got %v", err)
	}
}

func TestDeleteGrowthRecordEnforcesFamilyScope(t *testing.T) {
	service, _ := newTestService(t)

	record, err := service.CreateGrowthRecord(context.Background(), "family-1", domain.GrowthRecordInput{
		Category: domain.GrowthCategoryEncouragement,
		Title:    "鼓励",
	})
	if err != nil {
		t.Fatalf("CreateGrowthRecord() returned unexpected error: %v", err)
	}
	if err := service.DeleteGrowthRecord(context.Background(), "family-2", record.ID); err != domain.ErrGrowthRecordNotFound {
		t.Fatalf("expected cross-family delete to fail, got %v", err)
	}
	if err := service.DeleteGrowthRecord(context.Background(), "family-1", record.ID); err != nil {
		t.Fatalf("expected owner delete to succeed, got %v", err)
	}
}

func TestCreateHabitReminderExpandsEmptyWeekdays(t *testing.T) {
	service, _ := newTestService(t)

	reminder, err := service.CreateHabitReminder(context.Background(), "family-1", domain.HabitReminderInput{
		Label:        "刷牙",
		ReminderTime: "20:30",
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("CreateHabitReminder() returned unexpected error: %v", err)
	}
	if len(reminder.Weekdays) != 7 {
		t.Fatalf("expected every-day schedule, got %v", reminder.Weekdays)
	}
}

func TestCreateHabitReminderSortsAndDeduplicatesWeekdays(t *testing.T) {
	service, _ := newTestService(t)

	reminder, err := service.CreateHabitReminder(context.Background(), "family-1", domain.HabitReminderInput{
		Label:        "读书",
		ReminderTime: "07:05",
		Weekdays:     []int{5, 1, 5, 3},
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("CreateHabitReminder() returned unexpected error: %v", err)
	}
	want := []int{1, 3, 5}
	if len(reminder.Weekdays) != len(want) {
		t.Fatalf("expected %v, got %v", want, reminder.Weekdays)
	}
	for index, day := range want {
		if reminder.Weekdays[index] != day {
			t.Fatalf("expected %v, got %v", want, reminder.Weekdays)
		}
	}
}

func TestCreateHabitReminderRejectsInvalidTime(t *testing.T) {
	service, _ := newTestService(t)

	for _, value := range []string{"24:00", "9:00", "20:60", "2030", "aa:bb"} {
		_, err := service.CreateHabitReminder(context.Background(), "family-1", domain.HabitReminderInput{
			Label:        "提醒",
			ReminderTime: value,
		})
		if err != domain.ErrInvalidReminderTime {
			t.Fatalf("expected ErrInvalidReminderTime for %q, got %v", value, err)
		}
	}
}

func TestCreateHabitReminderRejectsOutOfRangeWeekday(t *testing.T) {
	service, _ := newTestService(t)

	_, err := service.CreateHabitReminder(context.Background(), "family-1", domain.HabitReminderInput{
		Label:        "提醒",
		ReminderTime: "08:00",
		Weekdays:     []int{0},
	})
	if err != domain.ErrInvalidReminderWeekday {
		t.Fatalf("expected ErrInvalidReminderWeekday, got %v", err)
	}
}

func TestUpdateHabitReminderEnforcesFamilyScope(t *testing.T) {
	service, _ := newTestService(t)

	reminder, err := service.CreateHabitReminder(context.Background(), "family-1", domain.HabitReminderInput{
		Label:        "喝水",
		ReminderTime: "10:00",
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("CreateHabitReminder() returned unexpected error: %v", err)
	}
	_, err = service.UpdateHabitReminder(context.Background(), "family-2", reminder.ID, domain.HabitReminderInput{
		Label:        "喝水",
		ReminderTime: "11:00",
		Enabled:      true,
	})
	if err != domain.ErrHabitReminderNotFound {
		t.Fatalf("expected cross-family update to fail, got %v", err)
	}
	updated, err := service.UpdateHabitReminder(context.Background(), "family-1", reminder.ID, domain.HabitReminderInput{
		Label:        "喝水",
		ReminderTime: "11:00",
		Enabled:      false,
	})
	if err != nil {
		t.Fatalf("expected owner update to succeed, got %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected the reminder to be disabled after update")
	}
	if updated.ReminderTime != "11:00" {
		t.Fatalf("expected updated time, got %q", updated.ReminderTime)
	}
}
