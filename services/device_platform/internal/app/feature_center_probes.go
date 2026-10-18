package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	featureCenterDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	featureCenterService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	notificationDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// placeholderHealthProbe is used only during assembly for probes whose real
// implementation is created later in application wiring. It deliberately
// declares itself unready so ValidateHealthProbeCoverage rejects any that are
// still present when the feature center is finally constructed.
type placeholderHealthProbe struct{}

func (placeholderHealthProbe) Check(
	context.Context,
	string,
) (featureCenterDomain.HealthStatus, error) {
	return featureCenterDomain.HealthUnknown, nil
}

func (placeholderHealthProbe) HealthProbeReady() bool {
	return false
}

// schemaReadinessProbe verifies that a feature's authoritative storage is
// deployed and queryable. It intentionally does not claim that the feature is
// healthy just because one row exists: an empty table is the normal first-run
// state, while a missing table or failed query is unavailable.
func schemaReadinessProbe(
	pool *pgxpool.Pool,
	query string,
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if pool == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			var marker int
			err := pool.QueryRow(ctx, query).Scan(&marker)
			if errors.Is(err, pgx.ErrNoRows) {
				// The schema and query are valid, but there is no runtime
				// evidence yet that the capability works.
				return featureCenterDomain.HealthDegraded, nil
			}
			if err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			// A successful read proves deployment, not functional health.
			// Only a capability-specific probe may report healthy.
			return featureCenterDomain.HealthDegraded, nil
		},
	)
}

// voiceGatewayReadinessProbe checks the remote voice gateway's readiness
// endpoint instead of treating local signing configuration as proof that the
// realtime service is reachable. The public WebSocket URL is converted to its
// HTTP origin because /readyz is served by the same gateway process.
func voiceGatewayReadinessProbe(
	webSocketURL string,
	tokenIssuerConfigured bool,
	httpClient *http.Client,
) featureCenterService.HealthProbe {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if strings.TrimSpace(webSocketURL) == "" {
				return featureCenterDomain.HealthUnknown, nil
			}
			if !tokenIssuerConfigured {
				return featureCenterDomain.HealthDegraded, nil
			}
			readinessURL, ok := voiceGatewayReadinessURL(webSocketURL)
			if !ok {
				return featureCenterDomain.HealthUnknown, nil
			}
			request, err := http.NewRequestWithContext(
				ctx,
				http.MethodGet,
				readinessURL,
				nil,
			)
			if err != nil {
				return featureCenterDomain.HealthUnknown, nil
			}
			response, err := httpClient.Do(request)
			if err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			defer response.Body.Close()
			if response.StatusCode < http.StatusOK ||
				response.StatusCode >= http.StatusMultipleChoices {
				return featureCenterDomain.HealthUnavailable, nil
			}
			// The gateway only exposes the child voice session when the
			// realtime endpoint is explicitly enabled. A reachable process with
			// that endpoint disabled must degrade rather than report healthy,
			// otherwise the console would claim a capability devices cannot use.
			var probe struct {
				RealtimeEnabled *bool `json:"realtime_enabled"`
			}
			if err := json.NewDecoder(response.Body).Decode(&probe); err == nil &&
				probe.RealtimeEnabled != nil &&
				!*probe.RealtimeEnabled {
				return featureCenterDomain.HealthDegraded, nil
			}
			return featureCenterDomain.HealthHealthy, nil
		},
	)
}

// voiceGatewayReadinessURL maps a public WebSocket endpoint to the HTTP
// readiness URL on the same origin. It deliberately drops the WebSocket path
// and query because /readyz is a process-level endpoint.
func voiceGatewayReadinessURL(rawWebSocketURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawWebSocketURL))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	case "http", "https":
	default:
		return "", false
	}
	parsed.Path = "/readyz"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), true
}

// recommendationReadinessProbe verifies the recommendation catalog without a
// family scope. This separates "no playback history yet" from "recommendation
// unavailable": an empty catalog is a healthy first-deployment state, while a
// catalog read failure is not.
func recommendationReadinessProbe(
	service interface {
		CheckReadiness(ctx context.Context) error
	},
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if service == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			if err := service.CheckReadiness(ctx); err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			return featureCenterDomain.HealthHealthy, nil
		},
	)
}

// notificationReadinessProbe validates the durable notification store without
// treating optional external senders as a prerequisite. In-app notifications
// are built in; failed delivery rows are the actionable degradation signal.
func notificationReadinessProbe(
	service interface {
		DeliveryHealth(ctx context.Context) (notificationDomain.DeliveryHealth, error)
	},
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if service == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			health, err := service.DeliveryHealth(ctx)
			if err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			if health.FailedCount > 0 {
				return featureCenterDomain.HealthDegraded, nil
			}
			return featureCenterDomain.HealthHealthy, nil
		},
	)
}
