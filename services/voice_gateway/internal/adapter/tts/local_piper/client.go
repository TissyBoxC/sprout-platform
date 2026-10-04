// Package local_piper contains the local Piper-compatible speech synthesis
// adapter.
package local_piper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/httpx"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
)

const (
	defaultBaseURL = "http://127.0.0.1:5000"
	defaultFormat  = "wav"
	defaultTimeout = 60 * time.Second
	streamChunk    = 32 << 10
)

// Config controls the local Piper HTTP endpoint.
type Config struct {
	BaseURL    string
	APIKey     string
	Format     string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client implements tts.Synthesizer using a local Piper HTTP service.
type Client struct {
	baseURL    string
	apiKey     string
	format     string
	httpClient *http.Client
	timeout    time.Duration
}

// New creates a local Piper synthesizer. No API key is required by the common
// local Piper server; one is forwarded only when configured.
func New(config Config) *Client {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	format := strings.TrimSpace(config.Format)
	if format == "" {
		format = defaultFormat
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(config.APIKey),
		format:     format,
		httpClient: httpClient,
		timeout:    timeout,
	}
}

// Synthesize requests one local Piper response and returns a bounded audio
// stream. Provider status errors are surfaced synchronously.
func (c *Client) Synthesize(ctx context.Context, request tts.Request) (tts.Stream, error) {
	if c == nil {
		return nil, errors.New("local Piper TTS client is nil")
	}
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return nil, errors.New("local Piper TTS text is empty")
	}
	endpoint, err := httpx.JoinURL(c.baseURL, "/synthesize")
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"text":            text,
		"voice":           strings.TrimSpace(request.Voice),
		"language":        strings.TrimSpace(request.Language),
		"response_format": c.format,
	})
	if err != nil {
		return nil, fmt.Errorf("encode local Piper request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create local Piper request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "audio/*")
	if c.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("local Piper request failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		body, readErr := httpx.ReadBounded(response.Body, httpx.DefaultErrorBodyBytes)
		if readErr != nil {
			return nil, readErr
		}
		message := strings.TrimSpace(string(body))
		if len(message) > 256 {
			message = message[:256]
		}
		return nil, &httpx.ProviderError{
			StatusCode: response.StatusCode,
			Message:    message,
			Retryable:  response.StatusCode >= http.StatusInternalServerError,
		}
	}
	return newAudioStream(response.Body), nil
}

type audioStream struct {
	body io.ReadCloser
	out  chan []byte
	done chan struct{}
	once sync.Once

	mu  sync.Mutex
	err error
}

func newAudioStream(body io.ReadCloser) *audioStream {
	stream := &audioStream{
		body: body,
		out:  make(chan []byte, 8),
		done: make(chan struct{}),
	}
	go stream.read()
	return stream
}

func (s *audioStream) Audio() <-chan []byte { return s.out }

func (s *audioStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *audioStream) Close() error {
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

func (s *audioStream) read() {
	defer close(s.out)
	if s.body == nil {
		s.setError(errors.New("local Piper response body is empty"))
		return
	}
	buffer := make([]byte, streamChunk)
	readTotal := 0
	for {
		n, err := s.body.Read(buffer)
		if n > 0 {
			readTotal += n
			if readTotal > int(httpx.DefaultAudioBodyBytes) {
				s.setError(errors.New("local Piper response exceeds the audio body limit"))
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
			s.setError(fmt.Errorf("read local Piper response: %w", err))
			return
		}
	}
}

func (s *audioStream) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}
