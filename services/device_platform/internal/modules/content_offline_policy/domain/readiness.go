package domain

// ReadinessState is the offline-content capability health derived from the
// singleton download policy and its device acknowledgement ledger.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive offline-content capability snapshot. It must
// never contain family or device identifiers.
type Readiness struct {
	State ReadinessState
	// PolicyRevision is the currently effective policy revision.
	PolicyRevision int
	// OfflineEnabled reports whether devices may play cached content offline.
	OfflineEnabled bool
	// AcknowledgedDeviceCount is the number of devices with an acknowledged
	// policy revision.
	AcknowledgedDeviceCount int64
}
