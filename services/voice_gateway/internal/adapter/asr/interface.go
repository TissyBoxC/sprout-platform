// Package asr defines speech recognition adapter contracts.
package asr

import "context"

// Config controls one recognition stream.
type Config struct {
	Language string
}

// Result is one recognition result.
type Result struct {
	Text      string
	IsFinal   bool
	RequestID string
}

// Stream accepts audio and emits recognition results.
type Stream interface {
	// WriteAudio appends PCM bytes in the fixed 16 kHz mono int16 profile.
	// Implementations may buffer or forward the bytes and must reject writes
	// after Close.
	WriteAudio(data []byte) error

	// Results is closed after the final result or the first terminal error.
	// The channel is never closed before Close has been called.
	Results() <-chan Result

	// Err reports the terminal stream error, or nil when recognition completed.
	Err() error

	// Close finalizes the recognition request and is safe to call repeatedly.
	Close() error
}

// Recognizer creates recognition streams.
type Recognizer interface {
	Open(ctx context.Context, config Config) (Stream, error)
}
