package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	displaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/display/domain"
)

// displayTestService is an in-memory double so the handler tests exercise the
// real policy validation without a database.
type displayTestService struct {
	policy     displaydomain.Policy
	revision   int
	statuses   map[string]displaydomain.Status
	lastChange displaydomain.Input
}

func (s *displayTestService) Current(
	_ context.Context,
	_ string,
) (displaydomain.Policy, error) {
	policy := s.policy
	if policy.Revision == 0 {
		policy = displaydomain.Default()
	}
	policy.Revision = s.revision
	return policy, nil
}

func (s *displayTestService) Publish(
	_ context.Context,
	_ string,
	input displaydomain.Input,
) (displaydomain.Policy, error) {
	normalized, err := displaydomain.ValidateInput(input)
	if err != nil {
		return displaydomain.Policy{}, err
	}
	s.revision++
	s.lastChange = normalized
	policy := displaydomain.Default()
	policy.BrightnessPercent = normalized.BrightnessPercent
	policy.ColorTemperatureKelvin = normalized.ColorTemperatureKelvin
	policy.MinViewDistanceCM = normalized.MinViewDistanceCM
	policy.DailyLimitMinutes = normalized.DailyLimitMinutes
	policy.BreakIntervalMinutes = normalized.BreakIntervalMinutes
	policy.DimAfterSeconds = normalized.DimAfterSeconds
	policy.Revision = s.revision
	s.policy = policy
	return policy, nil
}

func (s *displayTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (displaydomain.Capability, error) {
	status, ok := s.statuses[deviceID]
	if !ok {
		return displaydomain.Capability{SchemaVersion: displaydomain.SchemaVersion}, nil
	}
	return displaydomain.Capability{
		Supported:       status.ScreenWidth > 0 && status.ScreenHeight > 0,
		TouchSupported:  status.TouchSupported,
		ScreenWidth:     status.ScreenWidth,
		ScreenHeight:    status.ScreenHeight,
		SchemaVersion:   displaydomain.SchemaVersion,
		PolicyRevision:  s.revision,
		AppliedRevision: status.PolicyRevision,
		UpToDate:        status.PolicyRevision >= s.revision,
	}, nil
}

func (s *displayTestService) ReportStatus(
	_ context.Context,
	_ string,
	deviceID string,
	input displaydomain.StatusInput,
) (displaydomain.Status, error) {
	normalized, err := displaydomain.ValidateStatus(input)
	if err != nil {
		return displaydomain.Status{}, err
	}
	if s.statuses == nil {
		s.statuses = map[string]displaydomain.Status{}
	}
	status := displaydomain.Status{
		DeviceID:          deviceID,
		BrightnessPercent: normalized.BrightnessPercent,
		ScreenWidth:       normalized.ScreenWidth,
		ScreenHeight:      normalized.ScreenHeight,
		TouchSupported:    normalized.TouchSupported,
		PolicyRevision:    normalized.PolicyRevision,
	}
	s.statuses[deviceID] = status
	return status, nil
}

func (s *displayTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]displaydomain.DeviceStatus, error) {
	devices := make([]displaydomain.DeviceStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		devices = append(devices, displaydomain.DeviceStatus{
			DeviceID:          status.DeviceID,
			BrightnessPercent: status.BrightnessPercent,
			ScreenWidth:       status.ScreenWidth,
			ScreenHeight:      status.ScreenHeight,
			TouchSupported:    status.TouchSupported,
			UpToDate:          status.PolicyRevision >= s.revision,
		})
	}
	return devices, nil
}

func (s *displayTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (displaydomain.AdminStats, error) {
	return displaydomain.AdminStats{
		WindowDays:  windowDays,
		DeviceCount: uint64(len(s.statuses)),
	}, nil
}

func TestDisplayCurrentRequiresAuthentication(t *testing.T) {
	handler := displayHandler{service: &displayTestService{}}
	request := englishRequest(http.MethodGet, "/api/v1/display/policy", "", "")
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDisplayPublishAndReadBack(t *testing.T) {
	handler := displayHandler{service: &displayTestService{}}
	body := `{"brightness_percent":75,"color_temperature_kelvin":3200,` +
		`"min_view_distance_cm":35,"daily_limit_minutes":90,` +
		`"break_interval_minutes":15,"dim_after_seconds":45,` +
		`"auto_brightness":true,"night_light":true,"distance_reminder":true}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/display/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	payload := decodeDisplayPolicyPayload(t, recorder.Body.Bytes())
	if payload.BrightnessPercent != 75 || payload.DailyLimitMinutes != 90 {
		t.Fatalf("unexpected policy: %+v", payload)
	}
	if payload.Revision != 1 {
		t.Fatalf("revision = %d, want 1", payload.Revision)
	}
}

func TestDisplayPublishRejectsOutOfRange(t *testing.T) {
	handler := displayHandler{service: &displayTestService{}}
	body := `{"brightness_percent":0,"color_temperature_kelvin":9000,` +
		`"min_view_distance_cm":5,"daily_limit_minutes":1,` +
		`"break_interval_minutes":1,"dim_after_seconds":1}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/display/policy",
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

func TestDisplayCapabilityIncludesReportedDevice(t *testing.T) {
	service := &displayTestService{
		revision: 2,
		statuses: map[string]displaydomain.Status{
			"device-1": {
				DeviceID:       "device-1",
				ScreenWidth:    480,
				ScreenHeight:   320,
				TouchSupported: true,
				PolicyRevision: 2,
			},
		},
	}
	handler := displayHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/display/capability",
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
		Data displaydomain.Capability `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Supported || !envelope.Data.TouchSupported || !envelope.Data.UpToDate {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

func TestDisplayDevicePolicyRequiresSession(t *testing.T) {
	handler := deviceDisplayHandler{
		handler:        &displayHandler{service: &displayTestService{}},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/display/policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.devicePolicy(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

type displayPolicyPayloadResult struct {
	BrightnessPercent      int  `json:"brightness_percent"`
	ColorTemperatureKelvin int  `json:"color_temperature_kelvin"`
	MinViewDistanceCM      int  `json:"min_view_distance_cm"`
	DailyLimitMinutes      int  `json:"daily_limit_minutes"`
	BreakIntervalMinutes   int  `json:"break_interval_minutes"`
	DimAfterSeconds        int  `json:"dim_after_seconds"`
	Revision               int  `json:"revision"`
	AutoBrightness         bool `json:"auto_brightness"`
}

func decodeDisplayPolicyPayload(
	t *testing.T,
	raw []byte,
) displayPolicyPayloadResult {
	t.Helper()
	var envelope struct {
		Data displayPolicyPayloadResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode display policy: %v", err)
	}
	return envelope.Data
}
