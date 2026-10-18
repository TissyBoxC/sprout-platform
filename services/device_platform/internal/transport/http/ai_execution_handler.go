package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/TissyBoxC/sprout-platform/packages/go/observability"
	aiexecution "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/execution"
)

const (
	aiExecutionMaxBodyBytes = 1 << 20
	aiUpstreamErrorMaxBytes = 64 << 10
)

type aiExecutionRequest struct {
	Model string `json:"model"`
}

type aiExecutionPayload struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream,omitempty"`
	Messages json.RawMessage `json:"messages"`

	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Stop        json.RawMessage `json:"stop,omitempty"`

	Tools      json.RawMessage `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

// aiExecution forwards one ticket-authorized chat request to sub2api. The
// upstream provider key is decrypted only in this process and is never
// returned to the caller.
func (handler internalHandler) aiExecution(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.executionStore == nil ||
		handler.bindingService == nil ||
		handler.aiService == nil {
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	upstreamURL, err := handler.upstreamChatURL(handler.requireHTTPSUpstream)
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	ticketToken := strings.TrimSpace(request.URL.Query().Get("ticket"))
	if ticketToken == "" {
		writeError(response, request, http.StatusUnauthorized, "invalid_execution_ticket", "AI 服务授权已失效，请重新开始")
		return
	}
	ticket, err := handler.executionStore.Consume(request.Context(), ticketToken)
	if err != nil {
		writeAIExecutionTicketError(response, request, err)
		return
	}
	if ticket.DeviceID == "" ||
		ticket.ParentAccountID == "" ||
		ticket.Model == "" ||
		ticket.Concurrency < 1 {
		writeError(response, request, http.StatusUnauthorized, "invalid_execution_ticket", "AI 服务授权已失效，请重新开始")
		return
	}
	binding, err := handler.bindingService.GetByDeviceID(
		request.Context(),
		ticket.DeviceID,
	)
	if err != nil {
		writeInternalExecutionError(response, request, err)
		return
	}
	// A device can be unbound and rebound while a short-lived ticket is still
	// valid. Execute against the signer captured at issuance, and reject the
	// request if the durable binding no longer belongs to that guardian.
	if binding.ParentAccountID != ticket.ParentAccountID {
		writeError(response, request, http.StatusUnauthorized, "invalid_execution_ticket", "AI 服务授权已失效，请重新开始")
		return
	}

	var payload aiExecutionPayload
	if err := decodeAIExecutionPayload(
		response,
		request,
		&payload,
	); err != nil {
		writeAIExecutionBodyError(response, request, err)
		return
	}
	if strings.TrimSpace(payload.Model) == "" ||
		len(payload.Messages) == 0 {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查这次对话设置")
		return
	}
	if !payload.Stream {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "这次对话需要开启流式返回")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Model), ticket.Model) {
		writeError(response, request, http.StatusForbidden, "model_mismatch", "这次对话使用的模型不一致，请重新开始")
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查这次对话设置")
		return
	}
	credential, err := handler.aiService.AuthorizeExecution(
		request.Context(),
		ticket.ParentAccountID,
		ticket.Model,
	)
	if err != nil {
		writeInternalExecutionError(response, request, err)
		return
	}
	if credential == nil || strings.TrimSpace(credential.APIKey) == "" {
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}

	_, release, err := handler.executionStore.AcquireDeviceLease(
		request.Context(),
		ticket.DeviceID,
		ticket.Concurrency,
		handler.executionLeaseTTL,
	)
	if err != nil {
		if errors.Is(err, aiexecution.ErrTicketConcurrencyExceeded) {
			response.Header().Set("Retry-After", "2")
			writeError(response, request, http.StatusTooManyRequests, "execution_busy", "AI 正在处理上一条对话，请稍后再试")
			return
		}
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	defer release()

	upstreamRequest, err := http.NewRequestWithContext(
		request.Context(),
		http.MethodPost,
		upstreamURL.String(),
		bytes.NewReader(body),
	)
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	upstreamRequest.Header.Set("Authorization", "Bearer "+credential.APIKey)
	upstreamRequest.Header.Set("Content-Type", "application/json")
	accept := strings.TrimSpace(request.Header.Get("Accept"))
	if accept == "" {
		accept = "text/event-stream"
	}
	upstreamRequest.Header.Set("Accept", accept)
	setTrustedExecutionHeaders(
		upstreamRequest.Header,
		ticket,
		ticket.ParentAccountID,
		request.Header.Get(observability.PolicyVersionHeader),
	)

	httpClient := handler.aiHTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 0}
	}
	upstreamResponse, err := httpClient.Do(upstreamRequest)
	if err != nil {
		if request.Context().Err() != nil {
			return
		}
		writeError(response, request, http.StatusBadGateway, "ai_service_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	defer upstreamResponse.Body.Close()
	if upstreamResponse.StatusCode < 200 || upstreamResponse.StatusCode >= 300 {
		writeUpstreamExecutionError(response, request, upstreamResponse)
		return
	}
	if err := copyAIExecutionResponse(response, upstreamResponse); err != nil &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		// Headers may already be committed for a streaming response. There is
		// no safe second error body; closing the stream makes the relay retry
		// or surface a stable failure instead of presenting partial data as
		// complete.
		return
	}
}

func (handler internalHandler) upstreamChatURL(requireHTTPS bool) (*url.URL, error) {
	return resolveUpstreamChatURL(handler.aiUpstreamURL, requireHTTPS)
}

// resolveUpstreamChatURL validates the configured AI provider origin. HTTPS
// is always accepted with normal Go certificate and hostname verification.
// Plain HTTP is limited to explicit in-process test clients; the production
// router builds this URL with requireHTTPS set, preventing provider
// credentials and child conversation content from crossing a cleartext hop.
func resolveUpstreamChatURL(
	baseURL string,
	requireHTTPS bool,
) (*url.URL, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, errors.New("AI upstream URL is not configured")
	}
	parsed, err := url.Parse(base + "/v1/chat/completions")
	if err != nil || parsed.Host == "" ||
		parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("AI upstream URL is invalid")
	}
	if parsed.Scheme != "https" && (!strings.EqualFold(parsed.Scheme, "http") || requireHTTPS) {
		return nil, errors.New("AI upstream URL must use HTTPS")
	}
	return parsed, nil
}

func decodeAIExecutionPayload(
	response http.ResponseWriter,
	request *http.Request,
	destination any,
) error {
	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		aiExecutionMaxBodyBytes,
	)
	return decodeJSONBody(request, destination, aiExecutionMaxBodyBytes)
}

func writeAIExecutionBodyError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeError(response, request, http.StatusRequestEntityTooLarge, "request_too_large", "这次对话内容过长，请缩短后再试")
		return
	}
	if errors.Is(err, io.EOF) {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "这次对话内容为空，请重新发送")
		return
	}
	writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查这次对话内容")
}

func setTrustedExecutionHeaders(
	header http.Header,
	ticket aiexecution.Ticket,
	parentAccountID string,
	requestedPolicyVersion string,
) {
	header.Set(observability.TenantIDHeader, parentAccountID)
	header.Set(observability.DeviceIDHeader, ticket.DeviceID)
	header.Set(observability.SessionIDHeader, ticket.SessionID)
	header.Set("X-Sprout-Project", ticket.Project)
	// Policy versions are validated by sub2api. The relay may narrow the
	// safety policy but cannot choose the tenant, device, session or project.
	header.Del(observability.PolicyVersionHeader)
	policyVersion := strings.TrimSpace(requestedPolicyVersion)
	if policyVersion != "" &&
		len(policyVersion) <= 64 &&
		validPolicyVersion(policyVersion) {
		header.Set(observability.PolicyVersionHeader, policyVersion)
	}
}

func validPolicyVersion(value string) bool {
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

func copyAIExecutionResponse(
	response http.ResponseWriter,
	upstream *http.Response,
) error {
	for _, header := range []string{
		"Content-Type",
		"Cache-Control",
		"X-Accel-Buffering",
		"X-Request-ID",
	} {
		if value := upstream.Header.Get(header); value != "" {
			response.Header().Set(header, value)
		}
	}
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(upstream.StatusCode)
	controller := http.NewResponseController(response)
	buffer := make([]byte, 32<<10)
	for {
		read, readErr := upstream.Body.Read(buffer)
		if read > 0 {
			if _, writeErr := response.Write(buffer[:read]); writeErr != nil {
				return writeErr
			}
			if flushErr := controller.Flush(); flushErr != nil &&
				!errors.Is(flushErr, http.ErrNotSupported) {
				return flushErr
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func writeUpstreamExecutionError(
	response http.ResponseWriter,
	request *http.Request,
	upstream *http.Response,
) {
	_, _ = io.Copy(io.Discard, io.LimitReader(upstream.Body, aiUpstreamErrorMaxBytes+1))
	switch {
	case upstream.StatusCode == http.StatusUnauthorized ||
		upstream.StatusCode == http.StatusForbidden:
		writeError(response, request, http.StatusBadGateway, "ai_credential_rejected", "AI 服务授权需要更新，请联系维护人员")
	case upstream.StatusCode == http.StatusTooManyRequests:
		response.Header().Set("Retry-After", "5")
		writeError(response, request, http.StatusTooManyRequests, "ai_capacity_limited", "AI 服务当前较忙，请稍后再试")
	case upstream.StatusCode == http.StatusBadRequest ||
		upstream.StatusCode == http.StatusUnprocessableEntity:
		writeError(response, request, http.StatusBadRequest, "ai_request_rejected", "这次对话无法处理，请调整后重试")
	case upstream.StatusCode >= http.StatusInternalServerError:
		writeError(response, request, http.StatusBadGateway, "ai_service_unavailable", "AI 服务暂时不可用，请稍后重试")
	default:
		writeError(response, request, http.StatusBadGateway, "ai_service_rejected", "AI 服务没有完成这次对话，请稍后重试")
	}
}

func writeAIExecutionTicketError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	if errors.Is(err, aiexecution.ErrTicketInvalid) {
		writeError(response, request, http.StatusUnauthorized, "invalid_execution_ticket", "AI 服务授权已失效，请重新开始")
		return
	}
	if errors.Is(err, aiexecution.ErrTicketConcurrencyExceeded) {
		response.Header().Set("Retry-After", "2")
		writeError(response, request, http.StatusTooManyRequests, "execution_busy", "AI 正在处理上一条对话，请稍后再试")
		return
	}
	writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
}
