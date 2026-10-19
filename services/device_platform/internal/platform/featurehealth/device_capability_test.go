package featurehealth

import (
	"context"
	"testing"
	"time"
)

func TestDeviceCapabilityStatusStateMatrix(t *testing.T) {
	cases := []struct {
		name     string
		facts    DeviceCapabilityFacts
		required bool
		want     Status
	}{
		{
			name:  "no expected devices is unknown",
			facts: DeviceCapabilityFacts{},
			want:  StatusUnknown,
		},
		{
			name:     "required capability without devices is degraded",
			facts:    DeviceCapabilityFacts{},
			required: true,
			want:     StatusDegraded,
		},
		{
			name: "complete current reports are healthy",
			facts: DeviceCapabilityFacts{
				ExpectedDevices: 2,
				ReportedDevices: 2,
			},
			want: StatusHealthy,
		},
		{
			name: "missing state is degraded",
			facts: DeviceCapabilityFacts{
				ExpectedDevices: 2,
				ReportedDevices: 1,
			},
			want: StatusDegraded,
		},
		{
			name: "explicit fault is degraded",
			facts: DeviceCapabilityFacts{
				ExpectedDevices: 2,
				ReportedDevices: 2,
				FaultedDevices:  1,
			},
			want: StatusDegraded,
		},
		{
			name: "stale state is degraded",
			facts: DeviceCapabilityFacts{
				ExpectedDevices: 2,
				ReportedDevices: 2,
				StaleDevices:    1,
			},
			want: StatusDegraded,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := DeviceCapabilityStatus(test.facts, test.required)
			if err != nil {
				t.Fatalf("derive status: %v", err)
			}
			if got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDeviceCapabilityStatusRejectsInconsistentFacts(t *testing.T) {
	cases := []DeviceCapabilityFacts{
		{ExpectedDevices: 1, ReportedDevices: 2},
		{ExpectedDevices: 1, FaultedDevices: 2},
		{ExpectedDevices: 1, StaleDevices: 2},
	}
	for _, facts := range cases {
		status, err := DeviceCapabilityStatus(facts, false)
		if err == nil || status != StatusUnavailable {
			t.Fatalf("facts %+v: status = %q, err = %v", facts, status, err)
		}
	}
}

func TestValidateFreshness(t *testing.T) {
	if err := ValidateFreshness(24 * time.Hour); err != nil {
		t.Fatalf("valid freshness rejected: %v", err)
	}
	if err := ValidateFreshness(0); err == nil {
		t.Fatal("zero freshness accepted")
	}
	if err := ValidateFreshness(91 * 24 * time.Hour); err == nil {
		t.Fatal("unbounded freshness accepted")
	}
}

func TestStatusValid(t *testing.T) {
	for _, status := range []Status{
		StatusHealthy,
		StatusDegraded,
		StatusUnknown,
		StatusUnavailable,
	} {
		if !status.Valid() {
			t.Fatalf("status %q is invalid", status)
		}
	}
	if Status("invalid").Valid() {
		t.Fatal("invalid status accepted")
	}
}

func TestReadDeviceStateReadinessRejectsNilPool(t *testing.T) {
	got, err := ReadDeviceStateReadiness(
		context.Background(),
		DeviceStateQuery{
			Capability:      "display",
			StateTable:      "display_device_status",
			TimestampColumn: "reported_at",
		},
		time.Hour,
	)
	if err == nil || got.Status != StatusUnavailable {
		t.Fatalf("status = %q, err = %v", got.Status, err)
	}
}

func TestReadDeviceStateReadinessRejectsUnsafeQueryParts(t *testing.T) {
	for _, query := range []DeviceStateQuery{
		{
			StateTable:      "display_device_status; DROP TABLE devices",
			TimestampColumn: "reported_at",
		},
		{
			StateTable:      "display_device_status",
			TimestampColumn: "reported_at OR TRUE",
		},
		{
			StateTable:      "display_device_status",
			TimestampColumn: "reported_at",
			FaultCondition:  "TRUE; DROP TABLE device_credentials",
		},
	} {
		if _, err := ReadDeviceStateReadiness(
			context.Background(),
			query,
			time.Hour,
		); err == nil {
			t.Fatalf("unsafe query accepted: %+v", query)
		}
	}
}
