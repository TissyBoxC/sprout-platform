package domain

// ReadinessState is the growth-timeline capability health.
type ReadinessState string

const (
	// ReadinessHealthy means at least one source table answered a real read.
	ReadinessHealthy ReadinessState = "healthy"
	// ReadinessDegraded is reserved for a partial source: the timeline serves
	// but some entries cannot be read. The service reports it as unavailable
	// today rather than serving a gap, so nothing sets this yet.
	ReadinessDegraded ReadinessState = "degraded"
	// ReadinessUnavailable means no source could be read, so every guardian
	// would see an empty story.
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive timeline health reported to the Feature
// Center. It never contains a family or child identifier.
type Readiness struct {
	State ReadinessState
}
