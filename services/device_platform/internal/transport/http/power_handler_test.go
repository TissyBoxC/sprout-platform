package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	powerdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/power/domain"
)

// powerTestService is an in-memory double so the handler tests exercise the
// real policy validation without a database.
type powerTestService struct {
	policy     powerdomain.Policy
	revision   int
	statuses   map[string]powerdomain.Status
	lastChange powerdomain.Input
}

func (s *powerTestService) Current(
	_ context.Context,
	_ string,
) (powerdomain.Policy, error) {
	policy := s.policy
	if policy.SchemaVersion == 0 {
		policy = powerdomain.Default()
	}
	policy.Revision = s.revision
	return policy, nil
}

func (s *powerTestService) Publish(
	_ context.Context,
	_ string,
	input powerdomain.Input,
) (powerdomain.Policy, error) {
	normalized, err := powerdomain.ValidateInput(input)
	if err != nil {
		return powerdomain.Policy{}, err
	}
	s.revision++
	s.lastChange = normalized
	policy := powerdomain.Default()
	policy.Enabled = normalized.Enabled
	policy.LowPowerEnabled = normalized.LowPowerEnabled
	policy.DeepSleepEnabled = normalized.DeepSleepEnabled
	policy.ChargePriority = normalized.ChargePriority
	policy.LowBatteryWarnPercent = normalized.LowBatteryWarnPercent
	policy.LowPowerEnterPercent = normalized.LowPowerEnterPercent
	policy.SleepAfterSeconds = normalized.SleepAfterSeconds
	policy.Revision = s.revision
	s.policy = policy
	return policy, nil
}

func (s *powerTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (powerdomain.Capability, error) {
	status, ok := s.statuses[deviceID]
	if !ok {
		return powerdomain.Capability{SchemaVersion: powerdomain.SchemaVersion}, nil
	}
	return powerdomain.Capability{
		Supported:       status.ChargeState != powerdomain.ChargeUnknown,
		ChargeState:     string(status.ChargeState),
		PowerMode:       string(status.PowerMode),
		BatteryPercent:  status.BatteryPercent,
		Charging:        status.Charging,
		SchemaVersion:   powerdomain.SchemaVersion,
		PolicyRevision:  s.revision,
		AppliedRevision: status.PolicyRevision,
		UpToDate:        status.PolicyRevision >= s.revision,
	}, nil
}

func (s *powerTestService) ReportStatus(
	_ context.Context,
	_ string,
	deviceID string,
	input powerdomain.StatusInput,
) (powerdomain.Status, error) {
	normalized, err := powerdomain.ValidateStatus(input)
	if err != nil {
		return powerdomain.Status{}, err
	}
	if s.statuses == nil {
		s.statuses = map[string]powerdomain.Status{}
	}
	status := powerdomain.Status{
		DeviceID:       deviceID,
		ChargeState:    normalized.ChargeState,
		PowerMode:      normalized.PowerMode,
		BatteryPercent: normalized.BatteryPercent,
		Millivolts:     normalized.Millivolts,
		Charging:       normalized.Charging,
		PolicyRevision: normalized.PolicyRevision,
	}
	s.statuses[deviceID] = status
	return status, nil
}

func (s *powerTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]powerdomain.DeviceStatus, error) {
	devices := make([]powerdomain.DeviceStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		devices = append(devices, powerdomain.DeviceStatus{
			DeviceID:       status.DeviceID,
			ChargeState:    string(status.ChargeState),
			PowerMode:      string(status.PowerMode),
			BatteryPercent: status.BatteryPercent,
			Charging:       status.Charging,
			UpToDate:       status.PolicyRevision >= s.revision,
		})
	}
	return devices, nil
}

func (s *powerTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (powerdomain.AdminStats, error) {
	return powerdomain.AdminStats{
		WindowDays:  windowDays,
		DeviceCount: uint64(len(s.statuses)),
	}, nil
}

func TestPowerCurrentRequiresAuthentication(t *testing.T) {
	handler := powerHandler{service: &powerTestService{}}
	request := englishRequest(http.MethodGet, "/api/v1/power/policy", "", "")
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestPowerDevicePolicyRequiresSession(t *testing.T) {
	handler := devicePowerHandler{
		handler:        &powerHandler{service: &powerTestService{}},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/power/policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.devicePolicy(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestPowerPublishAndReadBack(t *testing.T) {
	handler := powerHandler{service: &powerTestService{}}
	body := `{"enabled":true,"low_power_enabled":true,` +
		`"deep_sleep_enabled":true,"charge_priority":false,` +
		`"low_battery_warn_percent":30,"low_power_enter_percent":20,` +
		`"sleep_after_seconds":900}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/power/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	payload := decodePowerPolicyPayload(t, recorder.Body.Bytes())
	if payload.SleepAfterSeconds != 900 || payload.LowBatteryWarnPercent != 30 {
		t.Fatalf("unexpected policy: %+v", payload)
	}
	if payload.Revision != 1 {
		t.Fatalf("revision = %d, want 1", payload.Revision)
	}
}

func TestPowerPublishRejectsOutOfRange(t *testing.T) {
	handler := powerHandler{service: &powerTestService{}}
	body := `{"enabled":true,"low_power_enabled":true,` +
		`"deep_sleep_enabled":true,"charge_priority":true,` +
		`"low_battery_warn_percent":1,"low_power_enter_percent":99,` +
		`"sleep_after_seconds":1}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/power/policy",
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

func TestPowerCapabilityIncludesReportedDevice(t *testing.T) {
	service := &powerTestService{
		revision: 2,
		statuses: map[string]powerdomain.Status{
			"device-1": {
				DeviceID:       "device-1",
				ChargeState:    powerdomain.ChargeDischarging,
				PowerMode:      powerdomain.ModeLowPower,
				BatteryPercent: 12,
				PolicyRevision: 2,
			},
		},
	}
	handler := powerHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/power/capability",
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
		Data powerdomain.Capability `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Supported || !envelope.Data.UpToDate {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

type powerPolicyPayloadResult struct {
	Enabled               bool `json:"enabled"`
	LowPowerEnabled       bool `json:"low_power_enabled"`
	DeepSleepEnabled      bool `json:"deep_sleep_enabled"`
	ChargePriority        bool `json:"charge_priority"`
	LowBatteryWarnPercent int  `json:"low_battery_warn_percent"`
	LowPowerEnterPercent  int  `json:"low_power_enter_percent"`
	SleepAfterSeconds     int  `json:"sleep_after_seconds"`
	Revision              int  `json:"revision"`
}

func decodePowerPolicyPayload(
	t *testing.T,
	raw []byte,
) powerPolicyPayloadResult {
	t.Helper()
	var envelope struct {
		Data powerPolicyPayloadResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode power policy: %v", err)
	}
	return envelope.Data
}
