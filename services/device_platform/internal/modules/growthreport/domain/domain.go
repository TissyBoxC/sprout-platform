// Package domain holds the growth-report value types and the rules that decide
// what a report may say.
//
// A report is a summary, never a transcript: every field here is an aggregate
// or a sentence derived from aggregates, so the type itself makes it hard to
// leak conversation text or a memory value into something a guardian shares.
package domain

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidReport is returned when a caller asks for a period that cannot be
// summarised, such as a negative limit or an unknown period type.
var ErrInvalidReport = errors.New("invalid growth report request")

// PeriodType selects the reporting window.
type PeriodType string

const (
	// PeriodWeekly covers one Monday-to-Monday window.
	PeriodWeekly PeriodType = "weekly"
	// PeriodMonthly covers one calendar month.
	PeriodMonthly PeriodType = "monthly"
)

// IsValid reports whether the value is a window the generator can build.
func (p PeriodType) IsValid() bool {
	return p == PeriodWeekly || p == PeriodMonthly
}

// Window returns the half-open [start, end) range that contains the instant.
//
// Both periods start on a UTC boundary so a report never straddles a daylight
// saving change and two runs of the generator cannot disagree about the range.
func (p PeriodType) Window(at time.Time) (time.Time, time.Time, error) {
	utc := at.UTC()
	day := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	switch p {
	case PeriodWeekly:
		// Go weekdays start on Sunday; the product reports Monday first because
		// a week's learning pattern reads better from a school week.
		offset := (int(day.Weekday()) + 6) % 7
		start := day.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 7), nil
	case PeriodMonthly:
		start := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0), nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf(
			"%w: unknown period type %q",
			ErrInvalidReport,
			string(p),
		)
	}
}

// PreviousWindow returns the window immediately before the given one.
//
// Growth only reads as growth against an earlier period, so every report is
// built together with its predecessor's numbers.
func (p PeriodType) PreviousWindow(
	start time.Time,
	end time.Time,
) (time.Time, time.Time, error) {
	switch p {
	case PeriodWeekly:
		return start.AddDate(0, 0, -7), start, nil
	case PeriodMonthly:
		previousStart := start.AddDate(0, -1, 0)
		return previousStart, start, nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf(
			"%w: unknown period type %q",
			ErrInvalidReport,
			string(p),
		)
	}
}

// PracticeTotals counts English practice within one window.
type PracticeTotals struct {
	Attempts int `json:"attempts"`
	Passed   int `json:"passed"`
	// AverageScore is the mean of per-attempt scores, rounded to one decimal so
	// the value reads cleanly in a sentence.
	AverageScore float64 `json:"average_score"`
}

// UsageTotals sums device activity within one window.
//
// Usage is recorded per device rather than per child, so a family with two
// devices on one account contributes both. The report states this as household
// activity instead of pretending it is one child's screen time.
type UsageTotals struct {
	ActiveSeconds       int `json:"active_seconds"`
	ConversationCount   int `json:"conversation_count"`
	ConversationSeconds int `json:"conversation_seconds"`
}

// CountTotals counts guardian-authored entries within one window.
type CountTotals struct {
	GrowthRecords int `json:"growth_records"`
	Memories      int `json:"memories"`
}

// Metrics is everything a report shows, including the comparison that makes it
// a growth report rather than a usage table.
type Metrics struct {
	Practice          PracticeTotals `json:"practice"`
	Usage             UsageTotals    `json:"usage"`
	Counts            CountTotals    `json:"counts"`
	PreviousPractice  PracticeTotals `json:"previous_practice"`
	PreviousUsage     UsageTotals    `json:"previous_usage"`
	PracticeDelta     int            `json:"practice_delta"`
	AccuracyDelta     float64        `json:"accuracy_delta"`
	ActiveSecondsDiff int            `json:"active_seconds_delta"`
}

// Accuracy returns the pass rate as a percentage, or zero when nothing was
// attempted so a report never divides by zero.
func (p PracticeTotals) Accuracy() float64 {
	if p.Attempts == 0 {
		return 0
	}
	return float64(p.Passed) / float64(p.Attempts) * 100
}

// NewMetrics pairs one window's totals with the previous window's totals and
// derives the deltas the summary sentence compares.
func NewMetrics(
	current PracticeTotals,
	previous PracticeTotals,
	currentUsage UsageTotals,
	previousUsage UsageTotals,
	counts CountTotals,
) Metrics {
	return Metrics{
		Practice:          current,
		PreviousPractice:  previous,
		Usage:             currentUsage,
		PreviousUsage:     previousUsage,
		Counts:            counts,
		PracticeDelta:     current.Attempts - previous.Attempts,
		AccuracyDelta:     current.Accuracy() - previous.Accuracy(),
		ActiveSecondsDiff: currentUsage.ActiveSeconds - previousUsage.ActiveSeconds,
	}
}

// Report is one stored growth report.
type Report struct {
	ID          string
	FamilyID    string
	ChildID     string
	PeriodType  PeriodType
	PeriodStart time.Time
	PeriodEnd   time.Time
	Metrics     Metrics
	Summary     string
	GeneratedAt time.Time
}

// Request describes one report the caller wants built.
type Request struct {
	FamilyID string
	ChildID  string
	Period   PeriodType
	// Now fixes the window. The generator passes the run instant so a retried
	// job cannot land in a different week.
	Now time.Time
}

// Validate rejects a request the generator cannot satisfy.
func (r Request) Validate() error {
	if r.FamilyID == "" || r.ChildID == "" {
		return fmt.Errorf("%w: family and child are required", ErrInvalidReport)
	}
	if !r.Period.IsValid() {
		return fmt.Errorf(
			"%w: unknown period type %q",
			ErrInvalidReport,
			string(r.Period),
		)
	}
	if r.Now.IsZero() {
		return fmt.Errorf("%w: run instant is required", ErrInvalidReport)
	}
	return nil
}
