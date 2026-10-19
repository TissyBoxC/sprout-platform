package domain

// ReadinessState is the capability health derived from the privacy lifecycle
// store. It never includes consent content or account identifiers.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the guardian consent, export, and deletion capability snapshot.
type Readiness struct {
	State                ReadinessState
	ConsentEventCount    int64
	PendingDeletionCount int64
	StaleDeletionCount   int64
	FailedDeletionCount  int64
	ExportReceiptCount   int64
	// RecentAuditEventCount counts durable audit rows inside the probe window.
	// A security operation without a durable audit trail is degraded even when
	// every lifecycle queue is otherwise readable.
	RecentAuditEventCount int64
	// AuditCoverageProof is true when a recent durable audit row exists. It is
	// derived by the service, never trusted from the repository snapshot.
	AuditCoverageProof   bool
	RetentionReaderReady bool
	DeletionWorkerReady  bool
}
