package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	inputdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/input/domain"
)

// inputTestService is an in-memory double so the handler tests exercise the
// real configuration validation without a database.
type inputTestService struct {
	config     inputdomain.Config
	revision   int
	statuses   map[string]inputdomain.Status
	lastChange inputdomain.Input
}

func (s *inputTestService) Current(
	_ context.Context,
	_ string,
) (inputdomain.Config, error) {
	config := s.config
	if config.SchemaVersion == 0 {
		config = inputdomain.Default()
	}
	config.Revision = s.revision
	return config, nil
}

func (s *inputTestService) Publish(
	_ context.Context,
	_ string,
	input inputdomain.Input,
) (inputdomain.Config, error) {
	normalized, err := inputdomain.ValidateInput(input)
	if err != nil {
		return inputdomain.Config{}, err
	}
	s.revision++
	s.lastChange = normalized
	config := inputdomain.Default()
	config.Enabled = normalized.Enabled
	config.TapEnabled = normalized.TapEnabled
	config.SwipeEnabled = normalized.SwipeEnabled
	config.LongPressEnabled = normalized.LongPressEnabled
	config.HapticEnabled = normalized.HapticEnabled
	config.Sensitivity = normalized.Sensitivity
	config.RotationDegrees = normalized.RotationDegrees
	config.LongPressMS = normalized.LongPressMS
	config.DoubleTapMS = normalized.DoubleTapMS
	config.SwipeDistancePX = normalized.SwipeDistancePX
	config.Revision = s.revision
	s.config = config
	return config, nil
}

func (s *inputTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (inputdomain.Capability, error) {
	status, ok := s.statuses[deviceID]
	if !ok {
		return inputdomain.Capability{SchemaVersion: inputdomain.SchemaVersion}, nil
	}
	return inputdomain.Capability{
		Supported:       status.TouchSupported,
		HapticSupported: status.HapticSupported,
		GestureCount:    status.GestureCount,
		SchemaVersion:   inputdomain.SchemaVersion,
		PolicyRevision:  s.revision,
		AppliedRevision: status.PolicyRevision,
		UpToDate:        status.PolicyRevision >= s.revision,
	}, nil
}

func (s *inputTestService) ReportStatus(
	_ context.Context,
	_ string,
	deviceID string,
	input inputdomain.StatusInput,
) (inputdomain.Status, error) {
	normalized, err := inputdomain.ValidateStatus(input)
	if err != nil {
		return inputdomain.Status{}, err
	}
	if s.statuses == nil {
		s.statuses = map[string]inputdomain.Status{}
	}
	status := inputdomain.Status{
		DeviceID:        deviceID,
		TouchSupported:  normalized.TouchSupported,
		HapticSupported: normalized.HapticSupported,
		GestureCount:    normalized.GestureCount,
		PolicyRevision:  normalized.PolicyRevision,
	}
	s.statuses[deviceID] = status
	return status, nil
}

func (s *inputTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]inputdomain.DeviceStatus, error) {
	devices := make([]inputdomain.DeviceStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		devices = append(devices, inputdomain.DeviceStatus{
			DeviceID:        status.DeviceID,
			TouchSupported:  status.TouchSupported,
			HapticSupported: status.HapticSupported,
			GestureCount:    status.GestureCount,
			UpToDate:        status.PolicyRevision >= s.revision,
		})
	}
	return devices, nil
}

func (s *inputTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (inputdomain.AdminStats, error) {
	return inputdomain.AdminStats{
		WindowDays:  windowDays,
		DeviceCount: uint64(len(s.statuses)),
	}, nil
}

func TestInputCurrentRequiresAuthentication(t *testing.T) {
	handler := inputHandler{service: &inputTestService{}}
	request := englishRequest(http.MethodGet, "/api/v1/touch/config", "", "")
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestInputPublishAndReadBack(t *testing.T) {
	handler := inputHandler{service: &inputTestService{}}
	body := `{"enabled":true,"tap_enabled":true,"swipe_enabled":true,` +
		`"long_press_enabled":true,"haptic_enabled":false,"sensitivity":75,` +
		`"rotation_degrees":90,"long_press_ms":600,"double_tap_ms":250,` +
		`"swipe_distance_px":80}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/touch/config",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	payload := decodeTouchConfigPayload(t, recorder.Body.Bytes())
	if payload.Sensitivity != 75 || payload.RotationDegrees != 90 {
		t.Fatalf("unexpected config: %+v", payload)
	}
	if payload.Revision != 1 {
		t.Fatalf("revision = %d, want 1", payload.Revision)
	}
}

func TestInputPublishRejectsOutOfRange(t *testing.T) {
	handler := inputHandler{service: &inputTestService{}}
	body := `{"sensitivity":0,"rotation_degrees":45,"long_press_ms":1,` +
		`"double_tap_ms":9999,"swipe_distance_px":1}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/touch/config",
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

func TestInputCapabilityIncludesReportedDevice(t *testing.T) {
	service := &inputTestService{
		revision: 2,
		statuses: map[string]inputdomain.Status{
			"device-1": {
				DeviceID:        "device-1",
				TouchSupported:  true,
				HapticSupported: true,
				GestureCount:    17,
				PolicyRevision:  2,
			},
		},
	}
	handler := inputHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/touch/capability",
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
		Data inputdomain.Capability `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Supported || !envelope.Data.HapticSupported || !envelope.Data.UpToDate {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

type touchConfigPayloadResult struct {
	Enabled          bool `json:"enabled"`
	TapEnabled       bool `json:"tap_enabled"`
	SwipeEnabled     bool `json:"swipe_enabled"`
	LongPressEnabled bool `json:"long_press_enabled"`
	HapticEnabled    bool `json:"haptic_enabled"`
	Sensitivity      int  `json:"sensitivity"`
	RotationDegrees  int  `json:"rotation_degrees"`
	LongPressMS      int  `json:"long_press_ms"`
	DoubleTapMS      int  `json:"double_tap_ms"`
	SwipeDistancePX  int  `json:"swipe_distance_px"`
	Revision         int  `json:"revision"`
}

func decodeTouchConfigPayload(
	t *testing.T,
	raw []byte,
) touchConfigPayloadResult {
	t.Helper()
	var envelope struct {
		Data touchConfigPayloadResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode touch config: %v", err)
	}
	return envelope.Data
}
