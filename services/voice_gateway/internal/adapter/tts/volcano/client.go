// Package volcano contains the Volcano Engine speech synthesis adapter.
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

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/httpx"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts/streamutil"
)

const (
	defaultEndpoint = "https://openspeech.bytedance.com/api/v1/tts"
	defaultCluster  = "volcengine_tts"
	defaultTimeout  = 60 * time.Second
)

// Config controls the Volcano Engine TTS endpoint.
type Config struct {
	Endpoint    string
	AppID       string
	AccessToken string
	Cluster     string
	HTTPClient  *http.Client
	Timeout     time.Duration
}

// Client implements tts.Synthesizer using Volcano Engine TTS.
type Client struct {
	endpoint    string
	appID       string
	accessToken string
	cluster     string
	httpClient  *http.Client
}

// New creates a Volcano Engine synthesizer.
func New(config Config) *Client {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	cluster := strings.TrimSpace(config.Cluster)
	if cluster == "" {
		cluster = defaultCluster
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
		endpoint:    endpoint,
		appID:       strings.TrimSpace(config.AppID),
		accessToken: strings.TrimSpace(config.AccessToken),
		cluster:     cluster,
		httpClient:  httpClient,
	}
}

// Synthesize requests one Volcano Engine audio response.
func (c *Client) Synthesize(ctx context.Context, request tts.Request) (tts.Stream, error) {
	if c == nil {
		return nil, errors.New("volcano TTS client is nil")
	}
	if c.appID == "" || c.accessToken == "" {
		return nil, errors.New("volcano TTS AppID and AccessToken must be configured")
	}
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return nil, errors.New("volcano TTS text is empty")
	}
	voice := strings.TrimSpace(request.Voice)
	if voice == "" {
		voice = "zh_female_qingxin"
	}
	payload, err := json.Marshal(map[string]any{
		"app": map[string]any{
			"appid":   c.appID,
			"token":   c.accessToken,
			"cluster": c.cluster,
		},
		"user": map[string]any{
			"uid": c.appID,
		},
		"audio": map[string]any{
			"voice_type":   voice,
			"encoding":     "wav",
			"sample_rate":  16000,
			"speed_ratio":  1.0,
			"volume_ratio": 1.0,
			"pitch_ratio":  1.0,
		},
		"request": map[string]any{
			"reqid":     fmt.Sprintf("sprout-%d", time.Now().UTC().UnixNano()),
			"text":      text,
			"text_type": "plain",
			"operation": "query",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode volcano TTS request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("create volcano TTS request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer; "+c.accessToken)
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("volcano TTS request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := httpx.ReadBounded(response.Body, httpx.DefaultErrorBodyBytes)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, providerError(response.StatusCode, responseBody)
	}
	var decoded struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode volcano TTS response: %w", err)
	}
	if decoded.Code != 0 {
		return nil, fmt.Errorf(
			"volcano TTS rejected request code=%d message=%s",
			decoded.Code,
			strings.TrimSpace(decoded.Message),
		)
	}
	audio, err := decodeBase64(decoded.Data)
	if err != nil {
		return nil, fmt.Errorf("decode volcano TTS audio: %w", err)
	}
	if len(audio) == 0 {
		return nil, errors.New("volcano TTS response did not contain audio")
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

func decodeBase64(data string) ([]byte, error) {
	// Volcano TTS returns standard base64. Keep the decoder local so the
	// adapter can add URL-safe fallback without changing the public contract.
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	cleaned := strings.TrimSpace(data)
	if cleaned == "" {
		return nil, errors.New("audio data is empty")
	}
	padding := 0
	if strings.HasSuffix(cleaned, "==") {
		padding = 2
		cleaned = strings.TrimSuffix(cleaned, "==")
	} else if strings.HasSuffix(cleaned, "=") {
		padding = 1
		cleaned = strings.TrimSuffix(cleaned, "=")
	}
	var output []byte
	var accumulator uint32
	bits := 0
	for _, char := range cleaned {
		index := strings.IndexRune(table, char)
		if index < 0 {
			return nil, fmt.Errorf("invalid base64 character %q", char)
		}
		accumulator = (accumulator << 6) | uint32(index)
		bits += 6
		if bits >= 8 {
			bits -= 8
			output = append(output, byte(accumulator>>bits))
		}
	}
	if padding == 2 && bits != 4 {
		return nil, errors.New("invalid base64 padding")
	}
	if padding == 1 && bits != 2 {
		return nil, errors.New("invalid base64 padding")
	}
	return output, nil
}
