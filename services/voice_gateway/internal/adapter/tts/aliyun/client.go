// Package aliyun contains the Alibaba Cloud speech synthesis adapter.
package aliyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/httpx"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts/streamutil"
)

const (
	defaultEndpoint = "https://nls-gateway-cn-shanghai.aliyuncs.com/stream/v1/tts"
	defaultFormat   = "wav"
	defaultTimeout  = 60 * time.Second
)

// Config controls the Alibaba Cloud TTS endpoint.
type Config struct {
	Endpoint   string
	AppKey     string
	Token      string
	Format     string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client implements tts.Synthesizer using Alibaba Cloud NLS.
type Client struct {
	endpoint   string
	appKey     string
	token      string
	format     string
	httpClient *http.Client
}

// New creates an Alibaba Cloud synthesizer.
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

// Synthesize requests one Alibaba Cloud audio response.
func (c *Client) Synthesize(ctx context.Context, request tts.Request) (tts.Stream, error) {
	if c == nil {
		return nil, errors.New("aliyun TTS client is nil")
	}
	if c.appKey == "" || c.token == "" {
		return nil, errors.New("aliyun TTS AppKey and Token must be configured")
	}
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return nil, errors.New("aliyun TTS text is empty")
	}
	voice := strings.TrimSpace(request.Voice)
	if voice == "" {
		voice = "xiaoyun"
	}
	payload, err := json.Marshal(map[string]any{
		"appkey":          c.appKey,
		"token":           c.token,
		"text":            text,
		"format":          c.format,
		"sample_rate":     16000,
		"voice":           voice,
		"volume":          50,
		"speed":           100,
		"pitch":           100,
		"enable_subtitle": false,
	})
	if err != nil {
		return nil, fmt.Errorf("encode aliyun TTS request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("create aliyun TTS request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-NLS-Token", c.token)
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("aliyun TTS request failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		body, readErr := httpx.ReadBounded(response.Body, httpx.DefaultErrorBodyBytes)
		if readErr != nil {
			return nil, readErr
		}
		return nil, providerError(response.StatusCode, body)
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		defer response.Body.Close()
		body, readErr := httpx.ReadBounded(response.Body, httpx.DefaultErrorBodyBytes)
		if readErr != nil {
			return nil, readErr
		}
		var decoded struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, fmt.Errorf("decode aliyun TTS error: %w", err)
		}
		if decoded.Status != 20000000 {
			return nil, fmt.Errorf("aliyun TTS rejected request status=%d message=%s", decoded.Status, decoded.Message)
		}
		return nil, errors.New("aliyun TTS returned JSON instead of audio")
	}
	return streamutil.FromReader(response.Body), nil
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
