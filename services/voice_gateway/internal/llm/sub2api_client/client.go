// Package sub2api_client implements an OpenAI-compatible chat client for the
// platform-owned AI execution proxy.
//
// The client never holds an upstream provider key. For every attempt it asks
// device_platform for a one-time ticket scoped to the device, session, model,
// and project, then sends the unchanged chat body through the authenticated
// execution proxy. The proxy injects the real Authorization header.
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
	"net/url"
	"strings"
	"sync"
	"time"

	platformobservability "github.com/TissyBoxC/sprout-platform/packages/go/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/platform/observability"
)

const (
	defaultTimeout      = 90 * time.Second
	defaultMaxAttempts  = 3
	defaultRetryBackoff = 250 * time.Millisecond
	maxResponseLineSize = 1 << 20
	maxErrorBodyBytes   = 64 << 10
)

// Client sends streaming chat requests through device_platform.
//
// The service token authenticates the voice gateway to device_platform and is
// never included in errors or logs. A one-time ticket authorizes exactly one
// provider execution. A single Client is safe for concurrent use.
type Client struct {
	BaseURL      string
	ServiceToken string
	HTTPClient   *http.Client
	Timeout      time.Duration
	MaxAttempts  int
	RetryBackoff time.Duration
	Sleep        func(context.Context, time.Duration) error
}

// New creates a platform execution client with sensible production defaults.
func New(baseURL string, serviceToken string) *Client {
	return &Client{
		BaseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		ServiceToken: strings.TrimSpace(serviceToken),
		Timeout:      defaultTimeout,
		MaxAttempts:  defaultMaxAttempts,
		RetryBackoff: defaultRetryBackoff,
	}
}

func validPolicyVersion(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
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
		baseURL = "http://127.0.0.1:8081"
	}
	if len(strings.TrimSpace(c.ServiceToken)) < 32 {
		return nil, errors.New("device platform service token is not configured")
	}
	if strings.TrimSpace(request.Model) == "" {
		return nil, errors.New("chat model is empty")
	}
	if strings.TrimSpace(request.DeviceID) == "" {
		return nil, errors.New("chat device id is empty")
	}
	if strings.TrimSpace(request.SessionID) == "" {
		return nil, errors.New("chat session id is empty")
	}
	if policyVersion := strings.TrimSpace(request.PolicyVersion); policyVersion != "" &&
		!validPolicyVersion(policyVersion) {
		return nil, errors.New("chat policy version is invalid")
	}
	if len(request.Messages) == 0 {
		return nil, errors.New("chat messages are empty")
	}
	for _, message := range request.Messages {
		if strings.TrimSpace(message.Role) == "" || strings.TrimSpace(message.Content) == "" {
			return nil, errors.New("chat message is incomplete")
		}
	}

	encoded, err := json.Marshal(chatRequest{
		Model:    request.Model,
		Messages: request.Messages,
		Stream:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("encode chat request: %w", err)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		timeout := c.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	maxAttempts := c.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	retryBackoff := c.RetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = defaultRetryBackoff
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = waitForRetry
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ticket, err := c.issueTicket(ctx, baseURL, request)
		if err != nil {
			if !isRetryableExecutionError(err) || attempt == maxAttempts {
				return nil, err
			}
			if err := sleep(ctx, retryDelay(retryBackoff, attempt)); err != nil {
				return nil, fmt.Errorf("chat ticket retry canceled: %w", err)
			}
			continue
		}
		httpRequest, err := newChatRequest(
			ctx,
			baseURL+"/internal/v1/ai-execution",
			c.ServiceToken,
			ticket,
			request,
			encoded,
		)
		if err != nil {
			return nil, err
		}
		response, err := httpClient.Do(httpRequest)
		if err != nil {
			if !isRetryableTransportError(err) || attempt == maxAttempts {
				return nil, fmt.Errorf("AI execution request failed: %w", err)
			}
			// The ticket may already have been consumed by the proxy. A retry
			// asks for a fresh ticket instead of reusing a possibly spent one.
			if err := sleep(ctx, retryDelay(retryBackoff, attempt)); err != nil {
				return nil, fmt.Errorf("AI execution retry canceled: %w", err)
			}
			continue
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
				_ = response.Body.Close()
				return nil, observability.NewSafeError(
					"AI service returned an unsupported response",
					nil,
				)
			}
			stream := &chatStream{
				response: response,
				chunks:   make(chan string, 8),
				done:     make(chan struct{}),
				closed:   make(chan struct{}),
				ctx:      ctx,
			}
			go stream.read()
			return stream, nil
		}

		statusErr := readProviderStatusError(response)
		if !isRetryableStatus(response.StatusCode) || attempt == maxAttempts {
			return nil, statusErr
		}
		if err := sleep(ctx, retryDelay(retryBackoff, attempt)); err != nil {
			return nil, fmt.Errorf("AI execution retry canceled: %w", err)
		}
	}
	return nil, errors.New("AI execution request failed")
}

type executionTicket struct {
	Ticket    string `json:"ticket"`
	ExpiresIn int    `json:"expires_in"`
	Model     string `json:"model"`
}

type executionEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

type retryableExecutionError struct {
	cause error
}

func (err *retryableExecutionError) Error() string {
	if err == nil || err.cause == nil {
		return "temporary AI execution error"
	}
	return err.cause.Error()
}

func (err *retryableExecutionError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (c *Client) issueTicket(
	ctx context.Context,
	baseURL string,
	request llm.Request,
) (string, error) {
	endpoint, err := url.Parse(baseURL + "/internal/v1/devices/" +
		url.PathEscape(strings.TrimSpace(request.DeviceID)) +
		"/ai-execution-ticket")
	if err != nil {
		return "", fmt.Errorf("create execution ticket URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("model", strings.TrimSpace(request.Model))
	query.Set("project", "voice")
	query.Set("session_id", strings.TrimSpace(request.SessionID))
	endpoint.RawQuery = query.Encode()

	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint.String(),
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("create execution ticket request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.ServiceToken)
	httpRequest.Header.Set("Accept", "application/json")
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: c.timeout()}
	}
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		if isRetryableTransportError(err) {
			return "", &retryableExecutionError{cause: err}
		}
		return "", fmt.Errorf("request execution ticket: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("read execution ticket response: %w", err)
	}
	if len(body) > maxErrorBodyBytes {
		return "", errors.New("execution ticket response is too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		statusErr := fmt.Errorf("execution ticket request failed status=%d", response.StatusCode)
		if isRetryableStatus(response.StatusCode) {
			return "", &retryableExecutionError{cause: statusErr}
		}
		return "", statusErr
	}
	var envelope executionEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", errors.New("decode execution ticket response")
	}
	if envelope.Error != nil {
		if envelope.Error.Retryable {
			return "", &retryableExecutionError{
				cause: errors.New("execution ticket was temporarily unavailable"),
			}
		}
		return "", errors.New("execution ticket was rejected")
	}
	var ticket executionTicket
	if err := json.Unmarshal(envelope.Data, &ticket); err != nil {
		return "", errors.New("decode execution ticket")
	}
	ticket.Ticket = strings.TrimSpace(ticket.Ticket)
	if ticket.Ticket == "" || ticket.ExpiresIn <= 0 {
		return "", errors.New("execution ticket response is incomplete")
	}
	return ticket.Ticket, nil
}

func newChatRequest(
	ctx context.Context,
	endpoint string,
	serviceToken string,
	ticket string,
	request llm.Request,
	encoded []byte,
) (*http.Request, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse AI execution endpoint: %w", err)
	}
	query := parsed.Query()
	query.Set("ticket", ticket)
	parsed.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		parsed.String(),
		bytes.NewReader(encoded),
	)
	if err != nil {
		return nil, fmt.Errorf("create AI execution request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+serviceToken)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	// device_platform validates these values and replaces the tenant, device,
	// session, and project headers with ticket-bound values. Sending the
	// policy revision lets the platform narrow the child-safety policy without
	// trusting the relay to choose its tenant or identity.
	if policyVersion := strings.TrimSpace(request.PolicyVersion); policyVersion != "" {
		httpRequest.Header.Set(platformobservability.PolicyVersionHeader, policyVersion)
	}
	return httpRequest, nil
}

func (c *Client) timeout() time.Duration {
	if c != nil && c.Timeout > 0 {
		return c.Timeout
	}
	return defaultTimeout
}

func readProviderStatusError(response *http.Response) error {
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes+1))
	if readErr != nil {
		return observability.NewSafeError(
			fmt.Sprintf("AI execution request failed status=%d", response.StatusCode),
			readErr,
		)
	}
	if len(body) > maxErrorBodyBytes {
		body = body[:maxErrorBodyBytes]
	}
	return observability.NewSafeError(
		fmt.Sprintf("AI execution request failed status=%d", response.StatusCode),
		errors.New(observability.RedactString(string(body))),
	)
}

func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netError net.Error
	return errors.As(err, &netError) && (netError.Timeout() || netError.Temporary())
}

func isRetryableExecutionError(err error) bool {
	var retryable *retryableExecutionError
	return errors.As(err, &retryable)
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	delay := base
	for index := 1; index < attempt; index++ {
		delay *= 2
	}
	return delay
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
	ctx      context.Context
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
		// Cancellation can race with an intentional Close. Record the
		// caller's context error before signaling closed so the reader does
		// not mistake cancellation for a deliberate shutdown.
		if contextErr := s.contextError(); contextErr != nil {
			s.setError(contextErr)
		}
		close(s.closed)
		if s.response != nil && s.response.Body != nil {
			closeErr = s.response.Body.Close()
		}
		<-s.done
	})
	return closeErr
}

func (s *chatStream) contextError() error {
	if s == nil {
		return nil
	}
	if s.ctx != nil && s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	if s.response != nil && s.response.Request != nil {
		return s.response.Request.Context().Err()
	}
	return nil
}

func (s *chatStream) read() {
	defer close(s.done)
	defer close(s.chunks)
	if contextErr := s.contextError(); contextErr != nil {
		s.setError(contextErr)
		return
	}
	if s.response == nil || s.response.Body == nil {
		s.setError(errors.New("AI execution response body is empty"))
		return
	}

	reader := bufio.NewReaderSize(s.response.Body, 64<<10)
	eventName := ""
	for {
		line, err := readLine(reader, maxResponseLineSize)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// A deliberate Close or caller cancellation also ends the
				// body read. Neither is an interrupted provider stream.
				if contextErr := s.contextError(); contextErr != nil {
					s.setError(contextErr)
					return
				}
				if s.isClosed() {
					return
				}
				s.setError(fmt.Errorf("%w: upstream closed the event stream", llm.ErrStreamInterrupted))
				return
			}
			if contextErr := s.contextError(); contextErr != nil {
				s.setError(contextErr)
				return
			}
			if s.isClosed() ||
				errors.Is(err, net.ErrClosed) {
				return
			}
			s.setError(fmt.Errorf("read AI execution stream: %w", err))
			return
		}
		textLine := strings.TrimSpace(string(line))
		if textLine == "" {
			eventName = ""
			continue
		}
		if strings.HasPrefix(textLine, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(textLine, "event:"))
			continue
		}
		if strings.HasPrefix(textLine, ":") ||
			strings.HasPrefix(textLine, "id:") ||
			strings.HasPrefix(textLine, "retry:") {
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
		if strings.EqualFold(eventName, "error") {
			s.setError(errors.New("AI execution stream reported an upstream error"))
			return
		}
		var chunk struct {
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
			Choices []struct {
				FinishReason *string `json:"finish_reason"`
				Delta        struct {
					Content string `json:"content"`
				} `json:"delta"`
				Text string `json:"text"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			s.setError(fmt.Errorf("decode AI execution stream chunk: %w", err))
			return
		}
		if chunk.Error != nil {
			s.setError(errors.New("AI execution stream reported an upstream error"))
			return
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil &&
				*choice.FinishReason != "" &&
				*choice.FinishReason != "stop" {
				s.setError(fmt.Errorf(
					"%w: provider finish reason %s",
					llm.ErrStreamInterrupted,
					observability.RedactString(*choice.FinishReason),
				))
				return
			}
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

func (s *chatStream) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
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
			return nil, errors.New("AI execution stream line exceeds limit")
		}
		line = append(line, part...)
		if !isPrefix {
			return line, nil
		}
	}
}
