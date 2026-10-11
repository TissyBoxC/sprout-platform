// Package sub2api implements the vision model client on top of the sub2api
// gateway's OpenAI-compatible chat completions endpoint.
//
// The contract exchange is text only: the gateway sends a bounded instruction
// prompt describing the task and expects a strict JSON object with labels and
// a short summary. Raw image bytes are never sent through this client, so the
// gateway has no path to leak pixels even if a deployment misconfigures the
// vision pipeline.
package sub2api

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
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/platform/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/vision"
)

const (
	defaultTimeout     = 60 * time.Second
	maxResponseBytes   = 256 << 10
	maxPromptRunes     = 500
	provenanceProject  = "vision"
	visionSystemPrompt = "你是面向幼儿的识图助手。只根据设备已完成的安全识别结果作答，不要索取或描述儿童隐私。" +
		"你必须只输出一个 JSON 对象，格式为 " +
		`{"labels":[{"label":"简短词语","confidence":0}],"summary":"一句话中文说明"}` +
		"，不要输出任何解释、Markdown 或其他字段。labels 最多 16 个，confidence 为 0 到 10000 的整数。"
)

// Config carries the sub2api connection settings for the vision client.
type Config struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client calls sub2api for one bounded vision inference.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	timeout    time.Duration
}

// New creates a sub2api vision client. A missing API key or model is reported
// on the first Analyze call so gateway startup keeps working when vision is
// only enabled in some deployments.
func New(config Config) *Client {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
		apiKey:     strings.TrimSpace(config.APIKey),
		model:      strings.TrimSpace(config.Model),
		httpClient: config.HTTPClient,
		timeout:    timeout,
	}
}

// Analyze performs one bounded vision inference.
func (c *Client) Analyze(
	ctx context.Context,
	request vision.ModelRequest,
) (vision.ModelResult, error) {
	if c == nil {
		return vision.ModelResult{}, vision.ErrModelUnavailable
	}
	if c.apiKey == "" {
		return vision.ModelResult{}, fmt.Errorf("%w: sub2api API key is not configured", vision.ErrModelUnavailable)
	}
	if c.model == "" {
		return vision.ModelResult{}, fmt.Errorf("%w: vision model is not configured", vision.ErrModelUnavailable)
	}
	endpoint, err := httpx.JoinURL(defaultBaseURL(c.baseURL), "/v1/chat/completions")
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, err)
	}

	prompt := buildPrompt(request)
	encoded, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []message{
			{Role: "system", Content: visionSystemPrompt},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: encode request: %v", vision.ErrModelUnavailable, err)
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(encoded),
	)
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: create request: %v", vision.ErrModelUnavailable, err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	if request.DeviceID != "" {
		httpRequest.Header.Set("X-Sprout-Device-ID", request.DeviceID)
	}
	if request.TaskID != "" {
		httpRequest.Header.Set("X-Sprout-Vision-Task-ID", request.TaskID)
	}
	httpRequest.Header.Set("X-Sprout-Project", provenanceProject)

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: c.timeout}
	}
	body, response, err := httpx.DoBounded(ctx, client, httpRequest, maxResponseBytes)
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, observability.SafeErrorString(err))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return vision.ModelResult{}, fmt.Errorf(
			"%w: status=%d",
			vision.ErrModelUnavailable,
			response.StatusCode,
		)
	}

	result, err := decodeResponse(body)
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, err)
	}
	result.Model = c.model
	return result, nil
}

func buildPrompt(request vision.ModelRequest) string {
	prompt := strings.TrimSpace(request.Prompt)
	if len([]rune(prompt)) > maxPromptRunes {
		prompt = string([]rune(prompt)[:maxPromptRunes])
	}
	builder := strings.Builder{}
	builder.WriteString("任务类型：")
	builder.WriteString(string(request.TaskType))
	if prompt != "" {
		builder.WriteString("\n家长要求：")
		builder.WriteString(prompt)
	}
	builder.WriteString(fmt.Sprintf(
		"\n画面信息：宽 %d，高 %d，字节数 %d。",
		request.FrameWidth,
		request.FrameHeight,
		request.FrameBytes,
	))
	return builder.String()
}

func decodeResponse(body []byte) (vision.ModelResult, error) {
	var envelope chatResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return vision.ModelResult{}, errors.New("decode response")
	}
	if len(envelope.Choices) == 0 {
		return vision.ModelResult{}, errors.New("response has no choices")
	}
	content := strings.TrimSpace(envelope.Choices[0].Message.Content)
	if content == "" {
		return vision.ModelResult{}, errors.New("response content is empty")
	}
	content = stripCodeFence(content)

	var decoded struct {
		Labels  []vision.Label `json:"labels"`
		Summary string         `json:"summary"`
	}
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		return vision.ModelResult{}, errors.New("decode model content")
	}
	return vision.ModelResult{
		Labels:  decoded.Labels,
		Summary: decoded.Summary,
	}, nil
}

func stripCodeFence(content string) string {
	if !strings.HasPrefix(content, "```") {
		return content
	}
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimPrefix(content, "json")
	content = strings.TrimSpace(content)
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func defaultBaseURL(baseURL string) string {
	if strings.TrimSpace(baseURL) == "" {
		return "http://127.0.0.1:8080"
	}
	return baseURL
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}
