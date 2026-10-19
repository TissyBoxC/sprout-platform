package domain

// ReadinessState is the recommendation capability health derived from the
// published catalog and the family signal readers.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive recommendation capability snapshot. It must
// never contain family identifiers or the titles of recommended packages.
type Readiness struct {
	State ReadinessState
	// CandidateCount is the number of published catalog candidates.
	CandidateCount int
	// SignalsReady reports whether favourites and playback history can be read
	// without a family scope.
	SignalsReady bool
}
