package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	bluetoothaudiodomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/bluetooth_audio/domain"
)

// bluetoothAudioTestService is an in-memory double so the handler tests exercise
// the real validation and lifecycle without a database.
type bluetoothAudioTestService struct {
	policy  bluetoothaudiodomain.Policy
	devices map[string]bluetoothaudiodomain.Status
}

func newBluetoothAudioTestService() *bluetoothAudioTestService {
	return &bluetoothAudioTestService{
		policy:  bluetoothaudiodomain.Default(),
		devices: map[string]bluetoothaudiodomain.Status{},
	}
}

func (s *bluetoothAudioTestService) Catalog() ([]string, []string, []string, []string) {
	return bluetoothaudiodomain.PreferredCodecs,
		bluetoothaudiodomain.ActiveCodecs,
		bluetoothaudiodomain.AdapterStates,
		bluetoothaudiodomain.DisconnectReasons
}

func (s *bluetoothAudioTestService) Current(
	_ context.Context,
	_ string,
) (bluetoothaudiodomain.Policy, error) {
	return s.policy, nil
}

func (s *bluetoothAudioTestService) Publish(
	_ context.Context,
	_ string,
	input bluetoothaudiodomain.Input,
) (bluetoothaudiodomain.Policy, error) {
	normalized, err := bluetoothaudiodomain.ValidateInput(input)
	if err != nil {
		return bluetoothaudiodomain.Policy{}, err
	}
	s.policy = bluetoothaudiodomain.Policy{
		SchemaVersion:      bluetoothaudiodomain.SchemaVersion,
		Enabled:            normalized.Enabled,
		SpeakerModeAllowed: normalized.SpeakerModeAllowed,
		PairingAllowed:     normalized.PairingAllowed,
		AutoReconnect:      normalized.AutoReconnect,
		PreferredCodec:     normalized.PreferredCodec,
		MaxVolumePercent:   normalized.MaxVolumePercent,
		DisplayName:        normalized.DisplayName,
		Revision:           s.policy.Revision + 1,
	}
	return s.policy, nil
}

func (s *bluetoothAudioTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (bluetoothaudiodomain.Capability, error) {
	status, ok := s.devices[deviceID]
	if !ok {
		return bluetoothaudiodomain.Capability{
			Supported:      false,
			AdapterState:   bluetoothaudiodomain.AdapterOff,
			ActiveCodec:    bluetoothaudiodomain.ActiveCodecUnknown,
			SchemaVersion:  bluetoothaudiodomain.SchemaVersion,
			PolicyRevision: s.policy.Revision,
		}, nil
	}
	return bluetoothaudiodomain.Capability{
		Supported:       status.BluetoothSupported,
		SpeakerMode:     status.SpeakerMode,
		AdapterState:    status.AdapterState,
		ActiveCodec:     status.ActiveCodec,
		VolumePercent:   status.VolumePercent,
		SchemaVersion:   bluetoothaudiodomain.SchemaVersion,
		PolicyRevision:  s.policy.Revision,
		AppliedRevision: status.PolicyRevision,
		UpToDate:        status.PolicyRevision >= s.policy.Revision,
	}, nil
}

func (s *bluetoothAudioTestService) ReportStatus(
	_ context.Context,
	familyID string,
	deviceID string,
	input bluetoothaudiodomain.StatusInput,
) (bluetoothaudiodomain.Status, error) {
	normalized, err := bluetoothaudiodomain.ValidateStatus(input)
	if err != nil {
		return bluetoothaudiodomain.Status{}, err
	}
	status := bluetoothaudiodomain.Status{
		DeviceID:             deviceID,
		FamilyID:             familyID,
		BluetoothSupported:   normalized.BluetoothSupported,
		SpeakerMode:          normalized.SpeakerMode,
		AdapterState:         normalized.AdapterState,
		PairedDeviceCount:    normalized.PairedDeviceCount,
		ConnectedDeviceCount: normalized.ConnectedDeviceCount,
		ActiveCodec:          normalized.ActiveCodec,
		VolumePercent:        normalized.VolumePercent,
		LastDisconnectReason: normalized.LastDisconnectReason,
		PolicyRevision:       normalized.PolicyRevision,
	}
	s.devices[deviceID] = status
	return status, nil
}

func (s *bluetoothAudioTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]bluetoothaudiodomain.DeviceStatus, error) {
	return []bluetoothaudiodomain.DeviceStatus{}, nil
}

func (s *bluetoothAudioTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (bluetoothaudiodomain.AdminStats, error) {
	return bluetoothaudiodomain.AdminStats{WindowDays: windowDays, DeviceCount: 5}, nil
}

func TestBluetoothAudioPolicyRequiresAuthentication(t *testing.T) {
	handler := bluetoothAudioHandler{service: newBluetoothAudioTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/bluetooth-audio/policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestBluetoothAudioPolicyReturnsConservativeDefault(t *testing.T) {
	handler := bluetoothAudioHandler{service: newBluetoothAudioTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/bluetooth-audio/policy",
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
				Enabled        bool `json:"enabled"`
				PairingAllowed bool `json:"pairing_allowed"`
			} `json:"policy"`
			PreferredCodecs []string `json:"preferred_codecs"`
			AdapterStates   []string `json:"adapter_states"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Policy.Enabled || envelope.Data.Policy.PairingAllowed {
		t.Fatalf("default policy must keep bluetooth audio and pairing off")
	}
	if len(envelope.Data.PreferredCodecs) != len(bluetoothaudiodomain.PreferredCodecs) ||
		len(envelope.Data.AdapterStates) != len(bluetoothaudiodomain.AdapterStates) {
		t.Fatalf("unexpected catalog: %+v", envelope.Data)
	}
}

func TestBluetoothAudioPublishRejectsUnknownCodec(t *testing.T) {
	handler := bluetoothAudioHandler{service: newBluetoothAudioTestService()}
	body := `{"enabled":true,"speaker_mode_allowed":true,"pairing_allowed":true,"auto_reconnect":true,"preferred_codec":"aptx","max_volume_percent":70}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/bluetooth-audio/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestBluetoothAudioPublishAcceptsBoundedPolicy(t *testing.T) {
	service := newBluetoothAudioTestService()
	handler := bluetoothAudioHandler{service: service}
	body := `{"enabled":true,"speaker_mode_allowed":true,"pairing_allowed":true,"auto_reconnect":false,"preferred_codec":"aac","max_volume_percent":60,"display_name":"客厅音箱"}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/bluetooth-audio/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !service.policy.Enabled || !service.policy.PairingAllowed {
		t.Fatalf("published policy lost field values: %+v", service.policy)
	}
	if service.policy.PreferredCodec != bluetoothaudiodomain.CodecAAC {
		t.Fatalf("codec = %q", service.policy.PreferredCodec)
	}
	if service.policy.Revision != 1 {
		t.Fatalf("revision = %d, want 1", service.policy.Revision)
	}
}

func TestBluetoothAudioCapabilityUnknownDevice(t *testing.T) {
	handler := bluetoothAudioHandler{service: newBluetoothAudioTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-missing/bluetooth-audio/capability",
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
			Supported    bool   `json:"supported"`
			AdapterState string `json:"adapter_state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Supported || envelope.Data.AdapterState != "off" {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

func TestBluetoothAudioReportStatusRefusedWithoutSession(t *testing.T) {
	handler := deviceBluetoothAudioHandler{
		handler:        &bluetoothAudioHandler{service: newBluetoothAudioTestService()},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodPost,
		"/api/v1/devices/device-1/bluetooth-audio/status",
		`{"bluetooth_supported":true,"speaker_mode":true,"adapter_state":"connected","paired_device_count":1,"connected_device_count":1,"active_codec":"aac","volume_percent":40,"last_disconnect_reason":"none"}`,
		"",
	)
	recorder := httptest.NewRecorder()
	handler.reportStatus(recorder, request)
	// No binding service means the session is refused before validation.
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestBluetoothAudioAdminStats(t *testing.T) {
	handler := bluetoothAudioHandler{service: newBluetoothAudioTestService()}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/admin/bluetooth-audio/stats?window_days=30",
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
