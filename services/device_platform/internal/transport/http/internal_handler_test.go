package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
)

// stubInternalPolicyService returns one fixed effective policy so the internal
// envelope can be verified without a database.
type stubInternalPolicyService struct {
	effective *policydomain.EffectivePolicy
	err       error
}

func (service stubInternalPolicyService) GetEffective(
	_ context.Context,
	_ string,
) (*policydomain.EffectivePolicy, error) {
	if service.err != nil {
		return nil, service.err
	}
	return service.effective, nil
}

// TestInternalEffectivePolicyReturnsAggregatedPolicy proves the internal
// endpoint returns the most-restrictive effective snapshot, not a per-child
// list that internal callers could misread.
func TestInternalEffectivePolicyReturnsAggregatedPolicy(t *testing.T) {
	updatedAt := time.Date(2026, 10, 8, 6, 30, 0, 0, time.UTC)
	handler := internalHandler{
		policyService: stubInternalPolicyService{
			effective: &policydomain.EffectivePolicy{
				PolicyVersion:     7,
				DailyLimitMinutes: 45,
				AllowedCategories: []string{"story", "bedtime"},
				DisabledPeriods: []policydomain.DisabledPeriod{
					{StartTime: "21:00", EndTime: "06:30"},
				},
				MaxVolumePercent: 60,
				SourceChildCount: 2,
				UpdatedAt:        updatedAt,
			},
		},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/internal/v1/parent-policies?family_id=family_001",
		nil,
	)
	recorder := httptest.NewRecorder()
	handler.effectivePolicies(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Policy struct {
				PolicyVersion     int      `json:"policy_version"`
				DailyLimitMinutes int      `json:"daily_limit_minutes"`
				AllowedCategories []string `json:"allowed_categories"`
				AggregationMode   string   `json:"aggregation_mode"`
				SourceChildCount  int      `json:"source_child_count"`
			} `json:"policy"`
			Policies []any `json:"policies"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.Policies != nil {
		t.Fatalf("internal endpoint still returns a policy list: %s", recorder.Body.String())
	}
	policy := payload.Data.Policy
	if policy.PolicyVersion != 7 ||
		policy.DailyLimitMinutes != 45 ||
		policy.AggregationMode != "most_restrictive" ||
		policy.SourceChildCount != 2 ||
		len(policy.AllowedCategories) != 2 {
		t.Fatalf("unexpected effective policy: %s", recorder.Body.String())
	}
}

// TestInternalEffectivePolicyRejectsMissingFamily keeps the query contract
// explicit so internal callers fail fast on an empty family id.
func TestInternalEffectivePolicyRejectsMissingFamily(t *testing.T) {
	handler := internalHandler{
		policyService: stubInternalPolicyService{
			effective: &policydomain.EffectivePolicy{DailyLimitMinutes: 60},
		},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/internal/v1/parent-policies",
		nil,
	)
	recorder := httptest.NewRecorder()
	handler.effectivePolicies(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}
