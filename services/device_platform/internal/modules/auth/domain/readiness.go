package domain

// ReadinessState is the capability health derived from authoritative account
// and session persistence. It deliberately excludes credentials and personal
// data.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the account and login capability snapshot consumed by the
// management health probe.
type Readiness struct {
	State                ReadinessState
	ActiveParentCount    int64
	ActiveAdminCount     int64
	ActiveSessionCount   int64
	MFAEnabledAdminCount int64
	MFACipherReady       bool
	MFAAuditorReady      bool
}
