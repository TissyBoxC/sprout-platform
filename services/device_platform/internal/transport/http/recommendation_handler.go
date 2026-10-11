package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	recommendationdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/domain"
)

// recommendationService is the guardian-facing recommendation surface.
//
// The HTTP layer depends on this narrow interface so the module stays
// removable and tests can substitute a small fake.
type recommendationService interface {
	Recommend(
		ctx context.Context,
		familyID string,
		query recommendationdomain.Query,
	) ([]recommendationdomain.Recommendation, error)
}

type recommendationHandler struct {
	service recommendationService
}

func (handler recommendationHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	query := recommendationdomain.Query{
		AgeTier:           strings.TrimSpace(request.URL.Query().Get("age_tier")),
		Limit:             parsePositiveQueryInt(request.URL.Query().Get("limit"), 0),
		ExcludePackageIDs: request.URL.Query()["exclude"],
	}
	recommendations, err := handler.service.Recommend(
		request.Context(),
		familyID,
		query,
	)
	if err != nil {
		writeRecommendationError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(recommendations))
	for index := range recommendations {
		result = append(result, recommendationResponse(&recommendations[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"recommendations": result,
	})
}

func recommendationResponse(item *recommendationdomain.Recommendation) map[string]any {
	return map[string]any{
		"package_id": item.PackageID,
		"title":      item.Title,
		"category":   item.Category,
		"age_tiers":  item.AgeTiers,
		"score":      item.Score,
		"reason":     item.Reason,
	}
}

func writeRecommendationError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, recommendationdomain.ErrInvalidAgeTier):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_age_tier", "请选择有效的年龄层")
	case errors.Is(err, recommendationdomain.ErrInvalidQuery):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_query", "请检查推荐请求")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
