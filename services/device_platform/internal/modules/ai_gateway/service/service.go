// Package service owns parent AI account provisioning and credential lifecycle.
package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/repository"
	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

const (
	statusActive    = "active"
	statusSuspended = "suspended"
)

// Provider calls the private sub2api account and API key API.
type Provider interface {
	GetRuntimeConfig(ctx context.Context) (*domain.ProviderRuntimeConfig, error)
	GetAccount(
		ctx context.Context,
		providerAccountID string,
	) (*domain.ProviderAccount, error)
	CreateAccount(
		ctx context.Context,
		request domain.ProviderAccount,
		password string,
	) (*domain.ProviderAccount, error)
	UpdateAccount(
		ctx context.Context,
		providerAccountID string,
		request domain.ProviderAccount,
		reason string,
	) (*domain.ProviderAccount, error)
	CreateAPIKey(
		ctx context.Context,
		providerAccountID string,
		request domain.ProviderAPIKey,
	) (*domain.ProviderAPIKey, error)
	RotateAPIKey(
		ctx context.Context,
		providerAccountID string,
		apiKeyID int64,
		request domain.ProviderAPIKey,
	) (*domain.ProviderAPIKey, error)
	DeleteAPIKey(
		ctx context.Context,
		providerAccountID string,
		apiKeyID int64,
	) error
	DeleteAccount(ctx context.Context, providerAccountID string) error
}

// RuntimeConfigForAdmin returns the authoritative gateway defaults and model
// latency values used by the management console.
func (s *Service) RuntimeConfigForAdmin(
	ctx context.Context,
) (*domain.ProviderRuntimeConfig, error) {
	return s.provider.GetRuntimeConfig(ctx)
}

// CredentialCipher encrypts provider API keys before they reach PostgreSQL.
type CredentialCipher interface {
	Encrypt(plaintext []byte) (ciphertext []byte, nonce []byte, err error)
	Decrypt(ciphertext []byte, nonce []byte) ([]byte, error)
}

// Service implements auth.AIAccountProvisioner and administrative AI flows.
type Service struct {
	repository         repository.Repository
	provider           Provider
	cipher             CredentialCipher
	timeSource         clock.Clock
	policyReader       RuntimePolicyReader
	defaultBalance     float64
	defaultModels      []string
	defaultConcurrency int
}

// providerModelCatalog is the live model policy returned by the gateway.
type providerModelCatalog struct {
	availableModels []string
}

// RuntimePolicyReader supplies the operator defaults used when a new AI
// account is provisioned. A missing reader keeps the configured bootstrap
// values, which is required for the first administrator-created account.
type RuntimePolicyReader interface {
	RuntimePolicy(ctx context.Context) (*operationsdomain.RuntimePolicy, error)
}

// Options contains AI account service dependencies and defaults.
type Options struct {
	Repository         repository.Repository
	Provider           Provider
	Cipher             CredentialCipher
	Clock              clock.Clock
	PolicyReader       RuntimePolicyReader
	DefaultBalanceUSD  float64
	DefaultModels      []string
	DefaultConcurrency int
}

// New creates the AI account service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("AI account repository is required")
	}
	if options.Provider == nil {
		return nil, errors.New("AI account provider is required")
	}
	if options.Cipher == nil {
		return nil, errors.New("AI credential cipher is required")
	}
	if options.DefaultConcurrency < 1 {
		return nil, errors.New("AI default concurrency must be positive")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository:         options.Repository,
		provider:           options.Provider,
		cipher:             options.Cipher,
		timeSource:         timeSource,
		policyReader:       options.PolicyReader,
		defaultBalance:     options.DefaultBalanceUSD,
		defaultModels:      append([]string(nil), options.DefaultModels...),
		defaultConcurrency: options.DefaultConcurrency,
	}, nil
}

// SetPolicyReader wires the operations policy after both modules are built.
// Keeping it separate avoids an initialization cycle between AI and settings.
func (s *Service) SetPolicyReader(reader RuntimePolicyReader) {
	s.policyReader = reader
}

// effectiveDefaults returns the current operator defaults. A policy read
// failure is fatal for a new account because silently falling back could grant
// a different balance or model pool than the administrator configured.
func (s *Service) effectiveDefaults(
	ctx context.Context,
) (float64, int, []string, error) {
	if s.policyReader == nil {
		return s.defaultBalance, s.defaultConcurrency,
			append([]string(nil), s.defaultModels...), nil
	}
	policy, err := s.policyReader.RuntimePolicy(ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	models := append([]string(nil), policy.DefaultModels...)
	if len(models) == 0 {
		models = append([]string(nil), s.defaultModels...)
	}
	return policy.DefaultBalanceUSD, policy.DefaultConcurrency, models, nil
}

// resolveModelCatalog prefers the gateway's live model catalog because it is
// the only source that can identify a model currently reachable by a parent.
// A catalog failure falls back to the configured policy so sign-in and account
// repair remain available during transient gateway outages.
func (s *Service) resolveModelCatalog(
	ctx context.Context,
) (providerModelCatalog, error) {
	runtimeConfig, err := s.provider.GetRuntimeConfig(ctx)
	if err == nil && runtimeConfig != nil {
		availableModels := modelIDsFromRuntimeConfig(runtimeConfig.Models)
		if len(availableModels) > 0 {
			return providerModelCatalog{
				availableModels: availableModels,
			}, nil
		}
	}

	_, _, defaultModels, defaultsErr := s.effectiveDefaults(ctx)
	if defaultsErr != nil {
		return providerModelCatalog{}, defaultsErr
	}
	availableModels := normalizeModelIDs(defaultModels)
	return providerModelCatalog{
		availableModels: availableModels,
	}, nil
}

// repairModelCatalog fills a legacy empty pool and pushes the effective
// allowlist to the provider. An existing valid selection is retained.
func (s *Service) repairModelCatalog(
	ctx context.Context,
	account *domain.Account,
	catalog providerModelCatalog,
) error {
	if account == nil {
		return domain.ErrAccountNotFound
	}
	if len(account.AvailableModels) == 0 && len(catalog.availableModels) > 0 {
		account.AvailableModels = append(
			[]string(nil),
			catalog.availableModels...,
		)
	}
	if len(account.AvailableModels) == 0 {
		return nil
	}

	account.SelectedModels = defaultSelectedModels(
		account.SelectedModels,
		account.AvailableModels,
	)
	allowedModels := effectiveModels(
		account.SelectedModels,
		account.AvailableModels,
	)
	providerAccount, err := s.provider.GetAccount(
		ctx,
		account.ProviderAccountID,
	)
	if err != nil {
		return err
	}
	if providerAccount == nil {
		return domain.ErrProviderUnavailable
	}
	status := providerAccount.Status
	if status == "" {
		status = account.Status
	}
	concurrencyLimit := providerAccount.ConcurrencyLimit
	if concurrencyLimit < 1 {
		concurrencyLimit = account.ConcurrencyLimit
	}
	providerAccount, err = s.provider.UpdateAccount(
		ctx,
		account.ProviderAccountID,
		domain.ProviderAccount{
			ProviderAccountID: account.ProviderAccountID,
			Status:            status,
			ConcurrencyLimit:  concurrencyLimit,
			AllowedModels:     allowedModels,
		},
		"repair parent AI model catalog",
	)
	if err != nil {
		return err
	}
	account.Status = providerAccount.Status
	account.ConcurrencyLimit = providerAccount.ConcurrencyLimit
	account.AllowedModels = allowedModels
	return nil
}

// EnsureForParent creates or repairs the account projection and API key.
//
// The operation is idempotent. A retry that finds a provider account with a
// missing platform credential rotates the recorded provider key instead of
// accumulating usable credentials. When the provider account was created by a
// concurrent request, the rejected create is reconciled by one account read.
func (s *Service) EnsureForParent(
	ctx context.Context,
	parentAccountID string,
	parentEmail string,
) (*authdomain.AIAccountSummary, error) {
	parentAccountID = strings.TrimSpace(parentAccountID)
	if parentAccountID == "" {
		return nil, domain.ErrAccountNotFound
	}
	existing, err := s.repository.GetByParentAccountID(ctx, parentAccountID)
	if err != nil && !errors.Is(err, domain.ErrAccountNotFound) {
		return nil, err
	}
	if isProviderCredentialReady(existing) {
		if len(existing.AvailableModels) == 0 {
			catalog, catalogErr := s.resolveModelCatalog(ctx)
			if catalogErr != nil {
				return nil, catalogErr
			}
			if err := s.repairModelCatalog(ctx, existing, catalog); err != nil {
				return nil, err
			}
		} else {
			existing.AllowedModels = effectiveModels(
				existing.SelectedModels,
				existing.AvailableModels,
			)
		}
		existing.UpdatedAt = s.timeSource.Now().UTC()
		if err := s.repository.UpdateFromProvider(ctx, existing); err != nil {
			return nil, err
		}
		return s.summaryFromAccount(existing), nil
	}

	providerAccountID := providerAccountIDForParent(parentAccountID)
	providerAccount, catalog, err := s.loadOrCreateProviderAccount(
		ctx,
		providerAccountID,
		parentAccountID,
		parentEmail,
	)
	if err != nil {
		return nil, err
	}

	existingKeyID := int64(0)
	if existing != nil {
		existingKeyID = existing.ProviderAPIKeyID
	}
	providerKey, staleKeyID, err := s.ensureProviderKey(
		ctx,
		providerAccountID,
		existingKeyID,
	)
	if err != nil {
		return nil, err
	}
	encryptedKey, nonce, err := s.cipher.Encrypt([]byte(providerKey.Key))
	if err != nil {
		// A provider key was created or rotated but cannot be used without its
		// encrypted copy, so remove the known key before returning.
		_ = s.provider.DeleteAPIKey(ctx, providerAccountID, providerKey.ID)
		return nil, fmt.Errorf("encrypt AI credential: %w", err)
	}

	now := s.timeSource.Now().UTC()
	if existing == nil {
		availableModels := append([]string(nil), catalog.availableModels...)
		if len(availableModels) == 0 {
			availableModels = append(
				[]string(nil),
				providerAccount.AllowedModels...,
			)
		}
		selectedModels := defaultSelectedModels(
			nil,
			availableModels,
		)
		allowedModels := effectiveModels(selectedModels, availableModels)
		existing = &domain.Account{
			ID:                   uuid.NewString(),
			ParentAccountID:      parentAccountID,
			ProviderAccountID:    providerAccountID,
			ProviderAccountEmail: controlledEmail(parentEmail, parentAccountID),
			Status:               providerAccount.Status,
			BalanceUSD:           providerAccount.BalanceUSD,
			ConcurrencyLimit:     providerAccount.ConcurrencyLimit,
			APIKeyCiphertext:     encryptedKey,
			APIKeyNonce:          nonce,
			ProviderAPIKeyID:     providerKey.ID,
			CredentialKeyVersion: 1,
			AvailableModels:      availableModels,
			SelectedModels:       selectedModels,
			AllowedModels:        allowedModels,
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		stored, err := s.repository.UpsertProvisioning(ctx, existing)
		if err != nil {
			// Do not leave a usable provider credential if the platform could
			// not persist its encrypted copy.
			_ = s.provider.DeleteAPIKey(ctx, providerAccountID, providerKey.ID)
			return nil, err
		}
		if stored.ProviderAPIKeyID != providerKey.ID {
			// A concurrent request stored a complete credential first. The key
			// created by this request is now redundant.
			_ = s.provider.DeleteAPIKey(ctx, providerAccountID, providerKey.ID)
		}
		s.deleteStaleProviderKey(ctx, providerAccountID, staleKeyID, stored)
		return s.summaryFromAccount(stored), nil
	}
	existing.APIKeyCiphertext = encryptedKey
	existing.APIKeyNonce = nonce
	existing.ProviderAPIKeyID = providerKey.ID
	existing.CredentialKeyVersion = 1
	existing.Status = providerAccount.Status
	existing.BalanceUSD = providerAccount.BalanceUSD
	existing.ConcurrencyLimit = providerAccount.ConcurrencyLimit
	if len(existing.AvailableModels) == 0 {
		if err := s.repairModelCatalog(ctx, existing, catalog); err != nil {
			return nil, err
		}
	} else {
		existing.SelectedModels = intersectModels(
			existing.SelectedModels,
			existing.AvailableModels,
		)
		existing.AllowedModels = effectiveModels(
			existing.SelectedModels,
			existing.AvailableModels,
		)
	}
	existing.UpdatedAt = now
	stored, err := s.repository.UpsertProvisioning(ctx, existing)
	if err != nil {
		// The platform projection did not have a usable credential, so the new
		// provider key must not remain orphaned when persistence fails.
		_ = s.provider.DeleteAPIKey(ctx, providerAccountID, providerKey.ID)
		return nil, err
	}
	if stored.ProviderAPIKeyID != providerKey.ID {
		_ = s.provider.DeleteAPIKey(ctx, providerAccountID, providerKey.ID)
	}
	s.deleteStaleProviderKey(ctx, providerAccountID, staleKeyID, stored)
	return s.summaryFromAccount(stored), nil
}

// loadOrCreateProviderAccount returns the provider account, creating it when
// absent. A rejected create is retried once by reading the account, because a
// concurrent request can win the create between our read and write.
func (s *Service) loadOrCreateProviderAccount(
	ctx context.Context,
	providerAccountID string,
	parentAccountID string,
	parentEmail string,
) (*domain.ProviderAccount, providerModelCatalog, error) {
	catalog, catalogErr := s.resolveModelCatalog(ctx)
	if catalogErr != nil {
		return nil, providerModelCatalog{}, catalogErr
	}
	selectedModels := defaultSelectedModels(
		nil,
		catalog.availableModels,
	)
	allowedModels := effectiveModels(selectedModels, catalog.availableModels)

	providerAccount, err := s.provider.GetAccount(ctx, providerAccountID)
	if err == nil {
		if providerAccount == nil {
			return nil, providerModelCatalog{}, domain.ErrProviderUnavailable
		}
		return providerAccount, catalog, nil
	}
	if !errors.Is(err, domain.ErrAccountNotFound) {
		return nil, providerModelCatalog{}, err
	}
	defaultBalance, defaultConcurrency, _, defaultsErr :=
		s.effectiveDefaults(ctx)
	if defaultsErr != nil {
		return nil, providerModelCatalog{}, defaultsErr
	}
	providerAccount, err = s.provider.CreateAccount(
		ctx,
		domain.ProviderAccount{
			ProviderAccountID:    providerAccountID,
			ProviderAccountEmail: controlledEmail(parentEmail, parentAccountID),
			Status:               statusActive,
			BalanceUSD:           defaultBalance,
			HasBalanceUSD:        true,
			ConcurrencyLimit:     defaultConcurrency,
			AllowedModels:        allowedModels,
		},
		randomProviderPassword(),
	)
	if err != nil && errors.Is(err, domain.ErrProviderRejected) {
		recovered, recoverErr := s.provider.GetAccount(ctx, providerAccountID)
		if recoverErr == nil && recovered != nil {
			return recovered, catalog, nil
		}
	}
	if err != nil {
		return nil, providerModelCatalog{}, err
	}
	if providerAccount == nil {
		return nil, providerModelCatalog{}, domain.ErrProviderUnavailable
	}
	return providerAccount, catalog, nil
}

// ensureProviderKey returns a usable provider credential.
//
// When the platform already records a key id it rotates that key in place.
// If the provider reports the recorded key as rejected, a replacement is
// created and the stale id is returned so it can be removed after the new
// credential has been persisted. Never remove the stale key before a usable
// replacement is durable.
func (s *Service) ensureProviderKey(
	ctx context.Context,
	providerAccountID string,
	existingKeyID int64,
) (*domain.ProviderAPIKey, int64, error) {
	request := domain.ProviderAPIKey{
		Name:     "sprout-platform",
		QuotaUSD: 0,
	}
	if existingKeyID <= 0 {
		providerKey, err := s.provider.CreateAPIKey(
			ctx,
			providerAccountID,
			request,
		)
		return providerKey, 0, err
	}
	providerKey, err := s.provider.RotateAPIKey(
		ctx,
		providerAccountID,
		existingKeyID,
		request,
	)
	if err == nil {
		return providerKey, 0, nil
	}
	if !errors.Is(err, domain.ErrProviderRejected) {
		// A transient provider failure must not create a second credential;
		// the recorded key may still be valid.
		return nil, 0, err
	}
	providerKey, createErr := s.provider.CreateAPIKey(
		ctx,
		providerAccountID,
		request,
	)
	if createErr != nil {
		return nil, 0, createErr
	}
	return providerKey, existingKeyID, nil
}

// deleteStaleProviderKey removes a superseded credential only after the
// replacement projection is durable. Deletion is best effort because the
// stale key is no longer referenced by the platform.
func (s *Service) deleteStaleProviderKey(
	ctx context.Context,
	providerAccountID string,
	staleKeyID int64,
	stored *domain.Account,
) {
	if staleKeyID <= 0 || stored == nil || stored.ProviderAPIKeyID == staleKeyID {
		return
	}
	_ = s.provider.DeleteAPIKey(ctx, providerAccountID, staleKeyID)
}

// GetForParent returns the parent-safe AI account summary.
func (s *Service) GetForParent(
	ctx context.Context,
	parentAccountID string,
) (*authdomain.AIAccountSummary, error) {
	account, err := s.repository.GetByParentAccountID(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	return s.summaryFromAccount(account), nil
}

// ListForAdmin returns all platform AI account projections for operations.
func (s *Service) ListForAdmin(ctx context.Context) ([]domain.Account, error) {
	return s.repository.List(ctx)
}

// UpdateForAdmin applies an explicit admin change to provider and platform.
//
// availableModels is the platform-approved pool shown to guardians. The
// effective provider allowlist stays the guardian's selection restricted to
// that pool, so an administrator edit never widens a guardian's restriction.
func (s *Service) UpdateForAdmin(
	ctx context.Context,
	providerAccountID string,
	status string,
	balanceUSD float64,
	concurrencyLimit int,
	availableModels []string,
	reason string,
) (*domain.Account, error) {
	account, err := s.repository.GetByProviderAccountID(ctx, providerAccountID)
	if err != nil {
		return nil, err
	}
	availableModels = normalizeModelIDs(availableModels)
	// Older clients omit the model field entirely. Treat that as "keep the
	// current pool" so a quota-only edit cannot silently disable every model.
	if len(availableModels) == 0 {
		if len(account.AvailableModels) > 0 {
			availableModels = append(
				[]string(nil),
				account.AvailableModels...,
			)
		} else {
			catalog, catalogErr := s.resolveModelCatalog(ctx)
			if catalogErr != nil {
				return nil, catalogErr
			}
			availableModels = append(
				[]string(nil),
				catalog.availableModels...,
			)
			if len(account.SelectedModels) == 0 &&
				len(availableModels) > 0 {
				account.SelectedModels = defaultSelectedModels(
					nil,
					availableModels,
				)
			}
		}
	}
	previousSelection := append([]string(nil), account.SelectedModels...)
	selectedModels := intersectModels(account.SelectedModels, availableModels)
	if len(previousSelection) > 0 &&
		len(selectedModels) == 0 &&
		len(availableModels) > 0 {
		// The previous selection was removed from the pool. Keep one
		// deterministic model instead of widening the restriction to all.
		selectedModels = []string{availableModels[0]}
	}
	allowedModels := effectiveModels(selectedModels, availableModels)
	providerAccount, err := s.provider.UpdateAccount(
		ctx,
		providerAccountID,
		domain.ProviderAccount{
			ProviderAccountID: providerAccountID,
			Status:            status,
			BalanceUSD:        balanceUSD,
			HasBalanceUSD:     true,
			ConcurrencyLimit:  concurrencyLimit,
			AllowedModels:     append([]string(nil), allowedModels...),
		},
		reason,
	)
	if err != nil {
		return nil, err
	}
	account.Status = providerAccount.Status
	account.BalanceUSD = providerAccount.BalanceUSD
	account.ConcurrencyLimit = providerAccount.ConcurrencyLimit
	account.AvailableModels = availableModels
	account.SelectedModels = selectedModels
	account.AllowedModels = providerAccount.AllowedModels
	account.UpdatedAt = s.timeSource.Now().UTC()
	if err := s.repository.UpdateFromProvider(ctx, account); err != nil {
		return nil, err
	}
	return account, nil
}

// UpdateModelsForParent lets a guardian choose from the platform-approved pool.
// An empty selection means "all available models". The effective allowlist
// pushed to the provider is always a subset of that pool.
func (s *Service) UpdateModelsForParent(
	ctx context.Context,
	parentAccountID string,
	selectedModels []string,
) (*authdomain.AIAccountSummary, error) {
	account, err := s.repository.GetByParentAccountID(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	normalizedModels, err := normalizeModelSelection(selectedModels)
	if err != nil {
		return nil, err
	}
	availableModels := account.AvailableModels
	if len(availableModels) == 0 {
		catalog, catalogErr := s.resolveModelCatalog(ctx)
		if catalogErr != nil {
			return nil, catalogErr
		}
		availableModels = append(
			[]string(nil),
			catalog.availableModels...,
		)
	}
	if !isModelSubset(normalizedModels, availableModels) {
		return nil, domain.ErrModelNotAllowed
	}
	allowedModels := effectiveModels(normalizedModels, availableModels)
	providerAccount, err := s.provider.GetAccount(ctx, account.ProviderAccountID)
	if err != nil {
		return nil, err
	}
	updatedProviderAccount, err := s.provider.UpdateAccount(
		ctx,
		account.ProviderAccountID,
		domain.ProviderAccount{
			ProviderAccountID: account.ProviderAccountID,
			Status:            providerAccount.Status,
			BalanceUSD:        providerAccount.BalanceUSD,
			HasBalanceUSD:     true,
			ConcurrencyLimit:  providerAccount.ConcurrencyLimit,
			AllowedModels:     allowedModels,
		},
		"parent model selection",
	)
	if err != nil {
		return nil, err
	}
	account.Status = updatedProviderAccount.Status
	account.BalanceUSD = updatedProviderAccount.BalanceUSD
	account.ConcurrencyLimit = updatedProviderAccount.ConcurrencyLimit
	account.AvailableModels = availableModels
	account.SelectedModels = normalizedModels
	account.AllowedModels = updatedProviderAccount.AllowedModels
	account.UpdatedAt = s.timeSource.Now().UTC()
	if err := s.repository.UpdateFromProvider(ctx, account); err != nil {
		return nil, err
	}
	return s.summaryFromAccount(account), nil
}

// CredentialForDevice returns an internal credential for server-side relay use.
// It must never be exposed through parent, admin, firmware, or public APIs.
func (s *Service) CredentialForDevice(
	ctx context.Context,
	parentAccountID string,
) (*domain.Credential, error) {
	account, err := s.repository.GetByParentAccountID(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	if account.Status != statusActive {
		return nil, domain.ErrCredentialInvalid
	}
	plaintext, err := s.cipher.Decrypt(
		account.APIKeyCiphertext,
		account.APIKeyNonce,
	)
	if err != nil {
		return nil, fmt.Errorf("decrypt AI credential: %w", err)
	}
	return &domain.Credential{
		ProviderAccountID: account.ProviderAccountID,
		APIKey:            string(plaintext),
	}, nil
}

// DeleteForParent removes the provider-side account for one guardian. It is
// idempotent because deletion may be retried after a transient provider or
// database failure. The platform projection is removed only after the
// provider has accepted the deletion.
func (s *Service) DeleteForParent(
	ctx context.Context,
	parentAccountID string,
) error {
	parentAccountID = strings.TrimSpace(parentAccountID)
	if parentAccountID == "" {
		return domain.ErrAccountNotFound
	}
	account, err := s.repository.GetByParentAccountID(ctx, parentAccountID)
	if errors.Is(err, domain.ErrAccountNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.provider.DeleteAccount(ctx, account.ProviderAccountID); err != nil &&
		!errors.Is(err, domain.ErrAccountNotFound) {
		return err
	}
	return s.repository.DeleteByParentAccountID(ctx, parentAccountID)
}

func normalizeModelSelection(models []string) ([]string, error) {
	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 128 {
			return nil, domain.ErrModelNotAllowed
		}
		key := strings.ToLower(model)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, model)
	}
	return normalized, nil
}

func modelIDsFromRuntimeConfig(
	models []domain.ProviderModelLatency,
) []string {
	modelIDs := make([]string, 0, len(models))
	for _, model := range models {
		modelID := strings.TrimSpace(model.Model)
		if modelID == "" || len(modelID) > 128 {
			continue
		}
		modelIDs = append(modelIDs, modelID)
	}
	return normalizeModelIDs(modelIDs)
}

func normalizeModelIDs(models []string) []string {
	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 128 {
			continue
		}
		key := strings.ToLower(model)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, model)
	}
	return normalized
}

// defaultSelectedModels preserves a valid explicit selection and otherwise
// selects the entire available pool. Guardians start with every approved
// model rather than a single recommendation, and may narrow the selection
// later in either client.
func defaultSelectedModels(
	selectedModels []string,
	availableModels []string,
) []string {
	selectedModels = intersectModels(selectedModels, availableModels)
	if len(selectedModels) > 0 {
		return selectedModels
	}
	if len(availableModels) == 0 {
		return nil
	}
	return append([]string(nil), availableModels...)
}

func isModelSubset(selectedModels []string, availableModels []string) bool {
	if len(selectedModels) == 0 {
		return true
	}
	available := make(map[string]struct{}, len(availableModels))
	for _, model := range availableModels {
		available[strings.ToLower(strings.TrimSpace(model))] = struct{}{}
	}
	if len(available) == 0 {
		return true
	}
	for _, model := range selectedModels {
		if _, exists := available[strings.ToLower(model)]; !exists {
			return false
		}
	}
	return true
}

// intersectModels keeps the guardian's explicit selection inside the pool.
// An empty selection is preserved because it means "all available".
func intersectModels(selectedModels []string, availableModels []string) []string {
	if len(selectedModels) == 0 {
		return nil
	}
	available := make(map[string]struct{}, len(availableModels))
	for _, model := range availableModels {
		available[strings.ToLower(strings.TrimSpace(model))] = struct{}{}
	}
	intersection := make([]string, 0, len(selectedModels))
	for _, model := range selectedModels {
		if _, exists := available[strings.ToLower(strings.TrimSpace(model))]; exists {
			intersection = append(intersection, model)
		}
	}
	return intersection
}

// effectiveModels resolves the allowlist pushed to the provider. An empty
// selection means the guardian accepts every platform-approved model.
func effectiveModels(selectedModels []string, availableModels []string) []string {
	if len(selectedModels) == 0 {
		return append([]string(nil), availableModels...)
	}
	return append([]string(nil), selectedModels...)
}

func (s *Service) summaryFromAccount(
	account *domain.Account,
) *authdomain.AIAccountSummary {
	return &authdomain.AIAccountSummary{
		Status:           account.Status,
		BalanceUSD:       account.BalanceUSD,
		ConcurrencyLimit: account.ConcurrencyLimit,
		AvailableModels:  append([]string(nil), account.AvailableModels...),
		SelectedModels:   append([]string(nil), account.SelectedModels...),
		AllowedModels:    append([]string(nil), account.AllowedModels...),
		ProviderReady:    isProviderCredentialReady(account),
	}
}

func isProviderCredentialReady(account *domain.Account) bool {
	return account != nil &&
		len(account.APIKeyCiphertext) > 0 &&
		len(account.APIKeyNonce) > 0 &&
		account.ProviderAPIKeyID > 0
}

// AESGCMCipher encrypts credentials with a versioned, authenticated key.
type AESGCMCipher struct {
	aead cipher.AEAD
}

// NewAESGCMCipher derives a 256-bit key from configuration material.
func NewAESGCMCipher(secret string) (*AESGCMCipher, error) {
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("AI credential key must contain at least 32 characters")
	}
	derivedKey := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(derivedKey[:])
	if err != nil {
		return nil, fmt.Errorf("create credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create credential AEAD: %w", err)
	}
	return &AESGCMCipher{aead: aead}, nil
}

// Encrypt returns ciphertext and a unique nonce.
func (c *AESGCMCipher) Encrypt(plaintext []byte) ([]byte, []byte, error) {
	if c == nil || c.aead == nil {
		return nil, nil, errors.New("credential cipher is not configured")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generate credential nonce: %w", err)
	}
	return c.aead.Seal(nil, nonce, plaintext, nil), nonce, nil
}

// Decrypt authenticates and decrypts stored credential material.
func (c *AESGCMCipher) Decrypt(ciphertext []byte, nonce []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("credential cipher is not configured")
	}
	if len(nonce) != c.aead.NonceSize() {
		return nil, errors.New("credential nonce has an invalid length")
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("credential authentication failed")
	}
	return plaintext, nil
}

func providerAccountIDForParent(parentAccountID string) string {
	compact := strings.ReplaceAll(strings.ToLower(parentAccountID), "-", "")
	return "parent_" + compact
}

func controlledEmail(parentEmail string, parentAccountID string) string {
	compact := strings.ReplaceAll(strings.ToLower(parentAccountID), "-", "")
	if len(compact) > 20 {
		compact = compact[:20]
	}
	return "parent." + compact + "@ai.sprout.local"
}

func randomProviderPassword() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// The caller will surface the provider failure; this path cannot safely
		// continue because a weak provider password would be created.
		panic("AI provider password source unavailable")
	}
	return "sp-" + hex.EncodeToString(bytes)
}
