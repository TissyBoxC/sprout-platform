package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	learningdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/learning_visualization/domain"
)

// learningVisualizationTestService is an in-memory double so the handler tests
// exercise the real validation and lifecycle without a database.
type learningVisualizationTestService struct {
	policy  learningdomain.Policy
	devices map[string]learningdomain.Status
}

func newLearningVisualizationTestService() *learningVisualizationTestService {
	return &learningVisualizationTestService{
		policy:  learningdomain.Default(),
		devices: map[string]learningdomain.Status{},
	}
}

func (s *learningVisualizationTestService) Catalog() ([]string, []string, []string) {
	return learningdomain.FeedbackStyles,
		learningdomain.AnimationLevels,
		learningdomain.VisualStates
}

func (s *learningVisualizationTestService) Current(
	_ context.Context,
	_ string,
) (learningdomain.Policy, error) {
	return s.policy, nil
}

func (s *learningVisualizationTestService) Publish(
	_ context.Context,
	_ string,
	input learningdomain.Input,
) (learningdomain.Policy, error) {
	normalized, err := learningdomain.ValidateInput(input)
	if err != nil {
		return learningdomain.Policy{}, err
	}
	s.policy = learningdomain.Policy{
		SchemaVersion:        learningdomain.SchemaVersion,
		Enabled:              normalized.Enabled,
		FeedbackStyle:        normalized.FeedbackStyle,
		AnimationLevel:       normalized.AnimationLevel,
		TouchFeedbackEnabled: normalized.TouchFeedbackEnabled,
		RewardEffectsEnabled: normalized.RewardEffectsEnabled,
		LessonLinkageEnabled: normalized.LessonLinkageEnabled,
		MaxSessionMinutes:    normalized.MaxSessionMinutes,
		Revision:             s.policy.Revision + 1,
	}
	return s.policy, nil
}

func (s *learningVisualizationTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (learningdomain.Capability, error) {
	status, ok := s.devices[deviceID]
	if !ok {
		return learningdomain.Capability{
			Supported:      false,
			VisualState:    learningdomain.VisualIdle,
			FeedbackStyle:  learningdomain.StyleNeutral,
			AnimationLevel: learningdomain.AnimationOff,
			SchemaVersion:  learningdomain.SchemaVersion,
			PolicyRevision: s.policy.Revision,
		}, nil
	}
	return learningdomain.Capability{
		Supported:       status.VisualizationSupported,
		TouchSupported:  status.TouchSupported,
		LinkedLessonID:  status.LinkedLessonID,
		VisualState:     status.VisualState,
		FeedbackStyle:   status.FeedbackStyle,
		AnimationLevel:  status.AnimationLevel,
		SchemaVersion:   learningdomain.SchemaVersion,
		PolicyRevision:  s.policy.Revision,
		AppliedRevision: status.PolicyRevision,
		UpToDate:        status.PolicyRevision >= s.policy.Revision,
	}, nil
}

func (s *learningVisualizationTestService) ReportStatus(
	_ context.Context,
	familyID string,
	deviceID string,
	input learningdomain.StatusInput,
) (learningdomain.Status, error) {
	normalized, err := learningdomain.ValidateStatus(input)
	if err != nil {
		return learningdomain.Status{}, err
	}
	status := learningdomain.Status{
		DeviceID:               deviceID,
		FamilyID:               familyID,
		VisualizationSupported: normalized.VisualizationSupported,
		TouchSupported:         normalized.TouchSupported,
		LinkedLessonID:         normalized.LinkedLessonID,
		VisualState:            normalized.VisualState,
		FeedbackStyle:          normalized.FeedbackStyle,
		AnimationLevel:         normalized.AnimationLevel,
		FeedbackEvents:         normalized.FeedbackEvents,
		SessionMinutes:         normalized.SessionMinutes,
		PolicyRevision:         normalized.PolicyRevision,
	}
	s.devices[deviceID] = status
	return status, nil
}

func (s *learningVisualizationTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]learningdomain.DeviceStatus, error) {
	return []learningdomain.DeviceStatus{}, nil
}

func (s *learningVisualizationTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (learningdomain.AdminStats, error) {
	return learningdomain.AdminStats{WindowDays: windowDays, DeviceCount: 5}, nil
}

func TestLearningVisualizationPolicyRequiresAuthentication(t *testing.T) {
	handler := learningVisualizationHandler{
		service: newLearningVisualizationTestService(),
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/learning-visualization/policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestLearningVisualizationPolicyReturnsConservativeDefault(t *testing.T) {
	handler := learningVisualizationHandler{
		service: newLearningVisualizationTestService(),
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/learning-visualization/policy",
		"",
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Policy struct {
				Enabled        bool   `json:"enabled"`
				AnimationLevel string `json:"animation_level"`
			} `json:"policy"`
			FeedbackStyles  []string `json:"feedback_styles"`
			AnimationLevels []string `json:"animation_levels"`
			VisualStates    []string `json:"visual_states"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Policy.Enabled ||
		envelope.Data.Policy.AnimationLevel != learningdomain.AnimationOff {
		t.Fatalf("default policy must keep visualization off with animation off")
	}
	if len(envelope.Data.FeedbackStyles) != len(learningdomain.FeedbackStyles) ||
		len(envelope.Data.AnimationLevels) != len(learningdomain.AnimationLevels) ||
		len(envelope.Data.VisualStates) != len(learningdomain.VisualStates) {
		t.Fatalf("unexpected catalog: %+v", envelope.Data)
	}
}

func TestLearningVisualizationPublishRejectsUnknownStyle(t *testing.T) {
	handler := learningVisualizationHandler{
		service: newLearningVisualizationTestService(),
	}
	body := `{"enabled":true,"feedback_style":"flashy","animation_level":"full","max_session_minutes":20}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/learning-visualization/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestLearningVisualizationPublishAcceptsBoundedPolicy(t *testing.T) {
	service := newLearningVisualizationTestService()
	handler := learningVisualizationHandler{service: service}
	body := `{"enabled":true,"feedback_style":"celebration","animation_level":"full","touch_feedback_enabled":true,"reward_effects_enabled":false,"lesson_linkage_enabled":true,"max_session_minutes":30}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/learning-visualization/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !service.policy.Enabled || service.policy.RewardEffectsEnabled {
		t.Fatalf("published policy lost field values: %+v", service.policy)
	}
	if service.policy.FeedbackStyle != learningdomain.StyleCelebration {
		t.Fatalf("style = %q", service.policy.FeedbackStyle)
	}
	if service.policy.Revision != 1 {
		t.Fatalf("revision = %d, want 1", service.policy.Revision)
	}
}

func TestLearningVisualizationCapabilityUnknownDevice(t *testing.T) {
	handler := learningVisualizationHandler{
		service: newLearningVisualizationTestService(),
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-missing/learning-visualization/capability",
		"",
		"family-1",
	)
	request.SetPathValue("device_id", "device-missing")
	recorder := httptest.NewRecorder()
	handler.capability(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Supported      bool   `json:"supported"`
			AnimationLevel string `json:"animation_level"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Supported ||
		envelope.Data.AnimationLevel != learningdomain.AnimationOff {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

func TestLearningVisualizationReportStatusRefusedWithoutSession(t *testing.T) {
	handler := deviceLearningVisualizationHandler{
		handler: &learningVisualizationHandler{
			service: newLearningVisualizationTestService(),
		},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodPost,
		"/api/v1/devices/device-1/learning-visualization/status",
		`{"visualization_supported":true,"touch_supported":true,"linked_lesson_id":"colors_basic","visual_state":"lesson","feedback_style":"encouraging","animation_level":"subtle","feedback_events":4,"session_minutes":6}`,
		"",
	)
	recorder := httptest.NewRecorder()
	handler.reportStatus(recorder, request)
	// No binding service means the session is refused before validation.
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestLearningVisualizationAdminStats(t *testing.T) {
	handler := learningVisualizationHandler{
		service: newLearningVisualizationTestService(),
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/admin/learning-visualization/stats?window_days=30",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.adminStats(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			WindowDays  int `json:"window_days"`
			DeviceCount int `json:"device_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.WindowDays != 30 || envelope.Data.DeviceCount != 5 {
		t.Fatalf("unexpected stats: %+v", envelope.Data)
	}
}
