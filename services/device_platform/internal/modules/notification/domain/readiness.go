package domain

// ReadinessState is the capability health derived from notification
// persistence and delivery queue state.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive notification capability snapshot.
type Readiness struct {
	State                  ReadinessState
	NotificationCount      int64
	PendingDeliveryCount   int64
	FailedDeliveryCount    int64
	ExhaustedDeliveryCount int64
	ConfiguredChannelCount int
}
