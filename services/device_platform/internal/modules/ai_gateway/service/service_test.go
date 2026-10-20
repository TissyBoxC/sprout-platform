package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/repository"
	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
)

func TestEnsureForParentPersistsCredentialOnFirstCreate(t *testing.T) {
	repository := &memoryRepository{}
	provider := &stubProvider{
		account: nil,
		key:     &domain.ProviderAPIKey{ID: 41, UserID: 7, Key: "provider-secret"},
		runtimeConfig: &domain.ProviderRuntimeConfig{
			RecommendedModel: "model-b",
			Models: []domain.ProviderModelLatency{
				{Model: "model-a", PrimaryLatencyMs: intPointer(300)},
				{Model: "model-b", PrimaryLatencyMs: intPointer(120)},
			},
		},
	}
	service := newTestService(t, repository, provider)

	summary, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	)
	if err != nil {
		t.Fatalf("ensure parent AI account: %v", err)
	}
	if !summary.ProviderReady {
		t.Fatal("expected the returned summary to be provider-ready")
	}
	if repository.created == nil {
		t.Fatal("expected one account create")
	}
	if len(repository.created.APIKeyCiphertext) == 0 ||
		len(repository.created.APIKeyNonce) == 0 {
		t.Fatal("expected encrypted credential fields before the insert")
	}
	if repository.created.ProviderAPIKeyID != provider.key.ID {
		t.Fatalf(
			"expected provider key id %d, got %d",
			provider.key.ID,
			repository.created.ProviderAPIKeyID,
		)
	}
	if provider.createdKeyCalls != 1 || provider.rotateKeyCalls != 0 {
		t.Fatalf(
			"expected one create and no rotate, got create=%d rotate=%d",
			provider.createdKeyCalls,
			provider.rotateKeyCalls,
		)
	}
	if repository.created == nil || repository.created.BalanceUSD != 5 {
		t.Fatalf(
			"expected the configured initial balance to be persisted, got %#v",
			repository.created,
		)
	}
	if !reflect.DeepEqual(
		repository.created.AvailableModels,
		[]string{"model-a", "model-b"},
	) {
		t.Fatalf(
			"expected the full runtime model pool, got %v",
			repository.created.AvailableModels,
		)
	}
	if !reflect.DeepEqual(repository.created.SelectedModels, []string{"model-b"}) {
		t.Fatalf(
			"expected the lowest-latency model to be preselected, got %v",
			repository.created.SelectedModels,
		)
	}
	if !reflect.DeepEqual(
		provider.createdAccount.AllowedModels,
		[]string{"model-b"},
	) {
		t.Fatalf(
			"expected the effective allowlist on create, got %v",
			provider.createdAccount.AllowedModels,
		)
	}
}

func TestEnsureForParentBackfillsLegacyModelPool(t *testing.T) {
	repository := &memoryRepository{
		account: &domain.Account{
			ID:                   "ai-account-001",
			ParentAccountID:      "parent-001",
			ProviderAccountID:    "parent_parent001",
			APIKeyCiphertext:     []byte("ciphertext"),
			APIKeyNonce:          []byte("nonce"),
			ProviderAPIKeyID:     19,
			CredentialKeyVersion: 1,
			Status:               statusActive,
			BalanceUSD:           6,
			ConcurrencyLimit:     2,
		},
	}
	provider := &stubProvider{
		account: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        6,
			ConcurrencyLimit:  2,
		},
		runtimeConfig: &domain.ProviderRuntimeConfig{
			RecommendedModel: "model-b",
			Models: []domain.ProviderModelLatency{
				{Model: "model-a", PrimaryLatencyMs: intPointer(300)},
				{Model: "model-b", PrimaryLatencyMs: intPointer(120)},
			},
		},
	}
	service := newTestService(t, repository, provider)

	summary, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	)
	if err != nil {
		t.Fatalf("repair legacy AI account: %v", err)
	}
	if !reflect.DeepEqual(
		summary.AvailableModels,
		[]string{"model-a", "model-b"},
	) {
		t.Fatalf("expected the runtime pool, got %v", summary.AvailableModels)
	}
	if !reflect.DeepEqual(
		summary.SelectedModels,
		[]string{"model-b"},
	) {
		t.Fatalf("expected the lowest-latency model, got %v", summary.SelectedModels)
	}
}

func TestEnsureForParentPreservesValidLegacySelection(t *testing.T) {
	repository := &memoryRepository{
		account: &domain.Account{
			ID:                   "ai-account-001",
			ParentAccountID:      "parent-001",
			ProviderAccountID:    "parent_parent001",
			APIKeyCiphertext:     []byte("ciphertext"),
			APIKeyNonce:          []byte("nonce"),
			ProviderAPIKeyID:     19,
			CredentialKeyVersion: 1,
			Status:               statusActive,
			BalanceUSD:           6,
			ConcurrencyLimit:     2,
			SelectedModels:       []string{"model-a"},
		},
	}
	provider := &stubProvider{
		account: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        6,
			ConcurrencyLimit:  2,
		},
		runtimeConfig: &domain.ProviderRuntimeConfig{
			RecommendedModel: "model-b",
			Models: []domain.ProviderModelLatency{
				{Model: "model-a", PrimaryLatencyMs: intPointer(300)},
				{Model: "model-b", PrimaryLatencyMs: intPointer(120)},
			},
		},
	}
	service := newTestService(t, repository, provider)

	summary, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	)
	if err != nil {
		t.Fatalf("repair legacy AI account: %v", err)
	}
	if !reflect.DeepEqual(summary.SelectedModels, []string{"model-a"}) {
		t.Fatalf("expected the existing selection, got %v", summary.SelectedModels)
	}
	if !reflect.DeepEqual(summary.AvailableModels, []string{"model-a", "model-b"}) {
		t.Fatalf("expected the runtime pool, got %v", summary.AvailableModels)
	}
}

func intPointer(value int) *int {
	return &value
}

func TestEnsureForParentFallsBackToDefaultModelsWhenRuntimeConfigFails(
	t *testing.T,
) {
	provider := &stubProvider{
		runtimeConfigErr: errors.New("runtime config unavailable"),
		key:              &domain.ProviderAPIKey{ID: 41, UserID: 7, Key: "provider-secret"},
	}
	service := newTestService(t, &memoryRepository{}, provider)

	summary, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	)
	if err != nil {
		t.Fatalf("provision with runtime config failure: %v", err)
	}
	if !reflect.DeepEqual(summary.AvailableModels, []string{"model-a"}) {
		t.Fatalf("expected the fallback pool, got %v", summary.AvailableModels)
	}
	if !reflect.DeepEqual(summary.SelectedModels, []string{"model-a"}) {
		t.Fatalf("expected a fallback selection, got %v", summary.SelectedModels)
	}
}

func TestUpdateForAdminPreservesModelPoolWhenModelsAreOmitted(t *testing.T) {
	repository := &memoryRepository{
		account: &domain.Account{
			ID:                "ai-account-001",
			ParentAccountID:   "parent-001",
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        4,
			ConcurrencyLimit:  1,
			AvailableModels:   []string{"model-a", "model-b"},
			SelectedModels:    []string{"model-b"},
			AllowedModels:     []string{"model-b"},
		},
	}
	provider := &stubProvider{
		account: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        10,
			ConcurrencyLimit:  2,
		},
	}
	service := newTestService(t, repository, provider)

	account, err := service.UpdateForAdmin(
		context.Background(),
		"parent_parent001",
		statusActive,
		10,
		2,
		nil,
		"admin quota update",
	)
	if err != nil {
		t.Fatalf("update AI account: %v", err)
	}
	if !reflect.DeepEqual(account.AvailableModels, []string{"model-a", "model-b"}) {
		t.Fatalf("expected the existing pool, got %v", account.AvailableModels)
	}
	if !reflect.DeepEqual(account.SelectedModels, []string{"model-b"}) {
		t.Fatalf("expected the existing selection, got %v", account.SelectedModels)
	}
	if !reflect.DeepEqual(account.AllowedModels, []string{"model-b"}) {
		t.Fatalf("expected the effective allowlist, got %v", account.AllowedModels)
	}
}

func TestUpdateForAdminBackfillsLegacyModelPoolFromRuntimeConfig(t *testing.T) {
	repository := &memoryRepository{
		account: &domain.Account{
			ID:                "ai-account-001",
			ParentAccountID:   "parent-001",
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        4,
			ConcurrencyLimit:  1,
		},
	}
	provider := &stubProvider{
		account: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        10,
			ConcurrencyLimit:  2,
		},
		runtimeConfig: &domain.ProviderRuntimeConfig{
			RecommendedModel: "model-b",
			Models: []domain.ProviderModelLatency{
				{Model: "model-a"},
				{Model: "model-b"},
			},
		},
	}
	service := newTestService(t, repository, provider)

	account, err := service.UpdateForAdmin(
		context.Background(),
		"parent_parent001",
		statusActive,
		10,
		2,
		nil,
		"admin quota update",
	)
	if err != nil {
		t.Fatalf("update legacy AI account: %v", err)
	}
	if !reflect.DeepEqual(account.AvailableModels, []string{"model-a", "model-b"}) {
		t.Fatalf("expected the runtime pool, got %v", account.AvailableModels)
	}
	if !reflect.DeepEqual(
		account.SelectedModels,
		[]string{"model-b"},
	) {
		t.Fatalf("expected the lowest-latency model, got %v", account.SelectedModels)
	}
}

func TestEnsureForParentDeletesProviderKeyWhenProvisioningPersistFails(t *testing.T) {
	createErr := errors.New("database unavailable")
	repository := &memoryRepository{upsertErr: createErr}
	provider := &stubProvider{
		account: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			ConcurrencyLimit:  1,
		},
		key: &domain.ProviderAPIKey{ID: 73, UserID: 7, Key: "provider-secret"},
	}
	service := newTestService(t, repository, provider)

	if _, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	); !errors.Is(err, createErr) {
		t.Fatalf("expected the create error, got %v", err)
	}
	if !reflect.DeepEqual(provider.deletedKeyIDs, []int64{73}) {
		t.Fatalf("expected the created key to be deleted, got %v", provider.deletedKeyIDs)
	}
}

func TestEnsureForParentCreatesReplacementProviderKeyWhenProjectionIsMissing(t *testing.T) {
	repository := &memoryRepository{}
	provider := &stubProvider{
		createAccountResult: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        8,
			ConcurrencyLimit:  2,
		},
		key: &domain.ProviderAPIKey{ID: 84, UserID: 7, Key: "rotated-secret"},
	}
	repository.account = &domain.Account{
		ID:                "ai-account-001",
		ParentAccountID:   "parent-001",
		ProviderAccountID: "parent_parent001",
		Status:            statusActive,
		ConcurrencyLimit:  1,
		ProviderAPIKeyID:  19,
	}
	service := newTestService(t, repository, provider)

	if _, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	); err != nil {
		t.Fatalf("repair parent AI account: %v", err)
	}
	if provider.createdKeyCalls != 0 || provider.rotateKeyCalls != 1 {
		t.Fatalf(
			"expected one rotate and no create, got create=%d rotate=%d",
			provider.createdKeyCalls,
			provider.rotateKeyCalls,
		)
	}
	if repository.updated == nil || repository.updated.ProviderAPIKeyID != 84 {
		t.Fatal("expected the replacement credential to be persisted")
	}
}

func TestEnsureForParentRotatesRecordedProviderKeyOnRepair(t *testing.T) {
	repository := &memoryRepository{
		account: &domain.Account{
			ID:                "ai-account-001",
			ParentAccountID:   "parent-001",
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			ConcurrencyLimit:  1,
			ProviderAPIKeyID:  19,
		},
	}
	provider := &stubProvider{
		account: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			ConcurrencyLimit:  2,
		},
		key: &domain.ProviderAPIKey{ID: 84, UserID: 7, Key: "rotated-secret"},
	}
	service := newTestService(t, repository, provider)

	if _, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	); err != nil {
		t.Fatalf("repair parent AI account: %v", err)
	}
	if provider.rotateKeyCalls != 1 || provider.createdKeyCalls != 0 {
		t.Fatalf(
			"expected one rotate and no create, got create=%d rotate=%d",
			provider.createdKeyCalls,
			provider.rotateKeyCalls,
		)
	}
	if len(provider.deletedKeyIDs) != 0 {
		t.Fatalf("expected no orphaned key deletion, got %v", provider.deletedKeyIDs)
	}
	if repository.updated == nil || repository.updated.ProviderAPIKeyID != 84 {
		t.Fatal("expected the rotated credential to be persisted")
	}
}

func TestEnsureForParentReconcilesConcurrentProviderAccountCreate(t *testing.T) {
	repository := &memoryRepository{}
	provider := &stubProvider{
		account: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        8,
			ConcurrencyLimit:  2,
			AllowedModels:     []string{"model-a"},
		},
		getAccountInitialNotFound: true,
		createAccountErr:          domain.ErrProviderRejected,
		key:                       &domain.ProviderAPIKey{ID: 84, UserID: 7, Key: "provider-secret"},
	}
	service := newTestService(t, repository, provider)

	if _, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	); err != nil {
		t.Fatalf("reconcile concurrent provider account create: %v", err)
	}
	if provider.getAccountCalls != 2 {
		t.Fatalf("expected the account to be re-read once, got %d reads", provider.getAccountCalls)
	}
	if provider.createdKeyCalls != 1 {
		t.Fatalf("expected one provider key create, got %d", provider.createdKeyCalls)
	}
}

func TestEnsureForParentDeletesStaleProviderKeyAfterReplacementPersists(t *testing.T) {
	repository := &memoryRepository{
		account: &domain.Account{
			ID:                "ai-account-001",
			ParentAccountID:   "parent-001",
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			ConcurrencyLimit:  1,
			ProviderAPIKeyID:  19,
		},
	}
	provider := &stubProvider{
		account: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			ConcurrencyLimit:  2,
		},
		key:          &domain.ProviderAPIKey{ID: 84, UserID: 7, Key: "replacement-secret"},
		rotateKeyErr: domain.ErrProviderRejected,
	}
	service := newTestService(t, repository, provider)

	if _, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	); err != nil {
		t.Fatalf("repair parent AI account: %v", err)
	}
	if !reflect.DeepEqual(provider.deletedKeyIDs, []int64{19}) {
		t.Fatalf("expected stale provider key 19 to be deleted, got %v", provider.deletedKeyIDs)
	}
	if repository.updated == nil || repository.updated.ProviderAPIKeyID != 84 {
		t.Fatal("expected the replacement credential to be persisted before cleanup")
	}
}

func TestEnsureForParentSendsConfiguredInitialBalanceWhenCreatingAccount(t *testing.T) {
	repository := &memoryRepository{}
	provider := &stubProvider{
		key: &domain.ProviderAPIKey{ID: 91, UserID: 7, Key: "provider-secret"},
	}

	summary, err := newTestService(t, repository, provider).EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	)
	if err != nil {
		t.Fatalf("ensure parent AI account: %v", err)
	}
	if provider.createdAccount == nil {
		t.Fatal("expected one provider account create")
	}
	if !provider.createdAccount.HasBalanceUSD {
		t.Fatal("expected create request to include the configured balance")
	}
	if provider.createdAccount.BalanceUSD != 5 {
		t.Fatalf(
			"expected configured balance 5, got %v",
			provider.createdAccount.BalanceUSD,
		)
	}
	if summary.BalanceUSD != 5 {
		t.Fatalf("expected configured balance 5, got %v", summary.BalanceUSD)
	}
}

func TestEnsureForParentRepairsMissingProjectionWithoutOverwritingProviderBalance(
	t *testing.T,
) {
	repository := &memoryRepository{
		account: &domain.Account{
			ID:                "ai-account-001",
			ParentAccountID:   "parent-001",
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        99,
			ConcurrencyLimit:  1,
			AvailableModels:   []string{"model-a"},
			AllowedModels:     []string{"model-a"},
		},
	}
	provider := &stubProvider{
		account: &domain.ProviderAccount{
			ProviderAccountID: "parent_parent001",
			Status:            statusActive,
			BalanceUSD:        8,
			ConcurrencyLimit:  2,
			AllowedModels:     []string{"model-a"},
		},
		key: &domain.ProviderAPIKey{ID: 84, UserID: 7, Key: "replacement-secret"},
	}
	service := newTestService(t, repository, provider)

	summary, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	)
	if err != nil {
		t.Fatalf("repair parent AI account: %v", err)
	}
	if provider.createdKeyCalls != 1 || provider.rotateKeyCalls != 0 {
		t.Fatalf(
			"expected one create and no rotate, got create=%d rotate=%d",
			provider.createdKeyCalls,
			provider.rotateKeyCalls,
		)
	}
	if repository.updated == nil {
		t.Fatal("expected the repaired credential to be persisted")
	}
	if repository.updated.ProviderAPIKeyID != 84 {
		t.Fatalf("expected provider key id 84, got %d", repository.updated.ProviderAPIKeyID)
	}
	if repository.updated.BalanceUSD != 8 {
		t.Fatalf("expected provider balance 8, got %v", repository.updated.BalanceUSD)
	}
	if summary.BalanceUSD != 8 {
		t.Fatalf("expected summary balance 8, got %v", summary.BalanceUSD)
	}
}

func TestEnsureForParentReturnsExistingReadyAccountWithoutProviderCalls(t *testing.T) {
	repository := &memoryRepository{
		account: &domain.Account{
			ID:                   "ai-account-001",
			ParentAccountID:      "parent-001",
			ProviderAccountID:    "parent_parent001",
			APIKeyCiphertext:     []byte("ciphertext"),
			APIKeyNonce:          []byte("nonce"),
			ProviderAPIKeyID:     19,
			CredentialKeyVersion: 1,
			Status:               statusActive,
			BalanceUSD:           4,
			ConcurrencyLimit:     2,
			AvailableModels:      []string{"model-a"},
			AllowedModels:        []string{"model-a"},
		},
	}
	provider := &stubProvider{}
	service := newTestService(t, repository, provider)

	if _, err := service.EnsureForParent(
		context.Background(),
		"parent-001",
		"guardian@example.com",
	); err != nil {
		t.Fatalf("read parent AI account: %v", err)
	}
	if provider.getAccountCalls != 0 ||
		provider.createdKeyCalls != 0 ||
		provider.rotateKeyCalls != 0 {
		t.Fatal("a ready account must not call the provider")
	}
}

func TestCreateParentWithAIAccountPropagatesProvisioningFailure(t *testing.T) {
	parentService := &parentCreatorStub{
		account: &authdomain.ParentAccount{
			ID:    "parent-001",
			Email: "guardian@example.com",
		},
	}
	aiService := newTestService(t, &memoryRepository{}, &stubProvider{})

	_, summary, err := aiService.CreateParentWithAIAccount(
		context.Background(),
		parentService,
		authdomain.RegisterInput{},
	)
	if err == nil {
		t.Fatal("expected provisioning failure to be returned")
	}
	if summary != nil {
		t.Fatal("expected no summary when provisioning fails")
	}
}

func TestUsageReconciliationForAdminResolvesProviderAccountID(t *testing.T) {
	accountRepository := &memoryRepository{
		account: &domain.Account{
			ID:                "account-1",
			ParentAccountID:   "parent-001",
			ProviderAccountID: "parent_abc",
		},
	}
	provider := &stubProvider{
		usageReconciliation: &domain.ProviderUsageReconciliation{
			ProviderAccountID: "parent_abc",
			Totals:            domain.ProviderUsageTotals{Requests: 3},
		},
	}
	service := newTestService(t, accountRepository, provider)

	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	usage, err := service.UsageReconciliationForAdmin(
		context.Background(),
		"parent-001",
		start,
		end,
	)
	if err != nil {
		t.Fatalf("usage reconciliation: %v", err)
	}
	if provider.lastUsageProviderAccount != "parent_abc" {
		t.Fatalf(
			"resolved provider account = %q, want parent_abc",
			provider.lastUsageProviderAccount,
		)
	}
	if !provider.lastUsageStart.Equal(start) || !provider.lastUsageEnd.Equal(end) {
		t.Fatalf(
			"forwarded window = %v..%v, want %v..%v",
			provider.lastUsageStart,
			provider.lastUsageEnd,
			start,
			end,
		)
	}
	if usage == nil || usage.Totals.Requests != 3 {
		t.Fatalf("unexpected reconciliation result: %#v", usage)
	}
}

func TestUsageReconciliationForAdminFailsWhenAccountMissing(t *testing.T) {
	service := newTestService(t, &memoryRepository{}, &stubProvider{})
	_, err := service.UsageReconciliationForAdmin(
		context.Background(),
		"parent-missing",
		time.Time{},
		time.Time{},
	)
	if !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("expected account-not-found, got %v", err)
	}
}

func TestChildPolicyAlignmentForAdminMapsEveryCanonicalTier(t *testing.T) {
	provider := &stubProvider{
		childPolicy: &domain.ProviderChildPolicy{
			Enabled:              true,
			RequirePolicyVersion: true,
			InjectSafetyBoundary: true,
			PolicyVersions: []domain.ProviderPolicyVersionBinding{
				{Version: "age_3_4", AgeBand: "3-5"},
				{Version: "age_5_6", AgeBand: "6-8"},
				{Version: "age_7_8", AgeBand: "9-12"},
			},
			AgeBands: []domain.ProviderAgeBandPolicy{
				{Name: "3-5", AllowedModels: []string{"gpt-5-mini"}, MaxOutputTokens: 768, ContentRating: "G"},
				{Name: "6-8", AllowedModels: []string{"gpt-5-mini"}, MaxOutputTokens: 1536, ContentRating: "G"},
				{Name: "9-12", AllowedModels: []string{"gpt-5-mini"}, MaxOutputTokens: 3072, ContentRating: "PG"},
			},
		},
	}
	service := newTestService(t, &memoryRepository{}, provider)

	alignment, err := service.ChildPolicyAlignmentForAdmin(context.Background())
	if err != nil {
		t.Fatalf("align child policy: %v", err)
	}
	if !alignment.Aligned {
		t.Fatalf("expected a fully bound policy to be aligned: %#v", alignment)
	}
	if len(alignment.Tiers) != 3 {
		t.Fatalf("expected three canonical tiers, got %#v", alignment.Tiers)
	}
	if alignment.Tiers[0].AgeTier != "age_3_4" ||
		!alignment.Tiers[0].Bound ||
		alignment.Tiers[0].AgeBand != "3-5" ||
		alignment.Tiers[0].ContentRating != "G" {
		t.Fatalf("unexpected youngest tier alignment: %#v", alignment.Tiers[0])
	}
	if alignment.Tiers[2].ContentRating != "PG" ||
		alignment.Tiers[2].MaxOutputTokens != 3072 {
		t.Fatalf("unexpected oldest tier alignment: %#v", alignment.Tiers[2])
	}
}

func TestChildPolicyAlignmentForAdminFlagsUnboundTier(t *testing.T) {
	provider := &stubProvider{
		childPolicy: &domain.ProviderChildPolicy{
			Enabled:              true,
			RequirePolicyVersion: true,
			PolicyVersions: []domain.ProviderPolicyVersionBinding{
				{Version: "age_3_4", AgeBand: "3-5"},
				{Version: "age_5_6", AgeBand: "6-8"},
			},
			AgeBands: []domain.ProviderAgeBandPolicy{
				{Name: "3-5", AllowedModels: []string{"gpt-5-mini"}, MaxOutputTokens: 768, ContentRating: "G"},
				{Name: "6-8", AllowedModels: []string{"gpt-5-mini"}, MaxOutputTokens: 1536, ContentRating: "G"},
			},
		},
	}
	service := newTestService(t, &memoryRepository{}, provider)

	alignment, err := service.ChildPolicyAlignmentForAdmin(context.Background())
	if err != nil {
		t.Fatalf("align child policy: %v", err)
	}
	if alignment.Aligned {
		t.Fatal("expected a policy missing the oldest tier to be flagged unaligned")
	}
	if alignment.Tiers[2].AgeTier != "age_7_8" || alignment.Tiers[2].Bound {
		t.Fatalf("expected the oldest tier to be reported unbound: %#v", alignment.Tiers[2])
	}
}

func TestChildPolicyAlignmentForAdminFlagsMissingBand(t *testing.T) {
	provider := &stubProvider{
		childPolicy: &domain.ProviderChildPolicy{
			Enabled: true,
			PolicyVersions: []domain.ProviderPolicyVersionBinding{
				{Version: "age_3_4", AgeBand: "3-5"},
				{Version: "age_5_6", AgeBand: "6-8"},
				{Version: "age_7_8", AgeBand: "unknown-band"},
			},
			AgeBands: []domain.ProviderAgeBandPolicy{
				{Name: "3-5", AllowedModels: []string{"gpt-5-mini"}, MaxOutputTokens: 768, ContentRating: "G"},
				{Name: "6-8", AllowedModels: []string{"gpt-5-mini"}, MaxOutputTokens: 1536, ContentRating: "G"},
			},
		},
	}
	service := newTestService(t, &memoryRepository{}, provider)

	alignment, err := service.ChildPolicyAlignmentForAdmin(context.Background())
	if err != nil {
		t.Fatalf("align child policy: %v", err)
	}
	if alignment.Aligned {
		t.Fatal("expected a binding to a missing band to be flagged unaligned")
	}
	if !alignment.Tiers[2].AgeBandMissing || alignment.Tiers[2].Bound {
		t.Fatalf("expected the oldest tier to report a missing band: %#v", alignment.Tiers[2])
	}
}

func TestChildPolicyAlignmentForAdminPropagatesProviderError(t *testing.T) {
	provider := &stubProvider{childPolicyErr: domain.ErrProviderUnavailable}
	service := newTestService(t, &memoryRepository{}, provider)

	if _, err := service.ChildPolicyAlignmentForAdmin(context.Background()); !errors.Is(err, domain.ErrProviderUnavailable) {
		t.Fatalf("expected provider-unavailable, got %v", err)
	}
}

func newTestService(
	t *testing.T,
	accountRepository repository.Repository,
	provider Provider,
) *Service {
	t.Helper()
	cipher, err := NewAESGCMCipher("test-credential-key-at-least-32-characters")
	if err != nil {
		t.Fatalf("create credential cipher: %v", err)
	}
	service, err := New(Options{
		Repository:         accountRepository,
		Provider:           provider,
		Cipher:             cipher,
		DefaultBalanceUSD:  5,
		DefaultModels:      []string{"model-a"},
		DefaultConcurrency: 1,
	})
	if err != nil {
		t.Fatalf("create AI account service: %v", err)
	}
	return service
}

type parentCreatorStub struct {
	account *authdomain.ParentAccount
}

func (s *parentCreatorStub) CreateParent(
	_ context.Context,
	_ authdomain.RegisterInput,
) (*authdomain.ParentAccount, *authdomain.AIAccountSummary, error) {
	return s.account, nil, nil
}

type memoryRepository struct {
	account   *domain.Account
	created   *domain.Account
	updated   *domain.Account
	upsertErr error
}

func (r *memoryRepository) GetByParentAccountID(
	_ context.Context,
	parentAccountID string,
) (*domain.Account, error) {
	if r.account == nil || r.account.ParentAccountID != parentAccountID {
		return nil, domain.ErrAccountNotFound
	}
	copy := *r.account
	copy.AvailableModels = append([]string(nil), r.account.AvailableModels...)
	copy.SelectedModels = append([]string(nil), r.account.SelectedModels...)
	copy.AllowedModels = append([]string(nil), r.account.AllowedModels...)
	return &copy, nil
}

func (r *memoryRepository) GetByProviderAccountID(
	_ context.Context,
	providerAccountID string,
) (*domain.Account, error) {
	if r.account == nil || r.account.ProviderAccountID != providerAccountID {
		return nil, domain.ErrAccountNotFound
	}
	copy := *r.account
	return &copy, nil
}

func (r *memoryRepository) List(context.Context) ([]domain.Account, error) {
	if r.account == nil {
		return []domain.Account{}, nil
	}
	return []domain.Account{*r.account}, nil
}

func (r *memoryRepository) DeleteByParentAccountID(
	_ context.Context,
	parentAccountID string,
) error {
	if r.account != nil && r.account.ParentAccountID == parentAccountID {
		r.account = nil
	}
	return nil
}

func (r *memoryRepository) UpsertProvisioning(
	_ context.Context,
	account *domain.Account,
) (*domain.Account, error) {
	if r.upsertErr != nil {
		return nil, r.upsertErr
	}
	copy := *account
	if r.account == nil {
		r.created = &copy
	}
	r.account = &copy
	r.updated = &copy
	return &copy, nil
}

func (r *memoryRepository) UpdateFromProvider(
	_ context.Context,
	account *domain.Account,
) error {
	copy := *account
	r.account = &copy
	r.updated = &copy
	return nil
}

func (r *memoryRepository) UpdateStatus(
	_ context.Context,
	parentAccountID string,
	status string,
) error {
	if r.account == nil || r.account.ParentAccountID != parentAccountID {
		return domain.ErrAccountNotFound
	}
	r.account.Status = status
	return nil
}

type stubProvider struct {
	account                   *domain.ProviderAccount
	createAccountResult       *domain.ProviderAccount
	createAccountErr          error
	runtimeConfig             *domain.ProviderRuntimeConfig
	runtimeConfigErr          error
	childPolicy               *domain.ProviderChildPolicy
	childPolicyErr            error
	getAccountInitialNotFound bool
	createdAccount            *domain.ProviderAccount
	key                       *domain.ProviderAPIKey
	rotateKeyErr              error
	getAccountCalls           int
	createdKeyCalls           int
	rotateKeyCalls            int
	deletedKeyIDs             []int64
	deletedAccountIDs         []string
	usageReconciliation       *domain.ProviderUsageReconciliation
	usageReconciliationErr    error
	lastUsageProviderAccount  string
	lastUsageStart            time.Time
	lastUsageEnd              time.Time
}

func (p *stubProvider) GetRuntimeConfig(
	_ context.Context,
) (*domain.ProviderRuntimeConfig, error) {
	if p.runtimeConfigErr != nil {
		return nil, p.runtimeConfigErr
	}
	if p.runtimeConfig == nil {
		return &domain.ProviderRuntimeConfig{}, nil
	}
	copy := *p.runtimeConfig
	copy.Models = append([]domain.ProviderModelLatency(nil), p.runtimeConfig.Models...)
	return &copy, nil
}

func (p *stubProvider) GetChildPolicy(
	_ context.Context,
) (*domain.ProviderChildPolicy, error) {
	if p.childPolicyErr != nil {
		return nil, p.childPolicyErr
	}
	if p.childPolicy == nil {
		return &domain.ProviderChildPolicy{}, nil
	}
	copy := *p.childPolicy
	copy.AgeBands = append([]domain.ProviderAgeBandPolicy(nil), p.childPolicy.AgeBands...)
	copy.PolicyVersions = append(
		[]domain.ProviderPolicyVersionBinding(nil),
		p.childPolicy.PolicyVersions...,
	)
	return &copy, nil
}

func (p *stubProvider) GetAccount(
	_ context.Context,
	_ string,
) (*domain.ProviderAccount, error) {
	p.getAccountCalls++
	if p.getAccountInitialNotFound && p.getAccountCalls == 1 {
		return nil, domain.ErrAccountNotFound
	}
	if p.account == nil {
		return nil, domain.ErrAccountNotFound
	}
	copy := *p.account
	copy.AllowedModels = append([]string(nil), p.account.AllowedModels...)
	return &copy, nil
}

func (p *stubProvider) CreateAccount(
	_ context.Context,
	request domain.ProviderAccount,
	_ string,
) (*domain.ProviderAccount, error) {
	if p.createAccountErr != nil {
		return nil, p.createAccountErr
	}
	requestCopy := request
	requestCopy.AllowedModels = append([]string(nil), request.AllowedModels...)
	p.createdAccount = &requestCopy
	if p.createAccountResult != nil {
		resultCopy := *p.createAccountResult
		resultCopy.AllowedModels = append(
			[]string(nil),
			p.createAccountResult.AllowedModels...,
		)
		p.account = &resultCopy
		return &resultCopy, nil
	}
	p.account = &requestCopy
	return &requestCopy, nil
}

func (p *stubProvider) UpdateAccount(
	_ context.Context,
	_ string,
	request domain.ProviderAccount,
	_ string,
) (*domain.ProviderAccount, error) {
	copy := request
	copy.AllowedModels = append([]string(nil), request.AllowedModels...)
	p.createdAccount = &copy
	p.account = &copy
	return &copy, nil
}

func (p *stubProvider) CreateAPIKey(
	_ context.Context,
	_ string,
	_ domain.ProviderAPIKey,
) (*domain.ProviderAPIKey, error) {
	p.createdKeyCalls++
	if p.key == nil {
		return nil, domain.ErrProviderUnavailable
	}
	copy := *p.key
	return &copy, nil
}

func (p *stubProvider) RotateAPIKey(
	_ context.Context,
	_ string,
	_ int64,
	_ domain.ProviderAPIKey,
) (*domain.ProviderAPIKey, error) {
	p.rotateKeyCalls++
	if p.rotateKeyErr != nil {
		return nil, p.rotateKeyErr
	}
	if p.key == nil {
		return nil, domain.ErrProviderUnavailable
	}
	copy := *p.key
	return &copy, nil
}

func (p *stubProvider) DeleteAPIKey(
	_ context.Context,
	_ string,
	apiKeyID int64,
) error {
	p.deletedKeyIDs = append(p.deletedKeyIDs, apiKeyID)
	return nil
}

func (p *stubProvider) DeleteAccount(
	_ context.Context,
	providerAccountID string,
) error {
	p.deletedAccountIDs = append(p.deletedAccountIDs, providerAccountID)
	p.account = nil
	return nil
}

func (p *stubProvider) GetUsageReconciliation(
	_ context.Context,
	providerAccountID string,
	startTime time.Time,
	endTime time.Time,
) (*domain.ProviderUsageReconciliation, error) {
	p.lastUsageProviderAccount = providerAccountID
	p.lastUsageStart = startTime
	p.lastUsageEnd = endTime
	if p.usageReconciliationErr != nil {
		return nil, p.usageReconciliationErr
	}
	if p.usageReconciliation == nil {
		return &domain.ProviderUsageReconciliation{}, nil
	}
	copy := *p.usageReconciliation
	copy.Models = append(
		[]domain.ProviderUsageModelStat(nil),
		p.usageReconciliation.Models...,
	)
	return &copy, nil
}

var _ repository.Repository = (*memoryRepository)(nil)
var _ Provider = (*stubProvider)(nil)
