// Package domain defines the guardian alarm reminder schedule and the bounded
// per-alarm firing acknowledgement for the alarm_reminder module.
//
// A family owns a bounded set of alarms. Each alarm keeps a local wall-clock
// time, an ISO weekday set, a label, an optional prompt and the child it
// belongs to. The device renders and rings the alarm locally from its own tone
// catalog; the platform stores only the schedule and the bounded firing state,
// never any audio.
package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrInvalidAlarm reports a malformed alarm payload.
	ErrInvalidAlarm = errors.New("invalid alarm reminder")
	// ErrAlarmNotFound reports a missing alarm.
	ErrAlarmNotFound = errors.New("alarm reminder not found")
	// ErrInvalidAlarmEvent reports a malformed firing acknowledgement.
	ErrInvalidAlarmEvent = errors.New("invalid alarm reminder event")
)

// Approved bounds.
const (
	LabelMaxLength  = 48
	PromptMaxLength = 240
	// AlarmCapacity bounds how many alarms one family may schedule.
	AlarmCapacity = 64
	MinSnoozeMin  = 1
	MaxSnoozeMin  = 30
)

// IsoWeekdayCount is the number of ISO weekdays (1 = Monday ... 7 = Sunday).
const IsoWeekdayCount = 7

// Alarm firing states the platform recognises.
const (
	StateRinging   = "ringing"
	StateSnoozed   = "snoozed"
	StateDismissed = "dismissed"
	StateMissed    = "missed"
)

// AlarmStates is the ordered firing-state catalog exposed to clients.
var AlarmStates = []string{StateRinging, StateSnoozed, StateDismissed, StateMissed}

// Alarm is one family-scoped scheduled alarm.
type Alarm struct {
	ID        string
	FamilyID  string
	ChildID   string
	Label     string
	Prompt    string
	AlarmTime string
	Weekdays  []int
	Enabled   bool
	SnoozeMin int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AlarmInput contains the editable fields accepted from a guardian.
type AlarmInput struct {
	ChildID   string
	Label     string
	Prompt    string
	AlarmTime string
	Weekdays  []int
	Enabled   bool
	SnoozeMin int
}

// AlarmEventInput is one bounded firing acknowledgement from a device.
type AlarmEventInput struct {
	AlarmDate string
	State     string
}

// AlarmEvent is the stored latest firing state for one alarm and local date.
type AlarmEvent struct {
	ID         string
	AlarmID    string
	FamilyID   string
	DeviceID   string
	AlarmDate  string
	State      string
	ReportedAt time.Time
}

// DeviceAlarm is one alarm row delivered to a bound device, carrying the
// concrete schedule the device matches locally.
type DeviceAlarm struct {
	ID        string    `json:"id"`
	ChildID   string    `json:"child_id"`
	Label     string    `json:"label"`
	Prompt    string    `json:"prompt"`
	AlarmTime string    `json:"alarm_time"`
	Weekdays  []int     `json:"weekdays"`
	SnoozeMin int       `json:"snooze_minutes"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ValidateAlarmInput normalizes and validates one alarm payload. Weekdays are
// de-duplicated and sorted so the stored set is canonical; an empty weekday set
// means "every day" and is expanded by the caller.
func ValidateAlarmInput(input AlarmInput) (AlarmInput, error) {
	normalized := AlarmInput{
		ChildID:   strings.TrimSpace(input.ChildID),
		Label:     strings.TrimSpace(input.Label),
		Prompt:    strings.TrimSpace(input.Prompt),
		AlarmTime: strings.TrimSpace(input.AlarmTime),
		Enabled:   input.Enabled,
		SnoozeMin: input.SnoozeMin,
	}
	if runeLength(normalized.Label) < 1 ||
		runeLength(normalized.Label) > LabelMaxLength {
		return AlarmInput{}, ErrInvalidAlarm
	}
	if runeLength(normalized.Prompt) > PromptMaxLength {
		return AlarmInput{}, ErrInvalidAlarm
	}
	if !isClockTime(normalized.AlarmTime) {
		return AlarmInput{}, ErrInvalidAlarm
	}
	weekdays, ok := normalizeWeekdays(input.Weekdays)
	if !ok {
		return AlarmInput{}, ErrInvalidAlarm
	}
	normalized.Weekdays = weekdays
	if normalized.SnoozeMin < MinSnoozeMin ||
		normalized.SnoozeMin > MaxSnoozeMin {
		return AlarmInput{}, ErrInvalidAlarm
	}
	return normalized, nil
}

// ValidateAlarmEvent normalizes and validates one firing acknowledgement.
func ValidateAlarmEvent(input AlarmEventInput) (AlarmEventInput, error) {
	normalized := AlarmEventInput{
		AlarmDate: strings.TrimSpace(input.AlarmDate),
		State:     strings.TrimSpace(input.State),
	}
	if !isAlarmDate(normalized.AlarmDate) {
		return AlarmEventInput{}, ErrInvalidAlarmEvent
	}
	switch normalized.State {
	case StateRinging, StateSnoozed, StateDismissed, StateMissed:
	default:
		return AlarmEventInput{}, ErrInvalidAlarmEvent
	}
	return normalized, nil
}

// IsState reports whether a firing state is accepted.
func IsState(state string) bool {
	for _, candidate := range AlarmStates {
		if candidate == state {
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

// isAlarmDate reports whether value is a YYYY-MM-DD local date.
func isAlarmDate(value string) bool {
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return false
	}
	for index, char := range value {
		if index == 4 || index == 7 {
			continue
		}
		if char < '0' || char > '9' {
			return false
		}
	}
	month := twoDigits(value[5])*10 + twoDigits(value[6])
	day := twoDigits(value[8])*10 + twoDigits(value[9])
	return month >= 1 && month <= 12 && day >= 1 && day <= 31
}

// normalizeWeekdays de-duplicates, validates and sorts an ISO weekday set.
func normalizeWeekdays(weekdays []int) ([]int, bool) {
	seen := make([]bool, IsoWeekdayCount+1)
	result := make([]int, 0, len(weekdays))
	for _, weekday := range weekdays {
		if weekday < 1 || weekday > IsoWeekdayCount {
			return nil, false
		}
		if seen[weekday] {
			continue
		}
		seen[weekday] = true
		result = append(result, weekday)
	}
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

func runeLength(value string) int {
	return utf8.RuneCountInString(value)
}
