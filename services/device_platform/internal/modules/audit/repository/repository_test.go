package repository

import (
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
)

func TestDeriveHealthStateUsesSequenceForSameHeartbeatEvents(t *testing.T) {
	reportedAt := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	latestFailure := domain.ModuleFailure{
		Sequence:   11,
		ReportedAt: reportedAt,
	}
	snapshot := &domain.Snapshot{
		LatestFailure: &latestFailure,
		RecoveryEvents: []domain.RecoveryEvent{
			{
				Sequence:   12,
				ReportedAt: reportedAt,
			},
		},
	}

	if healthState := deriveHealthState(snapshot); healthState != domain.HealthDegraded {
		t.Fatalf("expected degraded after a later recovery, got %q", healthState)
	}

	snapshot.RecoveryEvents[0].Sequence = 10
	if healthState := deriveHealthState(snapshot); healthState != domain.HealthFaulted {
		t.Fatalf("expected faulted when the failure is newer, got %q", healthState)
	}
}
