package domain

// ReadinessState is the capability health derived from child profile
// persistence and the policy dependency needed for safe execution.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive child profile capability snapshot.
type Readiness struct {
	State               ReadinessState
	ProfileCount        int64
	FamiliesWithProfile int64
	// OrphanedProfileCount counts profiles without the mandatory parent policy.
	// It is an invariant violation, not normal first-deployment emptiness.
	OrphanedProfileCount int64
	// PolicyCount counts the policies joined to these profiles. It is compared
	// with ProfileCount so one profile cannot resolve a different policy.
	PolicyCount            int64
	PolicyProvisionerReady bool
}
