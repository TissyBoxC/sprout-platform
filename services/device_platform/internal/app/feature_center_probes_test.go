package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	featureCenterDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	notificationDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
)

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
