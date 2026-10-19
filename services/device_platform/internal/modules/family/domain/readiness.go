package domain

// ReadinessState is the capability health derived from membership and
// invitation persistence. It never exposes account identifiers or emails.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the family membership capability snapshot.
type Readiness struct {
	State                  ReadinessState
	FamilyCount            int64
	MemberCount            int64
	OwnerCount             int64
	OwnerlessFamilyCount   int64
	MultiOwnerFamilyCount  int64
	PendingInvitationCount int64
	// ParentAccountCount is the number of active guardian accounts. Because
	// every account bootstraps exactly one family, a count mismatch exposes a
	// broken registration chain that an owner-only query cannot see.
	ParentAccountCount int64
}
