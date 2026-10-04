// Package streamutil contains shared buffered synthesis stream behavior.
package streamutil

import (
	"errors"
	"io"
	"sync"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/httpx"
)

// FromBytes streams one complete audio response in bounded chunks. It never
// blocks the producer and is safe for repeated Close calls.
func FromBytes(audio []byte, chunkSize int) *Stream {
	if chunkSize <= 0 {
		chunkSize = 32 << 10
	}
	stream := &Stream{
		out:  make(chan []byte, 8),
		done: make(chan struct{}),
	}
	go stream.emit(audio, chunkSize)
	return stream
}

// FromReader streams an open HTTP response body and closes it when complete.
func FromReader(body io.ReadCloser) *Stream {
	stream := &Stream{
		body: body,
		out:  make(chan []byte, 8),
		done: make(chan struct{}),
	}
	go stream.read()
	return stream
}

// Stream is an in-memory tts.Stream-compatible transport. It is defined here
// without importing tts so provider adapters can use it without a cycle.
type Stream struct {
	body io.ReadCloser
	out  chan []byte
	done chan struct{}
	once sync.Once

	mu  sync.Mutex
	err error
}

// Audio returns the ordered audio chunks.
func (s *Stream) Audio() <-chan []byte {
	return s.out
}

// Err returns a terminal stream error, if any.
func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close stops emission and is idempotent.
func (s *Stream) Close() error {
	var closeErr error
	s.once.Do(func() {
		close(s.done)
		if s.body != nil {
			closeErr = s.body.Close()
		}
	})
	<-s.done
	return closeErr
}

func (s *Stream) emit(audio []byte, chunkSize int) {
	defer close(s.out)
	if len(audio) == 0 {
		s.setError(errors.New("provider returned no audio"))
		return
	}
	for start := 0; start < len(audio); start += chunkSize {
		end := start + chunkSize
		if end > len(audio) {
			end = len(audio)
		}
		chunk := append([]byte(nil), audio[start:end]...)
		select {
		case s.out <- chunk:
		case <-s.done:
			return
		}
	}
}

func (s *Stream) read() {
	defer close(s.out)
	if s.body == nil {
		s.setError(errors.New("provider response body is empty"))
		return
	}
	buffer := make([]byte, 32<<10)
	total := 0
	for {
		n, err := s.body.Read(buffer)
		if n > 0 {
			total += n
			if total > int(httpx.DefaultAudioBodyBytes) {
				s.setError(errors.New("provider audio response exceeds the body limit"))
				return
			}
			chunk := append([]byte(nil), buffer[:n]...)
			select {
			case s.out <- chunk:
			case <-s.done:
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			s.setError(err)
			return
		}
	}
}

func (s *Stream) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}
