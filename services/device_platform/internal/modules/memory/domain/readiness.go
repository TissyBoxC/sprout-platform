package domain

// ReadinessState is the child memory capability health derived from the stored
// memory table.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive child memory capability snapshot. It must
// never contain a family id, child id, memory key, or memory value.
type Readiness struct {
	State ReadinessState
	// MemoryCount is the total number of stored memories.
	MemoryCount int64
	// ChildCount is the number of children that currently have at least one
	// stored memory.
	ChildCount int64
	// OverLimitChildCount is the number of children at or above the configured
	// per-child limit. A non-zero value is a degraded snapshot: new facts for
	// those children will be refused until the guardian prunes them.
	OverLimitChildCount int64
	// Limit is the per-child cap the snapshot was computed against.
	Limit int
}
