package domain

// ReadinessState is the growth-report capability health derived from the stored
// report table.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive growth-report snapshot reported to the Feature
// Center. It must never contain a family id or child id.
type Readiness struct {
	State ReadinessState
	// ReportCount is the total number of generated reports.
	ReportCount int64
	// ChildCount is the number of children that have at least one report.
	ChildCount int64
	// StaleDays is how many days have passed since the newest generation. A
	// weekly report is produced at least once a week, so a large value means the
	// generator stopped even though the process is still running.
	StaleDays int64
	// HasAny is false before the first report is generated, which is a young
	// deployment rather than a fault but still not a healthy state.
	HasAny bool
}
