package domain

// ReadinessState is the alarm reminder capability health derived from alarm
// schedules and device firing acknowledgements.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive alarm reminder capability snapshot. It must
// never contain a family identifier, alarm label, prompt or device identifier.
type Readiness struct {
	State ReadinessState
	// AlarmCount is the total number of stored alarm schedules.
	AlarmCount int64
	// EnabledCount is the number of alarms currently active.
	EnabledCount int64
	// EventCount is the number of stored firing acknowledgements.
	EventCount int64
}
