// Package tencent contains the Tencent Cloud speech synthesis adapter.
package tencent

import (
	"bytes"
	"context"
	"encoding/base64"
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
	defaultEndpoint = "https://tts.tencentcloudapi.com"
	defaultRegion   = "ap-shanghai"
	defaultTimeout  = 60 * time.Second
)

// Config controls Tencent Cloud TTS API 3.0.
type Config struct {
	Endpoint   string
	Region     string
	SecretID   string
	SecretKey  string
	HTTPClient *http.Client
	Timeout    time.Duration
	Now        func() time.Time
}

// Client implements tts.Synthesizer using Tencent Cloud TextToVoice.
type Client struct {
	endpoint   string
	region     string
	secretID   string
	secretKey  string
	httpClient *http.Client
	now        func() time.Time
}

// New creates a Tencent Cloud synthesizer.
func New(config Config) *Client {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	region := strings.TrimSpace(config.Region)
	if region == "" {
		region = defaultRegion
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		endpoint:   endpoint,
		region:     region,
		secretID:   strings.TrimSpace(config.SecretID),
		secretKey:  strings.TrimSpace(config.SecretKey),
		httpClient: httpClient,
		now:        now,
	}
}

// Synthesize requests one Tencent Cloud audio response.
func (c *Client) Synthesize(ctx context.Context, request tts.Request) (tts.Stream, error) {
	if c == nil {
		return nil, errors.New("tencent TTS client is nil")
	}
	if c.secretID == "" || c.secretKey == "" {
		return nil, errors.New("tencent TTS SecretID and SecretKey must be configured")
	}
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return nil, errors.New("tencent TTS text is empty")
	}
	voiceType := 101001
	requestBody := map[string]any{
		"Text":       text,
		"SessionId":  fmt.Sprintf("sprout-%d", c.now().UTC().UnixNano()),
		"ModelType":  1,
		"VoiceType":  voiceType,
		"Codec":      "wav",
		"SampleRate": 16000,
		"Speed":      0,
		"Volume":     0,
		"Pitch":      0,
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("encode tencent TTS request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("create tencent TTS request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpRequest.Header.Set("X-TC-Action", "TextToVoice")
	httpRequest.Header.Set("X-TC-Version", "2019-08-23")
	signedAt := c.now()
	httpRequest.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", signedAt.UTC().Unix()))
	if c.region != "" {
		httpRequest.Header.Set("X-TC-Region", c.region)
	}
	if err := httpx.SignTencentCloudRequest(httpRequest, payload, httpx.TencentCloudCredentials{
		SecretID:  c.secretID,
		SecretKey: c.secretKey,
		Service:   "tts",
		Region:    c.region,
		Host:      httpRequest.URL.Host,
	}, signedAt); err != nil {
		return nil, err
	}
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("tencent TTS request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := httpx.ReadBounded(response.Body, httpx.DefaultAudioBodyBytes)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, providerError(response.StatusCode, responseBody)
	}
	var envelope struct {
		Response struct {
			Audio     string `json:"Audio"`
			SessionID string `json:"SessionId"`
			RequestID string `json:"RequestId"`
			Error     *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode tencent TTS response: %w", err)
	}
	if envelope.Response.Error != nil {
		return nil, fmt.Errorf(
			"tencent TTS rejected request code=%s message=%s",
			envelope.Response.Error.Code,
			envelope.Response.Error.Message,
		)
	}
	if envelope.Response.Audio == "" {
		return nil, errors.New("tencent TTS response did not contain audio")
	}
	audio, err := base64.StdEncoding.DecodeString(envelope.Response.Audio)
	if err != nil {
		return nil, fmt.Errorf("decode tencent TTS audio: %w", err)
	}
	if len(audio) == 0 {
		return nil, errors.New("tencent TTS audio is empty")
	}
	return streamutil.FromBytes(audio, 32<<10), nil
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
