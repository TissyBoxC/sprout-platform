package service

import (
	"reflect"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
)

func TestEffectiveModelsExpandsEmptySelectionToWholePool(t *testing.T) {
	pool := []string{"model-a", "model-b"}

	got := effectiveModels(nil, pool)

	if !reflect.DeepEqual(got, pool) {
		t.Fatalf("expected the whole pool, got %v", got)
	}
}

func TestEffectiveModelsKeepsExplicitSelection(t *testing.T) {
	pool := []string{"model-a", "model-b"}
	selected := []string{"model-b"}

	got := effectiveModels(selected, pool)

	if !reflect.DeepEqual(got, selected) {
		t.Fatalf("expected the explicit selection, got %v", got)
	}
}

// A guardian who unchecks a model must be able to re-check it later, so the
// pool is preserved rather than being overwritten by the selection.
func TestEffectiveModelsDoesNotMutatePoolSlice(t *testing.T) {
	pool := []string{"model-a", "model-b"}

	got := effectiveModels(nil, pool)
	got[0] = "mutated"

	if pool[0] != "model-a" {
		t.Fatal("effectiveModels must not alias the available pool")
	}
}

func TestIntersectModelsDropsModelsOutsideThePool(t *testing.T) {
	selected := []string{"model-a", "retired-model"}
	pool := []string{"model-a", "model-b"}

	got := intersectModels(selected, pool)

	if !reflect.DeepEqual(got, []string{"model-a"}) {
		t.Fatalf("expected only in-pool models, got %v", got)
	}
}

func TestIntersectModelsPreservesEmptySelection(t *testing.T) {
	got := intersectModels(nil, []string{"model-a"})

	if got != nil {
		t.Fatalf("expected a nil selection to stay empty, got %v", got)
	}
}

func TestIsModelSubsetRejectsUnknownModel(t *testing.T) {
	if isModelSubset([]string{"model-x"}, []string{"model-a"}) {
		t.Fatal("expected an unknown model to be rejected")
	}
}

func TestNormalizeModelSelectionTrimsAndDeduplicates(t *testing.T) {
	got, err := normalizeModelSelection([]string{" model-a ", "MODEL-A", "model-b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, []string{"model-a", "model-b"}) {
		t.Fatalf("unexpected normalized selection: %v", got)
	}
}

func TestNormalizeModelSelectionRejectsBlankModel(t *testing.T) {
	if _, err := normalizeModelSelection([]string{"   "}); err == nil {
		t.Fatal("expected a blank model to be rejected")
	}
}

func TestModelIDsFromRuntimeConfigTrimsDeduplicatesAndPreservesOrder(t *testing.T) {
	got := modelIDsFromRuntimeConfig([]domain.ProviderModelLatency{
		{Model: " model-b "},
		{Model: "model-a"},
		{Model: "MODEL-B"},
		{Model: "   "},
	})

	if !reflect.DeepEqual(got, []string{"model-b", "model-a"}) {
		t.Fatalf("unexpected runtime model pool: %v", got)
	}
}

func TestDefaultSelectedModelsPreservesValidSelection(t *testing.T) {
	got := defaultSelectedModelsWithCatalog(
		[]string{"model-b"},
		[]string{"model-a", "model-b"},
		providerModelCatalog{recommendedModel: "model-a"},
	)

	if !reflect.DeepEqual(got, []string{"model-b"}) {
		t.Fatalf("expected the explicit selection, got %v", got)
	}
}

func TestDefaultSelectedModelsSelectsWholePoolForNewAccount(t *testing.T) {
	pool := []string{"model-a", "model-b"}

	got := defaultSelectedModelsWithCatalog(nil, pool, providerModelCatalog{})

	if !reflect.DeepEqual(got, pool) {
		t.Fatalf("expected the whole pool, got %v", got)
	}
}

func TestDefaultSelectedModelsDoesNotAliasAvailablePool(t *testing.T) {
	pool := []string{"model-a", "model-b"}

	got := defaultSelectedModelsWithCatalog(nil, pool, providerModelCatalog{})
	got[0] = "mutated"

	if pool[0] != "model-a" {
		t.Fatal("defaultSelectedModels must not alias the available pool")
	}
}

func TestDefaultSelectedModelsUsesLowestLatencyRecommendation(t *testing.T) {
	catalog := providerModelCatalog{
		availableModels:  []string{"slow", "unmeasured", "fast"},
		recommendedModel: "fast",
		latencies: map[string]int{
			"slow": 340,
			"fast": 120,
		},
	}

	got := defaultSelectedModelsWithCatalog(nil, catalog.availableModels, catalog)

	if !reflect.DeepEqual(got, []string{"fast"}) {
		t.Fatalf("expected the lowest-latency model, got %v", got)
	}
}

func TestDefaultSelectedModelsFallsBackToWholePoolWhenLatencyUnknown(t *testing.T) {
	pool := []string{"model-a", "model-b"}
	got := defaultSelectedModelsWithCatalog(
		nil,
		pool,
		providerModelCatalog{availableModels: pool},
	)
	if !reflect.DeepEqual(got, pool) {
		t.Fatalf("expected the whole unmeasured pool, got %v", got)
	}
}
