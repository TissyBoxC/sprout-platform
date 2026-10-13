package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	weardomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/wearable/domain"
)

// wearableTestService is an in-memory double so the handler tests exercise the
// real validation and lifecycle without a database.
type wearableTestService struct {
	policy   weardomain.LocationPolicy
	contacts map[string]weardomain.EmergencyContact
	geofence weardomain.Geofence
	hasGeo   bool
	statuses map[string]weardomain.DeviceStatus
	sos      map[string]weardomain.SOSEvent
}

func newWearableTestService() *wearableTestService {
	return &wearableTestService{
		contacts: map[string]weardomain.EmergencyContact{},
		statuses: map[string]weardomain.DeviceStatus{},
		sos:      map[string]weardomain.SOSEvent{},
	}
}

func (s *wearableTestService) Policy(
	_ context.Context,
	familyID string,
) (weardomain.LocationPolicy, error) {
	if s.policy.FamilyID == "" {
		s.policy = weardomain.LocationPolicy{
			FamilyID:              familyID,
			Precision:             weardomain.PrecisionCoarse,
			UploadIntervalSeconds: 900,
			RetentionDays:         7,
			GeofenceRadiusMeters:  500,
		}
	}
	return s.policy, nil
}

func (s *wearableTestService) UpdatePolicy(
	_ context.Context,
	familyID string,
	input weardomain.LocationPolicy,
) (weardomain.LocationPolicy, error) {
	normalized, err := weardomain.ValidatePolicy(input)
	if err != nil {
		return weardomain.LocationPolicy{}, err
	}
	normalized.FamilyID = familyID
	normalized.UpdatedAt = time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)
	s.policy = normalized
	return normalized, nil
}

func (s *wearableTestService) CreateContact(
	_ context.Context,
	familyID string,
	input weardomain.ContactInput,
) (weardomain.EmergencyContact, error) {
	normalized, err := weardomain.ValidateContactInput(input)
	if err != nil {
		return weardomain.EmergencyContact{}, err
	}
	contact := weardomain.EmergencyContact{
		ID:          "contact-1",
		FamilyID:    familyID,
		DisplayName: normalized.DisplayName,
		Phone:       normalized.Phone,
		Role:        normalized.Role,
		Priority:    normalized.Priority,
		Enabled:     normalized.Enabled,
	}
	s.contacts[contact.ID] = contact
	return contact, nil
}

func (s *wearableTestService) UpdateContact(
	_ context.Context,
	familyID string,
	contactID string,
	input weardomain.ContactInput,
) (weardomain.EmergencyContact, error) {
	normalized, err := weardomain.ValidateContactInput(input)
	if err != nil {
		return weardomain.EmergencyContact{}, err
	}
	if _, ok := s.contacts[contactID]; !ok {
		return weardomain.EmergencyContact{}, weardomain.ErrContactNotFound
	}
	contact := weardomain.EmergencyContact{
		ID:          contactID,
		FamilyID:    familyID,
		DisplayName: normalized.DisplayName,
		Phone:       normalized.Phone,
		Role:        normalized.Role,
		Priority:    normalized.Priority,
		Enabled:     normalized.Enabled,
	}
	s.contacts[contactID] = contact
	return contact, nil
}

func (s *wearableTestService) DeleteContact(
	_ context.Context,
	_ string,
	contactID string,
) error {
	if _, ok := s.contacts[contactID]; !ok {
		return weardomain.ErrContactNotFound
	}
	delete(s.contacts, contactID)
	return nil
}

func (s *wearableTestService) Contacts(
	_ context.Context,
	_ string,
) ([]weardomain.EmergencyContact, error) {
	contacts := make([]weardomain.EmergencyContact, 0, len(s.contacts))
	for _, contact := range s.contacts {
		contacts = append(contacts, contact)
	}
	return contacts, nil
}

func (s *wearableTestService) Geofence(
	_ context.Context,
	_ string,
) (weardomain.Geofence, bool, error) {
	return s.geofence, s.hasGeo, nil
}

func (s *wearableTestService) UpdateGeofence(
	_ context.Context,
	familyID string,
	input weardomain.GeofenceInput,
) (weardomain.Geofence, error) {
	normalized, err := weardomain.ValidateGeofenceInput(input)
	if err != nil {
		return weardomain.Geofence{}, err
	}
	s.geofence = weardomain.Geofence{
		ID:           "geofence-1",
		FamilyID:     familyID,
		Latitude:     normalized.Latitude,
		Longitude:    normalized.Longitude,
		RadiusMeters: normalized.RadiusMeters,
		Enabled:      normalized.Enabled,
	}
	s.hasGeo = true
	return s.geofence, nil
}

func (s *wearableTestService) ReportLocation(
	_ context.Context,
	familyID string,
	deviceID string,
	report weardomain.LocationReport,
) (weardomain.DeviceStatus, error) {
	normalized, err := weardomain.ValidateLocationReport(report)
	if err != nil {
		return weardomain.DeviceStatus{}, err
	}
	state := weardomain.GeofenceUnknown
	if s.hasGeo && s.geofence.Enabled {
		state = weardomain.GeofenceInside
	}
	status := weardomain.DeviceStatus{
		DeviceID:          deviceID,
		FamilyID:          familyID,
		LocationEnabled:   true,
		PermissionGranted: true,
		Precision:         normalized.Precision,
		SOSState:          weardomain.SOSStateIdle,
		Latitude:          normalized.Latitude,
		Longitude:         normalized.Longitude,
		AccuracyMeters:    normalized.AccuracyMeters,
		GeofenceState:     state,
	}
	s.statuses[deviceID] = status
	return status, nil
}

func (s *wearableTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]weardomain.DeviceStatusView, error) {
	views := make([]weardomain.DeviceStatusView, 0, len(s.statuses))
	for _, status := range s.statuses {
		views = append(views, weardomain.DeviceStatusView{
			DeviceID:      status.DeviceID,
			Precision:     status.Precision,
			SOSState:      status.SOSState,
			GeofenceState: status.GeofenceState,
		})
	}
	return views, nil
}

func (s *wearableTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (weardomain.DeviceStatusView, error) {
	status, ok := s.statuses[deviceID]
	if !ok {
		return weardomain.DeviceStatusView{
			DeviceID:      deviceID,
			Precision:     weardomain.PrecisionCoarse,
			SOSState:      weardomain.SOSStateIdle,
			GeofenceState: weardomain.GeofenceUnknown,
		}, nil
	}
	return weardomain.DeviceStatusView{
		DeviceID:      status.DeviceID,
		Precision:     status.Precision,
		SOSState:      status.SOSState,
		GeofenceState: status.GeofenceState,
	}, nil
}

func (s *wearableTestService) TriggerSOS(
	_ context.Context,
	familyID string,
	input weardomain.SOSInput,
) (weardomain.SOSEvent, error) {
	normalized, err := weardomain.ValidateSOSInput(input)
	if err != nil {
		return weardomain.SOSEvent{}, err
	}
	event := weardomain.SOSEvent{
		ID:          "sos-1",
		FamilyID:    familyID,
		DeviceID:    normalized.DeviceID,
		Latitude:    normalized.Latitude,
		Longitude:   normalized.Longitude,
		State:       weardomain.SOSStateTriggered,
		TriggeredAt: time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC),
	}
	s.sos[event.ID] = event
	return event, nil
}

func (s *wearableTestService) CancelSOS(
	_ context.Context,
	_ string,
	eventID string,
) (weardomain.SOSEvent, error) {
	event, ok := s.sos[eventID]
	if !ok || event.State != weardomain.SOSStateTriggered {
		return weardomain.SOSEvent{}, weardomain.ErrSOSNotFound
	}
	event.State = weardomain.SOSStateCancelled
	s.sos[eventID] = event
	return event, nil
}

func (s *wearableTestService) SOSHistory(
	_ context.Context,
	_ string,
	_ int,
) ([]weardomain.SOSEvent, error) {
	events := make([]weardomain.SOSEvent, 0, len(s.sos))
	for _, event := range s.sos {
		events = append(events, event)
	}
	return events, nil
}

func (s *wearableTestService) ActiveSOS(
	_ context.Context,
	familyID string,
) ([]weardomain.SOSEvent, error) {
	events := make([]weardomain.SOSEvent, 0, 2)
	for _, event := range s.sos {
		if event.FamilyID == familyID && event.State == weardomain.SOSStateTriggered {
			events = append(events, event)
		}
	}
	return events, nil
}

func (s *wearableTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (weardomain.AdminStats, error) {
	return weardomain.AdminStats{
		WindowDays:  windowDays,
		DeviceCount: len(s.statuses),
	}, nil
}

func TestWearablePolicyRequiresAuthentication(t *testing.T) {
	handler := wearableHandler{service: newWearableTestService()}
	request := englishRequest(http.MethodGet, "/api/v1/wearable/policy", "", "")
	recorder := httptest.NewRecorder()
	handler.policy(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestWearableUpdatePolicy(t *testing.T) {
	handler := wearableHandler{service: newWearableTestService()}
	body := `{"enabled":true,"permission_granted":true,"precision":"balanced","upload_interval_seconds":300,"retention_days":30,"geofence_radius_meters":200}`
	request := englishRequest(http.MethodPut, "/api/v1/wearable/policy", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.updatePolicy(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			PermissionGranted bool   `json:"permission_granted"`
			Precision         string `json:"precision"`
			RetentionDays     int    `json:"retention_days"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.PermissionGranted || envelope.Data.Precision != "balanced" {
		t.Fatalf("unexpected policy: %+v", envelope.Data)
	}
}

func TestWearableUpdatePolicyRejectsBadPrecision(t *testing.T) {
	handler := wearableHandler{service: newWearableTestService()}
	body := `{"enabled":true,"permission_granted":true,"precision":"wild"}`
	request := englishRequest(http.MethodPut, "/api/v1/wearable/policy", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.updatePolicy(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d",
			recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestWearableDevicePolicyRequiresSession(t *testing.T) {
	handler := deviceWearableHandler{
		handler:        &wearableHandler{service: newWearableTestService()},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/wearable/policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.devicePolicy(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestWearableCreateContact(t *testing.T) {
	handler := wearableHandler{service: newWearableTestService()}
	body := `{"display_name":"奶奶","phone":"13800000000","role":"guardian","priority":1,"enabled":true}`
	request := englishRequest(http.MethodPost, "/api/v1/wearable/contacts", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.createContact(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Phone       string `json:"phone"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.DisplayName != "奶奶" || envelope.Data.Phone != "13800000000" {
		t.Fatalf("unexpected contact: %+v", envelope.Data)
	}
}

func TestWearableCreateContactRejectsBadRole(t *testing.T) {
	handler := wearableHandler{service: newWearableTestService()}
	body := `{"display_name":"奶奶","phone":"13800000000","role":"stranger"}`
	request := englishRequest(http.MethodPost, "/api/v1/wearable/contacts", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.createContact(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d",
			recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestWearableGeofenceRoundTrip(t *testing.T) {
	service := newWearableTestService()
	handler := wearableHandler{service: service}
	body := `{"latitude":1.35,"longitude":103.81,"radius_meters":500,"enabled":true}`
	request := englishRequest(http.MethodPut, "/api/v1/wearable/geofence", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.updateGeofence(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	get := englishRequest(http.MethodGet, "/api/v1/wearable/geofence", "", "family-1")
	recorder = httptest.NewRecorder()
	handler.geofence(recorder, get)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var envelope struct {
		Data struct {
			Geofence map[string]any `json:"geofence"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Geofence == nil {
		t.Fatalf("expected a geofence in the response")
	}
}

func TestWearableGeofenceRejectsBadRadius(t *testing.T) {
	handler := wearableHandler{service: newWearableTestService()}
	body := `{"latitude":1.35,"longitude":103.81,"radius_meters":10,"enabled":true}`
	request := englishRequest(http.MethodPut, "/api/v1/wearable/geofence", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.updateGeofence(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d",
			recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestWearableSOSCancel(t *testing.T) {
	service := newWearableTestService()
	handler := wearableHandler{service: service}
	service.sos["sos-1"] = weardomain.SOSEvent{
		ID:        "sos-1",
		FamilyID:  "family-1",
		DeviceID:  "device-1",
		State:     weardomain.SOSStateTriggered,
		Latitude:  1.35,
		Longitude: 103.81,
	}
	active := englishRequest(http.MethodGet, "/api/v1/wearable/sos/active", "", "family-1")
	recorder := httptest.NewRecorder()
	handler.activeSOS(recorder, active)
	if recorder.Code != http.StatusOK {
		t.Fatalf("active status = %d, want %d", recorder.Code, http.StatusOK)
	}

	cancel := englishRequest(
		http.MethodPut,
		"/api/v1/wearable/sos/sos-1/cancel",
		"",
		"family-1",
	)
	cancel.SetPathValue("event_id", "sos-1")
	recorder = httptest.NewRecorder()
	handler.cancelSOS(recorder, cancel)
	if recorder.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			State string `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.State != "cancelled" {
		t.Fatalf("expected cancelled, got %q", envelope.Data.State)
	}
}

func TestWearableCapabilityWithoutStatus(t *testing.T) {
	handler := wearableHandler{service: newWearableTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-9/wearable/capability",
		"",
		"family-1",
	)
	request.SetPathValue("device_id", "device-9")
	recorder := httptest.NewRecorder()
	handler.capability(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data weardomain.DeviceStatusView `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.SOSState != weardomain.SOSStateIdle {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

func TestWearableAdminStats(t *testing.T) {
	service := newWearableTestService()
	service.statuses["device-1"] = weardomain.DeviceStatus{DeviceID: "device-1"}
	handler := wearableHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/admin/wearable/stats?window_days=30",
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
	if envelope.Data.WindowDays != 30 || envelope.Data.DeviceCount != 1 {
		t.Fatalf("unexpected stats: %+v", envelope.Data)
	}
}
