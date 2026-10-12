// Package provider implements the sub2api internal account API client.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gatewaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
)

const responseSchemaVersion = "1.0.0"

// Client calls the private service API exposed by the sprout sub2api fork.
type Client struct {
	baseURL      *url.URL
	serviceToken string
	httpClient   *http.Client
}

// New creates a sub2api internal API client.
func New(baseURL string, serviceToken string, httpClient *http.Client) (*Client, error) {
	parsedBaseURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !parsedBaseURL.IsAbs() || parsedBaseURL.Host == "" {
		return nil, errors.New("AI provider base URL must be absolute")
	}
	if len(strings.TrimSpace(serviceToken)) < 32 {
		return nil, errors.New("AI provider service token must contain at least 32 characters")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:      parsedBaseURL,
		serviceToken: serviceToken,
		httpClient:   httpClient,
	}, nil
}

// GetAccount returns one account or domain.ErrAccountNotFound.
func (c *Client) GetAccount(
	ctx context.Context,
	providerAccountID string,
) (*gatewaydomain.ProviderAccount, error) {
	var response providerAccountData
	status, err := c.doJSON(
		ctx,
		http.MethodGet,
		"/internal/sprout/v1/ai-accounts/"+url.PathEscape(providerAccountID),
		nil,
		&response,
	)
	if status == http.StatusNotFound {
		return nil, gatewaydomain.ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	return response.toDomain(), nil
}

// GetRuntimeConfig returns the gateway's authoritative defaults and model
// latency measurements. The platform never substitutes local defaults when
// this call fails because doing so could create accounts with a different
// opening balance.
func (c *Client) GetRuntimeConfig(
	ctx context.Context,
) (*gatewaydomain.ProviderRuntimeConfig, error) {
	var response providerRuntimeConfigData
	if _, err := c.doJSON(
		ctx,
		http.MethodGet,
		"/internal/sprout/v1/runtime-config",
		nil,
		&response,
	); err != nil {
		return nil, err
	}
	models := make([]gatewaydomain.ProviderModelLatency, 0, len(response.Models))
	for _, model := range response.Models {
		models = append(models, gatewaydomain.ProviderModelLatency{
			Model:                     model.Model,
			Status:                    model.Status,
			PrimaryLatencyMs:          model.PrimaryLatencyMs,
			AverageLatency7DaysMs:     model.AverageLatency7DaysMs,
			RecommendedForNewAccounts: model.RecommendedForNewAccounts,
		})
	}
	return &gatewaydomain.ProviderRuntimeConfig{
		DefaultBalanceUSD:  response.DefaultBalanceUSD,
		DefaultConcurrency: response.DefaultConcurrency,
		RecommendedModel:   response.RecommendedModel,
		Models:             models,
	}, nil
}

// GetChildPolicy returns the child-safety policy the gateway is enforcing so
// the platform can align its own age bands and content ratings. The gateway
// never returns its safety-boundary text or service token through this call.
func (c *Client) GetChildPolicy(
	ctx context.Context,
) (*gatewaydomain.ProviderChildPolicy, error) {
	var response providerChildPolicyData
	if _, err := c.doJSON(
		ctx,
		http.MethodGet,
		"/internal/sprout/v1/child-policy",
		nil,
		&response,
	); err != nil {
		return nil, err
	}
	return response.toDomain(), nil
}

// CreateAccount creates one provider execution account.
func (c *Client) CreateAccount(
	ctx context.Context,
	request gatewaydomain.ProviderAccount,
	password string,
) (*gatewaydomain.ProviderAccount, error) {
	var response providerAccountData
	payload := map[string]any{
		"provider_account_id":    request.ProviderAccountID,
		"provider_account_email": strings.TrimSpace(request.ProviderAccountEmail),
		"password":               password,
		"concurrency_limit":      request.ConcurrencyLimit,
		"allowed_models":         request.AllowedModels,
	}
	if request.HasBalanceUSD {
		payload["balance_usd"] = request.BalanceUSD
	}
	if _, err := c.doJSON(
		ctx,
		http.MethodPost,
		"/internal/sprout/v1/ai-accounts",
		payload,
		&response,
	); err != nil {
		return nil, err
	}
	return response.toDomain(), nil
}

// UpdateAccount updates provider-side status, balance, concurrency, and models.
func (c *Client) UpdateAccount(
	ctx context.Context,
	providerAccountID string,
	request gatewaydomain.ProviderAccount,
	reason string,
) (*gatewaydomain.ProviderAccount, error) {
	status := request.Status
	balance := request.BalanceUSD
	concurrency := request.ConcurrencyLimit
	allowedModels := request.AllowedModels
	payload := map[string]any{
		"status":            status,
		"concurrency_limit": concurrency,
		"allowed_models":    allowedModels,
		"reason":            strings.TrimSpace(reason),
	}
	if request.HasBalanceUSD {
		payload["balance_usd"] = balance
	}
	var response providerAccountData
	if _, err := c.doJSON(
		ctx,
		http.MethodPut,
		"/internal/sprout/v1/ai-accounts/"+url.PathEscape(providerAccountID),
		payload,
		&response,
	); err != nil {
		return nil, err
	}
	return response.toDomain(), nil
}

// CreateAPIKey creates one provider credential and returns it exactly once.
func (c *Client) CreateAPIKey(
	ctx context.Context,
	providerAccountID string,
	request gatewaydomain.ProviderAPIKey,
) (*gatewaydomain.ProviderAPIKey, error) {
	payload := map[string]any{
		"name":       strings.TrimSpace(request.Name),
		"quota_usd":  request.QuotaUSD,
		"expires_at": request.ExpiresAt,
	}
	return c.writeAPIKey(
		ctx,
		"/internal/sprout/v1/ai-accounts/"+url.PathEscape(providerAccountID)+"/api-keys",
		payload,
	)
}

// RotateAPIKey replaces one provider credential without leaving a key gap.
func (c *Client) RotateAPIKey(
	ctx context.Context,
	providerAccountID string,
	apiKeyID int64,
	request gatewaydomain.ProviderAPIKey,
) (*gatewaydomain.ProviderAPIKey, error) {
	payload := map[string]any{
		"name":       strings.TrimSpace(request.Name),
		"quota_usd":  request.QuotaUSD,
		"expires_at": request.ExpiresAt,
	}
	return c.writeAPIKey(
		ctx,
		"/internal/sprout/v1/ai-accounts/"+
			url.PathEscape(providerAccountID)+
			"/api-keys/"+
			strconv.FormatInt(apiKeyID, 10)+
			"/rotate",
		payload,
	)
}

// DeleteAPIKey deletes one provider credential. Repeated deletion succeeds.
func (c *Client) DeleteAPIKey(
	ctx context.Context,
	providerAccountID string,
	apiKeyID int64,
) error {
	status, err := c.doJSON(
		ctx,
		http.MethodDelete,
		"/internal/sprout/v1/ai-accounts/"+
			url.PathEscape(providerAccountID)+
			"/api-keys/"+
			strconv.FormatInt(apiKeyID, 10),
		nil,
		nil,
	)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

// DeleteAccount deletes the controlled provider account and every credential
// it owns. A missing account is already deleted, so this method is idempotent.
func (c *Client) DeleteAccount(
	ctx context.Context,
	providerAccountID string,
) error {
	status, err := c.doJSON(
		ctx,
		http.MethodDelete,
		"/internal/sprout/v1/ai-accounts/"+url.PathEscape(providerAccountID),
		nil,
		nil,
	)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

// GetUsageReconciliation returns the AI gateway's aggregated, content-free
// usage for one controlled provider account over the given window. It is used
// to reconcile platform spend against the gateway without reading sub2api
// tables. A zero time is omitted so the gateway applies its own default window.
func (c *Client) GetUsageReconciliation(
	ctx context.Context,
	providerAccountID string,
	startTime time.Time,
	endTime time.Time,
) (*gatewaydomain.ProviderUsageReconciliation, error) {
	query := url.Values{}
	if !startTime.IsZero() {
		query.Set("start_time", startTime.UTC().Format(time.RFC3339))
	}
	if !endTime.IsZero() {
		query.Set("end_time", endTime.UTC().Format(time.RFC3339))
	}
	var response providerUsageReconciliationData
	status, err := c.doJSONWithQuery(
		ctx,
		http.MethodGet,
		"/internal/sprout/v1/usage/"+url.PathEscape(providerAccountID),
		query,
		nil,
		&response,
	)
	if status == http.StatusNotFound {
		return nil, gatewaydomain.ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	return response.toDomain(), nil
}

func (c *Client) writeAPIKey(
	ctx context.Context,
	path string,
	payload map[string]any,
) (*gatewaydomain.ProviderAPIKey, error) {
	var response providerAPIKeyData
	if _, err := c.doJSON(ctx, http.MethodPost, path, payload, &response); err != nil {
		return nil, err
	}
	if strings.TrimSpace(response.Key) == "" || response.ID <= 0 || response.UserID <= 0 {
		return nil, errors.New("AI provider returned an invalid credential")
	}
	return &gatewaydomain.ProviderAPIKey{
		ID:        response.ID,
		UserID:    response.UserID,
		Key:       response.Key,
		Name:      response.Name,
		Status:    response.Status,
		QuotaUSD:  response.QuotaUSD,
		ExpiresAt: response.ExpiresAt,
	}, nil
}

func (c *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	requestBody any,
	result any,
) (int, error) {
	return c.doJSONWithQuery(ctx, method, path, nil, requestBody, result)
}

func (c *Client) doJSONWithQuery(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	requestBody any,
	result any,
) (int, error) {
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	if len(query) > 0 {
		requestURL.RawQuery = query.Encode()
	}
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return 0, fmt.Errorf("encode AI provider request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return 0, fmt.Errorf("create AI provider request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.serviceToken)
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", gatewaydomain.ErrProviderUnavailable, err)
	}
	defer response.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return response.StatusCode, fmt.Errorf("%w: read response", gatewaydomain.ErrProviderUnavailable)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if result == nil {
			return response.StatusCode, nil
		}
		var envelope successEnvelope
		if err := json.Unmarshal(rawBody, &envelope); err != nil {
			return response.StatusCode, fmt.Errorf("decode AI provider response: %w", err)
		}
		if envelope.SchemaVersion != responseSchemaVersion {
			return response.StatusCode, fmt.Errorf("unsupported AI provider response schema")
		}
		encodedData, err := json.Marshal(envelope.Data)
		if err != nil {
			return response.StatusCode, fmt.Errorf("decode AI provider data: %w", err)
		}
		if err := json.Unmarshal(encodedData, result); err != nil {
			return response.StatusCode, fmt.Errorf("decode AI provider data: %w", err)
		}
		return response.StatusCode, nil
	}

	var envelope errorEnvelope
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return response.StatusCode, gatewaydomain.ErrProviderRejected
	}
	if envelope.Error.Retryable || response.StatusCode >= 500 {
		return response.StatusCode, gatewaydomain.ErrProviderUnavailable
	}
	return response.StatusCode, fmt.Errorf(
		"%w: %s",
		gatewaydomain.ErrProviderRejected,
		envelope.Error.Code,
	)
}

type successEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Data          any    `json:"data"`
}

type errorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

type providerAccountData struct {
	ProviderAccountID string   `json:"provider_account_id"`
	UserID            int64    `json:"user_id"`
	Status            string   `json:"status"`
	BalanceUSD        float64  `json:"balance_usd"`
	ConcurrencyLimit  int      `json:"concurrency_limit"`
	AllowedModels     []string `json:"allowed_models"`
}

type providerRuntimeConfigData struct {
	DefaultBalanceUSD  float64                    `json:"default_balance_usd"`
	DefaultConcurrency int                        `json:"default_concurrency"`
	RecommendedModel   string                     `json:"recommended_model"`
	Models             []providerModelLatencyData `json:"models"`
}

type providerModelLatencyData struct {
	Model                     string `json:"model"`
	Status                    string `json:"status"`
	PrimaryLatencyMs          *int   `json:"primary_latency_ms"`
	AverageLatency7DaysMs     *int   `json:"average_latency_7d_ms"`
	RecommendedForNewAccounts bool   `json:"recommended_for_new_accounts"`
}

type providerChildPolicyData struct {
	Enabled              bool                            `json:"enabled"`
	RequirePolicyVersion bool                            `json:"require_policy_version"`
	DefaultAgeBand       string                          `json:"default_age_band"`
	InjectSafetyBoundary bool                            `json:"inject_safety_boundary"`
	PolicyVersions       []providerPolicyVersionData     `json:"policy_versions"`
	AgeBands             []providerAgeBandPolicyData     `json:"age_bands"`
}

type providerPolicyVersionData struct {
	Version string `json:"version"`
	AgeBand string `json:"age_band"`
}

type providerAgeBandPolicyData struct {
	Name            string   `json:"name"`
	AllowedModels   []string `json:"allowed_models"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	ContentRating   string   `json:"content_rating"`
}

func (data providerChildPolicyData) toDomain() *gatewaydomain.ProviderChildPolicy {
	versions := make(
		[]gatewaydomain.ProviderPolicyVersionBinding,
		0,
		len(data.PolicyVersions),
	)
	for _, binding := range data.PolicyVersions {
		versions = append(versions, gatewaydomain.ProviderPolicyVersionBinding{
			Version: binding.Version,
			AgeBand: binding.AgeBand,
		})
	}
	bands := make(
		[]gatewaydomain.ProviderAgeBandPolicy,
		0,
		len(data.AgeBands),
	)
	for _, band := range data.AgeBands {
		bands = append(bands, gatewaydomain.ProviderAgeBandPolicy{
			Name:            band.Name,
			AllowedModels:   append([]string(nil), band.AllowedModels...),
			MaxOutputTokens: band.MaxOutputTokens,
			ContentRating:   band.ContentRating,
		})
	}
	return &gatewaydomain.ProviderChildPolicy{
		Enabled:              data.Enabled,
		RequirePolicyVersion: data.RequirePolicyVersion,
		DefaultAgeBand:       data.DefaultAgeBand,
		InjectSafetyBoundary: data.InjectSafetyBoundary,
		PolicyVersions:       versions,
		AgeBands:             bands,
	}
}

func (data providerAccountData) toDomain() *gatewaydomain.ProviderAccount {
	return &gatewaydomain.ProviderAccount{
		ProviderAccountID: data.ProviderAccountID,
		UserID:            data.UserID,
		Status:            data.Status,
		BalanceUSD:        data.BalanceUSD,
		ConcurrencyLimit:  data.ConcurrencyLimit,
		AllowedModels:     append([]string(nil), data.AllowedModels...),
	}
}

type providerAPIKeyData struct {
	UserID    int64      `json:"user_id"`
	ID        int64      `json:"api_key_id"`
	Name      string     `json:"name"`
	Key       string     `json:"api_key"`
	Status    string     `json:"status"`
	QuotaUSD  float64    `json:"quota_usd"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type providerUsageReconciliationData struct {
	ProviderAccountID string                   `json:"provider_account_id"`
	UserID            int64                    `json:"user_id"`
	StartTime         time.Time                `json:"start_time"`
	EndTime           time.Time                `json:"end_time"`
	Totals            providerUsageTotalsData  `json:"totals"`
	Models            []providerUsageModelData `json:"models"`
}

type providerUsageTotalsData struct {
	Requests            int64   `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	CostUSD             float64 `json:"cost_usd"`
	ActualCostUSD       float64 `json:"actual_cost_usd"`
	AverageDurationMs   float64 `json:"average_duration_ms"`
}

type providerUsageModelData struct {
	Model               string  `json:"model"`
	Requests            int64   `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	CostUSD             float64 `json:"cost_usd"`
	ActualCostUSD       float64 `json:"actual_cost_usd"`
}

func (data providerUsageReconciliationData) toDomain() *gatewaydomain.ProviderUsageReconciliation {
	models := make([]gatewaydomain.ProviderUsageModelStat, 0, len(data.Models))
	for _, model := range data.Models {
		models = append(models, gatewaydomain.ProviderUsageModelStat{
			Model:               model.Model,
			Requests:            model.Requests,
			InputTokens:         model.InputTokens,
			OutputTokens:        model.OutputTokens,
			CacheCreationTokens: model.CacheCreationTokens,
			CacheReadTokens:     model.CacheReadTokens,
			TotalTokens:         model.TotalTokens,
			CostUSD:             model.CostUSD,
			ActualCostUSD:       model.ActualCostUSD,
		})
	}
	return &gatewaydomain.ProviderUsageReconciliation{
		ProviderAccountID: data.ProviderAccountID,
		UserID:            data.UserID,
		StartTime:         data.StartTime.UTC(),
		EndTime:           data.EndTime.UTC(),
		Totals: gatewaydomain.ProviderUsageTotals{
			Requests:            data.Totals.Requests,
			InputTokens:         data.Totals.InputTokens,
			OutputTokens:        data.Totals.OutputTokens,
			CacheCreationTokens: data.Totals.CacheCreationTokens,
			CacheReadTokens:     data.Totals.CacheReadTokens,
			TotalTokens:         data.Totals.TotalTokens,
			CostUSD:             data.Totals.CostUSD,
			ActualCostUSD:       data.Totals.ActualCostUSD,
			AverageDurationMs:   data.Totals.AverageDurationMs,
		},
		Models: models,
	}
}
