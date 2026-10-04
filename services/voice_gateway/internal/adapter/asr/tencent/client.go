// Package tencent contains the Tencent Cloud speech recognition adapter.
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

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr/streamutil"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/httpx"
)

const (
	defaultEndpoint = "https://asr.tencentcloudapi.com"
	defaultRegion   = "ap-shanghai"
	defaultTimeout  = 60 * time.Second
	maxAudioBytes   = 25 << 20
)

// Config controls Tencent Cloud ASR API 3.0.
//
// SecretID and SecretKey sign every request with TC3-HMAC-SHA256. They are
// never included in errors or logs.
type Config struct {
	Endpoint   string
	Region     string
	SecretID   string
	SecretKey  string
	HTTPClient *http.Client
	Timeout    time.Duration
	Now        func() time.Time
}

// Client implements asr.Recognizer using Tencent Cloud SentenceRecognition.
type Client struct {
	endpoint   string
	region     string
	secretID   string
	secretKey  string
	httpClient *http.Client
	now        func() time.Time
}

// New creates a Tencent Cloud recognizer.
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

// Open starts one Tencent Cloud recognition stream.
func (c *Client) Open(ctx context.Context, config asr.Config) (asr.Stream, error) {
	if c == nil {
		return nil, errors.New("tencent ASR client is nil")
	}
	if c.secretID == "" || c.secretKey == "" {
		return nil, errors.New("tencent ASR SecretID and SecretKey must be configured")
	}
	language := strings.TrimSpace(config.Language)
	if language == "" {
		language = "zh-CN"
	}
	return streamutil.New(ctx, language, maxAudioBytes, c.transcribe), nil
}

func (c *Client) transcribe(ctx context.Context, language string, audio []byte) (asr.Result, error) {
	if len(audio) == 0 {
		return asr.Result{}, errors.New("tencent ASR audio is empty")
	}
	requestBody := map[string]any{
		"ProjectId":      0,
		"SubServiceType": 2,
		"EngSerViceType": "16k_zh",
		"SourceType":     1,
		"VoiceFormat":    "pcm",
		"Data":           base64.StdEncoding.EncodeToString(audio),
		"DataLen":        len(audio),
	}
	if language != "" && language != "zh-CN" {
		requestBody["EngSerViceType"] = language
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return asr.Result{}, fmt.Errorf("encode tencent ASR request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return asr.Result{}, fmt.Errorf("create tencent ASR request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("X-TC-Action", "SentenceRecognition")
	request.Header.Set("X-TC-Version", "2019-06-14")
	signedAt := c.now()
	request.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", signedAt.UTC().Unix()))
	if c.region != "" {
		request.Header.Set("X-TC-Region", c.region)
	}
	if err := httpx.SignTencentCloudRequest(request, payload, httpx.TencentCloudCredentials{
		SecretID:  c.secretID,
		SecretKey: c.secretKey,
		Service:   "asr",
		Region:    c.region,
		Host:      request.URL.Host,
	}, signedAt); err != nil {
		return asr.Result{}, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return asr.Result{}, fmt.Errorf("tencent ASR request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := httpx.ReadBounded(response.Body, httpx.DefaultErrorBodyBytes)
	if err != nil {
		return asr.Result{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return asr.Result{}, providerError(response.StatusCode, responseBody)
	}
	var envelope struct {
		Response struct {
			Result    string `json:"Result"`
			RequestID string `json:"RequestId"`
			Error     *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return asr.Result{}, fmt.Errorf("decode tencent ASR response: %w", err)
	}
	if envelope.Response.Error != nil {
		return asr.Result{}, fmt.Errorf(
			"tencent ASR rejected request code=%s message=%s",
			envelope.Response.Error.Code,
			envelope.Response.Error.Message,
		)
	}
	if strings.TrimSpace(envelope.Response.Result) == "" {
		return asr.Result{}, errors.New("tencent ASR response did not contain text")
	}
	return asr.Result{
		Text:      strings.TrimSpace(envelope.Response.Result),
		IsFinal:   true,
		RequestID: envelope.Response.RequestID,
	}, nil
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
