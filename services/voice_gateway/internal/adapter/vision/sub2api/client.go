// Package sub2api implements the vision model client on top of the
// platform-owned AI execution proxy.
//
// The contract exchange is text only: the gateway sends a bounded instruction
// prompt describing the task and expects a strict JSON object with labels and
// a short summary. Raw image bytes are never sent through this client, so the
// gateway has no path to leak pixels even if a deployment misconfigures the
// vision pipeline.
package sub2api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	platformobservability "github.com/TissyBoxC/sprout-platform/packages/go/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/httpx"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/platform/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/vision"
)

const (
	defaultTimeout     = 60 * time.Second
	maxResponseBytes   = 256 << 10
	maxResponseLine    = 64 << 10
	maxPromptRunes     = 500
	provenanceProject  = "vision"
	visionSystemPrompt = "你是面向幼儿的识图助手。只根据设备已完成的安全识别结果作答，不要索取或描述儿童隐私。" +
		"你必须只输出一个 JSON 对象，格式为 " +
		`{"labels":[{"label":"简短词语","confidence":0}],"summary":"一句话中文说明"}` +
		"，不要输出任何解释、Markdown 或其他字段。labels 最多 16 个，confidence 为 0 到 10000 的整数。"
)

// Config carries the platform execution settings for the vision client.
type Config struct {
	BaseURL      string
	ServiceToken string
	Model        string
	HTTPClient   *http.Client
	Timeout      time.Duration
}

// Client calls device_platform for one bounded vision inference.
type Client struct {
	baseURL      string
	serviceToken string
	model        string
	httpClient   *http.Client
	timeout      time.Duration
}

// New creates a platform execution vision client. A missing service token or
// model is reported on the first Analyze call so gateway startup keeps working
// when vision is only enabled in some deployments.
func New(config Config) *Client {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL:      strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
		serviceToken: strings.TrimSpace(config.ServiceToken),
		model:        strings.TrimSpace(config.Model),
		httpClient:   config.HTTPClient,
		timeout:      timeout,
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
	if len(c.serviceToken) < 32 {
		return vision.ModelResult{}, fmt.Errorf("%w: device platform service token is not configured", vision.ErrModelUnavailable)
	}
	if c.model == "" {
		return vision.ModelResult{}, fmt.Errorf("%w: vision model is not configured", vision.ErrModelUnavailable)
	}
	policyVersion := strings.TrimSpace(request.PolicyVersion)
	if policyVersion != "" && !validPolicyVersion(policyVersion) {
		return vision.ModelResult{}, fmt.Errorf(
			"%w: child-safety policy version is invalid",
			vision.ErrModelUnavailable,
		)
	}
	deviceID := strings.TrimSpace(request.DeviceID)
	sessionID := strings.TrimSpace(request.TaskID)
	if deviceID == "" || sessionID == "" {
		return vision.ModelResult{}, fmt.Errorf("%w: vision identity is incomplete", vision.ErrModelUnavailable)
	}
	ticketEndpoint, err := httpx.JoinURL(defaultBaseURL(c.baseURL), "/internal/v1/devices/"+
		url.PathEscape(deviceID)+"/ai-execution-ticket")
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, err)
	}
	parsedTicket, err := url.Parse(ticketEndpoint)
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, err)
	}
	query := parsedTicket.Query()
	query.Set("model", c.model)
	query.Set("project", provenanceProject)
	query.Set("session_id", sessionID)
	parsedTicket.RawQuery = query.Encode()

	prompt := buildPrompt(request)
	encoded, err := json.Marshal(chatRequest{
		Model:  c.model,
		Stream: true,
		Messages: []message{
			{Role: "system", Content: visionSystemPrompt},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: encode request: %v", vision.ErrModelUnavailable, err)
	}
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: c.timeout}
	}
	ticket, err := c.issueTicket(ctx, client, parsedTicket.String())
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, observability.SafeErrorString(err))
	}
	endpoint, err := httpx.JoinURL(defaultBaseURL(c.baseURL), "/internal/v1/ai-execution")
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, err)
	}
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, err)
	}
	endpointQuery := parsedEndpoint.Query()
	endpointQuery.Set("ticket", ticket)
	parsedEndpoint.RawQuery = endpointQuery.Encode()
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		parsedEndpoint.String(),
		bytes.NewReader(encoded),
	)
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: create request: %v", vision.ErrModelUnavailable, err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.serviceToken)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if policyVersion != "" {
		httpRequest.Header.Set(platformobservability.PolicyVersionHeader, policyVersion)
	}

	response, err := client.Do(httpRequest)
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, observability.SafeErrorString(err))
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return vision.ModelResult{}, fmt.Errorf(
			"%w: status=%d",
			vision.ErrModelUnavailable,
			response.StatusCode,
		)
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return vision.ModelResult{}, fmt.Errorf(
			"%w: response is not an event stream",
			vision.ErrModelUnavailable,
		)
	}

	content, err := readSSEContent(response.Body)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return vision.ModelResult{}, errors.Join(vision.ErrModelUnavailable, ctxErr)
		}
		return vision.ModelResult{}, errors.Join(vision.ErrModelUnavailable, err)
	}
	result, err := decodeContent(content)
	if err != nil {
		return vision.ModelResult{}, fmt.Errorf("%w: %v", vision.ErrModelUnavailable, err)
	}
	result.Model = c.model
	return result, nil
}

func (c *Client) issueTicket(
	ctx context.Context,
	client *http.Client,
	endpoint string,
) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", errors.New("create vision execution ticket request")
	}
	request.Header.Set("Authorization", "Bearer "+c.serviceToken)
	request.Header.Set("Accept", "application/json")
	body, response, err := httpx.DoBounded(ctx, client, request, maxResponseBytes)
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("execution ticket request failed status=%d", response.StatusCode)
	}
	var envelope struct {
		Data struct {
			Ticket    string `json:"ticket"`
			ExpiresIn int    `json:"expires_in"`
		} `json:"data"`
		Error *struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", errors.New("decode vision execution ticket")
	}
	if envelope.Error != nil {
		return "", errors.New("vision execution ticket was rejected")
	}
	ticket := strings.TrimSpace(envelope.Data.Ticket)
	if ticket == "" || envelope.Data.ExpiresIn <= 0 {
		return "", errors.New("vision execution ticket is incomplete")
	}
	return ticket, nil
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

func decodeContent(content string) (vision.ModelResult, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return vision.ModelResult{}, errors.New("response content is empty")
	}
	content = stripCodeFence(content)

	var decoded struct {
		Labels  []vision.Label `json:"labels"`
		Summary string         `json:"summary"`
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return vision.ModelResult{}, errors.New("decode model content")
	}
	if err := ensureJSONEnd(decoder); err != nil {
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

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func validPolicyVersion(value string) bool {
	if strings.TrimSpace(value) == "" || len(value) > 64 {
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

func readSSEContent(reader io.Reader) (string, error) {
	if reader == nil {
		return "", vision.ErrModelStreamInterrupted
	}
	buffered := bufio.NewReaderSize(reader, 64<<10)
	builder := strings.Builder{}
	totalBytes := 0
	eventName := ""
	for {
		line, err := readSSELine(buffered, maxResponseLine)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", vision.ErrModelStreamInterrupted
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
			return "", fmt.Errorf("%w: %v", vision.ErrModelStreamInterrupted, observability.SafeErrorString(err))
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
			return builder.String(), nil
		}
		if data == "" {
			continue
		}
		if strings.EqualFold(eventName, "error") {
			return "", errors.New("model execution was rejected")
		}
		if err := appendSSEContent(&builder, data, maxResponseBytes); err != nil {
			return "", err
		}
		totalBytes += len(data)
		if totalBytes > maxResponseBytes {
			return "", errors.New("model stream response is too large")
		}
	}
}

func appendSSEContent(builder *strings.Builder, data string, limit int) error {
	if builder == nil {
		return errors.New("model stream accumulator is missing")
	}
	var chunk struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
		Content string `json:"content"`
		Delta   struct {
			Content string `json:"content"`
		} `json:"delta"`
		Choices []struct {
			FinishReason *string `json:"finish_reason"`
			Delta        struct {
				Content string `json:"content"`
			} `json:"delta"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return errors.New("decode model stream chunk")
	}
	if chunk.Error != nil {
		return errors.New("model execution was rejected")
	}
	if content := chunk.Delta.Content; content != "" {
		builder.WriteString(content)
	} else if chunk.Content != "" {
		builder.WriteString(chunk.Content)
	}
	if builder.Len() > limit {
		return errors.New("model stream response is too large")
	}
	for _, choice := range chunk.Choices {
		if choice.FinishReason != nil &&
			*choice.FinishReason != "" &&
			*choice.FinishReason != "stop" {
			return fmt.Errorf(
				"%w: provider stopped before completion",
				vision.ErrModelStreamInterrupted,
			)
		}
		content := choice.Delta.Content
		if content == "" {
			content = choice.Text
		}
		if content == "" {
			continue
		}
		builder.WriteString(content)
		if builder.Len() > limit {
			return errors.New("model stream response is too large")
		}
	}
	return nil
}

func readSSELine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		part, isPrefix, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(line)+len(part) > limit {
			return nil, errors.New("model stream line exceeds limit")
		}
		line = append(line, part...)
		if !isPrefix {
			return line, nil
		}
	}
}

func defaultBaseURL(baseURL string) string {
	if strings.TrimSpace(baseURL) == "" {
		return "http://127.0.0.1:8081"
	}
	return baseURL
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
