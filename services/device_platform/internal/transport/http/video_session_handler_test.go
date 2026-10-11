package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	videosessiondomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_session/domain"
)

// videoSessionTestService is an in-memory double so the handler tests exercise
// the real validation and lifecycle without a database.
type videoSessionTestService struct {
	policy  videosessiondomain.Policy
	devices map[string]videosessiondomain.Status
}

func newVideoSessionTestService() *videoSessionTestService {
	return &videoSessionTestService{
		policy:  videosessiondomain.Default(),
		devices: map[string]videosessiondomain.Status{},
	}
}

func (s *videoSessionTestService) Catalog() ([]string, []string) {
	return videosessiondomain.Qualities, videosessiondomain.States
}

func (s *videoSessionTestService) Current(
	_ context.Context,
	_ string,
) (videosessiondomain.Policy, error) {
	return s.policy, nil
}

func (s *videoSessionTestService) Publish(
	_ context.Context,
	_ string,
	input videosessiondomain.Input,
) (videosessiondomain.Policy, error) {
	normalized, err := videosessiondomain.ValidateInput(input)
	if err != nil {
		return videosessiondomain.Policy{}, err
	}
	s.policy = videosessiondomain.Policy{
		SchemaVersion:       videosessiondomain.SchemaVersion,
		Enabled:             normalized.Enabled,
		CameraAllowed:       normalized.CameraAllowed,
		MicrophoneAllowed:   normalized.MicrophoneAllowed,
		RequireChildConsent: normalized.RequireChildConsent,
		QualityFloor:        normalized.QualityFloor,
		RingTimeoutSeconds:  normalized.RingTimeoutSeconds,
		MaxSessionSeconds:   normalized.MaxSessionSeconds,
		Revision:            s.policy.Revision + 1,
	}
	return s.policy, nil
}

func (s *videoSessionTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (videosessiondomain.Capability, error) {
	status, ok := s.devices[deviceID]
	if !ok {
		return videosessiondomain.Capability{
			Supported:      false,
			CallState:      videosessiondomain.StateIdle,
			Quality:        videosessiondomain.QualityUnknown,
			SchemaVersion:  videosessiondomain.SchemaVersion,
			PolicyRevision: s.policy.Revision,
		}, nil
	}
	return videosessiondomain.Capability{
		Supported:         status.SessionSupported,
		CameraAllowed:     status.CameraAllowed,
		MicrophoneAllowed: status.MicrophoneAllowed,
		CallState:         status.CallState,
		Quality:           status.Quality,
		SchemaVersion:     videosessiondomain.SchemaVersion,
		PolicyRevision:    s.policy.Revision,
		AppliedRevision:   status.PolicyRevision,
		UpToDate:          status.PolicyRevision >= s.policy.Revision,
	}, nil
}

func (s *videoSessionTestService) ReportStatus(
	_ context.Context,
	familyID string,
	deviceID string,
	input videosessiondomain.StatusInput,
) (videosessiondomain.Status, error) {
	normalized, err := videosessiondomain.ValidateStatus(input)
	if err != nil {
		return videosessiondomain.Status{}, err
	}
	status := videosessiondomain.Status{
		DeviceID:          deviceID,
		FamilyID:          familyID,
		SessionSupported:  normalized.SessionSupported,
		CameraAllowed:     normalized.CameraAllowed,
		MicrophoneAllowed: normalized.MicrophoneAllowed,
		CallState:         normalized.CallState,
		Quality:           normalized.Quality,
		LastEndReason:     normalized.LastEndReason,
		PolicyRevision:    normalized.PolicyRevision,
	}
	s.devices[deviceID] = status
	return status, nil
}

func (s *videoSessionTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]videosessiondomain.DeviceStatus, error) {
	return []videosessiondomain.DeviceStatus{}, nil
}

func (s *videoSessionTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (videosessiondomain.AdminStats, error) {
	return videosessiondomain.AdminStats{WindowDays: windowDays, DeviceCount: 4}, nil
}

func TestVideoSessionPolicyRequiresAuthentication(t *testing.T) {
	handler := videoSessionHandler{service: newVideoSessionTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/video-session/policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestVideoSessionPolicyReturnsConservativeDefault(t *testing.T) {
	handler := videoSessionHandler{service: newVideoSessionTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/video-session/policy",
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
				CameraAllowed       bool `json:"camera_allowed"`
				MicrophoneAllowed   bool `json:"microphone_allowed"`
				RequireChildConsent bool `json:"require_child_consent"`
				RingTimeoutSeconds  int  `json:"ring_timeout_seconds"`
			} `json:"policy"`
			Qualities []string `json:"qualities"`
			States    []string `json:"states"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Policy.CameraAllowed ||
		envelope.Data.Policy.MicrophoneAllowed {
		t.Fatalf("default policy must deny capture")
	}
	if !envelope.Data.Policy.RequireChildConsent {
		t.Fatalf("default policy must require child consent")
	}
	if len(envelope.Data.Qualities) != len(videosessiondomain.Qualities) ||
		len(envelope.Data.States) != len(videosessiondomain.States) {
		t.Fatalf("unexpected catalog: %+v", envelope.Data)
	}
}

func TestVideoSessionPublishRejectsOutOfRangeRing(t *testing.T) {
	handler := videoSessionHandler{service: newVideoSessionTestService()}
	body := `{"enabled":true,"quality_floor":"good","ring_timeout_seconds":1,"max_session_seconds":900}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/video-session/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestVideoSessionPublishAcceptsBoundedPolicy(t *testing.T) {
	service := newVideoSessionTestService()
	handler := videoSessionHandler{service: service}
	body := `{"enabled":true,"camera_allowed":true,"microphone_allowed":true,"require_child_consent":false,"quality_floor":"fair","ring_timeout_seconds":45,"max_session_seconds":900}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/video-session/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !service.policy.CameraAllowed || !service.policy.MicrophoneAllowed {
		t.Fatalf("published policy lost media permissions: %+v", service.policy)
	}
	if service.policy.Revision != 1 {
		t.Fatalf("revision = %d, want 1", service.policy.Revision)
	}
}

func TestVideoSessionCapabilityUnknownDevice(t *testing.T) {
	handler := videoSessionHandler{service: newVideoSessionTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-missing/video-session/capability",
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
			Supported bool   `json:"supported"`
			CallState string `json:"call_state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Supported || envelope.Data.CallState != "idle" {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

func TestVideoSessionReportStatusRefusedWithoutSession(t *testing.T) {
	handler := deviceVideoSessionHandler{
		handler:        &videoSessionHandler{service: newVideoSessionTestService()},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodPost,
		"/api/v1/devices/device-1/video-session/status",
		`{"session_supported":true,"call_state":"active","quality":"good","last_end_reason":"none"}`,
		"",
	)
	recorder := httptest.NewRecorder()
	handler.reportStatus(recorder, request)
	// No binding service means the session is refused before validation.
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestVideoSessionAdminStats(t *testing.T) {
	handler := videoSessionHandler{service: newVideoSessionTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/admin/video-session/stats?window_days=30",
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
	if envelope.Data.WindowDays != 30 || envelope.Data.DeviceCount != 4 {
		t.Fatalf("unexpected stats: %+v", envelope.Data)
	}
}
