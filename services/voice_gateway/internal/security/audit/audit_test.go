package audit

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestMemorySinkRecordsNormalizedEvents(t *testing.T) {
	sink := NewMemorySink()
	sink.Record(Event{
		Reason:        "crisis_intervention",
		Direction:     DirectionInput,
		Action:        "escalate",
		PolicyVersion: "7",
	})
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].RecordedAt.IsZero() {
		t.Fatal("expected the sink to stamp RecordedAt")
	}
	if events[0].Reason != "crisis_intervention" || events[0].PolicyVersion != "7" {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}

// The audit event type must not be able to carry child content. This guards the
// narrow-field contract against a future field being added casually.
func TestEventCarriesOnlyNonSensitiveFields(t *testing.T) {
	fieldNames := make([]string, 0)
	eventType := reflect.TypeOf(Event{})
	for index := 0; index < eventType.NumField(); index++ {
		fieldNames = append(fieldNames, eventType.Field(index).Name)
	}
	want := []string{"Reason", "Direction", "Action", "PolicyVersion", "RecordedAt"}
	if !reflect.DeepEqual(fieldNames, want) {
		t.Fatalf("Event fields = %v, want %v", fieldNames, want)
	}
}

func TestContextSinkRoundTrip(t *testing.T) {
	sink := NewMemorySink()
	ctx := WithSink(context.Background(), sink)
	RecordThroughContext(ctx, Event{Reason: "allowed", Direction: DirectionInput, Action: "allow"})
	if got := sink.FilterByReason("allowed"); len(got) != 1 {
		t.Fatalf("expected one recorded event, got %d", len(got))
	}
	if FromContext(context.Background()) != nil {
		t.Fatal("expected no sink on a plain context")
	}
}

func TestNopSinkIgnoresEvents(t *testing.T) {
	// A nil function and the nop sink must both be safe to call.
	SinkFunc(nil).Record(Event{Reason: "x"})
	NopSink{}.Record(Event{Reason: "x"})
}

func TestReasonCodesAreStableTokens(t *testing.T) {
	sink := NewMemorySink()
	sink.Record(Event{Reason: "child_safety_self_harm", Direction: DirectionOutput, Action: "safe_reply"})
	for _, event := range sink.Events() {
		if strings.ContainsAny(event.Reason, " \n\t") {
			t.Fatalf("reason %q must be a single stable token", event.Reason)
		}
	}
}
