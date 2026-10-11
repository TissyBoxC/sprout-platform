package domain

import (
	"errors"
	"testing"
)

func validInput() AlarmInput {
	return AlarmInput{
		ChildID:   "child-1",
		Label:     "起床",
		Prompt:    "该起床啦，今天也要开心哦。",
		AlarmTime: "07:30",
		Weekdays:  []int{5, 1, 3, 1},
		Enabled:   true,
		SnoozeMin: 10,
	}
}

func TestValidateAlarmInputNormalizesWeekdays(t *testing.T) {
	normalized, err := ValidateAlarmInput(validInput())
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	// De-duplicated and sorted ascending.
	want := []int{1, 3, 5}
	if len(normalized.Weekdays) != len(want) {
		t.Fatalf("weekdays = %v, want %v", normalized.Weekdays, want)
	}
	for index, weekday := range want {
		if normalized.Weekdays[index] != weekday {
			t.Fatalf("weekdays = %v, want %v", normalized.Weekdays, want)
		}
	}
}

func TestValidateAlarmInputRejectsBadValues(t *testing.T) {
	cases := map[string]AlarmInput{
		"empty label": func() AlarmInput {
			bad := validInput()
			bad.Label = "   "
			return bad
		}(),
		"bad time": func() AlarmInput {
			bad := validInput()
			bad.AlarmTime = "25:00"
			return bad
		}(),
		"bad weekday": func() AlarmInput {
			bad := validInput()
			bad.Weekdays = []int{0, 8}
			return bad
		}(),
		"snooze too small": func() AlarmInput {
			bad := validInput()
			bad.SnoozeMin = 0
			return bad
		}(),
		"snooze too large": func() AlarmInput {
			bad := validInput()
			bad.SnoozeMin = 31
			return bad
		}(),
	}
	for name, input := range cases {
		if _, err := ValidateAlarmInput(input); !errors.Is(err, ErrInvalidAlarm) {
			t.Fatalf("%s: err = %v, want ErrInvalidAlarm", name, err)
		}
	}
}

func TestValidateAlarmInputEmptyWeekdaysAllowed(t *testing.T) {
	// An empty weekday set is valid and means "every day"; the service expands
	// it to a concrete schedule.
	input := validInput()
	input.Weekdays = nil
	normalized, err := ValidateAlarmInput(input)
	if err != nil {
		t.Fatalf("empty weekdays rejected: %v", err)
	}
	if len(normalized.Weekdays) != 0 {
		t.Fatalf("weekdays = %v, want empty", normalized.Weekdays)
	}
}

func TestValidateAlarmEvent(t *testing.T) {
	valid := AlarmEventInput{AlarmDate: "2026-03-04", State: StateDismissed}
	if _, err := ValidateAlarmEvent(valid); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	badDate := valid
	badDate.AlarmDate = "2026-13-40"
	if _, err := ValidateAlarmEvent(badDate); !errors.Is(err, ErrInvalidAlarmEvent) {
		t.Fatalf("err = %v, want ErrInvalidAlarmEvent", err)
	}
	badState := valid
	badState.State = "exploded"
	if _, err := ValidateAlarmEvent(badState); !errors.Is(err, ErrInvalidAlarmEvent) {
		t.Fatalf("err = %v, want ErrInvalidAlarmEvent", err)
	}
}

func TestIsState(t *testing.T) {
	if !IsState(StateRinging) || IsState("unknown") {
		t.Fatalf("state catalog check failed")
	}
}
