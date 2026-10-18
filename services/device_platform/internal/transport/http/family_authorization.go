package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	gatewaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

type familyAccessKey struct{}

// familyAuthorizer is the narrow authorization surface shared by family
// routes and parent-scoped handlers. Keeping it here prevents the HTTP layer
// from depending on the concrete service implementation.
type familyAuthorizer interface {
	Authorize(
		ctx context.Context,
		userID string,
		action familydomain.Action,
	) (familydomain.Access, error)
}

// requireFamilyAction resolves the stored family membership after
// authentication and authorizes one canonical action. The resolved Access is
// placed in the request context so handlers use the real family and owner ids
// rather than mistaking the acting account id for either.
func (handler familyHandler) requireFamilyAction(
	action familydomain.Action,
	next http.HandlerFunc,
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		accountID, ok := authenticatedAccountID(request)
		if !ok {
			writeError(
				response,
				request,
				http.StatusUnauthorized,
				"unauthenticated",
				"请重新登录",
			)
			return
		}
		access, err := handler.service.Authorize(request.Context(), accountID, action)
		if err != nil {
			writeFamilyError(response, request, err)
			return
		}
		next(
			response,
			request.WithContext(
				context.WithValue(request.Context(), familyAccessKey{}, access),
			),
		)
	}
}

func authenticatedFamilyAccess(request *http.Request) (familydomain.Access, bool) {
	if request == nil {
		return familydomain.Access{}, false
	}
	access, ok := request.Context().Value(familyAccessKey{}).(familydomain.Access)
	if !ok || !validFamilyAccess(access) {
		return familydomain.Access{}, false
	}
	return access, true
}

// authenticatedFamilyOwnerID returns the account that owns the stored family
// data. Invited guardians and viewers must use this id for children, devices,
// policies and AI records rather than their own login account id.
func authenticatedFamilyOwnerID(request *http.Request) (string, bool) {
	access, ok := authenticatedFamilyAccess(request)
	if !ok {
		return "", false
	}
	return access.OwnerUserID, true
}

func validFamilyAccess(access familydomain.Access) bool {
	return strings.TrimSpace(access.FamilyID) != "" &&
		strings.TrimSpace(access.ActorUserID) != "" &&
		strings.TrimSpace(access.OwnerUserID) != ""
}

func isFamilyPermissionDenied(err error) bool {
	return errors.Is(err, familydomain.ErrPermissionDenied)
}

// familyActionForReport maps the role's effective read permissions into the
// matrix clients use to hide unavailable controls without making them the
// security boundary.
func familyActionForReport(role string) map[string]bool {
	result := make(map[string]bool, len(familydomain.Actions))
	for _, action := range familydomain.Actions {
		result[string(action)] = familydomain.Can(role, action)
	}
	return result
}

// familyRoleMiddleware applies one action to a family route after
// authentication. Member removal intentionally bypasses this wrapper because
// its action depends on whether the caller is removing itself or another
// member; the service still enforces that distinction.
func (handler familyHandler) familyRoleMiddleware(
	action familydomain.Action,
) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return handler.requireFamilyAction(action, next)
	}
}

func resolveFamilyAccess(
	request *http.Request,
	resolve func(*http.Request) (familydomain.Access, bool),
) familydomain.Access {
	if resolve != nil {
		if access, ok := resolve(request); ok {
			return access
		}
	}
	return familydomain.Access{}
}

func parentScopedFor(
	authorizer familyAuthorizer,
	action familydomain.Action,
	next func(
		http.ResponseWriter,
		*http.Request,
		string,
	),
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		accountID, ok := authenticatedAccountID(request)
		if !ok {
			writeError(
				response,
				request,
				http.StatusUnauthorized,
				"unauthenticated",
				"请重新登录",
			)
			return
		}
		if authorizer == nil {
			writeError(
				response,
				request,
				http.StatusServiceUnavailable,
				"family_authorization_unavailable",
				"家庭权限服务暂时不可用，请稍后重试",
			)
			return
		}
		access, err := authorizer.Authorize(request.Context(), accountID, action)
		if err != nil {
			writeFamilyError(response, request, err)
			return
		}
		request = request.WithContext(
			context.WithValue(request.Context(), familyAccessKey{}, access),
		)
		next(response, request, access.OwnerUserID)
	}
}

// updateParentAIModelsAuthorized applies a model selection to the family
// owner's AI account after the caller completed ActionAIAccountManage.
func (handler adminHandler) updateParentAIModelsAuthorized(
	response http.ResponseWriter,
	request *http.Request,
	accountID string,
) {
	var payload updateAIModelsRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查选择的模型")
		return
	}
	summary, err := handler.service.UpdateModelsForParent(
		request.Context(),
		accountID,
		payload.SelectedModels,
	)
	if err != nil {
		switch {
		case errors.Is(err, gatewaydomain.ErrModelNotAllowed):
			writeError(response, request, http.StatusUnprocessableEntity, "model_not_available", "请选择当前可用的模型")
		case errors.Is(err, gatewaydomain.ErrAccountNotFound):
			writeError(response, request, http.StatusNotFound, "ai_account_not_found", "还没有可用的 AI 服务")
		case errors.Is(err, gatewaydomain.ErrProviderUnavailable),
			errors.Is(err, gatewaydomain.ErrProviderRejected):
			writeError(response, request, http.StatusBadGateway, "ai_service_unavailable", "AI 服务暂时不可用，请稍后重试")
		default:
			writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
		}
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"ai_account": map[string]any{
			"status":            summary.Status,
			"balance_usd":       summary.BalanceUSD,
			"concurrency_limit": summary.ConcurrencyLimit,
			"available_models":  summary.AvailableModels,
			"selected_models":   summary.SelectedModels,
			"allowed_models":    summary.AllowedModels,
			"provider_ready":    summary.ProviderReady,
		},
	})
}
