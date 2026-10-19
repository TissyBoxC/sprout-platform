package domain

// ReadinessState is the companionship capability health derived from growth
// records and habit reminder schedules.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive companionship capability snapshot. It must
// never contain a family or child identifier, growth title, note or reminder
// prompt.
type Readiness struct {
	State ReadinessState
	// GrowthRecordCount is the total number of stored growth timeline entries.
	GrowthRecordCount int64
	// HabitReminderCount is the total number of stored habit reminders.
	HabitReminderCount int64
	// EnabledHabitReminderCount is the number of active habit reminders.
	EnabledHabitReminderCount int64
}
