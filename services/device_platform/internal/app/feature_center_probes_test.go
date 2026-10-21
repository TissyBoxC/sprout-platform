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
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	featureCenterDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	featureCenterService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	memoryDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/domain"
	notificationDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
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

func TestMapReadinessStatusRejectsUnknownVocabulary(t *testing.T) {
	tests := []struct {
		status string
		want   featureCenterDomain.HealthStatus
	}{
		{status: "healthy", want: featureCenterDomain.HealthHealthy},
		{status: "degraded", want: featureCenterDomain.HealthDegraded},
		{status: "unavailable", want: featureCenterDomain.HealthUnavailable},
		{status: "unknown", want: featureCenterDomain.HealthUnknown},
		{status: "", want: featureCenterDomain.HealthUnavailable},
		{status: "ready", want: featureCenterDomain.HealthUnavailable},
	}
	for _, test := range tests {
		if got := mapReadinessStatus(test.status); got != test.want {
			t.Fatalf(
				"mapReadinessStatus(%q) = %q, want %q",
				test.status,
				got,
				test.want,
			)
		}
	}
}

func TestServiceReadinessProbeMapsServiceStateAndFailure(t *testing.T) {
	probe := serviceReadinessProbe(
		func(context.Context) (string, error) { return "degraded", nil },
		func(status string) string { return status },
	)
	status, err := probe.Check(context.Background(), "auth")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthDegraded {
		t.Fatalf("status = %q, want degraded", status)
	}

	probe = serviceReadinessProbe(
		func(context.Context) (string, error) {
			return "healthy", errors.New("database read failed")
		},
		func(status string) string { return status },
	)
	status, err = probe.Check(context.Background(), "auth")
	if err != nil {
		t.Fatalf("check failing probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
	}
}

func TestDeviceCapabilityReadinessProbeRetainsUnknown(t *testing.T) {
	probe := deviceCapabilityReadinessProbe(
		func(context.Context) (featurehealth.DeviceCapabilityReadiness, error) {
			return featurehealth.DeviceCapabilityReadiness{
				Status: featurehealth.StatusUnknown,
			}, nil
		},
	)
	status, err := probe.Check(context.Background(), "vision")
	if err != nil {
		t.Fatalf("check probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnknown {
		t.Fatalf("status = %q, want unknown", status)
	}
}

func TestDeviceCapabilityReadinessProbeMapsUnavailableAndFailure(t *testing.T) {
	probe := deviceCapabilityReadinessProbe(
		func(context.Context) (featurehealth.DeviceCapabilityReadiness, error) {
			return featurehealth.DeviceCapabilityReadiness{
				Status: featurehealth.StatusUnavailable,
			}, nil
		},
	)
	status, err := probe.Check(context.Background(), "device_runtime")
	if err != nil {
		t.Fatalf("check unavailable probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
	}

	probe = deviceCapabilityReadinessProbe(
		func(context.Context) (featurehealth.DeviceCapabilityReadiness, error) {
			return featurehealth.DeviceCapabilityReadiness{
				Status: featurehealth.StatusHealthy,
			}, errors.New("query failed")
		},
	)
	status, err = probe.Check(context.Background(), "device_runtime")
	if err != nil {
		t.Fatalf("check failing probe: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
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

// TestCoverageValidationRunsAfterEveryProbeIsResolved guards the ordering that
// a real deployment depends on. Probes are registered in two phases: a block of
// service-backed probes, then another block once later services exist. The
// coverage check rejects any active feature whose probe is still a placeholder,
// so running it between the two phases refuses to start the service even though
// every probe is eventually registered. Source inspection is enough here because
// the failure is purely positional and needs no database.
func TestCoverageValidationRunsAfterEveryProbeIsResolved(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("read app.go: %v", err)
	}
	body := string(source)

	validationAt := strings.Index(body, "featureCenterService.ValidateHealthProbeCoverage(")
	if validationAt < 0 {
		t.Fatal("coverage validation call not found in app.go")
	}

	// The last assignment wins: every probe must already be resolved by then.
	lastAssignmentAt := strings.LastIndex(body, "featureCenterHealthProbes[")
	if lastAssignmentAt < 0 {
		t.Fatal("no probe assignments found in app.go")
	}
	if validationAt < lastAssignmentAt {
		t.Fatalf(
			"coverage validation at offset %d runs before the last probe "+
				"assignment at offset %d; placeholders would fail startup",
			validationAt, lastAssignmentAt,
		)
	}
}

// TestRouterReceivesEveryConstructedService guards against a service being
// created, health-probed and then never handed to the transport. Such a
// service looks fully wired in tests that exercise it directly while the HTTP
// surface silently answers 404, which is exactly how the billing routes went
// missing in a release. The check is textual because the failure is an absent
// field, not a behaviour a unit test can observe.
func TestRouterReceivesEveryConstructedService(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("read app.go: %v", err)
	}
	body := string(source)

	optionsAt := strings.Index(body, "platformhttp.RouterOptions{")
	if optionsAt < 0 {
		t.Fatal("RouterOptions literal not found in app.go")
	}
	routerOptions := body[optionsAt:]

	// Every service constructed with `X, err := <pkg>.New(` must appear in the
	// router options, either directly or through a wrapper such as Billing.
	// Outbound clients share that constructor shape but are deliberately absent
	// from the transport surface, so they are named here instead of guessed.
	outbound := map[string]bool{"aiClient": true, "mqttClient": true}
	constructed := regexp.MustCompile(`(?m)^\t([a-zA-Z][a-zA-Z0-9]*), err := [a-zA-Z0-9_]+\.New\(`)
	for _, match := range constructed.FindAllStringSubmatch(body[:optionsAt], -1) {
		name := match[1]
		if outbound[name] {
			continue
		}
		if !strings.Contains(routerOptions, " "+name+",\n") &&
			!strings.Contains(routerOptions, " "+name+"{") &&
			!strings.Contains(routerOptions, "{ "+name+" }") &&
			!strings.Contains(routerOptions, ": "+name+",\n") &&
			!strings.Contains(routerOptions, ": "+name+"{") {
			t.Errorf(
				"service %q is constructed but never passed to RouterOptions",
				name,
			)
		}
	}
}

// memoryReadinessStub is a scripted memory readiness source.
type memoryReadinessStub struct {
	readiness memoryDomain.Readiness
	err       error
}

// Readiness returns the scripted snapshot.
func (stub memoryReadinessStub) Readiness(
	context.Context,
) (memoryDomain.Readiness, error) {
	return stub.readiness, stub.err
}

func TestChildMemoryReadinessProbeReportsHealthyOnEmptyStore(t *testing.T) {
	probe := childMemoryReadinessProbe(memoryReadinessStub{
		readiness: memoryDomain.Readiness{State: memoryDomain.ReadinessHealthy},
	})
	status, err := probe.Check(context.Background(), "child_memory")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if status != featureCenterDomain.HealthHealthy {
		t.Fatalf("status = %q, want healthy", status)
	}
}

func TestChildMemoryReadinessProbeDegradesAtTheChildCap(t *testing.T) {
	probe := childMemoryReadinessProbe(memoryReadinessStub{
		readiness: memoryDomain.Readiness{
			State:               memoryDomain.ReadinessDegraded,
			OverLimitChildCount: 1,
		},
	})
	status, err := probe.Check(context.Background(), "child_memory")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if status != featureCenterDomain.HealthDegraded {
		t.Fatalf("status = %q, want degraded", status)
	}
}

func TestChildMemoryReadinessProbeUnavailableOnReadFailure(t *testing.T) {
	probe := childMemoryReadinessProbe(memoryReadinessStub{
		err: errors.New("relation child_memories does not exist"),
	})
	status, err := probe.Check(context.Background(), "child_memory")
	if err != nil {
		t.Fatalf("check must not surface the storage error: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
	}
}

func TestChildMemoryReadinessProbeUnavailableWithoutService(t *testing.T) {
	probe := childMemoryReadinessProbe(nil)
	status, err := probe.Check(context.Background(), "child_memory")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if status != featureCenterDomain.HealthUnavailable {
		t.Fatalf("status = %q, want unavailable", status)
	}
}
