// Package llm defines text conversation gateway contracts.
package llm

import (
	"context"
	"errors"
)

// ErrStreamInterrupted means the provider closed an event stream before sending
// its terminal [DONE] marker. Consumers must treat any emitted partial text as
// incomplete and must not speak it as a finished reply.
var ErrStreamInterrupted = errors.New("chat stream interrupted before completion")

// Message is one chat message.
type Message struct {
	Role    string
	Content string
}

// Request describes one chat request.
//
// DeviceID and SessionID are pseudonymous provenance labels attached for the
// AI gateway. They carry no personal data; SessionID is the gateway session
// identifier, never recognized speech or model output.
type Request struct {
	Model    string
	Messages []Message
	DeviceID string
	// PolicyVersion is the effective family child-safety policy revision
	// attached so the AI gateway can enforce age-appropriate model and
	// parameter policy. It is a monotonic revision number, never child data.
	PolicyVersion string
	SessionID     string
}

// Stream emits model response chunks.
type Stream interface {
	// Chunks emits response text deltas in order.
	Chunks() <-chan string

	// Err reports the terminal stream error, or nil after clean completion.
	Err() error

	// Close cancels the stream and is safe to call repeatedly.
	Close() error
}

// Client connects the voice gateway to an LLM gateway.
type Client interface {
	Chat(ctx context.Context, request Request) (Stream, error)
}
