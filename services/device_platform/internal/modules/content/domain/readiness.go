package domain

// ReadinessState is the content-library capability health derived from the
// authoritative package, review and delivery stores.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive content-library capability snapshot. It must
// never contain package titles, asset keys, review reasons or user identifiers.
type Readiness struct {
	State ReadinessState
	// CatalogRevision is the persisted delivery cursor.
	CatalogRevision int64
	// PublishedPackageCount is the number of published logical packages.
	PublishedPackageCount int64
	// ReviewedPackageCount is the number of packages that have ever been
	// submitted for review, including currently published and withdrawn rows.
	ReviewedPackageCount int64
	// AssetReaderReady reports whether publication and download can verify the
	// shared content blob store.
	AssetReaderReady bool
	// PolicyReaderReady reports whether family filtering can be enforced. Its
	// absence degrades the device path without making admin catalog reads fail.
	PolicyReaderReady bool
}

// EngagementReadiness is the non-sensitive favourite and play-history
// capability snapshot. It contains aggregate counts only.
type EngagementReadiness struct {
	State            ReadinessState
	FavoriteCount    int64
	PlayHistoryCount int64
}

// ThemePackReadiness is the non-sensitive theme-pack lifecycle snapshot. It
// contains aggregate counts only.
type ThemePackReadiness struct {
	State          ReadinessState
	ThemePackCount int64
	PublishedCount int64
	DraftCount     int64
	WithdrawnCount int64
}
