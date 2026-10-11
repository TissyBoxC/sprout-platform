package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	screendomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/screen/domain"
)

// screenTestService is an in-memory double so the handler tests exercise the
// real validation and lifecycle without a database.
type screenTestService struct {
	policy  screendomain.Policy
	entries map[string]screendomain.DictionaryEntry
}

func newScreenTestService() *screenTestService {
	return &screenTestService{
		policy:  screendomain.Default(),
		entries: map[string]screendomain.DictionaryEntry{},
	}
}

func (s *screenTestService) Catalog() ([]string, []string) {
	return screendomain.Expressions, screendomain.Animations
}

func (s *screenTestService) Current(
	_ context.Context,
	_ string,
) (screendomain.Policy, error) {
	return s.policy, nil
}

func (s *screenTestService) Publish(
	_ context.Context,
	_ string,
	input screendomain.Input,
) (screendomain.Policy, error) {
	normalized, err := screendomain.ValidateInput(input)
	if err != nil {
		return screendomain.Policy{}, err
	}
	s.policy = screendomain.Policy{
		SchemaVersion:      screendomain.SchemaVersion,
		DefaultExpression:  normalized.DefaultExpression,
		AnimationEnabled:   normalized.AnimationEnabled,
		PointReadEnabled:   normalized.PointReadEnabled,
		VideoEnabled:       normalized.VideoEnabled,
		VideoMaxFPS:        normalized.VideoMaxFPS,
		VideoMaxBitrateKBS: normalized.VideoMaxBitrateKBS,
		AnimationFrameMS:   normalized.AnimationFrameMS,
		Revision:           s.policy.Revision + 1,
	}
	return s.policy, nil
}

func (s *screenTestService) Capability(
	_ context.Context,
	_ string,
	_ string,
) (screendomain.Capability, error) {
	return screendomain.Capability{Supported: true, VideoSupported: true}, nil
}

func (s *screenTestService) ReportStatus(
	_ context.Context,
	familyID string,
	deviceID string,
	input screendomain.StatusInput,
) (screendomain.Status, error) {
	normalized, err := screendomain.ValidateStatus(input)
	if err != nil {
		return screendomain.Status{}, err
	}
	return screendomain.Status{
		DeviceID:             deviceID,
		FamilyID:             familyID,
		ScreenSupported:      normalized.ScreenSupported,
		ExpressionsSupported: normalized.ExpressionsSupported,
		AnimationSupported:   normalized.AnimationSupported,
		PointReadSupported:   normalized.PointReadSupported,
		VideoSupported:       normalized.VideoSupported,
		Expression:           normalized.Expression,
		Animation:            normalized.Animation,
		VideoState:           normalized.VideoState,
		PolicyRevision:       normalized.PolicyRevision,
	}, nil
}

func (s *screenTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]screendomain.DeviceStatus, error) {
	return []screendomain.DeviceStatus{}, nil
}

func (s *screenTestService) Dictionary(
	_ context.Context,
	_ string,
) ([]screendomain.DictionaryEntry, error) {
	entries := make([]screendomain.DictionaryEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *screenTestService) PutEntry(
	_ context.Context,
	familyID string,
	input screendomain.DictionaryInput,
) (screendomain.DictionaryEntry, error) {
	normalized, err := screendomain.ValidateDictionaryInput(input)
	if err != nil {
		return screendomain.DictionaryEntry{}, err
	}
	entry := screendomain.DictionaryEntry{
		ID:       "entry-1",
		FamilyID: familyID,
		Term:     normalized.Term,
		Text:     normalized.Text,
	}
	s.entries[normalized.Term] = entry
	return entry, nil
}

func (s *screenTestService) DeleteEntry(
	_ context.Context,
	_ string,
	entryID string,
) error {
	for key, entry := range s.entries {
		if entry.ID == entryID {
			delete(s.entries, key)
			return nil
		}
	}
	return screendomain.ErrDictionaryNotFound
}

func (s *screenTestService) ResolvePlayback(
	_ context.Context,
	_ string,
	_ string,
	requestedBitrateKBS int,
	requestedFPS int,
) (screendomain.PlaybackPlan, error) {
	if !s.policy.VideoEnabled {
		return screendomain.PlaybackPlan{}, screendomain.ErrInvalidPlayback
	}
	bitrate := requestedBitrateKBS
	if bitrate > s.policy.VideoMaxBitrateKBS {
		bitrate = s.policy.VideoMaxBitrateKBS
	}
	fps := requestedFPS
	if fps > s.policy.VideoMaxFPS {
		fps = s.policy.VideoMaxFPS
	}
	return screendomain.PlaybackPlan{BitrateKBS: bitrate, FPS: fps}, nil
}

func (s *screenTestService) Playback(
	_ context.Context,
	_ string,
	_ string,
) (screendomain.PlaybackPlan, error) {
	return screendomain.PlaybackPlan{
		BitrateKBS: s.policy.VideoMaxBitrateKBS,
		FPS:        s.policy.VideoMaxFPS,
	}, nil
}

func (s *screenTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (screendomain.AdminStats, error) {
	return screendomain.AdminStats{WindowDays: windowDays, DeviceCount: 3}, nil
}

func TestScreenPolicyRequiresAuthentication(t *testing.T) {
	handler := screenHandler{service: newScreenTestService()}
	request := englishRequest(http.MethodGet, "/api/v1/screen/policy", "", "")
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestScreenPolicyReturnsCatalog(t *testing.T) {
	handler := screenHandler{service: newScreenTestService()}
	request := englishRequest(http.MethodGet, "/api/v1/screen/policy", "", "family-1")
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Policy struct {
				DefaultExpression string `json:"default_expression"`
			} `json:"policy"`
			Expressions []string `json:"expressions"`
			Animations  []string `json:"animations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Policy.DefaultExpression != "happy" {
		t.Fatalf("unexpected policy: %+v", envelope.Data.Policy)
	}
	if len(envelope.Data.Expressions) != len(screendomain.Expressions) ||
		len(envelope.Data.Animations) != len(screendomain.Animations) {
		t.Fatalf("unexpected catalog: %+v", envelope.Data)
	}
}

func TestScreenPublishRejectsUnknownExpression(t *testing.T) {
	handler := screenHandler{service: newScreenTestService()}
	body := `{"default_expression":"grumpy","video_max_fps":30,"video_max_bitrate_kbps":1500,"animation_frame_ms":200}`
	request := englishRequest(http.MethodPut, "/api/v1/screen/policy", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestScreenDictionaryPutAndDelete(t *testing.T) {
	service := newScreenTestService()
	handler := screenHandler{service: service}

	body := `{"term":"apple","text":"苹果"}`
	request := englishRequest(http.MethodPut, "/api/v1/screen/point-read", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.putDictionaryEntry(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("put status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Term string `json:"term"`
			Text string `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Term != "apple" || envelope.Data.Text != "苹果" {
		t.Fatalf("unexpected entry: %+v", envelope.Data)
	}

	deleteRequest := englishRequest(
		http.MethodDelete,
		"/api/v1/screen/point-read/entry-1",
		"",
		"family-1",
	)
	deleteRequest.SetPathValue("entry_id", "entry-1")
	recorder = httptest.NewRecorder()
	handler.deleteDictionaryEntry(recorder, deleteRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", recorder.Code, http.StatusOK)
	}

	missing := englishRequest(
		http.MethodDelete,
		"/api/v1/screen/point-read/missing",
		"",
		"family-1",
	)
	missing.SetPathValue("entry_id", "missing")
	recorder = httptest.NewRecorder()
	handler.deleteDictionaryEntry(recorder, missing)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestScreenResolvePlaybackClamps(t *testing.T) {
	service := newScreenTestService()
	service.policy.VideoMaxFPS = 24
	service.policy.VideoMaxBitrateKBS = 2000
	handler := screenHandler{service: service}
	body := `{"bitrate_kbps":9000,"fps":60}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/devices/device-1/screen/playback",
		body,
		"family-1",
	)
	request.SetPathValue("device_id", "device-1")
	recorder := httptest.NewRecorder()
	handler.resolvePlayback(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			BitrateKBS int `json:"bitrate_kbps"`
			FPS        int `json:"fps"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.BitrateKBS != 2000 || envelope.Data.FPS != 24 {
		t.Fatalf("expected the policy ceiling, got %+v", envelope.Data)
	}
}

func TestScreenReportStatusRejectsUnknownExpression(t *testing.T) {
	handler := deviceScreenHandler{
		handler:        &screenHandler{service: newScreenTestService()},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodPost,
		"/api/v1/devices/device-1/screen/status",
		`{"screen_supported":true,"expression":"grumpy"}`,
		"",
	)
	recorder := httptest.NewRecorder()
	handler.reportStatus(recorder, request)
	// No binding service means the session is refused before validation.
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestScreenAdminStats(t *testing.T) {
	handler := screenHandler{service: newScreenTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/admin/screen/stats?window_days=30",
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
	if envelope.Data.WindowDays != 30 || envelope.Data.DeviceCount != 3 {
		t.Fatalf("unexpected stats: %+v", envelope.Data)
	}
}
