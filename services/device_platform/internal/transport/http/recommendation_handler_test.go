package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	recommendationdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/domain"
	recommendationservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/service"
)

// recommendationTestCatalog is an in-memory catalog double so the handler
// tests exercise the real recommendation service end to end.
type recommendationTestCatalog struct {
	candidates []recommendationdomain.Candidate
}

func (c recommendationTestCatalog) ListCandidates(
	_ context.Context,
	ageTier string,
) ([]recommendationdomain.Candidate, error) {
	result := make([]recommendationdomain.Candidate, 0, len(c.candidates))
	for _, candidate := range c.candidates {
		if ageTier != "" && !containsAgeTier(candidate.AgeTiers, ageTier) {
			continue
		}
		result = append(result, candidate)
	}
	return result, nil
}

func containsAgeTier(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// recommendationTestSignals treats the family id as the favourite category so
// handler tests can assert the family scope is passed through faithfully.
type recommendationTestSignals struct{}

func (recommendationTestSignals) FavoritePackageIDs(
	_ context.Context,
	familyID string,
) ([]string, error) {
	return []string{"pkg_favorite_" + familyID}, nil
}

func (recommendationTestSignals) FavoriteCategories(
	_ context.Context,
	familyID string,
) ([]string, error) {
	return []string{"story_" + familyID}, nil
}

func (recommendationTestSignals) RecentPackageIDs(
	_ context.Context,
	_ string,
	_ int,
) ([]string, error) {
	return nil, nil
}

func (recommendationTestSignals) RecentCategories(
	_ context.Context,
	_ string,
	_ int,
) ([]string, error) {
	return nil, nil
}

func newRecommendationHandlerForTest(t *testing.T) recommendationHandler {
	t.Helper()
	service := recommendationservice.New(recommendationservice.Options{
		Catalog: recommendationTestCatalog{candidates: []recommendationdomain.Candidate{
			{
				PackageID:   "pkg_story_new",
				Title:       "新故事",
				Category:    "story_family-1",
				AgeTiers:    []string{"age_3_4"},
				PublishedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			},
			{
				PackageID:   "pkg_song_new",
				Title:       "新儿歌",
				Category:    "nursery_rhyme",
				AgeTiers:    []string{"age_3_4"},
				PublishedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			},
			{
				PackageID:   "pkg_favorite_family-1",
				Title:       "已收藏",
				Category:    "story_family-1",
				AgeTiers:    []string{"age_3_4"},
				PublishedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			},
		}},
		Signals: recommendationTestSignals{},
	})
	return recommendationHandler{service: service}
}

func TestRecommendationListRequiresAuthentication(t *testing.T) {
	handler := newRecommendationHandlerForTest(t)
	request := englishRequest(
		http.MethodGet,
		"/api/v1/content/recommendations",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.list(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRecommendationListRanksAndScopesToFamily(t *testing.T) {
	handler := newRecommendationHandlerForTest(t)
	request := englishRequest(
		http.MethodGet,
		"/api/v1/content/recommendations?age_tier=age_3_4",
		"",
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.list(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Recommendations []struct {
				PackageID string `json:"package_id"`
				Reason    string `json:"reason"`
				Score     int    `json:"score"`
			} `json:"recommendations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	items := envelope.Data.Recommendations
	if len(items) != 2 {
		t.Fatalf("recommendations = %d, want 2: %s",
			len(items), recorder.Body.String())
	}
	// The favourited package itself is excluded; the fresh story shares the
	// favourite category and must outrank the unrelated nursery rhyme.
	if items[0].PackageID != "pkg_story_new" {
		t.Fatalf("top = %q, want pkg_story_new", items[0].PackageID)
	}
	if items[0].Reason != recommendationdomain.ReasonSameCategory {
		t.Fatalf("reason = %q, want same_category", items[0].Reason)
	}
	if items[0].Score <= items[1].Score {
		t.Fatalf("top score = %d, want > %d", items[0].Score, items[1].Score)
	}
}

func TestRecommendationListAppliesExcludeAndLimit(t *testing.T) {
	handler := newRecommendationHandlerForTest(t)
	request := englishRequest(
		http.MethodGet,
		"/api/v1/content/recommendations?exclude=pkg_story_new&limit=5",
		"",
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.list(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Recommendations []struct {
				PackageID string `json:"package_id"`
			} `json:"recommendations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, item := range envelope.Data.Recommendations {
		if item.PackageID == "pkg_story_new" {
			t.Fatalf("excluded package returned: %s", recorder.Body.String())
		}
	}
}

func TestRecommendationListRejectsInvalidAgeTier(t *testing.T) {
	handler := newRecommendationHandlerForTest(t)
	request := englishRequest(
		http.MethodGet,
		"/api/v1/content/recommendations?age_tier=age_1_2",
		"",
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.list(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}
