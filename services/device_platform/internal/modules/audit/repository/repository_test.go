package repository

import (
	"reflect"
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

type provisioningStatusStub struct {
	values []any
	err    error
}

func (stub *provisioningStatusStub) Scan(dest ...any) error {
	if stub.err != nil {
		return stub.err
	}
	if len(dest) != len(stub.values) {
		return nil
	}
	for index := range dest {
		switch target := dest[index].(type) {
		case **string:
			if stub.values[index] == nil {
				*target = nil
				continue
			}
			*target = stub.values[index].(*string)
		case **bool:
			if stub.values[index] == nil {
				*target = nil
				continue
			}
			*target = stub.values[index].(*bool)
		case **time.Time:
			if stub.values[index] == nil {
				*target = nil
				continue
			}
			*target = stub.values[index].(*time.Time)
		case **uint64:
			if stub.values[index] == nil {
				*target = nil
				continue
			}
			*target = stub.values[index].(*uint64)
		case *time.Time:
			value := stub.values[index]
			if value == nil {
				*target = time.Time{}
			} else {
				*target = value.(time.Time)
			}
		default:
			panic("unexpected scan target type " + reflect.TypeOf(target).String())
		}
	}
	return nil
}

func TestScanProvisioningStatusAcceptsNullOptionalColumns(t *testing.T) {
	var state, sessionState *string
	var wifiConfigured *bool
	var lastProvisionedAt *time.Time
	var droppedEvents *uint64
	var updatedAt time.Time
	row := &provisioningStatusStub{
		values: []any{nil, nil, nil, nil, nil, nil},
	}

	if err := scanProvisioningStatus(
		row,
		&state,
		&wifiConfigured,
		&sessionState,
		&lastProvisionedAt,
		&droppedEvents,
		&updatedAt,
	); err != nil {
		t.Fatalf("expected NULL optional columns to scan, got %v", err)
	}
	if state != nil || wifiConfigured != nil || sessionState != nil ||
		lastProvisionedAt != nil || droppedEvents != nil {
		t.Fatalf("expected nullable columns to remain nil")
	}
}

func TestProvisioningFreshnessUsesDeviceReportedTime(t *testing.T) {
	if provisioningFreshnessPredicate != "device_runtime_status.reported_at <= $7" {
		t.Fatalf(
			"provisioning status must be gated by the device-reported runtime timestamp, got %q",
			provisioningFreshnessPredicate,
		)
	}
}
