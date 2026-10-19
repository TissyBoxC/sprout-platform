package domain

// ReadinessState is the guardian playback capability health derived from the
// playback request store and the device/content validation adapters.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive playback capability snapshot. It must never
// contain family or device identifiers, notes or content titles.
type Readiness struct {
	State ReadinessState
	// RequestCount is the total number of stored playback requests.
	RequestCount int64
	// PendingCount is the number of pending or acknowledged requests.
	PendingCount int64
	// FailedCount is the number of requests that ended in failure.
	FailedCount int64
	// DeviceReaderReady reports whether requests can verify family ownership.
	DeviceReaderReady bool
	// PackageReaderReady reports whether requests can verify publication.
	PackageReaderReady bool
}
