package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

type adminFamilyMembershipHandler struct {
	service familyService
}

type adminFamilyRoleRevisionRequest struct {
	Role string `json:"role"`
}

type adminFamilyInvitationRevisionRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// listMembers returns one explicit target family's roster. The route is
// protected by AdminPermissionFamilyRead; no acting-member authorization is
// involved because an administrator is not a member of the target family.
func (handler adminFamilyMembershipHandler) listMembers(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := adminFamilyID(response, request)
	if !ok {
		return
	}
	members, revision, err := handler.service.AdminMembers(request.Context(), familyID)
	if err != nil {
		writeAdminFamilyMembershipError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(members))
	for index := range members {
		result = append(result, familyAdminMemberResponse(&members[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"family_id":           familyID,
		"membership_revision": revision,
		"members":             result,
	})
}

// listInvitations returns every invitation for the explicit target family.
func (handler adminFamilyMembershipHandler) listInvitations(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := adminFamilyID(response, request)
	if !ok {
		return
	}
	invitations, revision, err := handler.service.AdminInvitations(
		request.Context(),
		familyID,
	)
	if err != nil {
		writeAdminFamilyMembershipError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(invitations))
	for index := range invitations {
		result = append(result, familyInvitationResponse(&invitations[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"family_id":           familyID,
		"membership_revision": revision,
		"invitations":         result,
	})
}

// createInvitation creates an invitation as the target family's owner. The
// repository writes the administrator audit atomically with the invitation, so
// the handler must not add a second successful mutation audit.
func (handler adminFamilyMembershipHandler) createInvitation(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := adminFamilyID(response, request)
	if !ok {
		return
	}
	adminAccountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload adminFamilyInvitationRevisionRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	expectedRevision, ok := adminMembershipRevision(response, request)
	if !ok {
		return
	}
	invitation, err := handler.service.AdminCreateInvitation(
		request.Context(),
		familyID,
		adminAccountID,
		payload.Email,
		strings.TrimSpace(payload.Role),
		expectedRevision,
	)
	if err != nil {
		writeAdminFamilyMembershipError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"invitation": familyInvitationResponse(&invitation),
	})
}

// revokeInvitation revokes a pending invitation in the target family. The
// repository switches the state and writes the administrator audit together.
func (handler adminFamilyMembershipHandler) revokeInvitation(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := adminFamilyID(response, request)
	if !ok {
		return
	}
	adminAccountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	invitationID := strings.TrimSpace(request.PathValue("invitation_id"))
	expectedRevision, ok := adminMembershipRevision(response, request)
	if !ok {
		return
	}
	if err := handler.service.AdminRevokeInvitation(
		request.Context(),
		familyID,
		adminAccountID,
		invitationID,
		expectedRevision,
	); err != nil {
		writeAdminFamilyMembershipError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"revoked": true,
	})
}

// updateMemberRole changes one non-owner membership role. The repository keeps
// the role mutation and its audit atomic.
func (handler adminFamilyMembershipHandler) updateMemberRole(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := adminFamilyID(response, request)
	if !ok {
		return
	}
	adminAccountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload adminFamilyRoleRevisionRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	expectedRevision, ok := adminMembershipRevision(response, request)
	if !ok {
		return
	}
	targetUserID := strings.TrimSpace(request.PathValue("user_id"))
	member, err := handler.service.AdminUpdateMemberRole(
		request.Context(),
		familyID,
		adminAccountID,
		targetUserID,
		strings.TrimSpace(payload.Role),
		expectedRevision,
	)
	if err != nil {
		writeAdminFamilyMembershipError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"member": familyMemberResponse(&member),
	})
}

// removeMember removes one non-owner membership. The owner row cannot be
// removed through the administrative path, which preserves the one-owner
// invariant even when support staff are correcting a family.
func (handler adminFamilyMembershipHandler) removeMember(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := adminFamilyID(response, request)
	if !ok {
		return
	}
	adminAccountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	targetUserID := strings.TrimSpace(request.PathValue("user_id"))
	expectedRevision, ok := adminMembershipRevision(response, request)
	if !ok {
		return
	}
	if err := handler.service.AdminRemoveMember(
		request.Context(),
		familyID,
		adminAccountID,
		targetUserID,
		expectedRevision,
	); err != nil {
		writeAdminFamilyMembershipError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"removed": true,
	})
}

// adminMembershipRevision reads the required If-Match optimistic-concurrency
// value. Writes without a positive revision are rejected instead of silently
// overwriting a concurrent change.
func adminMembershipRevision(
	response http.ResponseWriter,
	request *http.Request,
) (int64, bool) {
	raw := strings.TrimSpace(request.Header.Get("If-Match"))
	raw = strings.Trim(raw, `"`)
	if raw == "" {
		writeError(response, request, http.StatusPreconditionRequired, "revision_required", "请先刷新家庭成员信息后再操作")
		return 0, false
	}
	revision, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || revision < 1 {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_revision", "成员信息版本不正确，请刷新后重试")
		return 0, false
	}
	return revision, true
}

func adminFamilyID(
	response http.ResponseWriter,
	request *http.Request,
) (string, bool) {
	familyID := strings.TrimSpace(request.PathValue("parent_account_id"))
	if familyID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择要管理的家长账号")
		return "", false
	}
	return familyID, true
}

func writeAdminFamilyMembershipError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, familydomain.ErrAccountNotFound):
		writeError(response, request, http.StatusNotFound, "family_not_found", "没有找到这个家长账号")
	case errors.Is(err, familydomain.ErrAccountNotParent):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_family", "这个账号不是家长账号")
	case errors.Is(err, familydomain.ErrPermissionDenied):
		writeError(response, request, http.StatusForbidden, "insufficient_permission", "当前账号没有执行该操作的权限")
	case errors.Is(err, familydomain.ErrMemberNotFound):
		writeError(response, request, http.StatusNotFound, "member_not_found", "没有找到这个家庭成员")
	case errors.Is(err, familydomain.ErrInvitationNotFound):
		writeError(response, request, http.StatusNotFound, "invitation_not_found", "没有找到这个邀请")
	case errors.Is(err, familydomain.ErrMemberExists):
		writeError(response, request, http.StatusConflict, "member_exists", "该账号已经是家庭成员")
	case errors.Is(err, familydomain.ErrLastOwner):
		writeError(response, request, http.StatusConflict, "last_owner", "家庭必须保留一位所有者")
	case errors.Is(err, familydomain.ErrInvitationState),
		errors.Is(err, familydomain.ErrInvitationConcurrent):
		writeError(response, request, http.StatusConflict, "invitation_state", "该邀请已处理或状态已变化")
	case errors.Is(err, familydomain.ErrRevisionConflict):
		writeError(response, request, http.StatusConflict, "membership_revision_conflict", "家庭成员或邀请已发生变化，请刷新后重试")
	case errors.Is(err, familydomain.ErrInvitationExpired):
		writeError(response, request, http.StatusUnprocessableEntity, "invitation_expired", "邀请已过期，请重新邀请")
	case errors.Is(err, familydomain.ErrInvalidInvitation):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_invitation", "请填写有效的邮箱和角色")
	case errors.Is(err, familydomain.ErrInvalidMember):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_member", "请选择有效的家庭成员和角色")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
