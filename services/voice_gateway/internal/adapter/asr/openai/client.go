// Package openai contains the OpenAI-compatible speech recognition adapter.
package openai

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/httpx"
)

const (
	defaultBaseURL  = "https://api.openai.com"
	defaultModel    = "whisper-1"
	defaultLanguage = "zh"
	defaultTimeout  = 60 * time.Second
	maxAudioBytes   = 25 << 20
)

// Config controls the OpenAI-compatible recognition endpoint.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
	// HTTPClient and Timeout are used by production deployments and tests.
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client implements asr.Recognizer using the OpenAI transcription endpoint.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	timeout    time.Duration
}

// New creates an OpenAI-compatible recognizer. BaseURL may point at an
// OpenAI-compatible proxy; APIKey is required before a request is made.
func New(config Config) *Client {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		model = defaultModel
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
		httpClient: httpClient,
		timeout:    timeout,
	}
}

// Open starts one recognition request. Audio written to the returned stream is
// buffered until Close, because the OpenAI transcription endpoint accepts a
// complete file rather than a long-lived stream.
func (c *Client) Open(ctx context.Context, config asr.Config) (asr.Stream, error) {
	if c == nil {
		return nil, errors.New("openai ASR client is nil")
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, errors.New("openai ASR API key is not configured")
	}
	language := strings.TrimSpace(config.Language)
	if language == "" {
		language = defaultLanguage
	}
	streamContext, cancel := context.WithCancel(ctx)
	return &stream{
		client:   c,
		ctx:      streamContext,
		cancel:   cancel,
		language: language,
		results:  make(chan asr.Result, 1),
		done:     make(chan struct{}),
	}, nil
}

type stream struct {
	client   *Client
	ctx      context.Context
	cancel   context.CancelFunc
	language string
	results  chan asr.Result

	mu     sync.Mutex
	audio  bytes.Buffer
	closed bool
	done   chan struct{}
	err    error
}

func (s *stream) WriteAudio(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("ASR stream is closed")
	}
	if s.audio.Len()+len(data) > maxAudioBytes {
		return errors.New("ASR audio exceeds the 25 MiB provider limit")
	}
	_, _ = s.audio.Write(data)
	return nil
}

func (s *stream) Results() <-chan asr.Result {
	return s.results
}

func (s *stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close finalizes the upload exactly once and waits for the result goroutine.
// It leaves the final result available on Results for the caller.
func (s *stream) Close() error {
	s.mu.Lock()
	if s.closed {
		done := s.done
		s.mu.Unlock()
		<-done
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.err
	}
	s.closed = true
	audio := append([]byte(nil), s.audio.Bytes()...)
	s.mu.Unlock()

	go s.transcribe(audio)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *stream) transcribe(audio []byte) {
	defer close(s.done)
	defer close(s.results)
	defer s.cancel()
	if len(audio) == 0 {
		s.setError(errors.New("ASR stream received no audio"))
		return
	}
	result, err := s.client.transcribe(s.ctx, s.language, audio)
	if err != nil {
		s.setError(err)
		return
	}
	s.results <- result
}

func (s *stream) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (c *Client) transcribe(ctx context.Context, language string, audio []byte) (asr.Result, error) {
	endpoint, err := httpx.JoinURL(c.baseURL, "/v1/audio/transcriptions")
	if err != nil {
		return asr.Result{}, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return asr.Result{}, fmt.Errorf("create transcription upload: %w", err)
	}
	if err := writePCMAsWAV(fileWriter, audio); err != nil {
		return asr.Result{}, err
	}
	if err := writer.WriteField("model", c.model); err != nil {
		return asr.Result{}, fmt.Errorf("write transcription model: %w", err)
	}
	if language != "" {
		if err := writer.WriteField("language", language); err != nil {
			return asr.Result{}, fmt.Errorf("write transcription language: %w", err)
		}
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return asr.Result{}, fmt.Errorf("write transcription response format: %w", err)
	}
	if err := writer.Close(); err != nil {
		return asr.Result{}, fmt.Errorf("close transcription upload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return asr.Result{}, fmt.Errorf("create transcription request: %w", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return asr.Result{}, fmt.Errorf("transcription request failed: %w", err)
	}
	defer response.Body.Close()
	payload, err := httpx.ReadBounded(response.Body, httpx.DefaultErrorBodyBytes)
	if err != nil {
		return asr.Result{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return asr.Result{}, providerError(response.StatusCode, payload)
	}
	var decoded struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return asr.Result{}, fmt.Errorf("decode transcription response: %w", err)
	}
	text := strings.TrimSpace(decoded.Text)
	if text == "" {
		return asr.Result{}, errors.New("transcription response did not contain text")
	}
	return asr.Result{
		Text:      text,
		IsFinal:   true,
		RequestID: response.Header.Get("x-request-id"),
	}, nil
}

func providerError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if len(message) > 256 {
		message = message[:256]
	}
	return &httpx.ProviderError{
		StatusCode: status,
		Message:    message,
		Retryable:  status == http.StatusTooManyRequests || status >= http.StatusInternalServerError,
	}
}

func writePCMAsWAV(destination io.Writer, pcm []byte) error {
	if len(pcm)%2 != 0 {
		return errors.New("ASR PCM length must be an even number of bytes")
	}
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+len(pcm)))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], 16000)
	binary.LittleEndian.PutUint32(header[28:32], 32000)
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(len(pcm)))
	if _, err := destination.Write(header); err != nil {
		return fmt.Errorf("write WAV header: %w", err)
	}
	if _, err := destination.Write(pcm); err != nil {
		return fmt.Errorf("write WAV payload: %w", err)
	}
	return nil
}
