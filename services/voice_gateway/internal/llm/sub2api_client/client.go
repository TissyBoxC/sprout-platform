// Package sub2api_client implements an OpenAI-compatible chat client for the
// sub2api gateway.
package sub2api_client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
)

const (
	defaultTimeout      = 90 * time.Second
	maxResponseLineSize = 1 << 20
	maxErrorBodyBytes   = 64 << 10
)

// Client sends streaming chat requests to sub2api.
//
// The configured APIKey is sent only in the Authorization header and is never
// included in errors or logs. A single Client is safe for concurrent use.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// New creates a sub2api client with sensible production defaults.
func New(baseURL string, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  strings.TrimSpace(apiKey),
		Timeout: defaultTimeout,
	}
}

// Chat starts one streaming chat completion.
//
// It returns a stream after the provider accepts the request. Provider status
// errors and malformed request payloads are returned synchronously; read and
// decode failures are reported through Stream.Err. The caller must Close the
// returned stream.
func (c *Client) Chat(ctx context.Context, request llm.Request) (llm.Stream, error) {
	if c == nil {
		return nil, errors.New("sub2api client is nil")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, errors.New("sub2api API key is not configured")
	}
	if strings.TrimSpace(request.Model) == "" {
		return nil, errors.New("sub2api chat model is empty")
	}
	if len(request.Messages) == 0 {
		return nil, errors.New("sub2api chat messages are empty")
	}
	for _, message := range request.Messages {
		if strings.TrimSpace(message.Role) == "" || strings.TrimSpace(message.Content) == "" {
			return nil, errors.New("sub2api chat message is incomplete")
		}
	}

	endpoint := baseURL + "/v1/chat/completions"
	encoded, err := json.Marshal(chatRequest{
		Model:    request.Model,
		Messages: request.Messages,
		Stream:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("encode sub2api chat request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(encoded),
	)
	if err != nil {
		return nil, fmt.Errorf("create sub2api chat request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")

	httpClient := c.HTTPClient
	if httpClient == nil {
		timeout := c.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("sub2api chat request failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes+1))
		if readErr != nil {
			return nil, fmt.Errorf("read sub2api error response: %w", readErr)
		}
		if len(body) > maxErrorBodyBytes {
			body = body[:maxErrorBodyBytes]
		}
		message := strings.TrimSpace(string(body))
		if len(message) > 256 {
			message = message[:256]
		}
		return nil, fmt.Errorf("sub2api chat request failed status=%d: %s", response.StatusCode, message)
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		defer response.Body.Close()
		return nil, fmt.Errorf("sub2api chat response is not event-stream: %s", response.Header.Get("Content-Type"))
	}

	stream := &chatStream{
		response: response,
		chunks:   make(chan string, 8),
		done:     make(chan struct{}),
		closed:   make(chan struct{}),
	}
	go stream.read()
	return stream, nil
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []llm.Message `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatStream struct {
	response *http.Response
	chunks   chan string
	done     chan struct{}
	closed   chan struct{}
	once     sync.Once

	mu  sync.Mutex
	err error
}

func (s *chatStream) Chunks() <-chan string {
	return s.chunks
}

func (s *chatStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close cancels the body read and waits for the reader goroutine. It is
// idempotent and safe to call from a different goroutine than Chunks consumer.
func (s *chatStream) Close() error {
	var closeErr error
	s.once.Do(func() {
		close(s.closed)
		if s.response != nil && s.response.Body != nil {
			closeErr = s.response.Body.Close()
		}
		<-s.done
	})
	return closeErr
}

func (s *chatStream) read() {
	defer close(s.done)
	defer close(s.chunks)
	if s.response == nil || s.response.Body == nil {
		s.setError(errors.New("sub2api chat response body is empty"))
		return
	}

	reader := bufio.NewReaderSize(s.response.Body, 64<<10)
	for {
		line, err := readLine(reader, maxResponseLineSize)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			// Cancellation/close is an expected control path, not a stream
			// failure visible to the caller.
			if errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) {
				return
			}
			s.setError(fmt.Errorf("read sub2api stream: %w", err))
			return
		}
		textLine := strings.TrimSpace(string(line))
		if textLine == "" || strings.HasPrefix(textLine, ":") {
			continue
		}
		if !strings.HasPrefix(textLine, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(textLine, "data:"))
		if data == "[DONE]" {
			return
		}
		if data == "" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Text string `json:"text"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			s.setError(fmt.Errorf("decode sub2api stream chunk: %w", err))
			return
		}
		for _, choice := range chunk.Choices {
			content := choice.Delta.Content
			if content == "" {
				content = choice.Text
			}
			if content == "" {
				continue
			}
			select {
			case s.chunks <- content:
			case <-s.closed:
				return
			}
		}
	}
}

func (s *chatStream) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func readLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		part, isPrefix, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(line)+len(part) > limit {
			return nil, errors.New("sub2api stream line exceeds limit")
		}
		line = append(line, part...)
		if !isPrefix {
			return line, nil
		}
	}
}
