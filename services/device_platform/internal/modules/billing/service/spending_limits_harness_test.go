package service

import (
	"context"
	"testing"
)

// recordingAuditRecorder captures billing audit events so a test can prove a
// guardian change or an over-limit refusal left a trail.
type recordingAuditRecorder struct {
	events []BillingAuditEvent
}

// RecordBillingAudit appends one event.
func (recorder *recordingAuditRecorder) RecordBillingAudit(
	ctx context.Context,
	event BillingAuditEvent,
) error {
	recorder.events = append(recorder.events, event)
	return nil
}

// newHarnessWithAudit builds the service set with an audit recorder attached.
//
// It goes through the billing facade so the order service and the cap service
// share the same enforcement object, exactly as the composition root wires them.
func newHarnessWithAudit(t *testing.T, audit AuditRecorder) *testHarness {
	t.Helper()
	harness := newHarness(t, nil)
	service, err := New(Options{
		Repository: harness.repository,
		Channels:   harness.channels,
		Clock:      harness.clock,
		Audit:      audit,
	})
	if err != nil {
		t.Fatalf("build audited service: %v", err)
	}
	harness.spendingLimits = service.SpendingLimits
	harness.orders = service.Orders
	return harness
}
