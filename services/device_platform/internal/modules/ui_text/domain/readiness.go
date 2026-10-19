package domain

// ReadinessState is the remote UI text capability health derived from the
// authoritative package store and publication signing dependencies.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive UI text capability snapshot. It must never
// contain entry values, review reasons or actor identifiers.
type Readiness struct {
	State ReadinessState
	// CatalogRevision is the persisted published-catalog cursor.
	CatalogRevision int64
	// PublishedPackageCount is the number of active published versions.
	PublishedPackageCount int64
	// DraftPackageCount is the number of versions still editable.
	DraftPackageCount int64
	// InReviewPackageCount is the number of versions awaiting a decision.
	InReviewPackageCount int64
	// SignatureVerifierReady reports whether publishing can verify detached
	// signatures against the configured trusted keyring.
	SignatureVerifierReady bool
}
