package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	featuredomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	featureservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
)

// FeatureCenterService is the management-facing feature registry surface.
type FeatureCenterService interface {
	List(ctx context.Context) ([]featuredomain.Feature, error)
	Get(ctx context.Context, featureID string) (*featuredomain.Feature, error)
	UpdateConfig(
		ctx context.Context,
		featureID string,
		values map[string]any,
		actorID string,
		expectedVersion int64,
	) (*featuredomain.Feature, error)
	CheckHealth(ctx context.Context, featureID string) (*featuredomain.Feature, error)
}

// FeatureCenterHandler exposes feature registry and configuration endpoints.
//
// It is intentionally independent of adminHandler so the feature center can be
// wired without changing existing management handlers.
type FeatureCenterHandler struct {
	service FeatureCenterService
}

// NewFeatureCenterHandler creates the admin feature-center handler.
func NewFeatureCenterHandler(service FeatureCenterService) *FeatureCenterHandler {
	return &FeatureCenterHandler{service: service}
}

// RegisterAdminRoutes registers every feature-center endpoint behind the
// caller-provided administrator middleware.
func (handler *FeatureCenterHandler) RegisterAdminRoutes(
	mux *http.ServeMux,
	requireAdmin func(http.HandlerFunc) http.HandlerFunc,
) {
	if handler == nil || handler.service == nil || mux == nil {
		return
	}
	if requireAdmin == nil {
		requireAdmin = func(next http.HandlerFunc) http.HandlerFunc {
			return func(response http.ResponseWriter, request *http.Request) {
				writeError(
					response,
					request,
					http.StatusForbidden,
					"insufficient_permission",
					"你没有权限访问此页面",
				)
			}
		}
	}
	mux.HandleFunc(
		"GET /api/v1/admin/feature-center",
		requireAdmin(handler.List),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/feature-center/{feature}",
		requireAdmin(handler.Get),
	)
	mux.HandleFunc(
		"PUT /api/v1/admin/feature-center/{feature}/config",
		requireAdmin(handler.UpdateConfig),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/feature-center/{feature}/health-check",
		requireAdmin(handler.HealthCheck),
	)
}

// List returns every registered feature.
func (handler *FeatureCenterHandler) List(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler == nil || handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "功能中心暂时无法读取，请稍后重试")
		return
	}
	features, err := handler.service.List(request.Context())
	if err != nil {
		writeFeatureCenterError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"features": features,
	})
}

// Get returns one feature by id.
func (handler *FeatureCenterHandler) Get(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler == nil || handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "功能中心暂时无法读取，请稍后重试")
		return
	}
	featureID := strings.TrimSpace(request.PathValue("feature"))
	current, err := handler.service.Get(request.Context(), featureID)
	if err != nil {
		writeFeatureCenterError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"feature": featureCenterFeatureResponse(current),
	})
}

type updateFeatureConfigRequest struct {
	Values          map[string]any `json:"values"`
	ExpectedVersion *int64         `json:"expected_version"`
	Version         *int64         `json:"version"`
}

// UpdateConfig validates and persists one feature configuration overlay.
func (handler *FeatureCenterHandler) UpdateConfig(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler == nil || handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "功能配置暂时无法保存，请稍后重试")
		return
	}
	actorID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload updateFeatureConfigRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查功能配置")
		return
	}
	expectedVersion, ok := featureConfigExpectedVersion(payload)
	if !ok {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查功能配置版本")
		return
	}
	current, err := handler.service.UpdateConfig(
		request.Context(),
		strings.TrimSpace(request.PathValue("feature")),
		payload.Values,
		actorID,
		expectedVersion,
	)
	if err != nil {
		writeFeatureCenterError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"feature": featureCenterFeatureResponse(current),
	})
}

// HealthCheck runs the feature health probe and returns the refreshed entry.
func (handler *FeatureCenterHandler) HealthCheck(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler == nil || handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "功能状态暂时无法检查，请稍后重试")
		return
	}
	current, err := handler.service.CheckHealth(
		request.Context(),
		strings.TrimSpace(request.PathValue("feature")),
	)
	if err != nil {
		writeFeatureCenterError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"feature": featureCenterFeatureResponse(current),
	})
}

func featureCenterFeatureResponse(
	feature *featuredomain.Feature,
) map[string]any {
	if feature == nil {
		return nil
	}
	return map[string]any{
		"id":                feature.ID,
		"name":              feature.Name,
		"description":       feature.Description,
		"category":          feature.Category,
		"owner":             feature.Owner,
		"status":            feature.Status,
		"health":            feature.Health,
		"health_detail":     feature.HealthDetail,
		"health_source":     feature.HealthSource,
		"config_version":    feature.ConfigVersion,
		"config_schema":     feature.ConfigSchema,
		"values":            feature.Values,
		"read_only_metrics": feature.ReadOnlyMetrics,
		"read_only_reason":  feature.ReadOnlyReason,
		"secret_configured": configuredSecrets(feature.Values),
		"updated_at":        feature.UpdatedAt,
	}
}

func configuredSecrets(values map[string]any) map[string]bool {
	configured := map[string]bool{}
	for key, value := range values {
		metadata, ok := value.(map[string]any)
		if !ok {
			continue
		}
		isConfigured, ok := metadata["configured"].(bool)
		if !ok {
			continue
		}
		configured[key] = isConfigured
	}
	return configured
}

func featureConfigExpectedVersion(payload updateFeatureConfigRequest) (int64, bool) {
	switch {
	case payload.ExpectedVersion != nil && payload.Version != nil:
		if *payload.ExpectedVersion != *payload.Version {
			return 0, false
		}
		return *payload.ExpectedVersion, *payload.ExpectedVersion >= 0
	case payload.ExpectedVersion != nil:
		return *payload.ExpectedVersion, *payload.ExpectedVersion >= 0
	case payload.Version != nil:
		return *payload.Version, *payload.Version >= 0
	default:
		return 0, false
	}
}

func writeFeatureCenterError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	var conflict *featuredomain.VersionConflictError
	var validation *featuredomain.ConfigValidationError
	switch {
	case errors.As(err, &conflict):
		writeError(
			response,
			request,
			http.StatusConflict,
			"config_version_conflict",
			"功能配置已被其他管理员更新，请刷新后重试",
		)
	case errors.Is(err, featuredomain.ErrFeatureNotFound):
		writeError(response, request, http.StatusNotFound, "feature_not_found", "没有找到这个功能")
	case errors.Is(err, featuredomain.ErrFeatureReadOnly):
		writeError(response, request, http.StatusUnprocessableEntity, "feature_read_only", "这个功能不支持在控制台修改配置")
	case errors.As(err, &validation):
		switch {
		case errors.Is(validation.Err, featuredomain.ErrUnknownConfigField):
			writeError(response, request, http.StatusUnprocessableEntity, "unknown_config_field", "功能配置包含不支持的字段")
		case errors.Is(validation.Err, featuredomain.ErrImmutableConfigField):
			writeError(response, request, http.StatusUnprocessableEntity, "immutable_config_field", "这个功能字段不能修改")
		default:
			writeError(response, request, http.StatusUnprocessableEntity, "invalid_config", "请检查功能配置")
		}
	case errors.Is(err, featuredomain.ErrInvalidConfig):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_config", "请检查功能配置")
	case errors.Is(err, featuredomain.ErrHealthCheckFailed):
		writeError(response, request, http.StatusBadGateway, "health_check_failed", "功能状态检查没有完成，请稍后重试")
	case errors.Is(err, featuredomain.ErrHealthCheckUnavailable):
		writeError(
			response,
			request,
			http.StatusNotImplemented,
			"health_check_unavailable",
			"该功能暂未接入健康检查",
		)
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

var _ FeatureCenterService = (*featureservice.Service)(nil)
