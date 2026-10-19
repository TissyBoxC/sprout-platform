package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/domain"
)

type stubCatalog struct {
	candidates []domain.Candidate
	err        error
}

func (c stubCatalog) ListCandidates(
	_ context.Context,
	_ string,
	_ string,
) ([]domain.Candidate, error) {
	return c.candidates, c.err
}

type stubSignals struct {
	favoriteIDs        []string
	favoriteCategories []string
	recentIDs          []string
	recentCategories   []string
	err                error
}

func (s stubSignals) FavoritePackageIDs(
	_ context.Context,
	_ string,
) ([]string, error) {
	return s.favoriteIDs, s.err
}

func (s stubSignals) FavoriteCategories(
	_ context.Context,
	_ string,
) ([]string, error) {
	return s.favoriteCategories, s.err
}

func (s stubSignals) RecentPackageIDs(
	_ context.Context,
	_ string,
	_ int,
) ([]string, error) {
	return s.recentIDs, s.err
}

func (s stubSignals) RecentCategories(
	_ context.Context,
	_ string,
	_ int,
) ([]string, error) {
	return s.recentCategories, s.err
}

func candidate(id, category string, ageTiers ...string) domain.Candidate {
	return domain.Candidate{
		PackageID:   id,
		Title:       "包 " + id,
		Category:    category,
		AgeTiers:    ageTiers,
		PublishedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestRecommendBoostsFavoriteCategory(t *testing.T) {
	service := New(Options{
		Catalog: stubCatalog{candidates: []domain.Candidate{
			candidate("story_new", "story", "age_3_4"),
			candidate("song_new", "nursery_rhyme", "age_3_4"),
		}},
		Signals: stubSignals{favoriteCategories: []string{"nursery_rhyme"}},
	})

	results, err := service.Recommend(context.Background(), "family-1", domain.Query{})
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].PackageID != "song_new" {
		t.Fatalf("top = %q, want song_new", results[0].PackageID)
	}
	if results[0].Reason != domain.ReasonSameCategory {
		t.Fatalf("reason = %q, want same_category", results[0].Reason)
	}
}

func TestRecommendExcludesConsumedPackages(t *testing.T) {
	service := New(Options{
		Catalog: stubCatalog{candidates: []domain.Candidate{
			candidate("story_a", "story", "age_3_4"),
			candidate("story_b", "story", "age_3_4"),
			candidate("story_c", "story", "age_3_4"),
		}},
		Signals: stubSignals{
			favoriteIDs: []string{"story_a"},
			recentIDs:   []string{"story_b"},
		},
	})

	results, err := service.Recommend(context.Background(), "family-1", domain.Query{})
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(results) != 1 || results[0].PackageID != "story_c" {
		t.Fatalf("results = %+v, want only story_c", results)
	}
}

func TestRecommendExcludesCallerSuppliedIDs(t *testing.T) {
	service := New(Options{
		Catalog: stubCatalog{candidates: []domain.Candidate{
			candidate("story_a", "story", "age_3_4"),
			candidate("story_b", "story", "age_3_4"),
		}},
	})

	results, err := service.Recommend(context.Background(), "family-1", domain.Query{
		ExcludePackageIDs: []string{"story_a"},
	})
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(results) != 1 || results[0].PackageID != "story_b" {
		t.Fatalf("results = %+v, want only story_b", results)
	}
}

func TestRecommendBoostsAgeTierMatch(t *testing.T) {
	service := New(Options{
		Catalog: stubCatalog{candidates: []domain.Candidate{
			candidate("match", "story", "age_5_6"),
			candidate("other", "story", "age_7_8"),
		}},
	})

	results, err := service.Recommend(context.Background(), "family-1", domain.Query{
		AgeTier: "age_5_6",
	})
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if results[0].PackageID != "match" {
		t.Fatalf("top = %q, want match", results[0].PackageID)
	}
	if results[0].Reason != domain.ReasonSameAgeTier {
		t.Fatalf("reason = %q, want same_age_tier", results[0].Reason)
	}
}

func TestRecommendClampsLimit(t *testing.T) {
	candidates := make([]domain.Candidate, 0, 30)
	for index := 0; index < 30; index++ {
		candidates = append(
			candidates,
			candidate(
				"pkg_"+string(rune('a'+index%26))+string(rune('a'+index/26)),
				"story",
				"age_3_4",
			),
		)
	}
	service := New(Options{Catalog: stubCatalog{candidates: candidates}})

	results, err := service.Recommend(context.Background(), "family-1", domain.Query{
		Limit: 9999,
	})
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(results) != MaxLimit {
		t.Fatalf("results = %d, want %d", len(results), MaxLimit)
	}
}

func TestRecommendRejectsInvalidAgeTier(t *testing.T) {
	service := New(Options{Catalog: stubCatalog{}})

	_, err := service.Recommend(context.Background(), "family-1", domain.Query{
		AgeTier: "age_1_2",
	})
	if !errors.Is(err, domain.ErrInvalidAgeTier) {
		t.Fatalf("err = %v, want ErrInvalidAgeTier", err)
	}
}

func TestRecommendPropagatesCatalogError(t *testing.T) {
	service := New(Options{
		Catalog: stubCatalog{err: errors.New("db down")},
	})

	if _, err := service.Recommend(context.Background(), "family-1", domain.Query{}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestCheckReadinessAcceptsEmptyCatalog(t *testing.T) {
	service := New(Options{
		Catalog: stubCatalog{},
		Signals: stubSignals{},
	})
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy || readiness.CandidateCount != 0 {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestReadinessMapsCatalogFailure(t *testing.T) {
	service := New(Options{
		Catalog: stubCatalog{err: errors.New("catalog unavailable")},
		Signals: stubSignals{},
	})
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestReadinessRejectsMissingCatalog(t *testing.T) {
	service := New(Options{})
	if _, err := service.Readiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("err = %v, want ErrReadinessUnavailable", err)
	}
}

func TestReadinessDegradesWithoutSignalReader(t *testing.T) {
	service := New(Options{Catalog: stubCatalog{}})
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded || readiness.SignalsReady {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
	if err := service.CheckReadiness(context.Background()); !errors.Is(
		err,
		domain.ErrReadinessUnavailable,
	) {
		t.Fatalf("check readiness err = %v, want ErrReadinessUnavailable", err)
	}
}
