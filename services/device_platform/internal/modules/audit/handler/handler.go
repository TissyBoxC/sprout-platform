// Package handler maps authenticated diagnostic requests onto the audit
// application service. It never returns raw persistence errors to operators.
package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
)

// Service is the narrow diagnostic surface required by the transport layer.
type Service interface {
	Get(ctx context.Context, deviceID string, limit int) (*domain.Snapshot, error)
	GetProvisioning(
		ctx context.Context,
		deviceID string,
		limit int,
	) (*domain.ProvisioningSnapshot, error)
}

// Handler serves administrator-facing diagnostic history.
type Handler struct {
	service Service
}

// New creates an audit HTTP handler.
func New(service Service) *Handler {
	return &Handler{service: service}
}

// GetDiagnostics handles GET /api/v1/admin/devices/{device_id}/diagnostics.
//
// The route is mounted behind requireAdmin. A device with no diagnostic
// records is a normal empty state for older firmware, so the response keeps
// the requested device id and reports the unknown health state instead of
// exposing an internal not-found error.
func (h *Handler) GetDiagnostics(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID := request.PathValue("device_id")
	limit := parseDiagnosticLimit(request.URL.Query().Get("limit"))
	snapshot, err := h.service.Get(request.Context(), deviceID, limit)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			httpapi.WriteSuccess(response, request, http.StatusOK, map[string]any{
				"device_id":          deviceID,
				"health_state":       domain.HealthUnknown,
				"boot_events":        []domain.BootEvent{},
				"failures":           []domain.ModuleFailure{},
				"recovery_events":    []domain.RecoveryEvent{},
				"interaction_events": []domain.InteractionEvent{},
				"latest_failure":     nil,
				"error_count":        0,
				"recovery_count":     0,
				// The empty state is a valid contract response, so it carries
				// the same retention metadata as a non-empty history.
				"retention_boot_events":        domain.RetentionBootEvents,
				"retention_failures":           domain.RetentionFailures,
				"retention_recovery_events":    domain.RetentionRecovery,
				"retention_interaction_events": domain.RetentionInteraction,
			})
			return
		}
		httpapi.WriteError(
			response,
			request,
			http.StatusInternalServerError,
			"service_error",
			"暂时无法读取设备诊断",
			false,
		)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, snapshot)
}

func parseDiagnosticLimit(raw string) int {
	const (
		defaultLimit = 20
		maxLimit     = 100
	)
	if raw == "" {
		return defaultLimit
	}
	value := 0
	for _, character := range raw {
		if character < '0' || character > '9' {
			return defaultLimit
		}
		value = value*10 + int(character-'0')
		if value > maxLimit {
			return maxLimit
		}
	}
	if value <= 0 {
		return defaultLimit
	}
	return value
}

// GetProvisioning handles
// GET /api/v1/admin/devices/{device_id}/provisioning.
//
// The route is mounted behind requireAdmin. A device that has never reported a
// provisioning payload is a normal empty state, so the response keeps the
// requested device id and reports the unprovisioned defaults instead of
// exposing an internal not-found error.
func (h *Handler) GetProvisioning(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID := request.PathValue("device_id")
	limit := parseDiagnosticLimit(request.URL.Query().Get("limit"))
	snapshot, err := h.service.GetProvisioning(request.Context(), deviceID, limit)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			httpapi.WriteSuccess(response, request, http.StatusOK, map[string]any{
				"device_id":                     deviceID,
				"state":                         domain.ProvisioningStateUnprovisioned,
				"wifi_configured":               false,
				"session_state":                 domain.SessionStateReady,
				"last_provisioned_at":           nil,
				"newest_sequence":               0,
				"dropped_events":                0,
				"events":                        []domain.ProvisioningEvent{},
				"retention_provisioning_events": domain.RetentionProvisioning,
			})
			return
		}
		httpapi.WriteError(
			response,
			request,
			http.StatusInternalServerError,
			"service_error",
			"暂时无法读取设备配网记录",
			false,
		)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, snapshot)
}
