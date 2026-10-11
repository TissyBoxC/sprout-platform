package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	videodomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_companion/domain"
)

// videoTestService is an in-memory double so the handler tests exercise the real
// validation and lifecycle without a database.
type videoTestService struct {
	contacts map[string]videodomain.Contact
	calls    map[string]videodomain.Call
	statuses map[string]videodomain.DeviceStatus
}

func newVideoTestService() *videoTestService {
	return &videoTestService{
		contacts: map[string]videodomain.Contact{},
		calls:    map[string]videodomain.Call{},
		statuses: map[string]videodomain.DeviceStatus{},
	}
}

func (s *videoTestService) CreateContact(
	_ context.Context,
	familyID string,
	input videodomain.ContactInput,
) (videodomain.Contact, error) {
	normalized, err := videodomain.ValidateContactInput(input)
	if err != nil {
		return videodomain.Contact{}, err
	}
	contact := videodomain.Contact{
		ID:          "contact-1",
		FamilyID:    familyID,
		DisplayName: normalized.DisplayName,
		Role:        normalized.Role,
		Enabled:     normalized.Enabled,
	}
	s.contacts[contact.ID] = contact
	return contact, nil
}

func (s *videoTestService) UpdateContact(
	_ context.Context,
	familyID string,
	contactID string,
	input videodomain.ContactInput,
) (videodomain.Contact, error) {
	normalized, err := videodomain.ValidateContactInput(input)
	if err != nil {
		return videodomain.Contact{}, err
	}
	contact := videodomain.Contact{
		ID:          contactID,
		FamilyID:    familyID,
		DisplayName: normalized.DisplayName,
		Role:        normalized.Role,
		Enabled:     normalized.Enabled,
	}
	s.contacts[contactID] = contact
	return contact, nil
}

func (s *videoTestService) DeleteContact(
	_ context.Context,
	_ string,
	contactID string,
) error {
	delete(s.contacts, contactID)
	return nil
}

func (s *videoTestService) Contacts(
	_ context.Context,
	_ string,
) ([]videodomain.Contact, error) {
	contacts := make([]videodomain.Contact, 0, len(s.contacts))
	for _, contact := range s.contacts {
		contacts = append(contacts, contact)
	}
	return contacts, nil
}

func (s *videoTestService) StartCall(
	_ context.Context,
	familyID string,
	input videodomain.CallInput,
) (videodomain.Call, error) {
	normalized, err := videodomain.ValidateCallInput(input)
	if err != nil {
		return videodomain.Call{}, err
	}
	call := videodomain.Call{
		ID:        "call-1",
		FamilyID:  familyID,
		DeviceID:  normalized.DeviceID,
		ContactID: normalized.ContactID,
		Initiator: normalized.Initiator,
		State:     videodomain.StateRinging,
		Quality:   videodomain.QualityUnknown,
		StartedAt: time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC),
	}
	s.calls[call.ID] = call
	return call, nil
}

func (s *videoTestService) UpdateCallState(
	_ context.Context,
	_ string,
	callID string,
	state string,
	quality string,
	endReason string,
) (videodomain.Call, error) {
	call, ok := s.calls[callID]
	if !ok {
		return videodomain.Call{}, videodomain.ErrCallNotFound
	}
	if !videodomain.SupportedState(state) {
		return videodomain.Call{}, videodomain.ErrInvalidCall
	}
	call.State = state
	call.Quality = quality
	call.EndReason = endReason
	s.calls[callID] = call
	return call, nil
}

func (s *videoTestService) CallHistory(
	_ context.Context,
	_ string,
	_ int,
) ([]videodomain.Call, error) {
	calls := make([]videodomain.Call, 0, len(s.calls))
	for _, call := range s.calls {
		calls = append(calls, call)
	}
	return calls, nil
}

func (s *videoTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (videodomain.Capability, error) {
	status, ok := s.statuses[deviceID]
	if !ok {
		return videodomain.Capability{SchemaVersion: videodomain.SchemaVersion}, nil
	}
	return videodomain.Capability{
		Supported:         true,
		CallState:         status.CallState,
		Quality:           status.Quality,
		CameraAllowed:     status.CameraAllowed,
		MicrophoneAllowed: status.MicrophoneAllowed,
		SchemaVersion:     videodomain.SchemaVersion,
	}, nil
}

func (s *videoTestService) ReportStatus(
	_ context.Context,
	familyID string,
	deviceID string,
	input videodomain.CallStatusInput,
) (videodomain.DeviceStatus, error) {
	normalized, err := videodomain.ValidateCallStatus(input)
	if err != nil {
		return videodomain.DeviceStatus{}, err
	}
	status := videodomain.DeviceStatus{
		DeviceID:          deviceID,
		FamilyID:          familyID,
		CallState:         normalized.CallState,
		Quality:           normalized.Quality,
		CameraAllowed:     normalized.CameraAllowed,
		MicrophoneAllowed: normalized.MicrophoneAllowed,
		LastCallID:        normalized.LastCallID,
	}
	s.statuses[deviceID] = status
	return status, nil
}

func (s *videoTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]videodomain.DeviceCallStatus, error) {
	devices := make([]videodomain.DeviceCallStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		devices = append(devices, videodomain.DeviceCallStatus{
			DeviceID:          status.DeviceID,
			CallState:         status.CallState,
			Quality:           status.Quality,
			CameraAllowed:     status.CameraAllowed,
			MicrophoneAllowed: status.MicrophoneAllowed,
		})
	}
	return devices, nil
}

func (s *videoTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (videodomain.AdminStats, error) {
	return videodomain.AdminStats{
		WindowDays: windowDays,
		CallCount:  uint64(len(s.calls)),
	}, nil
}

func TestVideoContactsRequireAuthentication(t *testing.T) {
	handler := videoCompanionHandler{service: newVideoTestService()}
	request := englishRequest(http.MethodGet, "/api/v1/video-companion/contacts", "", "")
	recorder := httptest.NewRecorder()
	handler.listContacts(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestVideoCreateContact(t *testing.T) {
	handler := videoCompanionHandler{service: newVideoTestService()}
	body := `{"display_name":"妈妈","role":"guardian","enabled":true}`
	request := englishRequest(
		http.MethodPost,
		"/api/v1/video-companion/contacts",
		body,
		"family-1",
	)
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
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.DisplayName != "妈妈" {
		t.Fatalf("unexpected contact: %+v", envelope.Data)
	}
}

func TestVideoCreateContactRejectsBadRole(t *testing.T) {
	handler := videoCompanionHandler{service: newVideoTestService()}
	body := `{"display_name":"妈妈","role":"stranger","enabled":true}`
	request := englishRequest(
		http.MethodPost,
		"/api/v1/video-companion/contacts",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.createContact(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d",
			recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestVideoStartAndUpdateCall(t *testing.T) {
	service := newVideoTestService()
	handler := videoCompanionHandler{service: service}
	start := englishRequest(
		http.MethodPost,
		"/api/v1/video-companion/calls",
		`{"device_id":"device-1","initiator":"guardian"}`,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.startCall(recorder, start)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("start status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	update := englishRequest(
		http.MethodPatch,
		"/api/v1/video-companion/calls/call-1",
		`{"state":"ended","quality":"good","end_reason":"hangup"}`,
		"family-1",
	)
	update.SetPathValue("call_id", "call-1")
	recorder = httptest.NewRecorder()
	handler.updateCall(recorder, update)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			State     string `json:"state"`
			EndReason string `json:"end_reason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.State != "ended" || envelope.Data.EndReason != "hangup" {
		t.Fatalf("unexpected call: %+v", envelope.Data)
	}
}

func TestVideoCapabilityComposes(t *testing.T) {
	service := newVideoTestService()
	service.statuses["device-1"] = videodomain.DeviceStatus{
		DeviceID:          "device-1",
		CallState:         videodomain.StateActive,
		Quality:           videodomain.QualityGood,
		CameraAllowed:     true,
		MicrophoneAllowed: true,
	}
	handler := videoCompanionHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/video-companion/capability",
		"",
		"family-1",
	)
	request.SetPathValue("device_id", "device-1")
	recorder := httptest.NewRecorder()
	handler.capability(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data videodomain.Capability `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Supported || !envelope.Data.CameraAllowed {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}
