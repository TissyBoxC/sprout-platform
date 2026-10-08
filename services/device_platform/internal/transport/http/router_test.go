package http

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/config"
	gatewayservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/service"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	releasestoreservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
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

func TestHealthEndpointReturnsTraceParent(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	NewRouter(newTestRouterOptions()).ServeHTTP(recorder, request)

	if recorder.Header().Get("traceparent") == "" {
		t.Fatal("expected traceparent response header")
	}
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
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
}

func TestInternalAPIContractDocumentsRuntimeEndpoint(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	if !bytes.Contains(contract, []byte("/internal/v1/runtime")) {
		t.Fatal("expected runtime endpoint in internal API contract")
	}
}

// The relay resolves AI credentials by device id, so the contract must keep the
// device-scoped endpoint documented alongside the runtime probe.
func TestInternalAPIContractDocumentsDeviceCredentialEndpoint(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	expected := "/internal/v1/devices/{device_id}/ai-credential"
	if !bytes.Contains(contract, []byte(expected)) {
		t.Fatalf("expected %s in internal API contract", expected)
	}
}

func TestInternalAPIContractDocumentsEffectiveParentPolicyEndpoint(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	expected := "/internal/v1/parent-policies"
	if !bytes.Contains(contract, []byte(expected)) {
		t.Fatalf("expected %s in internal API contract", expected)
	}
	if !bytes.Contains(contract, []byte("EffectiveParentPolicyEnvelope")) {
		t.Fatal("expected effective parent policy schema in internal API contract")
	}
}

func TestInternalCredentialRouteIsHiddenWhenDisabled(t *testing.T) {
	request := httptest.NewRequest(
		stdhttp.MethodGet,
		"/internal/v1/devices/device-0001/ai-credential",
		nil,
	)
	recorder := httptest.NewRecorder()

	NewRouter(newTestRouterOptions()).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusNotFound, recorder.Code)
	}
}

func TestInternalCredentialRouteRequiresServiceToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.BindingService = &bindingservice.Service{}
	options.AIService = &gatewayservice.Service{}

	request := httptest.NewRequest(
		stdhttp.MethodGet,
		"/internal/v1/devices/device-0001/ai-credential",
		nil,
	)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

func TestInternalReleaseUploadRequiresReleaseToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:            true,
		AuthToken:          strings.Repeat("t", 32),
		ReleaseUploadToken: strings.Repeat("r", 32),
	}
	options.ReleaseStoreService = new(releasestoreservice.Service)

	request := newInternalReleaseUploadRequest(t)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

func TestInternalReleaseUploadRejectsGeneralServiceToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:            true,
		AuthToken:          strings.Repeat("t", 32),
		ReleaseUploadToken: strings.Repeat("r", 32),
	}
	options.ReleaseStoreService = new(releasestoreservice.Service)

	request := newInternalReleaseUploadRequest(t)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

func TestInternalReleaseUploadRouteIsHiddenWithoutReleaseToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.ReleaseStoreService = new(releasestoreservice.Service)

	request := newInternalReleaseUploadRequest(t)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusNotFound, recorder.Code)
	}
}

func TestPublicReleasePublicationRouteRequiresReleaseToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:            true,
		AuthToken:          strings.Repeat("t", 32),
		ReleaseUploadToken: strings.Repeat("r", 32),
	}
	options.ReleaseStoreService = new(releasestoreservice.Service)

	request := newInternalReleaseUploadRequest(t)
	request.URL.Path = "/api/v1/release-publication/files"
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

type testEnvelope struct {
	SchemaVersion string             `json:"schema_version"`
	RequestID     string             `json:"request_id"`
	Error         *httpapi.ErrorBody `json:"error"`
}

func newInternalReleaseUploadRequest(t *testing.T) *stdhttp.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("version", "0.12.4"); err != nil {
		t.Fatalf("write version field: %v", err)
	}
	if err := writer.WriteField("channel", "stable"); err != nil {
		t.Fatalf("write channel field: %v", err)
	}
	if err := writer.WriteField("platform", "any"); err != nil {
		t.Fatalf("write platform field: %v", err)
	}
	if err := writer.WriteField("kind", "resource"); err != nil {
		t.Fatalf("write kind field: %v", err)
	}
	if err := writer.WriteField("filename", "resource.bin"); err != nil {
		t.Fatalf("write filename field: %v", err)
	}
	filePart, err := writer.CreateFormFile("file", "resource.bin")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := filePart.Write([]byte("release bytes")); err != nil {
		t.Fatalf("write file field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(
		stdhttp.MethodPost,
		"/internal/v1/release-files",
		&body,
	)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
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
	return RouterOptions{Logger: newTestLogger()}
}
