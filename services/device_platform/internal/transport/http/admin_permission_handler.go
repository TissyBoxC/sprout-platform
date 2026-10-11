package http

import (
	"context"
	"errors"
	"net/http"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
)

// adminPermissionService is the narrow permission-management surface the
// transport needs. It keeps HTTP free of the auth repository and service
// internals while still serving the permission screen.
type adminPermissionService interface {
	AdminPermissionCatalog() []string
	ListAdminPermissions(
		ctx context.Context,
	) ([]authdomain.AdminPermissionAccount, error)
	UpdateAdminPermissions(
		ctx context.Context,
		accountID string,
		permissions []string,
	) (authdomain.AdminPermissionAccount, error)
}

type adminPermissionHandler struct {
	service adminPermissionService
	audit   auditRecorder
}

type updateAdminPermissionRequest struct {
	Permissions []string `json:"permissions"`
}

// list returns every administrator with its permission set plus the assignable
// catalog, so the management screen can render without a second round trip.
func (handler adminPermissionHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用，请稍后重试")
		return
	}
	accounts, err := handler.service.ListAdminPermissions(request.Context())
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"catalog":  handler.service.AdminPermissionCatalog(),
		"accounts": adminPermissionAccountsResponse(accounts),
	})
}

// update replaces one administrator's permission set and records the change in
// the audit trail with the before/after counts only, never a raw request body.
func (handler adminPermissionHandler) update(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "服务暂时不可用，请稍后重试")
		return
	}
	accountID := request.PathValue("account_id")
	if accountID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	var payload updateAdminPermissionRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	updated, err := handler.service.UpdateAdminPermissions(
		request.Context(),
		accountID,
		payload.Permissions,
	)
	if err != nil {
		writeAdminPermissionError(response, request, err)
		return
	}
	handler.recordPermissionUpdate(request, updated)
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"account": adminPermissionAccountResponse(updated),
	})
}

func (handler adminPermissionHandler) recordPermissionUpdate(
	request *http.Request,
	account authdomain.AdminPermissionAccount,
) {
	if handler.audit == nil {
		return
	}
	actorAccountID, _ := authenticatedAccountID(request)
	_ = handler.audit.RecordAudit(
		request.Context(),
		actorAccountID,
		account.AccountID,
		"admin.permission.updated",
		map[string]any{
			"permission_count": len(account.Permissions),
			"permissions":      account.Permissions,
		},
	)
}

func adminPermissionAccountsResponse(
	accounts []authdomain.AdminPermissionAccount,
) []map[string]any {
	response := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		response = append(response, adminPermissionAccountResponse(account))
	}
	return response
}

func adminPermissionAccountResponse(
	account authdomain.AdminPermissionAccount,
) map[string]any {
	permissions := account.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	return map[string]any{
		"account_id":   account.AccountID,
		"email":        account.Email,
		"display_name": account.DisplayName,
		"status":       account.Status,
		"created_at":   account.CreatedAt,
		"permissions":  permissions,
	}
}

// writeAdminPermissionError keeps the permission endpoints' error mapping in
// one place. It is used only when writeAuthServiceError's generic mapping
// would hide the specific validation reason.
func writeAdminPermissionError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	if errors.Is(err, authdomain.ErrUnknownAdminPermission) {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_permission", "请选择有效的管理权限")
		return
	}
	writeAuthServiceError(response, request, err)
}
