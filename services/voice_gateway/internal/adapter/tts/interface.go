// Package tts defines speech synthesis adapter contracts.
package tts

import "context"

// Request describes one synthesis request.
type Request struct {
	Text     string
	Voice    string
	Language string
}

// Stream produces synthesized audio.
type Stream interface {
	// Audio emits provider audio bytes in order. The channel closes after the
	// final chunk or after a terminal error.
	Audio() <-chan []byte

	// Err reports the terminal synthesis error, or nil after clean completion.
	Err() error

	// Close cancels synthesis and is safe to call repeatedly.
	Close() error
}

// Synthesizer creates synthesis streams.
type Synthesizer interface {
	Synthesize(ctx context.Context, request Request) (Stream, error)
}
