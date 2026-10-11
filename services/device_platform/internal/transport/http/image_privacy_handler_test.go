package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	imageprivacydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/image_privacy/domain"
)

// imagePrivacyTestService is an in-memory double so the handler tests exercise
// the real validation and lifecycle without a database.
type imagePrivacyTestService struct {
	policy  imageprivacydomain.Policy
	devices map[string]imageprivacydomain.Status
}

func newImagePrivacyTestService() *imagePrivacyTestService {
	return &imagePrivacyTestService{
		policy:  imageprivacydomain.Default(),
		devices: map[string]imageprivacydomain.Status{},
	}
}

func (s *imagePrivacyTestService) Catalog() ([]string, []string, []string, []string) {
	return imageprivacydomain.ProcessingModes,
		imageprivacydomain.UploadScopes,
		imageprivacydomain.RedactionLevels,
		imageprivacydomain.BlockReasons
}

func (s *imagePrivacyTestService) Current(
	_ context.Context,
	_ string,
) (imageprivacydomain.Policy, error) {
	return s.policy, nil
}

func (s *imagePrivacyTestService) Publish(
	_ context.Context,
	_ string,
	input imageprivacydomain.Input,
) (imageprivacydomain.Policy, error) {
	normalized, err := imageprivacydomain.ValidateInput(input)
	if err != nil {
		return imageprivacydomain.Policy{}, err
	}
	s.policy = imageprivacydomain.Policy{
		SchemaVersion:       imageprivacydomain.SchemaVersion,
		Enabled:             normalized.Enabled,
		UploadAllowed:       normalized.UploadAllowed,
		LocalProcessingOnly: normalized.LocalProcessingOnly,
		ProcessingMode:      normalized.ProcessingMode,
		UploadScope:         normalized.UploadScope,
		RetentionDays:       normalized.RetentionDays,
		RedactionLevel:      normalized.RedactionLevel,
		RequireChildConsent: normalized.RequireChildConsent,
		Revision:            s.policy.Revision + 1,
	}
	return s.policy, nil
}

func (s *imagePrivacyTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (imageprivacydomain.Capability, error) {
	status, ok := s.devices[deviceID]
	if !ok {
		return imageprivacydomain.Capability{
			Supported:      false,
			ProcessingMode: imageprivacydomain.ModeOff,
			UploadScope:    imageprivacydomain.ScopeNone,
			RedactionLevel: imageprivacydomain.RedactionStrict,
			SchemaVersion:  imageprivacydomain.SchemaVersion,
			PolicyRevision: s.policy.Revision,
		}, nil
	}
	return imageprivacydomain.Capability{
		Supported:       status.CameraSupported,
		LocalProcessing: status.LocalProcessing,
		UploadAllowed:   status.UploadAllowed,
		ProcessingMode:  status.ProcessingMode,
		UploadScope:     status.UploadScope,
		RedactionLevel:  status.RedactionLevel,
		SchemaVersion:   imageprivacydomain.SchemaVersion,
		PolicyRevision:  s.policy.Revision,
		AppliedRevision: status.PolicyRevision,
		UpToDate:        status.PolicyRevision >= s.policy.Revision,
	}, nil
}

func (s *imagePrivacyTestService) ReportStatus(
	_ context.Context,
	familyID string,
	deviceID string,
	input imageprivacydomain.StatusInput,
) (imageprivacydomain.Status, error) {
	normalized, err := imageprivacydomain.ValidateStatus(input)
	if err != nil {
		return imageprivacydomain.Status{}, err
	}
	status := imageprivacydomain.Status{
		DeviceID:        deviceID,
		FamilyID:        familyID,
		CameraSupported: normalized.CameraSupported,
		LocalProcessing: normalized.LocalProcessing,
		UploadAllowed:   normalized.UploadAllowed,
		ProcessingMode:  normalized.ProcessingMode,
		UploadScope:     normalized.UploadScope,
		RedactionLevel:  normalized.RedactionLevel,
		ProcessedCount:  normalized.ProcessedCount,
		UploadedCount:   normalized.UploadedCount,
		BlockedCount:    normalized.BlockedCount,
		LastBlockReason: normalized.LastBlockReason,
		PolicyRevision:  normalized.PolicyRevision,
	}
	s.devices[deviceID] = status
	return status, nil
}

func (s *imagePrivacyTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]imageprivacydomain.DeviceStatus, error) {
	return []imageprivacydomain.DeviceStatus{}, nil
}

func (s *imagePrivacyTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (imageprivacydomain.AdminStats, error) {
	return imageprivacydomain.AdminStats{
		WindowDays:  windowDays,
		DeviceCount: 5,
	}, nil
}

func TestImagePrivacyPolicyRequiresAuthentication(t *testing.T) {
	handler := imagePrivacyHandler{service: newImagePrivacyTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/image-privacy/policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestImagePrivacyPolicyReturnsConservativeDefault(t *testing.T) {
	handler := imagePrivacyHandler{service: newImagePrivacyTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/image-privacy/policy",
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
				Enabled             bool   `json:"enabled"`
				UploadAllowed       bool   `json:"upload_allowed"`
				LocalProcessingOnly bool   `json:"local_processing_only"`
				ProcessingMode      string `json:"processing_mode"`
				UploadScope         string `json:"upload_scope"`
			} `json:"policy"`
			ProcessingModes []string `json:"processing_modes"`
			UploadScopes    []string `json:"upload_scopes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Policy.Enabled ||
		envelope.Data.Policy.UploadAllowed ||
		!envelope.Data.Policy.LocalProcessingOnly ||
		envelope.Data.Policy.ProcessingMode != imageprivacydomain.ModeMetadata ||
		envelope.Data.Policy.UploadScope != imageprivacydomain.ScopeNone {
		t.Fatalf("default policy is not conservative: %+v", envelope.Data.Policy)
	}
	if len(envelope.Data.ProcessingModes) != len(imageprivacydomain.ProcessingModes) ||
		len(envelope.Data.UploadScopes) != len(imageprivacydomain.UploadScopes) {
		t.Fatalf("unexpected catalog: %+v", envelope.Data)
	}
}

func TestImagePrivacyPublishRejectsUnknownProcessingMode(t *testing.T) {
	handler := imagePrivacyHandler{service: newImagePrivacyTestService()}
	body := `{"enabled":true,"upload_allowed":true,"local_processing_only":false,"processing_mode":"cloud","upload_scope":"thumbnails","retention_days":7,"redaction_level":"standard","require_child_consent":false}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/image-privacy/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestImagePrivacyPublishAcceptsBoundedPolicy(t *testing.T) {
	service := newImagePrivacyTestService()
	handler := imagePrivacyHandler{service: service}
	body := `{"enabled":true,"upload_allowed":true,"local_processing_only":false,"processing_mode":"local_full","upload_scope":"thumbnails","retention_days":7,"redaction_level":"standard","require_child_consent":false}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/image-privacy/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !service.policy.Enabled || !service.policy.UploadAllowed {
		t.Fatalf("published policy lost field values: %+v", service.policy)
	}
	if service.policy.ProcessingMode != imageprivacydomain.ModeLocalFull {
		t.Fatalf("mode = %q", service.policy.ProcessingMode)
	}
	if service.policy.Revision != 1 {
		t.Fatalf("revision = %d, want 1", service.policy.Revision)
	}
}

func TestImagePrivacyCapabilityUnknownDevice(t *testing.T) {
	handler := imagePrivacyHandler{service: newImagePrivacyTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-missing/image-privacy/capability",
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
			ProcessingMode string `json:"processing_mode"`
			UploadScope    string `json:"upload_scope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Supported ||
		envelope.Data.ProcessingMode != imageprivacydomain.ModeOff ||
		envelope.Data.UploadScope != imageprivacydomain.ScopeNone {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

func TestImagePrivacyReportStatusRefusedWithoutSession(t *testing.T) {
	handler := deviceImagePrivacyHandler{
		handler:        &imagePrivacyHandler{service: newImagePrivacyTestService()},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodPost,
		"/api/v1/devices/device-1/image-privacy/status",
		`{"camera_supported":true,"local_processing":true,"upload_allowed":true,"processing_mode":"metadata","upload_scope":"thumbnails","redaction_level":"strict","processed_count":4,"uploaded_count":1,"blocked_count":0,"last_block_reason":"none","policy_revision":1}`,
		"",
	)
	recorder := httptest.NewRecorder()
	handler.reportStatus(recorder, request)
	// No binding service means the session is refused before validation.
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestImagePrivacyAdminStats(t *testing.T) {
	handler := imagePrivacyHandler{service: newImagePrivacyTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/admin/image-privacy/stats?window_days=30",
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
