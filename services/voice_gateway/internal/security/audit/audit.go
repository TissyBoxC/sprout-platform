// Package audit records child-safety decisions made by the voice gateway.
//
// The record is deliberately narrow: it carries only a stable reason code, the
// direction of the check, the enforcement action, and the policy version. It
// never carries recognized speech, model output, prompts, audio, PCM, device
// identifiers, or child identifiers, so an audit trail can exist without
// becoming a second copy of the very content the gateway is protecting.
package audit

import (
	"context"
	"time"
)

// Direction identifies which side of a conversation a decision applied to.
type Direction string

const (
	// DirectionInput covers a recognized child utterance before it reaches a
	// model.
	DirectionInput Direction = "input"
	// DirectionOutput covers a model reply before it is synthesized.
	DirectionOutput Direction = "output"
	// DirectionPrompt covers a composed system prompt.
	DirectionPrompt Direction = "prompt"
	// DirectionVision covers a camera/vision task prompt or result.
	DirectionVision Direction = "vision"
	// DirectionPolicy covers a device content-policy resolution.
	DirectionPolicy Direction = "policy"
)

// Event is one non-sensitive child-safety decision record.
//
// Every field is a stable, enumerable token. Callers must never populate them
// with free-form text; the sink does not store content by construction.
type Event struct {
	// Reason is the stable decision reason code (for example
	// moderation.ReasonCrisis or a content-policy reason).
	Reason string
	// Direction is the side of the pipeline the decision applied to.
	Direction Direction
	// Action is the enforcement action such as allow, block, or escalate.
	Action string
	// PolicyVersion is the effective policy revision, or "0"/"" when the
	// decision did not depend on a stored family policy.
	PolicyVersion string
	// RecordedAt is filled by the sink when zero.
	RecordedAt time.Time
}

// Sink accepts child-safety audit events without coupling callers to storage.
//
// Implementations must be safe for concurrent use and must tolerate a nil or
// malformed event by dropping it rather than panicking, so an audit failure
// can never break the voice path.
type Sink interface {
	Record(event Event)
}

// SinkFunc adapts a function to the Sink interface.
type SinkFunc func(event Event)

// Record implements Sink.
func (fn SinkFunc) Record(event Event) {
	if fn != nil {
		fn(event)
	}
}

// NopSink drops every event. It is the zero configuration so a caller that
// forgets to wire a sink fails safe rather than failing to start.
type NopSink struct{}

// Record implements Sink.
func (NopSink) Record(Event) {}

// recorder is the shared normalization used by concrete sinks.
func normalize(event Event) Event {
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	return event
}

// ContextKey is the context key type for the per-turn audit sink.
type contextKey struct{}

// WithSink attaches a sink to a context so layered helpers can record without
// threading the sink through every signature.
func WithSink(ctx context.Context, sink Sink) context.Context {
	if ctx == nil || sink == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, sink)
}

// FromContext returns the attached sink, or nil when none is set.
func FromContext(ctx context.Context) Sink {
	if ctx == nil {
		return nil
	}
	value, _ := ctx.Value(contextKey{}).(Sink)
	return value
}

// RecordThroughContext records an event on the context sink when present.
func RecordThroughContext(ctx context.Context, event Event) {
	if sink := FromContext(ctx); sink != nil {
		sink.Record(event)
	}
}
