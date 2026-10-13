package http

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/session"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/usage"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/vision"
)

func TestHealthEndpoint(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	NewRouter(newTestRouterOptions()).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusOK, recorder.Code)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID response header")
	}
}

func TestReadyEndpointReportsRealtimeCapability(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		realtimeEnabled bool
		want            string
	}{
		{name: "disabled", realtimeEnabled: false, want: `"realtime_enabled":false`},
		{name: "enabled", realtimeEnabled: true, want: `"realtime_enabled":true`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			options := newTestRouterOptions()
			options.RealtimeEnabled = testCase.realtimeEnabled

			request := httptest.NewRequest(stdhttp.MethodGet, "/readyz", nil)
			recorder := httptest.NewRecorder()
			NewRouter(options).ServeHTTP(recorder, request)

			if recorder.Code != stdhttp.StatusOK {
				t.Fatalf("expected status %d, got %d", stdhttp.StatusOK, recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), testCase.want) {
				t.Fatalf(
					"ready response %s does not contain %s",
					recorder.Body.String(),
					testCase.want,
				)
			}
		})
	}
}

func TestInternalAPIIsHiddenWhenDisabled(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/internal/v1/runtime", nil)
	recorder := httptest.NewRecorder()

	NewRouter(newTestRouterOptions()).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusNotFound, recorder.Code)
	}
}

func TestInternalAPIRejectsMissingServiceToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}

	request := httptest.NewRequest(stdhttp.MethodGet, "/internal/v1/runtime", nil)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
	envelope := decodeEnvelope(t, recorder)
	if envelope.Error == nil || envelope.Error.Code != "unauthenticated" {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
}

func TestInternalAPIReturnsRuntimeEnvelope(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}

	request := httptest.NewRequest(stdhttp.MethodGet, "/internal/v1/runtime", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusOK, recorder.Code)
	}
	envelope := decodeEnvelope(t, recorder)
	if envelope.SchemaVersion != httpapi.SchemaVersion {
		t.Fatalf("unexpected schema version: %q", envelope.SchemaVersion)
	}
	if envelope.RequestID == "" {
		t.Fatal("expected response request ID")
	}
	if envelope.Error != nil {
		t.Fatalf("expected successful response, got error %+v", envelope.Error)
	}
}

func TestInternalAPIContractDocumentsRuntimeEndpoint(t *testing.T) {
	contract, err := os.ReadFile("../../../contracts/internal-api/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	if !bytes.Contains(contract, []byte("/internal/v1/runtime")) {
		t.Fatal("expected runtime endpoint in internal API contract")
	}
}

func TestInternalVoiceQualityEndpointReturnsReadOnlyMetrics(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.SessionMetrics = staticSessionMetrics{snapshots: []session.QualitySnapshot{
		{
			SessionID:       "session_quality",
			DeviceID:        "device_001",
			State:           session.StateListening,
			EchoConvergence: 512,
			NoiseFloor:      0.002,
			AgcGain:         1.5,
			VADConfidence:   640,
		},
	}}

	request := httptest.NewRequest(stdhttp.MethodGet, "/internal/v1/voice/quality", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", stdhttp.StatusOK, recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"echo_convergence":512`) ||
		!strings.Contains(body, `"vad_confidence":640`) {
		t.Fatalf("unexpected quality response: %s", body)
	}
}

type staticSessionMetrics struct {
	snapshots []session.QualitySnapshot
}

func (metrics staticSessionMetrics) QualitySnapshots() []session.QualitySnapshot {
	return metrics.snapshots
}

func TestInternalUsageEndpointRequiresServiceToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.UsageRecorder = &recordingUsageRepository{}

	request := httptest.NewRequest(
		stdhttp.MethodPost,
		"/internal/v1/usage/conversations",
		strings.NewReader(`{"device_id":"device-001"}`),
	)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

func TestInternalUsageEndpointRecordsConversation(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	usageRepository := &recordingUsageRepository{}
	options.UsageRecorder = usageRepository

	request := httptest.NewRequest(
		stdhttp.MethodPost,
		"/internal/v1/usage/conversations",
		strings.NewReader(`{
			"device_id": "device-001",
			"model": "model-a",
			"input_size": 12,
			"output_size": 34,
			"spent_usd": 0.0012
		}`),
	)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", stdhttp.StatusOK, recorder.Code, recorder.Body.String())
	}
	if usageRepository.record.DeviceID != "device-001" ||
		usageRepository.record.Model != "model-a" ||
		usageRepository.record.InputSize != 12 ||
		usageRepository.record.OutputSize != 34 ||
		usageRepository.record.SpentUSD != 0.0012 {
		t.Fatalf("unexpected usage record: %+v", usageRepository.record)
	}
}

func TestInternalVisionEndpointsRequireServiceToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.VisionService = &staticVisionService{}

	request := httptest.NewRequest(
		stdhttp.MethodPost,
		"/internal/v1/vision/tasks",
		strings.NewReader(`{"task_id":"task-123456"}`),
	)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

func TestInternalVisionAnalyzeReturnsBoundedResult(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.VisionService = &staticVisionService{
		result: vision.Result{
			TaskID:   "task-123456",
			DeviceID: "device-1234567",
			TaskType: vision.TaskTypeObjectRecognition,
			Labels:   []vision.Label{{Label: "apple", Confidence: 9000}},
			Summary:  "这是一颗苹果",
		},
	}

	request := httptest.NewRequest(
		stdhttp.MethodPost,
		"/internal/v1/vision/tasks",
		strings.NewReader(`{
			"task_id": "task-123456",
			"device_id": "device-1234567",
			"task_type": "object_recognition",
			"prompt": "这是什么",
			"frame_width": 640,
			"frame_height": 480,
			"frame_bytes": 131072
		}`),
	)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", stdhttp.StatusOK, recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"label":"apple"`) || !strings.Contains(body, `"confidence":9000`) {
		t.Fatalf("unexpected vision response: %s", body)
	}
}

func TestInternalVisionResultNotFound(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.VisionService = &staticVisionService{getErr: vision.ErrResultNotFound}

	request := httptest.NewRequest(
		stdhttp.MethodGet,
		"/internal/v1/vision/results/task-123456",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusNotFound, recorder.Code)
	}
	envelope := decodeEnvelope(t, recorder)
	if envelope.Error == nil || envelope.Error.Code != "vision_result_not_found" {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
}

func TestInternalVisionResultPassesDeviceScope(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	service := &recordingVisionService{}
	options.VisionService = service

	request := httptest.NewRequest(
		stdhttp.MethodGet,
		"/internal/v1/vision/results/task-123456?device_id=device-1234567",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", stdhttp.StatusOK, recorder.Code, recorder.Body.String())
	}
	if service.taskID != "task-123456" || service.deviceID != "device-1234567" {
		t.Fatalf("unexpected scoped lookup: task=%q device=%q", service.taskID, service.deviceID)
	}
}

func TestInternalVisionAnalyzeRejectsUnauthorizedDevice(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.VisionService = &staticVisionService{err: vision.ErrDeviceNotAuthorized}

	request := httptest.NewRequest(
		stdhttp.MethodPost,
		"/internal/v1/vision/tasks",
		strings.NewReader(`{
			"task_id": "task-123456",
			"device_id": "device-1234567",
			"task_type": "object_recognition",
			"frame_width": 640,
			"frame_height": 480,
			"frame_bytes": 131072
		}`),
	)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusForbidden {
		t.Fatalf("expected status %d, got %d: %s", stdhttp.StatusForbidden, recorder.Code, recorder.Body.String())
	}
	envelope := decodeEnvelope(t, recorder)
	if envelope.Error == nil || envelope.Error.Code != "vision_not_authorized" {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
}

type recordingVisionService struct {
	taskID   string
	deviceID string
}

func (s *recordingVisionService) Analyze(
	_ context.Context,
	_ vision.Request,
) (vision.Result, error) {
	return vision.Result{}, nil
}

func (s *recordingVisionService) GetResult(
	_ context.Context,
	taskID string,
	deviceID string,
) (vision.Result, error) {
	s.taskID = taskID
	s.deviceID = deviceID
	return vision.Result{TaskID: taskID, DeviceID: deviceID}, nil
}

type staticVisionService struct {
	result vision.Result
	err    error
	getErr error
}

func (s *staticVisionService) Analyze(
	_ context.Context,
	_ vision.Request,
) (vision.Result, error) {
	return s.result, s.err
}

func (s *staticVisionService) GetResult(
	_ context.Context,
	_ string,
	_ string,
) (vision.Result, error) {
	return s.result, s.getErr
}

type recordingUsageRepository struct {
	record usage.Record
}

func (r *recordingUsageRepository) Record(
	_ context.Context,
	record usage.Record,
) error {
	r.record = record
	return nil
}

type testEnvelope struct {
	SchemaVersion string             `json:"schema_version"`
	RequestID     string             `json:"request_id"`
	Error         *httpapi.ErrorBody `json:"error"`
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) testEnvelope {
	t.Helper()

	var envelope testEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response envelope: %v", err)
	}
	return envelope
}

func newTestRouterOptions() RouterOptions {
	return RouterOptions{
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	}
}
