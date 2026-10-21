package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	billingchannel "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
	billingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	featureCenterDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	featureCenterService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	memorydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/domain"
	growthReportDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/growthreport/domain"
	notificationDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
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

// serviceReadinessProbe projects one service-owned readiness method into the
// Feature Center contract. A returned error is always unavailable because an
// unreadable authoritative source cannot support a healthy or degraded claim.
// The method is generic over each module's readiness type while statusOf keeps
// the module status vocabulary inside this assembly boundary.
func serviceReadinessProbe[T any](
	check func(context.Context) (T, error),
	statusOf func(T) string,
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if check == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			readiness, err := check(ctx)
			if err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			return mapReadinessStatus(statusOf(readiness)), nil
		},
	)
}

// deviceCapabilityReadinessProbe projects aggregate hardware readiness. Unlike
// the generic adapter it preserves the explicit "unknown" state used when no
// device advertises a capability yet.
func deviceCapabilityReadinessProbe(
	check func(context.Context) (featurehealth.DeviceCapabilityReadiness, error),
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if check == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			readiness, err := check(ctx)
			if err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			switch readiness.Status {
			case featurehealth.StatusHealthy:
				return featureCenterDomain.HealthHealthy, nil
			case featurehealth.StatusDegraded:
				return featureCenterDomain.HealthDegraded, nil
			case featurehealth.StatusUnknown:
				return featureCenterDomain.HealthUnknown, nil
			default:
				return featureCenterDomain.HealthUnavailable, nil
			}
		},
	)
}

// mapReadinessStatus keeps feature modules on one small state vocabulary. Any
// future state must be added here and covered by tests rather than silently
// becoming healthy through a default branch.
func mapReadinessStatus(status string) featureCenterDomain.HealthStatus {
	switch status {
	case "healthy":
		return featureCenterDomain.HealthHealthy
	case "degraded":
		return featureCenterDomain.HealthDegraded
	case "unknown":
		return featureCenterDomain.HealthUnknown
	case "unavailable":
		return featureCenterDomain.HealthUnavailable
	default:
		return featureCenterDomain.HealthUnavailable
	}
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

// billingOrderBacklogProbe reports order health from the expiry backlog: an
// order past its payment deadline that the sweep has not closed yet is a real
// operational problem, while pending orders still inside their window are not.
func billingOrderBacklogProbe(
	service interface {
		Backlog(ctx context.Context) (int, int, error)
	},
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if service == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			_, overdue, err := service.Backlog(ctx)
			if err != nil {
				// An unreadable backlog is not a healthy or degraded claim.
				return featureCenterDomain.HealthUnavailable, nil
			}
			if overdue > 0 {
				return featureCenterDomain.HealthDegraded, nil
			}
			return featureCenterDomain.HealthHealthy, nil
		},
	)
}

// billingPaymentChannelProbe reports the worst state across every registered
// provider. A channel that is absent, errors, or reports an unknown status is
// unavailable: the payment feature must never read healthy while no provider
// can actually collect money (design doc §4).
func billingPaymentChannelProbe(
	channels interface {
		Health(ctx context.Context) map[string]billingchannel.HealthStatus
	},
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if channels == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			statuses := channels.Health(ctx)
			if len(statuses) == 0 {
				return featureCenterDomain.HealthUnavailable, nil
			}
			worst := featureCenterDomain.HealthHealthy
			for _, status := range statuses {
				switch status {
				case billingchannel.HealthStatusHealthy:
					continue
				case billingchannel.HealthStatusDegraded:
					worst = featureCenterDomain.HealthDegraded
				default:
					return featureCenterDomain.HealthUnavailable, nil
				}
			}
			return worst, nil
		},
	)
}

// billingReconciliationProbe reports the most recent reconciliation run. No
// run yet and a failed run both leave the day unverified, a completed run with
// a non-zero difference needs an operator, and only a completed zero-difference
// run is healthy (design doc §7).
func billingReconciliationProbe(
	service interface {
		LatestRun(ctx context.Context) (*billingdomain.ReconciliationRun, error)
	},
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if service == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			run, err := service.LatestRun(ctx)
			if err != nil || run == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			if run.Status != billingdomain.ReconciliationStatusCompleted {
				return featureCenterDomain.HealthUnavailable, nil
			}
			if run.DifferenceCents != 0 {
				return featureCenterDomain.HealthDegraded, nil
			}
			return featureCenterDomain.HealthHealthy, nil
		},
	)
}

// familySpendingLimitProbe verifies that the cap and rejection tables are
// readable. It is a real read, not a configuration inspection: if the migration
// is missing, the guardian cannot set a cap and the checkout guard silently
// fails, so the console must show the feature as unavailable rather than
// healthy.
func familySpendingLimitProbe(
	service interface {
		Readiness(ctx context.Context) error
	},
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if service == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			if err := service.Readiness(ctx); err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			return featureCenterDomain.HealthHealthy, nil
		},
	)
}

// childMemoryReadinessProbe verifies that the child memory table is readable.
//
// It is a real read, not a configuration inspection: if migration
// 0079_child_memories is missing, guardians cannot see a child's memories and
// devices cannot sync them, so the console must show the feature as
// unavailable rather than healthy. The probe reports degraded when a child has
// saturated the per-child cap, because new facts for that child are refused.
func childMemoryReadinessProbe(
	service interface {
		Readiness(ctx context.Context) (memorydomain.Readiness, error)
	},
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if service == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			return mapMemoryReadiness(ctx, service)
		},
	)
}

// mapMemoryReadiness keeps the readiness-to-health mapping in one place so the
// probe and its tests cannot drift.
func mapMemoryReadiness(
	ctx context.Context,
	service interface {
		Readiness(ctx context.Context) (memorydomain.Readiness, error)
	},
) (featureCenterDomain.HealthStatus, error) {
	readiness, err := service.Readiness(ctx)
	if err != nil {
		return featureCenterDomain.HealthUnavailable, nil
	}
	return mapReadinessStatus(string(readiness.State)), nil
}

// growthReportReadinessProbe verifies that reports are actually being produced.
//
// It reads the report table instead of trusting the generator's process state:
// the worker can be alive while its queries fail, and a console that showed the
// feature healthy while every period produced nothing would hide exactly the
// fault an operator needs to see.
func growthReportReadinessProbe(
	service interface {
		Readiness(ctx context.Context) (growthReportDomain.Readiness, error)
	},
) featureCenterService.HealthProbe {
	return featureCenterService.HealthProbeFunc(
		func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
			if service == nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			readiness, err := service.Readiness(ctx)
			if err != nil {
				return featureCenterDomain.HealthUnavailable, nil
			}
			return mapReadinessStatus(string(readiness.State)), nil
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
