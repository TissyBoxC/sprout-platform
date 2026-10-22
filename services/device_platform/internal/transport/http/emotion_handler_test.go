package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	emotiondomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/domain"
	emotionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/service"
)

// emotionMemoryStore is an in-memory double so the handler tests exercise the
// real service rules without a database.
type emotionMemoryStore struct {
	settings map[string]emotiondomain.Settings
	samples  []emotiondomain.Observation
	familyOf map[string]string
	childOf  map[string]string
	bindings map[string]bool
}

func newEmotionMemoryStore() *emotionMemoryStore {
	return &emotionMemoryStore{
		settings: map[string]emotiondomain.Settings{},
		familyOf: map[string]string{"device-1": "parent-1"},
		childOf:  map[string]string{"device-1": "child-1"},
		bindings: map[string]bool{"device-1|child-1": true},
	}
}

func (store *emotionMemoryStore) GetSettings(
	_ context.Context,
	familyID string,
) (emotiondomain.Settings, error) {
	settings, ok := store.settings[familyID]
	if !ok {
		return emotiondomain.Settings{}, emotionservice.StoreSentinelNotFound
	}
	return settings, nil
}

func (store *emotionMemoryStore) SaveSettings(
	_ context.Context,
	settings emotiondomain.Settings,
) error {
	store.settings[settings.FamilyID] = settings
	return nil
}

func (store *emotionMemoryStore) DeviceFamily(
	_ context.Context,
	deviceID string,
) (string, error) {
	return store.familyOf[deviceID], nil
}

func (store *emotionMemoryStore) DeviceChildBinding(
	_ context.Context,
	_, deviceID, childID string,
) (bool, error) {
	return store.bindings[deviceID+"|"+childID], nil
}

func (store *emotionMemoryStore) InsertObservation(
	_ context.Context,
	observation emotiondomain.Observation,
) error {
	if !store.bindings[observation.DeviceID+"|"+observation.ChildID] {
		return emotionservice.StoreSentinelNotBound
	}
	store.samples = append(store.samples, observation)
	return nil
}

func (store *emotionMemoryStore) CountLabels(
	_ context.Context,
	familyID, childID string,
	from, to time.Time,
) (map[emotiondomain.Label]emotiondomain.LabelCount, error) {
	counts := map[emotiondomain.Label]emotiondomain.LabelCount{}
	for _, sample := range store.samples {
		if sample.FamilyID != familyID {
			continue
		}
		if childID != "" && sample.ChildID != childID {
			continue
		}
		if sample.ObservedAt.Before(from) || !sample.ObservedAt.Before(to) {
			continue
		}
		entry := counts[sample.Label]
		entry.Label = sample.Label
		entry.Count++
		entry.ConfidenceSum += sample.Confidence
		counts[sample.Label] = entry
	}
	return counts, nil
}

func (store *emotionMemoryStore) DeleteBefore(
	_ context.Context,
	_ string,
	_ time.Time,
) (int, error) {
	return 0, nil
}

func (store *emotionMemoryStore) ListEnabledFamilies(
	_ context.Context,
	_ int,
) ([]emotiondomain.Settings, error) {
	return nil, nil
}

func (store *emotionMemoryStore) CountEnabledFamilies(_ context.Context) (int, error) {
	return 0, nil
}

func (store *emotionMemoryStore) CountObservationsSince(
	_ context.Context,
	_ time.Time,
) (int, error) {
	return 0, nil
}

type emotionMemorySwitch struct{ store *emotionMemoryStore }

func (switcher emotionMemorySwitch) SettingsEnabled(
	ctx context.Context,
	familyID string,
) (bool, error) {
	settings, err := switcher.store.GetSettings(ctx, familyID)
	if err != nil {
		return false, nil
	}
	return settings.Enabled, nil
}

type emotionMemoryDevices struct{ store *emotionMemoryStore }

func (devices emotionMemoryDevices) DeviceFamily(
	_ context.Context,
	deviceID string,
) (string, error) {
	return devices.store.familyOf[deviceID], nil
}

func (devices emotionMemoryDevices) DeviceChild(
	_ context.Context,
	deviceID string,
) (string, error) {
	return devices.store.childOf[deviceID], nil
}

func newEmotionHandlersForTest(t *testing.T) (
	guardianEmotionHandler,
	deviceEmotionHandler,
	*emotionMemoryStore,
) {
	t.Helper()
	store := newEmotionMemoryStore()
	service := emotionservice.NewService(emotionservice.Options{
		Store:    store,
		Switches: emotionMemorySwitch{store: store},
		Devices:  emotionMemoryDevices{store: store},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return guardianEmotionHandler{service: service},
		deviceEmotionHandler{service: service},
		store
}

func emotionRequest(method, target, body, familyAccountID string) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	request.SetPathValue("family_id", familyAccountID)
	return withOwnerFamilyContext(request, familyAccountID)
}

// decodeEmotionData reads the data field of the response envelope.
func decodeEmotionData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v: %s", err, recorder.Body.String())
	}
	return envelope.Data
}

// decodeEmotionError reads the stable error code of a failed response.
func decodeEmotionError(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error response: %v: %s", err, recorder.Body.String())
	}
	return envelope.Error.Code
}

func TestEmotionSettingsDefaultToDisabled(t *testing.T) {
	guardian, _, _ := newEmotionHandlersForTest(t)
	recorder := httptest.NewRecorder()
	guardian.settings(recorder, emotionRequest(
		http.MethodGet,
		"/api/v1/families/parent-1/emotion-settings",
		"", "parent-1",
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeEmotionData(t, recorder)
	if body["is_enabled"] != false {
		t.Fatalf("is_enabled = %v, want false", body["is_enabled"])
	}
}

func TestEmotionSettingsUpdateRequiresTheSwitch(t *testing.T) {
	guardian, _, _ := newEmotionHandlersForTest(t)
	recorder := httptest.NewRecorder()
	// A body without is_enabled must never be interpreted as "turn it on".
	guardian.updateSettings(recorder, emotionRequest(
		http.MethodPut,
		"/api/v1/families/parent-1/emotion-settings",
		`{"retention_days":14}`, "parent-1",
	))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if code := decodeEmotionError(t, recorder); code != "emotion_invalid_request" {
		t.Fatalf("code = %q, want emotion_invalid_request", code)
	}
}

func TestEmotionSettingsUpdateRejectsOutOfRangeRetention(t *testing.T) {
	guardian, _, _ := newEmotionHandlersForTest(t)
	for _, body := range []string{
		`{"is_enabled":true,"retention_days":0}`,
		`{"is_enabled":true,"retention_days":91}`,
		`{"is_enabled":true,"retention_days":-5}`,
	} {
		recorder := httptest.NewRecorder()
		guardian.updateSettings(recorder, emotionRequest(
			http.MethodPut,
			"/api/v1/families/parent-1/emotion-settings", body, "parent-1",
		))
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %s status = %d, want 422", body, recorder.Code)
		}
		if code := decodeEmotionError(t, recorder); code != "emotion_invalid_retention" {
			t.Fatalf("body %s code = %q, want emotion_invalid_retention", body, code)
		}
	}
}

func TestEmotionSettingsUpdateTurnsSensingOn(t *testing.T) {
	guardian, _, store := newEmotionHandlersForTest(t)
	recorder := httptest.NewRecorder()
	guardian.updateSettings(recorder, emotionRequest(
		http.MethodPut,
		"/api/v1/families/parent-1/emotion-settings",
		`{"is_enabled":true,"retention_days":14}`, "parent-1",
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if saved := store.settings["parent-1"]; !saved.Enabled || saved.RetentionDays != 14 {
		t.Fatalf("stored settings = %+v", saved)
	}
}

func TestEmotionRecordRefusedWhileDisabled(t *testing.T) {
	_, device, store := newEmotionHandlersForTest(t)
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/devices/device-1/emotion-observations", strings.NewReader(
			`{"child_id":"child-1","label":"calm","confidence":70,"observed_at":"`+
				time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)+`"}`))
	request.SetPathValue("device_id", "device-1")
	recorder := httptest.NewRecorder()
	device.record(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if code := decodeEmotionError(t, recorder); code != "emotion_disabled" {
		t.Fatalf("code = %q, want emotion_disabled", code)
	}
	if len(store.samples) != 0 {
		t.Fatal("a disabled family must store nothing")
	}
}

func TestEmotionRecordRefusedForUnboundChild(t *testing.T) {
	_, device, store := newEmotionHandlersForTest(t)
	store.settings["parent-1"] = emotiondomain.Settings{
		FamilyID: "parent-1", Enabled: true, RetentionDays: 30,
	}
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/devices/device-1/emotion-observations", strings.NewReader(
			`{"child_id":"child-9","label":"calm","confidence":70,"observed_at":"`+
				time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)+`"}`))
	request.SetPathValue("device_id", "device-1")
	recorder := httptest.NewRecorder()
	device.record(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", recorder.Code, recorder.Body.String())
	}
	if code := decodeEmotionError(t, recorder); code != "emotion_child_not_bound" {
		t.Fatalf("code = %q, want emotion_child_not_bound", code)
	}
}

func TestEmotionRecordRejectsUnknownLabel(t *testing.T) {
	_, device, store := newEmotionHandlersForTest(t)
	store.settings["parent-1"] = emotiondomain.Settings{
		FamilyID: "parent-1", Enabled: true, RetentionDays: 30,
	}
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/devices/device-1/emotion-observations", strings.NewReader(
			`{"label":"angry","confidence":70,"observed_at":"`+
				time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)+`"}`))
	request.SetPathValue("device_id", "device-1")
	recorder := httptest.NewRecorder()
	device.record(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
	if code := decodeEmotionError(t, recorder); code != "emotion_invalid_label" {
		t.Fatalf("code = %q, want emotion_invalid_label", code)
	}
}

// TestEmotionSummaryCarriesNoPointInTime is the central product assertion: the
// guardian payload must describe a period, never a moment, a device, or an
// individual sample.
func TestEmotionSummaryCarriesNoPointInTime(t *testing.T) {
	guardian, device, store := newEmotionHandlersForTest(t)
	store.settings["parent-1"] = emotiondomain.Settings{
		FamilyID: "parent-1", Enabled: true, RetentionDays: 30,
	}
	for index := 0; index < 5; index++ {
		request := httptest.NewRequest(http.MethodPost,
			"/api/v1/devices/device-1/emotion-observations", strings.NewReader(
				`{"label":"calm","confidence":80,"observed_at":"`+
					time.Now().UTC().Add(-time.Duration(index)*time.Hour).Format(time.RFC3339)+`"}`))
		request.SetPathValue("device_id", "device-1")
		recorder := httptest.NewRecorder()
		device.record(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("record %d status = %d: %s", index, recorder.Code, recorder.Body.String())
		}
	}

	request := emotionRequest(
		http.MethodGet,
		"/api/v1/families/parent-1/emotion-summary?child_id=child-1&days=7",
		"", "parent-1",
	)
	recorder := httptest.NewRecorder()
	guardian.summary(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeEmotionData(t, recorder)
	if body["total_samples"].(float64) != 5 {
		t.Fatalf("total_samples = %v, want 5", body["total_samples"])
	}
	// Only the window bounds may carry an instant.
	for _, key := range []string{
		"observations", "samples", "events", "details", "timeline",
		"observed_at", "device_id", "id",
	} {
		if _, leaked := body[key]; leaked {
			t.Fatalf("summary must not carry %q", key)
		}
	}
	labels, ok := body["labels"].([]any)
	if !ok {
		t.Fatalf("labels missing: %v", body["labels"])
	}
	if len(labels) != len(emotiondomain.AllLabels()) {
		t.Fatalf("labels = %d, want %d", len(labels), len(emotiondomain.AllLabels()))
	}
	for _, raw := range labels {
		entry := raw.(map[string]any)
		if _, leaked := entry["observed_at"]; leaked {
			t.Fatal("a label entry must not carry an instant")
		}
		if _, ok := entry["percentage"]; !ok {
			t.Fatal("a label entry must carry its share")
		}
	}
	calm := 0
	for _, raw := range labels {
		entry := raw.(map[string]any)
		if entry["label"] == string(emotiondomain.LabelCalm) {
			calm = int(entry["count"].(float64))
		}
	}
	if calm != 5 {
		t.Fatalf("calm count = %d, want 5", calm)
	}
}

func TestEmotionSummaryRejectsOutOfRangeDays(t *testing.T) {
	guardian, _, _ := newEmotionHandlersForTest(t)
	recorder := httptest.NewRecorder()
	guardian.summary(recorder, emotionRequest(
		http.MethodGet,
		"/api/v1/families/parent-1/emotion-summary?days=400",
		"", "parent-1",
	))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
	if code := decodeEmotionError(t, recorder); code != "emotion_invalid_days" {
		t.Fatalf("code = %q, want emotion_invalid_days", code)
	}
}

func TestEmotionSummaryRejectsNonNumericDays(t *testing.T) {
	guardian, _, _ := newEmotionHandlersForTest(t)
	recorder := httptest.NewRecorder()
	guardian.summary(recorder, emotionRequest(
		http.MethodGet,
		"/api/v1/families/parent-1/emotion-summary?days=seven",
		"", "parent-1",
	))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}
