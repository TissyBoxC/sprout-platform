package domain

// ReadinessState is the capability health derived from policy persistence.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive parent policy capability snapshot.
type Readiness struct {
	State              ReadinessState
	PolicyCount        int64
	FamilyCount        int64
	RevisionRowCount   int64
	VoiceDefaultsReady bool
}
