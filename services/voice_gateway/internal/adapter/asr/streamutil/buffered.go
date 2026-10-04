// Package streamutil contains shared buffered recognition stream behavior.
package streamutil

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
)

// TranscribeFunc sends one complete utterance to a provider and returns a
// final result.
type TranscribeFunc func(context.Context, string, []byte) (asr.Result, error)

// Buffered is a one-utterance ASR stream for providers whose HTTP API accepts
// a complete audio file. WriteAudio is safe for one producer and Close is
// idempotent and safe to call from another goroutine.
type Buffered struct {
	ctx        context.Context
	cancel     context.CancelFunc
	language   string
	limit      int
	transcribe TranscribeFunc
	results    chan asr.Result
	start      sync.Once
	done       chan struct{}

	mu     sync.Mutex
	audio  bytes.Buffer
	closed bool
	err    error
}

// New creates a buffered stream. limit must be positive and bounds the total
// PCM bytes accepted before the provider is called.
func New(
	ctx context.Context,
	language string,
	limit int,
	transcribe TranscribeFunc,
) *Buffered {
	if ctx == nil {
		ctx = context.Background()
	}
	streamContext, cancel := context.WithCancel(ctx)
	return &Buffered{
		ctx:        streamContext,
		cancel:     cancel,
		language:   language,
		limit:      limit,
		transcribe: transcribe,
		results:    make(chan asr.Result, 1),
		done:       make(chan struct{}),
	}
}

// WriteAudio appends PCM bytes until Close. It rejects writes once finalization
// starts and keeps hostile callers below the provider's upload limit.
func (s *Buffered) WriteAudio(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("ASR stream is closed")
	}
	if s.limit > 0 && s.audio.Len()+len(data) > s.limit {
		return errors.New("ASR audio exceeds the provider upload limit")
	}
	_, _ = s.audio.Write(data)
	return nil
}

// Results returns the single final result channel. The channel is closed when
// the stream reaches a terminal state.
func (s *Buffered) Results() <-chan asr.Result {
	return s.results
}

// Err returns the terminal provider error, or nil after a clean final result.
func (s *Buffered) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close starts the provider request once, waits for completion, and returns
// the terminal error. Concurrent and repeated Close calls are supported.
func (s *Buffered) Close() error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		audio := append([]byte(nil), s.audio.Bytes()...)
		s.mu.Unlock()
		s.start.Do(func() {
			go s.run(audio)
		})
	} else {
		s.mu.Unlock()
	}
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Buffered) run(audio []byte) {
	defer close(s.done)
	defer close(s.results)
	defer s.cancel()
	if len(audio) == 0 {
		s.setError(errors.New("ASR stream received no audio"))
		return
	}
	result, err := s.transcribe(s.ctx, s.language, audio)
	if err != nil {
		s.setError(err)
		return
	}
	s.results <- result
}

func (s *Buffered) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}
