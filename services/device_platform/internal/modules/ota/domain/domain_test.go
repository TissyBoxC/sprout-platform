package domain

import "testing"

func TestRollbackEventsRemainInProgressUntilTerminalEvent(t *testing.T) {
	t.Parallel()

	startedStatus := StatusForEvent(EventTypeRollbackStarted)
	if startedStatus != DeploymentStatusInstalling {
		t.Fatalf("rollback_started status = %q, want %q", startedStatus, DeploymentStatusInstalling)
	}
	if !CanTransitionDeployment(DeploymentStatusPendingVerify, startedStatus) {
		t.Fatalf("pending_verify must accept rollback_started (%q)", startedStatus)
	}

	terminalStatus := StatusForEvent(EventTypeRolledBack)
	if terminalStatus != DeploymentStatusRolledBack {
		t.Fatalf("rolled_back status = %q, want %q", terminalStatus, DeploymentStatusRolledBack)
	}
	if !CanTransitionDeployment(startedStatus, terminalStatus) {
		t.Fatalf("%q must accept rolled_back (%q)", startedStatus, terminalStatus)
	}

	for _, next := range []DeploymentStatus{
		DeploymentStatusQueued,
		DeploymentStatusDownloading,
		DeploymentStatusPendingVerify,
	} {
		if CanTransitionDeployment(terminalStatus, next) {
			t.Fatalf("terminal rolled_back must not transition to %q", next)
		}
	}
}
