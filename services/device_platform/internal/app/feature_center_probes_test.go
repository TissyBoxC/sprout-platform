package app

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	featureCenterDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	featureCenterService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	notificationDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
	"github.com/jackc/pgx/v5"
)

func TestPlaceholderHealthProbeFailsCoverageValidation(t *testing.T) {
	definitions := []featureCenterService.Definition{
		{
			Feature: featureCenterDomain.Feature{
				ID:     "recommendation",
				Status: featureCenterDomain.FeatureStatusActive,
			},
		},
	}
	err := featureCenterService.ValidateHealthProbeCoverage(
		definitions,
		map[string]featureCenterService.HealthProbe{
			"recommendation": placeholderHealthProbe{},
		},
	)
	if err == nil {
		t.Fatal("placeholder probe passed coverage validation")
	}
}

func TestSchemaReadinessProbeReturnsUnavailableWithoutDatabase(t *testing.T) {
	probe := schemaReadinessProbe(nil, "SELECT 1")
	status, err := probe.Check(context.Background(), "auth")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
	}
}

func TestSchemaReadinessStatusReturnsUnknownForReadableStorage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want featureCenterDomain.HealthStatus
	}{
		{name: "row returned", err: nil, want: featureCenterDomain.HealthUnknown},
		{name: "empty result", err: pgx.ErrNoRows, want: featureCenterDomain.HealthUnknown},
		{
			name: "query failed",
			err:  errors.New("relation does not exist"),
			want: featureCenterDomain.HealthUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := schemaReadinessStatus(test.err); got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHealthStatusRetainsAllFourStates(t *testing.T) {
	for _, status := range []featureCenterDomain.HealthStatus{
		featureCenterDomain.HealthHealthy,
		featureCenterDomain.HealthDegraded,
		featureCenterDomain.HealthUnavailable,
		featureCenterDomain.HealthUnknown,
	} {
		if !status.Valid() {
			t.Fatalf("health status %q is not valid", status)
		}
	}
}

func TestSchemaReadinessProbeSourceNeverFabricatesCapabilityHealth(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(".", "feature_center_probes.go"))
	if err != nil {
		t.Fatalf("read probe source: %v", err)
	}
	text := string(source)
	start := strings.Index(text, "func schemaReadinessProbe(")
	if start < 0 {
		t.Fatal("schemaReadinessProbe definition not found")
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatal("schemaReadinessProbe body not found")
	}
	body := text[start : start+end+3]
	if strings.Contains(body, "HealthHealthy") ||
		strings.Contains(body, "HealthDegraded") {
		t.Fatalf("schema readiness must not fabricate capability health:\n%s", body)
	}
	if !strings.Contains(body, "schemaReadinessStatus") {
		t.Fatalf("schema readiness must use the explicit status mapping:\n%s", body)
	}
}

// TestFeatureCenterProbeMapCoversEveryActiveFeature parses the real production
// assembly instead of a duplicated fixture. The v0.22.0 image registered
// app_update and voice_conversation without probes and then refused to start,
// so this test must fail before that composition can be published again.
func TestFeatureCenterProbeMapCoversEveryActiveFeature(t *testing.T) {
	appPath := filepath.Join(".", "app.go")
	source, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read app assembly: %v", err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), appPath, source, 0)
	if err != nil {
		t.Fatalf("parse app assembly: %v", err)
	}

	registered := make(map[string]struct{})
	for _, definition := range featureCenterService.DefaultRegistry() {
		if definition.Feature.Status == featureCenterDomain.FeatureStatusActive {
			registered[definition.Feature.ID] = struct{}{}
		}
	}

	assembled := make(map[string]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			return true
		}
		if index, ok := assignment.Lhs[0].(*ast.IndexExpr); ok {
			identifier, ok := index.X.(*ast.Ident)
			key, keyOK := index.Index.(*ast.BasicLit)
			if !ok || identifier.Name != "featureCenterHealthProbes" ||
				!keyOK || key.Kind != token.STRING {
				return true
			}
			featureID := strings.Trim(key.Value, `"`)
			assembled[featureID] = probeExpressionReady(assignment.Rhs[0])
			return false
		}
		identifier, ok := assignment.Lhs[0].(*ast.Ident)
		if !ok || identifier.Name != "featureCenterHealthProbes" {
			return true
		}
		literal, ok := assignment.Rhs[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, element := range literal.Elts {
			keyValue, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := keyValue.Key.(*ast.BasicLit)
			if !ok || key.Kind != token.STRING {
				continue
			}
			featureID := strings.Trim(key.Value, `"`)
			assembled[featureID] = probeExpressionReady(keyValue.Value)
		}
		return false
	})

	missing := make([]string, 0)
	for featureID := range registered {
		if ready, exists := assembled[featureID]; !exists || !ready {
			missing = append(missing, featureID)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("active features missing from production health probe map: %s", strings.Join(missing, ", "))
	}
}

// probeExpressionReady rejects an explicit assembly placeholder. A value that
// is replaced later in the composition root is still a real expression, so the
// final ValidateHealthProbeCoverage call remains responsible for runtime
// readiness after all delayed dependencies have been wired.
func probeExpressionReady(expression ast.Expr) bool {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return true
	}
	identifier, ok := literal.Type.(*ast.Ident)
	return !ok || identifier.Name != "placeholderHealthProbe"
}

func TestProbeExpressionReadyRejectsAssemblyPlaceholder(t *testing.T) {
	if probeExpressionReady(&ast.CompositeLit{
		Type: ast.NewIdent("placeholderHealthProbe"),
	}) {
		t.Fatal("assembly placeholder was accepted as a real health probe")
	}
	if !probeExpressionReady(ast.NewIdent("recommendationReadinessProbe")) {
		t.Fatal("delayed real health probe was rejected")
	}
}

func TestVoiceGatewayReadinessURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantValid bool
	}{
		{
			name:      "secure websocket",
			input:     "wss://voice.example.test/v1/voice?token=ignored",
			want:      "https://voice.example.test/readyz",
			wantValid: true,
		},
		{
			name:      "plain websocket",
			input:     "ws://127.0.0.1:8082/v1/voice",
			want:      "http://127.0.0.1:8082/readyz",
			wantValid: true,
		},
		{
			name:      "unsupported scheme",
			input:     "ftp://voice.example.test/v1/voice",
			wantValid: false,
		},
		{
			name:      "credentials rejected",
			input:     "wss://user:secret@voice.example.test/v1/voice",
			wantValid: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := voiceGatewayReadinessURL(test.input)
			if valid != test.wantValid {
				t.Fatalf("valid = %v, want %v", valid, test.wantValid)
			}
			if got != test.want {
				t.Fatalf("url = %q, want %q", got, test.want)
			}
		})
	}
}

func TestVoiceGatewayReadinessProbeReturnsUnknownWithoutConfiguration(t *testing.T) {
	probe := voiceGatewayReadinessProbe("", true, nil)
	status, err := probe.Check(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnknown {
		t.Fatalf("status = %q, want unknown", status)
	}
}

func TestVoiceGatewayReadinessProbeReturnsDegradedWithoutTokenIssuer(t *testing.T) {
	probe := voiceGatewayReadinessProbe(
		"wss://voice.example.test/v1/voice",
		false,
		nil,
	)
	status, err := probe.Check(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthDegraded {
		t.Fatalf("status = %q, want degraded", status)
	}
}

func TestVoiceGatewayReadinessProbeChecksRemoteReadyz(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/readyz" {
				t.Fatalf("path = %q, want /readyz", request.URL.Path)
			}
			response.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	probe := voiceGatewayReadinessProbe(server.URL+"/v1/voice", true, server.Client())
	status, err := probe.Check(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthHealthy {
		t.Fatalf("status = %q, want healthy", status)
	}
}

func TestVoiceGatewayReadinessProbeDegradesWhenRealtimeDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte(`{"status":"ready","realtime_enabled":false}`))
		},
	))
	defer server.Close()

	probe := voiceGatewayReadinessProbe(server.URL+"/v1/voice", true, server.Client())
	status, err := probe.Check(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthDegraded {
		t.Fatalf("status = %q, want degraded", status)
	}
}

func TestVoiceGatewayReadinessProbeTreatsLegacyReadyzAsHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte(`{"status":"ready"}`))
		},
	))
	defer server.Close()

	probe := voiceGatewayReadinessProbe(server.URL+"/v1/voice", true, server.Client())
	status, err := probe.Check(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthHealthy {
		t.Fatalf("status = %q, want healthy", status)
	}
}

func TestVoiceGatewayReadinessProbeMapsRemoteFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusServiceUnavailable)
		},
	))
	defer server.Close()

	probe := voiceGatewayReadinessProbe(server.URL+"/v1/voice", true, server.Client())
	status, err := probe.Check(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
	}
}

func TestVoiceGatewayReadinessProbeMapsNetworkFailure(t *testing.T) {
	probe := voiceGatewayReadinessProbe(
		"http://127.0.0.1:1/v1/voice",
		true,
		&http.Client{Timeout: 100 * time.Millisecond},
	)
	status, err := probe.Check(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
	}
}

type stubRecommendationProbe struct {
	err error
}

func (stub stubRecommendationProbe) CheckReadiness(context.Context) error {
	return stub.err
}

func TestRecommendationReadinessProbeAcceptsEmptyHistory(t *testing.T) {
	probe := recommendationReadinessProbe(stubRecommendationProbe{})
	status, err := probe.Check(context.Background(), "recommendation")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthHealthy {
		t.Fatalf("status = %q, want healthy", status)
	}
}

func TestRecommendationReadinessProbeMapsCatalogFailure(t *testing.T) {
	probe := recommendationReadinessProbe(stubRecommendationProbe{
		err: errors.New("catalog unavailable"),
	})
	status, err := probe.Check(context.Background(), "recommendation")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
	}
}

type stubNotificationProbe struct {
	health notificationDomain.DeliveryHealth
	err    error
}

func (stub stubNotificationProbe) DeliveryHealth(
	context.Context,
) (notificationDomain.DeliveryHealth, error) {
	return stub.health, stub.err
}

func TestNotificationReadinessProbeHealthyWithoutExternalSenders(t *testing.T) {
	probe := notificationReadinessProbe(stubNotificationProbe{})
	status, err := probe.Check(context.Background(), "notification")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthHealthy {
		t.Fatalf("status = %q, want healthy", status)
	}
}

func TestNotificationReadinessProbeDegradesOnFailedDelivery(t *testing.T) {
	probe := notificationReadinessProbe(stubNotificationProbe{
		health: notificationDomain.DeliveryHealth{FailedCount: 1},
	})
	status, err := probe.Check(context.Background(), "notification")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthDegraded {
		t.Fatalf("status = %q, want degraded", status)
	}
}

func TestNotificationReadinessProbeUnavailableOnRepositoryFailure(t *testing.T) {
	probe := notificationReadinessProbe(stubNotificationProbe{
		err: errors.New("database unavailable"),
	})
	status, err := probe.Check(context.Background(), "notification")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
	}
}
