package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	connectivitydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/connectivity/domain"
)

// connectivityTestService is an in-memory double so the handler tests exercise
// the real policy validation without a database.
type connectivityTestService struct {
	policy     connectivitydomain.Policy
	revision   int
	statuses   map[string]connectivitydomain.Status
	lastChange connectivitydomain.Input
}

func (s *connectivityTestService) Current(
	_ context.Context,
	_ string,
) (connectivitydomain.Policy, error) {
	policy := s.policy
	if policy.SchemaVersion == 0 {
		policy = connectivitydomain.Default()
	}
	policy.Revision = s.revision
	return policy, nil
}

func (s *connectivityTestService) Publish(
	_ context.Context,
	_ string,
	input connectivitydomain.Input,
) (connectivitydomain.Policy, error) {
	normalized, err := connectivitydomain.ValidateInput(input)
	if err != nil {
		return connectivitydomain.Policy{}, err
	}
	s.revision++
	s.lastChange = normalized
	policy := connectivitydomain.Default()
	policy.Enabled = normalized.Enabled
	policy.RoamingAllowed = normalized.RoamingAllowed
	policy.RemoteWakeEnabled = normalized.RemoteWakeEnabled
	policy.MeteredUploadLimit = normalized.MeteredUploadLimit
	policy.Preference = normalized.Preference
	policy.MonthlyQuotaMB = normalized.MonthlyQuotaMB
	policy.LowQuotaWarnPercent = normalized.LowQuotaWarnPercent
	policy.KeepaliveSeconds = normalized.KeepaliveSeconds
	policy.Revision = s.revision
	s.policy = policy
	return policy, nil
}

func (s *connectivityTestService) Capability(
	_ context.Context,
	_ string,
	deviceID string,
) (connectivitydomain.Capability, error) {
	status, ok := s.statuses[deviceID]
	if !ok {
		return connectivitydomain.Capability{SchemaVersion: connectivitydomain.SchemaVersion}, nil
	}
	return connectivitydomain.Capability{
		Supported:       status.Bearer != connectivitydomain.BearerNone,
		Roaming:         status.Roaming,
		ESIMState:       string(status.ESIMState),
		SignalPercent:   status.SignalPercent,
		QuotaMB:         s.policy.MonthlyQuotaMB,
		UsedMB:          status.UsedMB,
		SchemaVersion:   connectivitydomain.SchemaVersion,
		PolicyRevision:  s.revision,
		AppliedRevision: status.PolicyRevision,
		UpToDate:        status.PolicyRevision >= s.revision,
	}, nil
}

func (s *connectivityTestService) ReportStatus(
	_ context.Context,
	_ string,
	deviceID string,
	input connectivitydomain.StatusInput,
) (connectivitydomain.Status, error) {
	normalized, err := connectivitydomain.ValidateStatus(input)
	if err != nil {
		return connectivitydomain.Status{}, err
	}
	if s.statuses == nil {
		s.statuses = map[string]connectivitydomain.Status{}
	}
	status := connectivitydomain.Status{
		DeviceID:       deviceID,
		Bearer:         normalized.Bearer,
		Roaming:        normalized.Roaming,
		ESIMState:      normalized.ESIMState,
		SignalPercent:  normalized.SignalPercent,
		UsedMB:         normalized.UsedMB,
		PolicyRevision: normalized.PolicyRevision,
	}
	s.statuses[deviceID] = status
	return status, nil
}

func (s *connectivityTestService) DeviceStatuses(
	_ context.Context,
	_ string,
) ([]connectivitydomain.DeviceStatus, error) {
	devices := make([]connectivitydomain.DeviceStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		devices = append(devices, connectivitydomain.DeviceStatus{
			DeviceID:      status.DeviceID,
			Bearer:        string(status.Bearer),
			Roaming:       status.Roaming,
			ESIMState:     string(status.ESIMState),
			SignalPercent: status.SignalPercent,
			UsedMB:        status.UsedMB,
			UpToDate:      status.PolicyRevision >= s.revision,
		})
	}
	return devices, nil
}

func (s *connectivityTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (connectivitydomain.AdminStats, error) {
	return connectivitydomain.AdminStats{
		WindowDays:  windowDays,
		DeviceCount: uint64(len(s.statuses)),
	}, nil
}

func TestConnectivityCurrentRequiresAuthentication(t *testing.T) {
	handler := connectivityHandler{service: &connectivityTestService{}}
	request := englishRequest(http.MethodGet, "/api/v1/connectivity/policy", "", "")
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestConnectivityDevicePolicyRequiresSession(t *testing.T) {
	handler := deviceConnectivityHandler{
		handler:        &connectivityHandler{service: &connectivityTestService{}},
		bindingService: nil,
		runtimeService: nil,
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/connectivity/policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.devicePolicy(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestConnectivityPublishAndReadBack(t *testing.T) {
	handler := connectivityHandler{service: &connectivityTestService{}}
	body := `{"enabled":true,"roaming_allowed":true,` +
		`"remote_wake_enabled":true,"metered_upload_limit":false,` +
		`"preference":"cellular_first","monthly_quota_mb":4096,` +
		`"low_quota_warn_percent":25,"keepalive_seconds":600}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/connectivity/policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	payload := decodeCellularPolicyPayload(t, recorder.Body.Bytes())
	if payload.MonthlyQuotaMB != 4096 || payload.Preference != "cellular_first" {
		t.Fatalf("unexpected policy: %+v", payload)
	}
	if payload.Revision != 1 {
		t.Fatalf("revision = %d, want 1", payload.Revision)
	}
}

func TestConnectivityPublishRejectsInvalidPreference(t *testing.T) {
	handler := connectivityHandler{service: &connectivityTestService{}}
	body := `{"enabled":true,"preference":"bluetooth",` +
		`"monthly_quota_mb":2048,"low_quota_warn_percent":20,` +
		`"keepalive_seconds":300}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/connectivity/policy",
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

func TestConnectivityCapabilityIncludesReportedDevice(t *testing.T) {
	service := &connectivityTestService{
		revision: 2,
		policy:   connectivitydomain.Policy{Revision: 2, MonthlyQuotaMB: 2048},
		statuses: map[string]connectivitydomain.Status{
			"device-1": {
				DeviceID:       "device-1",
				Bearer:         connectivitydomain.BearerCellular,
				ESIMState:      connectivitydomain.ESIMActive,
				SignalPercent:  70,
				UsedMB:         2500,
				PolicyRevision: 2,
			},
		},
	}
	handler := connectivityHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/connectivity/capability",
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
		Data connectivitydomain.Capability `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Supported || !envelope.Data.UpToDate {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
}

type cellularPolicyPayloadResult struct {
	Enabled             bool   `json:"enabled"`
	RoamingAllowed      bool   `json:"roaming_allowed"`
	RemoteWakeEnabled   bool   `json:"remote_wake_enabled"`
	MeteredUploadLimit  bool   `json:"metered_upload_limit"`
	Preference          string `json:"preference"`
	MonthlyQuotaMB      int    `json:"monthly_quota_mb"`
	LowQuotaWarnPercent int    `json:"low_quota_warn_percent"`
	KeepaliveSeconds    int    `json:"keepalive_seconds"`
	Revision            int    `json:"revision"`
}

func decodeCellularPolicyPayload(
	t *testing.T,
	raw []byte,
) cellularPolicyPayloadResult {
	t.Helper()
	var envelope struct {
		Data cellularPolicyPayloadResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode cellular policy: %v", err)
	}
	return envelope.Data
}
