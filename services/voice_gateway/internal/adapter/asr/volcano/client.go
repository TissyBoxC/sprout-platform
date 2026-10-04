// Package volcano contains the Volcano Engine speech recognition adapter.
package volcano

import (
	"bytes"
	"context"
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
	defaultEndpoint = "https://openspeech.bytedance.com/api/v3/auc/bigmodel/recognize/flash"
	defaultAppID    = ""
	defaultTimeout  = 60 * time.Second
	maxAudioBytes   = 25 << 20
)

// Config controls the Volcano Engine big-model recognition endpoint.
//
// AppID, AccessToken, and Cluster identify the speech application. They are
// sent as request metadata and are never logged by the adapter.
type Config struct {
	Endpoint    string
	AppID       string
	AccessToken string
	Cluster     string
	HTTPClient  *http.Client
	Timeout     time.Duration
}

// Client implements asr.Recognizer using Volcano Engine's flash recognition
// endpoint. This endpoint accepts one complete utterance per request.
type Client struct {
	endpoint    string
	appID       string
	accessToken string
	cluster     string
	httpClient  *http.Client
}

// New creates a Volcano Engine recognizer.
func New(config Config) *Client {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	cluster := strings.TrimSpace(config.Cluster)
	if cluster == "" {
		cluster = "volcengine_streaming_common"
	}
	return &Client{
		endpoint:    endpoint,
		appID:       strings.TrimSpace(config.AppID),
		accessToken: strings.TrimSpace(config.AccessToken),
		cluster:     cluster,
		httpClient:  httpClient,
	}
}

// Open starts one Volcano Engine recognition stream.
func (c *Client) Open(ctx context.Context, config asr.Config) (asr.Stream, error) {
	if c == nil {
		return nil, errors.New("volcano ASR client is nil")
	}
	if c.appID == "" || c.accessToken == "" {
		return nil, errors.New("volcano ASR AppID and AccessToken must be configured")
	}
	language := strings.TrimSpace(config.Language)
	if language == "" {
		language = "zh-CN"
	}
	return streamutil.New(ctx, language, maxAudioBytes, c.transcribe), nil
}

func (c *Client) transcribe(ctx context.Context, language string, audio []byte) (asr.Result, error) {
	if len(audio) == 0 {
		return asr.Result{}, errors.New("volcano ASR audio is empty")
	}
	requestBody := map[string]any{
		"user": map[string]any{
			"uid": c.appID,
		},
		"audio": map[string]any{
			"format":   "pcm",
			"rate":     16000,
			"bits":     16,
			"channel":  1,
			"language": language,
			"data":     encodePayload(audio),
		},
		"request": map[string]any{
			"model_name": "bigmodel",
		},
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return asr.Result{}, fmt.Errorf("encode volcano ASR request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return asr.Result{}, fmt.Errorf("create volcano ASR request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer; "+c.accessToken)
	request.Header.Set("X-Api-App-Key", c.appID)
	request.Header.Set("X-Api-Access-Key", c.accessToken)
	request.Header.Set("X-Api-Resource-Id", c.cluster)
	request.Header.Set("X-Api-Request-Id", requestID())
	response, err := c.httpClient.Do(request)
	if err != nil {
		return asr.Result{}, fmt.Errorf("volcano ASR request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := httpx.ReadBounded(response.Body, httpx.DefaultErrorBodyBytes)
	if err != nil {
		return asr.Result{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return asr.Result{}, providerError(response.StatusCode, responseBody)
	}
	var decoded struct {
		Result struct {
			Text string `json:"text"`
		} `json:"result"`
		Text    string `json:"text"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return asr.Result{}, fmt.Errorf("decode volcano ASR response: %w", err)
	}
	if decoded.Code != 0 {
		return asr.Result{}, fmt.Errorf(
			"volcano ASR rejected request code=%d message=%s",
			decoded.Code,
			strings.TrimSpace(decoded.Message),
		)
	}
	text := strings.TrimSpace(decoded.Result.Text)
	if text == "" {
		text = strings.TrimSpace(decoded.Text)
	}
	if text == "" {
		return asr.Result{}, errors.New("volcano ASR response did not contain text")
	}
	return asr.Result{
		Text:      text,
		IsFinal:   true,
		RequestID: response.Header.Get("X-Api-Request-Id"),
	}, nil
}

func encodePayload(audio []byte) string {
	return base64Payload(audio)
}

func base64Payload(audio []byte) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var builder strings.Builder
	builder.Grow((len(audio) + 2) / 3 * 4)
	for index := 0; index < len(audio); index += 3 {
		remaining := len(audio) - index
		var value uint32
		value = uint32(audio[index]) << 16
		if remaining > 1 {
			value |= uint32(audio[index+1]) << 8
		}
		if remaining > 2 {
			value |= uint32(audio[index+2])
		}
		builder.WriteByte(table[(value>>18)&0x3f])
		builder.WriteByte(table[(value>>12)&0x3f])
		if remaining > 1 {
			builder.WriteByte(table[(value>>6)&0x3f])
		} else {
			builder.WriteByte('=')
		}
		if remaining > 2 {
			builder.WriteByte(table[value&0x3f])
		} else {
			builder.WriteByte('=')
		}
	}
	return builder.String()
}

func requestID() string {
	return fmt.Sprintf("vg-%d", time.Now().UTC().UnixNano())
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
