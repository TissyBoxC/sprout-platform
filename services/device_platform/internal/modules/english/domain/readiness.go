package domain

// ReadinessState is the English-learning capability health derived from the
// published lesson catalogue and practice-attempt store.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive English-learning capability snapshot. It must
// never contain lesson text, recognized speech, child identifiers or scores.
type Readiness struct {
	State ReadinessState
	// PublishedLessonCount is the number of published lessons.
	PublishedLessonCount int64
	// PracticeAttemptCount is the number of persisted practice attempts.
	PracticeAttemptCount int64
	// CatalogByAgeTier reports whether every approved age tier has at least one
	// published lesson. A missing tier degrades learning without blocking the
	// other age groups.
	CatalogByAgeTier map[string]bool
}
