// Package domain contains companionship types shared by the platform modules.
//
// Companionship covers the guardian-visible record of a child's growth, the
// encouragement moments the assistant produces, and the habit reminders a
// family schedules. Everything is family-scoped: a guardian only ever reads or
// writes rows owned by the authenticated family id.
package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrGrowthRecordNotFound   = errors.New("growth record not found")
	ErrHabitReminderNotFound  = errors.New("habit reminder not found")
	ErrInvalidGrowthRecord    = errors.New("invalid growth record")
	ErrInvalidHabitReminder   = errors.New("invalid habit reminder")
	ErrInvalidGrowthCategory  = errors.New("invalid growth record category")
	ErrInvalidReminderTime    = errors.New("invalid reminder time")
	ErrInvalidReminderWeekday = errors.New("invalid reminder weekday")
	ErrInvalidReminderLabel   = errors.New("invalid reminder label")
	ErrInvalidReminderPrompt  = errors.New("invalid reminder prompt")
	ErrReadinessUnavailable   = errors.New("companionship readiness is unavailable")
)

// Growth record categories. The set is deliberately small and stable so the
// console can group entries without guessing at free-form client strings.
const (
	GrowthCategoryMilestone     = "milestone"
	GrowthCategoryEncouragement = "encouragement"
	GrowthCategoryHabit         = "habit"
	GrowthCategoryEmotion       = "emotion"
)

// GrowthCategories is the canonical ordered list shared by validation and the
// console.
var GrowthCategories = []string{
	GrowthCategoryMilestone,
	GrowthCategoryEncouragement,
	GrowthCategoryHabit,
	GrowthCategoryEmotion,
}

const (
	GrowthTitleMaxLength = 80
	GrowthNoteMaxLength  = 500
	ReminderLabelMax     = 40
	ReminderPromptMax    = 200
)

// GrowthRecord is one guardian-visible timeline entry for a child.
type GrowthRecord struct {
	ID         string
	FamilyID   string
	ChildID    string
	Category   string
	Title      string
	Note       string
	RecordedAt time.Time
	CreatedAt  time.Time
}

// GrowthRecordInput contains the editable fields accepted from a guardian.
type GrowthRecordInput struct {
	ChildID  string
	Category string
	Title    string
	Note     string
}

// HabitReminder is one family-scoped schedule entry.
//
// Weekdays uses ISO-8601 numbering (1 = Monday ... 7 = Sunday) so the stored
// value matches the guardian's mental model and never depends on locale.
type HabitReminder struct {
	ID           string
	FamilyID     string
	ChildID      string
	Label        string
	Prompt       string
	ReminderTime string
	Weekdays     []int
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// HabitReminderInput contains the editable fields accepted from a guardian.
type HabitReminderInput struct {
	ChildID      string
	Label        string
	Prompt       string
	ReminderTime string
	Weekdays     []int
	Enabled      bool
}

// GrowthRecordQuery bounds and filters a growth timeline read.
type GrowthRecordQuery struct {
	ChildID    string
	Category   string
	Limit      int
	BeforeTime *time.Time
}

// ValidateGrowthRecordInput normalizes and validates one guardian submission.
// It returns a copy the caller can persist without re-trimming, so the stored
// value always matches what was validated.
func ValidateGrowthRecordInput(input GrowthRecordInput) (GrowthRecordInput, error) {
	normalized := GrowthRecordInput{
		ChildID:  trimSpace(input.ChildID),
		Category: trimSpace(input.Category),
		Title:    trimSpace(input.Title),
		Note:     trimSpace(input.Note),
	}
	if !isGrowthCategory(normalized.Category) {
		return GrowthRecordInput{}, ErrInvalidGrowthCategory
	}
	if lengthInRunes(normalized.Title) < 1 ||
		lengthInRunes(normalized.Title) > GrowthTitleMaxLength {
		return GrowthRecordInput{}, ErrInvalidGrowthRecord
	}
	if lengthInRunes(normalized.Note) > GrowthNoteMaxLength {
		return GrowthRecordInput{}, ErrInvalidGrowthRecord
	}
	return normalized, nil
}

// ValidateHabitReminderInput normalizes and validates one reminder submission.
//
// Weekdays are de-duplicated and sorted so the stored set is canonical; an
// empty weekday set means "every day" and is expanded by the caller.
func ValidateHabitReminderInput(input HabitReminderInput) (HabitReminderInput, error) {
	normalized := HabitReminderInput{
		ChildID:      trimSpace(input.ChildID),
		Label:        trimSpace(input.Label),
		Prompt:       trimSpace(input.Prompt),
		ReminderTime: trimSpace(input.ReminderTime),
		Enabled:      input.Enabled,
	}
	if lengthInRunes(normalized.Label) < 1 ||
		lengthInRunes(normalized.Label) > ReminderLabelMax {
		return HabitReminderInput{}, ErrInvalidReminderLabel
	}
	if lengthInRunes(normalized.Prompt) > ReminderPromptMax {
		return HabitReminderInput{}, ErrInvalidReminderPrompt
	}
	if !isClockTime(normalized.ReminderTime) {
		return HabitReminderInput{}, ErrInvalidReminderTime
	}
	weekdays, ok := normalizeWeekdays(input.Weekdays)
	if !ok {
		return HabitReminderInput{}, ErrInvalidReminderWeekday
	}
	normalized.Weekdays = weekdays
	return normalized, nil
}

// IsGrowthCategory reports whether category is one of the stable values.
func IsGrowthCategory(category string) bool {
	return isGrowthCategory(trimSpace(category))
}

func isGrowthCategory(category string) bool {
	for _, candidate := range GrowthCategories {
		if candidate == category {
			return true
		}
	}
	return false
}

// isClockTime reports whether value is a 24-hour HH:MM wall-clock time.
func isClockTime(value string) bool {
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	hour := twoDigits(value[0])*10 + twoDigits(value[1])
	minute := twoDigits(value[3])*10 + twoDigits(value[4])
	return hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59
}

// normalizeWeekdays de-duplicates, validates and sorts an ISO weekday set.
func normalizeWeekdays(weekdays []int) ([]int, bool) {
	seen := make([]bool, 8)
	result := make([]int, 0, len(weekdays))
	for _, weekday := range weekdays {
		if weekday < 1 || weekday > 7 {
			return nil, false
		}
		if seen[weekday] {
			continue
		}
		seen[weekday] = true
		result = append(result, weekday)
	}
	// Insertion sort keeps the helper allocation-free and the set tiny.
	for index := 1; index < len(result); index++ {
		for cursor := index; cursor > 0 && result[cursor-1] > result[cursor]; cursor-- {
			result[cursor-1], result[cursor] = result[cursor], result[cursor-1]
		}
	}
	return result, true
}

// twoDigits converts one ASCII digit byte to its value, or -1 when it is not a
// digit so the caller's range check rejects it.
func twoDigits(value byte) int {
	if value < '0' || value > '9' {
		return -1
	}
	return int(value - '0')
}

func trimSpace(value string) string {
	return strings.TrimSpace(value)
}

func lengthInRunes(value string) int {
	return utf8.RuneCountInString(value)
}
