// Package llm defines text conversation gateway contracts.
package llm

import "context"

// Message is one chat message.
type Message struct {
	Role    string
	Content string
}

// Request describes one chat request.
type Request struct {
	Model    string
	Messages []Message
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
