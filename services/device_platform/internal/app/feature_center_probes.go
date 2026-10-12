package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	featureCenterDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	featureCenterService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// nonzeroRowProbe treats an unprotected table read as the health signal for a
// feature. It returns degraded rather than unavailable when the table is empty,
// because "no rows yet" is a valid early-lifecycle state and never a failure.
// A missing table or a failed connection reports unavailable so the console
// never shows a healthy capability backed by a broken schema.
func nonzeroRowProbe(
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
				// The table exists but has no rows yet: the capability is
				// deployed and reachable, just not populated.
				return featureCenterDomain.HealthDegraded, nil
			}
			if err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			return featureCenterDomain.HealthHealthy, nil
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
