package domain

// ReadinessState is the usage-report capability health derived from the
// authoritative daily usage ledger and policy dependency.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive usage-report capability snapshot. It must
// never contain device identifiers, family identifiers or usage rows.
type Readiness struct {
	State ReadinessState
	// ReportedDayCount is the number of persisted device-day reports.
	ReportedDayCount int64
	// ReportingDeviceCount is the number of devices with at least one report.
	ReportingDeviceCount int64
	// ReportingFamilyCount is the number of guardian accounts with usage data.
	ReportingFamilyCount int64
	// PolicyReaderReady reports whether remaining-time summaries can resolve a
	// family's effective daily limit.
	PolicyReaderReady bool
}
