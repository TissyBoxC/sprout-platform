package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	motiondomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/motion/domain"
)

// motionTestService is an in-memory double so the handler tests exercise the
// real policy validation without a database.
type motionTestService struct {
	policy     motiondomain.Policy
	revision   int
	statuses   map[string]motiondomain.Status
	lastChange motiondomain.Input
}

func (s *motionTestService) Current(
	_ context.Context,
	_ string,
) (motiondomain.Policy, error) {
	policy := s.policy
	if policy.SchemaVersion == 0 {
		policy = motiondomain.Default()
	}
	policy.Revision = s.revision
	return policy, nil
}

func (s *motionTestService) Publish(
	_ context.Context,
	_ string,
	input motiondomain.Input,
) (motiondomain.Policy, error) {
	normalized, err := motiondomain.ValidateInput(input)
	if err != nil {
		return motiondomain.Policy{}, err
	}
	s.revision++
	s.lastChange = normalized
	policy := motiondomain.Default()
	policy.Enabled = normalized.Enabled
	policy.RequireStopAck = normalized.RequireStopAck
	policy.AllowLocomotion = normalized.AllowLocomotion
	policy.MaxSpeedPercent = normalized.MaxSpeedPercent
	policy.MaxRuntimeMS = normalized.MaxRuntimeMS
	policy.StopGraceMS = normalized.StopGraceMS
	policy.Revision = s.revision
	s.policy = policy
	return policy, nil
}

func (s *motionTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (motiondomain.Capability, error) {
	status, ok := s.statuses[deviceID]
	if !ok {
		return motiondomain.Capability{SchemaVersion: motiondomain.SchemaVersion}, nil
	}
	return motiondomain.Capability{
		Supported:       status.ActuatorCount > 0,
		State:           string(status.State),
		Action:          string(status.Action),
		ActuatorCount:   status.ActuatorCount,
		Faulted:         status.Faulted,
		AllowLocomotion: s.policy.AllowLocomotion,
		SchemaVersion:   motiondomain.SchemaVersion,
		PolicyRevision:  s.revision,
		AppliedRevision: status.PolicyRevision,
		UpToDate:        status.PolicyRevision >= s.revision,
	}, nil
}

func (s *motionTestService) ReportStatus(
	_ context.Context,
	_ string,
	deviceID string,
	input motiondomain.StatusInput,
) (motiondomain.Status, error) {
	normalized, err := motiondomain.ValidateStatus(input)
	if err != nil {
		return motiondomain.Status{}, err
	}
	if s.statuses == nil {
		s.statuses = map[string]motiondomain.Status{}
	}
	status := motiondomain.Status{
		DeviceID:       deviceID,
		State:          normalized.State,
		Action:         normalized.Action,
		ActuatorCount:  normalized.ActuatorCount,
		Faulted:        normalized.Faulted,
		PolicyRevision: normalized.PolicyRevision,
	}
	s.statuses[deviceID] = status
	return status, nil
}

func (s *motionTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]motiondomain.DeviceStatus, error) {
	devices := make([]motiondomain.DeviceStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		devices = append(devices, motiondomain.DeviceStatus{
			DeviceID:      status.DeviceID,
			State:         string(status.State),
			Action:        string(status.Action),
			ActuatorCount: status.ActuatorCount,
			Faulted:       status.Faulted,
			UpToDate:      status.PolicyRevision >= s.revision,
		})
	}
	return devices, nil
}

func (s *motionTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (motiondomain.AdminStats, error) {
	return motiondomain.AdminStats{
		WindowDays:  windowDays,
		DeviceCount: uint64(len(s.statuses)),
	}, nil
}

func TestMotionCurrentRequiresAuthentication(t *testing.T) {
	handler := motionHandler{service: &motionTestService{}}
	request := englishRequest(http.MethodGet, "/api/v1/motion/policy", "", "")
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestMotionDevicePolicyRequiresSession(t *testing.T) {
	handler := deviceMotionHandler{
		handler:        &motionHandler{service: &motionTestService{}},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/motion/policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.devicePolicy(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestMotionPublishAndReadBack(t *testing.T) {
	handler := motionHandler{service: &motionTestService{}}
	body := `{"enabled":true,"require_stop_ack":true,` +
		`"allow_locomotion":true,"max_speed_percent":80,` +
		`"max_runtime_ms":4000,"stop_grace_ms":250}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/motion/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	payload := decodeMotionPolicyPayload(t, recorder.Body.Bytes())
	if !payload.AllowLocomotion || payload.MaxSpeedPercent != 80 ||
		payload.MaxRuntimeMS != 4000 {
		t.Fatalf("unexpected policy: %+v", payload)
	}
	if payload.Revision != 1 {
		t.Fatalf("revision = %d, want 1", payload.Revision)
	}
}

func TestMotionPublishRejectsOutOfRange(t *testing.T) {
	handler := motionHandler{service: &motionTestService{}}
	body := `{"enabled":true,"require_stop_ack":true,` +
		`"allow_locomotion":false,"max_speed_percent":0,` +
		`"max_runtime_ms":1,"stop_grace_ms":0}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/motion/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d",
			recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestMotionCapabilityIncludesReportedDevice(t *testing.T) {
	service := &motionTestService{
		revision: 2,
		statuses: map[string]motiondomain.Status{
			"device-1": {
				DeviceID:       "device-1",
				State:          motiondomain.StateRunning,
				Action:         motiondomain.ActionWalk,
				ActuatorCount:  4,
				PolicyRevision: 2,
			},
		},
	}
	handler := motionHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/motion/capability",
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
		Data motiondomain.Capability `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Supported || !envelope.Data.UpToDate {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

type motionPolicyPayloadResult struct {
	Enabled         bool `json:"enabled"`
	RequireStopAck  bool `json:"require_stop_ack"`
	AllowLocomotion bool `json:"allow_locomotion"`
	MaxSpeedPercent int  `json:"max_speed_percent"`
	MaxRuntimeMS    int  `json:"max_runtime_ms"`
	StopGraceMS     int  `json:"stop_grace_ms"`
	Revision        int  `json:"revision"`
}

func decodeMotionPolicyPayload(
	t *testing.T,
	raw []byte,
) motionPolicyPayloadResult {
	t.Helper()
	var envelope struct {
		Data motionPolicyPayloadResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode motion policy: %v", err)
	}
	return envelope.Data
}
