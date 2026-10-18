// Package domain contains AI gateway account and credential types.
package domain

import (
	"errors"
	"time"
)

var (
	ErrAccountNotFound     = errors.New("AI account not found")
	ErrProviderRejected    = errors.New("AI provider rejected request")
	ErrProviderUnavailable = errors.New("AI provider unavailable")
	ErrCredentialInvalid   = errors.New("AI credential is invalid")
	ErrModelNotAllowed     = errors.New("AI model is not allowed")
)

// Account mirrors the platform's view of one provider execution account.
type Account struct {
	ID                   string
	ParentAccountID      string
	ParentEmail          string
	ParentDisplayName    string
	ProviderAccountID    string
	ProviderAccountEmail string
	APIKeyCiphertext     []byte
	APIKeyNonce          []byte
	ProviderAPIKeyID     int64
	CredentialKeyVersion int
	Status               string
	BalanceUSD           float64
	ConcurrencyLimit     int
	// AvailableModels is the platform-approved model pool shown to guardians.
	AvailableModels []string
	// SelectedModels is the guardian's choice. Empty means "all available".
	SelectedModels []string
	// AllowedModels is the effective allowlist sent to the provider.
	AllowedModels []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ProviderAccount is the response from the AI provider account API.
type ProviderAccount struct {
	ProviderAccountID    string
	ProviderAccountEmail string
	UserID               int64
	Status               string
	BalanceUSD           float64
	HasBalanceUSD        bool
	ConcurrencyLimit     int
	AllowedModels        []string
}

// ProviderRuntimeConfig is the authoritative default account policy and model
// latency snapshot published by the AI gateway.
type ProviderRuntimeConfig struct {
	DefaultBalanceUSD  float64
	DefaultConcurrency int
	RecommendedModel   string
	Models             []ProviderModelLatency
}

// ProviderModelLatency describes one model's availability and measured
// latency. Nil latency means no successful measurement is available yet.
type ProviderModelLatency struct {
	Model                     string
	Status                    string
	PrimaryLatencyMs          *int
	AverageLatency7DaysMs     *int
	RecommendedForNewAccounts bool
}

// ProviderAPIKey is a one-time plaintext credential returned by the provider.
// Callers must immediately encrypt and persist it, then discard it from memory.
type ProviderAPIKey struct {
	ID        int64
	UserID    int64
	Key       string
	Name      string
	Status    string
	QuotaUSD  float64
	ExpiresAt *time.Time
}

// ProviderUsageTotals is the content-free aggregate usage the AI gateway
// reports for one provider account over a bounded reconciliation window.
type ProviderUsageTotals struct {
	Requests            int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	TotalTokens         int64
	CostUSD             float64
	ActualCostUSD       float64
	AverageDurationMs   float64
}

// ProviderUsageModelStat is the per-model breakdown of reconciled usage.
type ProviderUsageModelStat struct {
	Model               string
	Requests            int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	TotalTokens         int64
	CostUSD             float64
	ActualCostUSD       float64
}

// ProviderUsageReconciliation is one provider account's aggregated usage for a
// bounded window, as published by the AI gateway's internal reconciliation API.
// It never carries prompt or response content.
type ProviderUsageReconciliation struct {
	ProviderAccountID string
	UserID            int64
	StartTime         time.Time
	EndTime           time.Time
	Totals            ProviderUsageTotals
	Models            []ProviderUsageModelStat
}

// ProviderChildPolicy is the child-safety policy the AI gateway enforces for
// requests tagged with a platform age tier. It mirrors the gateway's internal
// contract and deliberately excludes the injected safety-boundary text and the
// internal service token, which never leave the gateway.
type ProviderChildPolicy struct {
	Enabled              bool
	RequirePolicyVersion bool
	DefaultAgeBand       string
	InjectSafetyBoundary bool
	PolicyVersions       []ProviderPolicyVersionBinding
	AgeBands             []ProviderAgeBandPolicy
}

// ProviderPolicyVersionBinding maps one platform policy version (an age tier)
// to the age band the gateway enforces for it.
type ProviderPolicyVersionBinding struct {
	Version string
	AgeBand string
}

// ProviderAgeBandPolicy is the gateway's model allowlist, output-token ceiling,
// and content rating for one age band.
type ProviderAgeBandPolicy struct {
	Name            string
	AllowedModels   []string
	MaxOutputTokens int
	ContentRating   string
}

// ChildPolicyTierAlignment reports how one platform age tier maps onto the
// gateway's enforced child policy. Bound is true only when the gateway binds
// the tier to a configured age band, so an operator can see at a glance whether
// tagged requests for that age would be enforced or rejected outright.
type ChildPolicyTierAlignment struct {
	AgeTier         string
	AgeBand         string
	Bound           bool
	AgeBandMissing  bool
	AllowedModels   []string
	MaxOutputTokens int
	ContentRating   string
}

// ChildPolicyAlignment is the platform's aligned view of the child-safety
// policy the gateway is enforcing. Aligned is true only when the policy is
// enabled and every canonical platform age tier is bound to a configured age
// band, which is the precondition for tagged requests to pass enforcement.
type ChildPolicyAlignment struct {
	Enabled              bool
	RequirePolicyVersion bool
	DefaultAgeBand       string
	InjectSafetyBoundary bool
	AgeBands             []ProviderAgeBandPolicy
	Tiers                []ChildPolicyTierAlignment
	Aligned              bool
}

// Credential is the decrypted provider credential used by server-side services.
type Credential struct {
	ProviderAccountID string
	APIKey            string
	KeyID             int64
	ConcurrencyLimit  int
	AllowedModels     []string
}
