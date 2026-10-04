// Package realtime defines end-to-end realtime voice adapter contracts.
package realtime

import "context"

// Session is one bidirectional realtime voice session.
type Session interface {
	// WriteAudio sends one PCM segment in the configured realtime profile.
	WriteAudio([]byte) error

	// Audio emits provider response audio bytes until the session closes.
	Audio() <-chan []byte

	// Err reports the terminal session error, or nil after a clean close.
	Err() error

	// Close closes the provider connection and is safe to call repeatedly.
	Close() error
}

// Connector opens realtime voice sessions.
type Connector interface {
	Connect(ctx context.Context) (Session, error)
}
