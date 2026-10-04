// Package openai contains the OpenAI-compatible speech synthesis adapter.
package openai

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
	defaultBaseURL = "https://api.openai.com"
	defaultModel   = "gpt-4o-mini-tts"
	defaultFormat  = "wav"
	defaultVoice   = "alloy"
	defaultTimeout = 60 * time.Second
	streamChunk    = 32 << 10
)

// Config controls the OpenAI-compatible speech endpoint.
type Config struct {
	BaseURL    string
	APIKey     string
	Model      string
	Format     string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client implements tts.Synthesizer using /v1/audio/speech.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	format     string
	httpClient *http.Client
	timeout    time.Duration
}

// New creates an OpenAI-compatible synthesizer. APIKey is required before a
// request is made; BaseURL may point at a compatible local gateway.
func New(config Config) *Client {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		model = defaultModel
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
		model:      model,
		format:     format,
		httpClient: httpClient,
		timeout:    timeout,
	}
}

// Synthesize requests one complete speech response. The audio is streamed to
// Audio in bounded chunks without an extra provider round trip.
func (c *Client) Synthesize(ctx context.Context, request tts.Request) (tts.Stream, error) {
	if c == nil {
		return nil, errors.New("openai TTS client is nil")
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, errors.New("openai TTS API key is not configured")
	}
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return nil, errors.New("openai TTS text is empty")
	}
	voice := strings.TrimSpace(request.Voice)
	if voice == "" {
		voice = defaultVoice
	}
	endpoint, err := httpx.JoinURL(c.baseURL, "/v1/audio/speech")
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"model":           c.model,
		"input":           text,
		"voice":           voice,
		"response_format": c.format,
	})
	if err != nil {
		return nil, fmt.Errorf("encode openai TTS request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("create openai TTS request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "audio/*")
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("openai TTS request failed: %w", err)
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
			Retryable:  response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError,
		}
	}
	return newAudioStream(response.Body), nil
}

type audioStream struct {
	body io.ReadCloser
	out  chan []byte
	err  error
	once sync.Once
	done chan struct{}

	mu sync.Mutex
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

// Close cancels the provider body and waits until the reader goroutine exits.
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
		s.setError(errors.New("openai TTS response body is empty"))
		return
	}
	buffer := make([]byte, streamChunk)
	readTotal := 0
	for {
		n, err := s.body.Read(buffer)
		if n > 0 {
			readTotal += n
			if readTotal > int(httpx.DefaultAudioBodyBytes) {
				s.setError(errors.New("openai TTS response exceeds the audio body limit"))
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
			s.setError(fmt.Errorf("read openai TTS response: %w", err))
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
