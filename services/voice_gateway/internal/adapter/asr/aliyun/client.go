// Package aliyun contains the Alibaba Cloud speech recognition adapter.
package aliyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr/streamutil"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/httpx"
)

const (
	defaultEndpoint = "https://nls-gateway-cn-shanghai.aliyuncs.com/stream/v1/asr"
	defaultFormat   = "pcm"
	defaultTimeout  = 60 * time.Second
	maxAudioBytes   = 25 << 20
)

// Config controls the Alibaba Cloud one-sentence recognition endpoint.
//
// Token is an AccessToken obtained through Alibaba Cloud credential exchange.
// AppKey identifies the speech project. Both are required at Open time.
type Config struct {
	Endpoint   string
	AppKey     string
	Token      string
	Format     string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client implements asr.Recognizer using Alibaba Cloud NLS.
type Client struct {
	endpoint   string
	appKey     string
	token      string
	format     string
	httpClient *http.Client
}

// New creates an Alibaba Cloud recognizer. Missing credentials are reported by
// Open so callers get an explicit configuration error rather than a nil stream.
func New(config Config) *Client {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
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
		endpoint:   endpoint,
		appKey:     strings.TrimSpace(config.AppKey),
		token:      strings.TrimSpace(config.Token),
		format:     format,
		httpClient: httpClient,
	}
}

// Open starts one Alibaba Cloud recognition stream.
func (c *Client) Open(ctx context.Context, config asr.Config) (asr.Stream, error) {
	if c == nil {
		return nil, errors.New("aliyun ASR client is nil")
	}
	if c.appKey == "" || c.token == "" {
		return nil, errors.New("aliyun ASR AppKey and Token must be configured")
	}
	language := strings.TrimSpace(config.Language)
	if language == "" {
		language = "zh-CN"
	}
	return streamutil.New(ctx, language, maxAudioBytes, c.transcribe), nil
}

func (c *Client) transcribe(ctx context.Context, language string, audio []byte) (asr.Result, error) {
	if len(audio)%2 != 0 {
		return asr.Result{}, errors.New("aliyun ASR PCM length must be even")
	}
	endpoint, err := url.Parse(c.endpoint)
	if err != nil {
		return asr.Result{}, fmt.Errorf("parse aliyun ASR endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("appkey", c.appKey)
	query.Set("format", c.format)
	query.Set("sample_rate", "16000")
	if language != "" {
		query.Set("language", language)
	}
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(audio))
	if err != nil {
		return asr.Result{}, fmt.Errorf("create aliyun ASR request: %w", err)
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-NLS-Token", c.token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return asr.Result{}, fmt.Errorf("aliyun ASR request failed: %w", err)
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
		Status  int    `json:"status"`
		Message string `json:"message"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return asr.Result{}, fmt.Errorf("decode aliyun ASR response: %w", err)
	}
	if decoded.Status != 20000000 {
		return asr.Result{}, fmt.Errorf(
			"aliyun ASR rejected request status=%d message=%s",
			decoded.Status,
			strings.TrimSpace(decoded.Message),
		)
	}
	if strings.TrimSpace(decoded.Result) == "" {
		return asr.Result{}, errors.New("aliyun ASR response did not contain text")
	}
	return asr.Result{Text: strings.TrimSpace(decoded.Result), IsFinal: true}, nil
}

func providerError(status int, payload []byte) error {
	message := strings.TrimSpace(string(payload))
	if len(message) > 256 {
		message = message[:256]
	}
	return &httpx.ProviderError{
		StatusCode: status,
		Message:    message,
		Retryable:  status == http.StatusTooManyRequests || status >= http.StatusInternalServerError,
	}
}
