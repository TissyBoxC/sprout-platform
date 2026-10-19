package domain

// ReadinessState is the local-command capability health derived from command
// sets and their family revision clocks.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive local-command capability snapshot. It must
// never contain family identifiers, trigger phrases or command arguments.
type Readiness struct {
	State ReadinessState
	// CommandCount is the total number of stored command macros.
	CommandCount int64
	// EnabledCommandCount is the number of enabled command macros.
	EnabledCommandCount int64
	// FamilyCount is the number of families with at least one command revision.
	FamilyCount int64
	// CommandFamilyCount is the number of families that have command macros.
	// It is compared with FamilyCount so every command set has a revision
	// clock that devices can use for incremental synchronization.
	CommandFamilyCount int64
}
