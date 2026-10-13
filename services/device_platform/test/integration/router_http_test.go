// Package integration exercises the device platform HTTP transport end to end
// through the real router. These cases run the actual ServeMux the composition
// root mounts, so route registration, middleware ordering, and the shared
// response envelope are covered together.
//
// The cases here deliberately depend on no external service (PostgreSQL,
// Redis, or MQTT): the health, readiness, and internal API token boundary are
// fully determined by the router itself. That keeps the integration suite
// runnable on a developer machine when the infrastructure stack is down.
package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/config"
	platformhttp "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/transport/http"
)

const serviceToken = "integration-service-token"

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	return platformhttp.NewRouter(platformhttp.RouterOptions{
		InternalAPIConfig: config.InternalAPIConfig{
			Enabled:   true,
			AuthToken: serviceToken,
		},
	})
}

// TestHealthzReportsRunningService proves the liveness route is always present
// and does not require a database or internal token.
func TestHealthzReportsRunningService(t *testing.T) {
	server := httptest.NewServer(newTestRouter(t))
	defer server.Close()

	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", response.StatusCode)
	}

	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("health status = %q, want ok", body.Status)
	}
	if body.Service != "device-platform" {
		t.Fatalf("health service = %q, want device-platform", body.Service)
	}
}

// TestReadyzReportsReady proves the readiness route answers with the ready
// envelope instead of leaking internal dependency state.
func TestReadyzReportsReady(t *testing.T) {
	server := httptest.NewServer(newTestRouter(t))
	defer server.Close()

	response, err := http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /readyz status = %d, want 200", response.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode /readyz: %v", err)
	}
	if body.Status != "ready" {
		t.Fatalf("ready status = %q, want ready", body.Status)
	}
}

// TestInternalAPIRouteRequiresServiceToken proves the internal API boundary
// fails closed: an anonymous caller must receive the shared unauthenticated
// envelope before the handler runs.
func TestInternalAPIRouteRequiresServiceToken(t *testing.T) {
	server := httptest.NewServer(newTestRouter(t))
	defer server.Close()

	response, err := http.Get(server.URL + "/internal/v1/runtime")
	if err != nil {
		t.Fatalf("GET /internal/v1/runtime: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous internal status = %d, want 401", response.StatusCode)
	}

	var body struct {
		SchemaVersion string `json:"schema_version"`
		Error         *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode internal error: %v", err)
	}
	if body.SchemaVersion == "" {
		t.Fatal("internal error envelope is missing schema_version")
	}
	if body.Error == nil || body.Error.Code != "unauthenticated" {
		t.Fatalf("internal error code = %v, want unauthenticated", body.Error)
	}
}

// TestInternalAPIAcceptsValidServiceToken proves the same boundary lets a
// correctly authenticated service through to the real handler.
func TestInternalAPIAcceptsValidServiceToken(t *testing.T) {
	server := httptest.NewServer(newTestRouter(t))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/internal/v1/runtime", nil)
	if err != nil {
		t.Fatalf("build internal request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+serviceToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /internal/v1/runtime: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated internal status = %d, want 200", response.StatusCode)
	}

	var body struct {
		SchemaVersion string `json:"schema_version"`
		Data          struct {
			Service string `json:"service"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode internal success: %v", err)
	}
	if body.SchemaVersion == "" {
		t.Fatal("internal success envelope is missing schema_version")
	}
	if body.Data.Service != "device-platform" || body.Data.Status != "running" {
		t.Fatalf("runtime payload = %+v, want running device-platform", body.Data)
	}
}
