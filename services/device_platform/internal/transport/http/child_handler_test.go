package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
)

// stubPolicyService records the version the transport forwards so the
// optimistic-concurrency contract can be asserted without a database.
type stubPolicyService struct {
	updatedPolicy  *policydomain.Policy
	updateErr      error
	updateCalls    int
	lastExpected   int
	lastChildID    string
	lastDailyLimit int
	lastCategories []string
}

func (stub *stubPolicyService) Get(
	_ context.Context,
	_ string,
	_ string,
) (*policydomain.Policy, error) {
	return nil, policydomain.ErrPolicyNotFound
}

func (stub *stubPolicyService) List(
	_ context.Context,
	_ string,
) ([]policydomain.Policy, error) {
	return nil, nil
}

func (stub *stubPolicyService) Update(
	_ context.Context,
	_ string,
	_ string,
	_ policydomain.PolicyInput,
) (*policydomain.Policy, error) {
	return nil, stub.updateErr
}

func (stub *stubPolicyService) UpdateWithVersion(
	_ context.Context,
	_ string,
	childID string,
	input policydomain.PolicyInput,
	expectedVersion int,
) (*policydomain.Policy, error) {
	stub.updateCalls++
	stub.lastChildID = childID
	stub.lastExpected = expectedVersion
	stub.lastDailyLimit = input.DailyLimitMinutes
	stub.lastCategories = append([]string(nil), input.AllowedCategories...)
	if stub.updateErr != nil {
		return nil, stub.updateErr
	}
	return stub.updatedPolicy, nil
}

// TestUpdatePolicyReturnsConflictForStaleVersion proves the transport maps a
// stale optimistic-concurrency write to HTTP 409 version_conflict.
func TestUpdatePolicyReturnsConflictForStaleVersion(t *testing.T) {
	service := &stubPolicyService{updateErr: policydomain.ErrPolicyVersionConflict}
	handler := childHandler{policyService: service}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/children/child_001/policy",
		strings.NewReader(`{
			"policy_version": 1,
			"daily_limit_minutes": 30,
			"allowed_categories": ["story"],
			"disabled_periods": [],
			"max_volume_percent": 50
		}`),
	)
	request.SetPathValue("child_id", "child_001")
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"family_001",
	))
	recorder := httptest.NewRecorder()
	handler.updatePolicy(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "version_conflict" {
		t.Fatalf("error code = %q, want version_conflict", payload.Error.Code)
	}
	if service.lastExpected != 1 || service.lastChildID != "child_001" {
		t.Fatalf(
			"forwarded update unexpected: expected=%d child=%q",
			service.lastExpected,
			service.lastChildID,
		)
	}
}

// TestUpdatePolicySucceedsWithCurrentVersion proves a matching version still
// persists and the response carries the bumped policy.
func TestUpdatePolicySucceedsWithCurrentVersion(t *testing.T) {
	service := &stubPolicyService{
		updatedPolicy: &policydomain.Policy{
			ID:                "policy_001",
			FamilyID:          "family_001",
			ChildID:           "child_001",
			PolicyVersion:     2,
			DailyLimitMinutes: 45,
			AllowedCategories: []string{"story"},
			MaxVolumePercent:  50,
		},
	}
	handler := childHandler{policyService: service}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/children/child_001/policy",
		strings.NewReader(`{
			"policy_version": 1,
			"daily_limit_minutes": 45,
			"allowed_categories": ["story"],
			"disabled_periods": [],
			"max_volume_percent": 50
		}`),
	)
	request.SetPathValue("child_id", "child_001")
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"family_001",
	))
	recorder := httptest.NewRecorder()
	handler.updatePolicy(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Policy struct {
				PolicyVersion     int `json:"policy_version"`
				DailyLimitMinutes int `json:"daily_limit_minutes"`
			} `json:"policy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.Policy.PolicyVersion != 2 ||
		payload.Data.Policy.DailyLimitMinutes != 45 {
		t.Fatalf("unexpected policy response: %s", recorder.Body.String())
	}
	if service.updateCalls != 1 || service.lastExpected != 1 {
		t.Fatalf(
			"update not forwarded once with current version: calls=%d expected=%d",
			service.updateCalls,
			service.lastExpected,
		)
	}
}
