package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
	familyservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/service"
)

// familyService is the guardian-facing family membership surface.
//
// The HTTP layer depends on this narrow interface so the family module stays
// removable and tests can substitute a small fake.
type familyService interface {
	BootstrapOwner(
		ctx context.Context,
		familyID string,
		userID string,
	) (familydomain.Member, error)
	ResolveFamily(
		ctx context.Context,
		userID string,
	) (familydomain.Member, error)
	Authorize(
		ctx context.Context,
		userID string,
		action familydomain.Action,
	) (familydomain.Access, error)
	Members(
		ctx context.Context,
		familyID string,
		actorUserID string,
	) ([]familydomain.Member, error)
	Invitations(
		ctx context.Context,
		familyID string,
		actorUserID string,
	) ([]familydomain.Invitation, error)
	PendingInvitationsForAccount(
		ctx context.Context,
		userID string,
		email string,
	) ([]familydomain.Invitation, error)
	CreateInvitation(
		ctx context.Context,
		familyID string,
		actorUserID string,
		email string,
		role string,
	) (familydomain.Invitation, error)
	RevokeInvitation(
		ctx context.Context,
		familyID string,
		actorUserID string,
		invitationID string,
	) error
	AcceptInvitation(
		ctx context.Context,
		familyID string,
		invitationID string,
		userID string,
		email string,
	) (familydomain.Member, error)
	UpdateMemberRole(
		ctx context.Context,
		familyID string,
		actorUserID string,
		targetUserID string,
		role string,
	) (familydomain.Member, error)
	RemoveMember(
		ctx context.Context,
		familyID string,
		actorUserID string,
		targetUserID string,
	) error
	Capability(
		ctx context.Context,
		familyID string,
		actorUserID string,
	) (familydomain.Capability, error)
}

type familyHandler struct {
	service       familyService
	accountReader familyAccountReader
}

// familyAccountReader resolves the authenticated account's email so an
// invitation can be matched to the accepting login. It is a narrow interface so
// the family module never depends on the auth repository.
type familyAccountReader interface {
	AccountEmail(ctx context.Context, userID string) (string, error)
}

type familyInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type familyMemberRoleRequest struct {
	Role string `json:"role"`
}

// listMembers returns every member in the authenticated family.
func (handler familyHandler) listMembers(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, userID, ok := handler.authenticatedMember(response, request)
	if !ok {
		return
	}
	members, err := handler.service.Members(request.Context(), familyID, userID)
	if err != nil {
		writeFamilyError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(members))
	for index := range members {
		result = append(result, familyMemberResponse(&members[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"members": result,
	})
}

// listInvitations returns every invitation in the authenticated family.
func (handler familyHandler) listInvitations(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, userID, ok := handler.authenticatedMember(response, request)
	if !ok {
		return
	}
	invitations, err := handler.service.Invitations(
		request.Context(),
		familyID,
		userID,
	)
	if err != nil {
		writeFamilyError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(invitations))
	for index := range invitations {
		result = append(result, familyInvitationResponse(&invitations[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"invitations": result,
	})
}

// listMyInvitations returns the pending invitations addressed to the
// authenticated account's email. It is reachable by any signed-in account,
// including one with no membership yet, so an invitee can discover and accept
// an invitation without already knowing a family id.
func (handler familyHandler) listMyInvitations(
	response http.ResponseWriter,
	request *http.Request,
) {
	userID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	email, err := handler.accountEmail(request.Context(), userID)
	if err != nil {
		writeFamilyError(response, request, err)
		return
	}
	invitations, err := handler.service.PendingInvitationsForAccount(
		request.Context(),
		userID,
		email,
	)
	if err != nil {
		writeFamilyError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(invitations))
	for index := range invitations {
		result = append(result, familyInvitationResponse(&invitations[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"invitations": result,
	})
}

// createInvitation stores one pending invitation for the authenticated family.
func (handler familyHandler) createInvitation(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, userID, ok := handler.authenticatedMember(response, request)
	if !ok {
		return
	}
	var payload familyInvitationRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	invitation, err := handler.service.CreateInvitation(
		request.Context(),
		familyID,
		userID,
		payload.Email,
		strings.TrimSpace(payload.Role),
	)
	if err != nil {
		writeFamilyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"invitation": familyInvitationResponse(&invitation),
	})
}

// revokeInvitation cancels one pending invitation.
func (handler familyHandler) revokeInvitation(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, userID, ok := handler.authenticatedMember(response, request)
	if !ok {
		return
	}
	invitationID := strings.TrimSpace(request.PathValue("invitation_id"))
	if err := handler.service.RevokeInvitation(
		request.Context(),
		familyID,
		userID,
		invitationID,
	); err != nil {
		writeFamilyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"revoked": true,
	})
}

// acceptInvitation redeems a pending invitation for the authenticated account.
func (handler familyHandler) acceptInvitation(
	response http.ResponseWriter,
	request *http.Request,
) {
	userID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	familyID := strings.TrimSpace(request.PathValue("family_id"))
	invitationID := strings.TrimSpace(request.PathValue("invitation_id"))
	email, err := handler.accountEmail(request.Context(), userID)
	if err != nil {
		writeFamilyError(response, request, err)
		return
	}
	member, err := handler.service.AcceptInvitation(
		request.Context(),
		familyID,
		invitationID,
		userID,
		email,
	)
	if err != nil {
		writeFamilyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"member": familyMemberResponse(&member),
	})
}

// accountEmail resolves the authenticated account's email for invitation
// matching. The reader is optional so the family module stays removable; when
// it is absent the endpoint refuses rather than accepting an unmatched invite.
func (handler familyHandler) accountEmail(
	ctx context.Context,
	userID string,
) (string, error) {
	if handler.accountReader == nil {
		return "", familydomain.ErrInvalidInvitation
	}
	email, err := handler.accountReader.AccountEmail(ctx, userID)
	if err != nil {
		return "", err
	}
	return email, nil
}

// updateMemberRole changes one member's role within the authenticated family.
func (handler familyHandler) updateMemberRole(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, userID, ok := handler.authenticatedMember(response, request)
	if !ok {
		return
	}
	var payload familyMemberRoleRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	targetUserID := strings.TrimSpace(request.PathValue("user_id"))
	member, err := handler.service.UpdateMemberRole(
		request.Context(),
		familyID,
		userID,
		targetUserID,
		strings.TrimSpace(payload.Role),
	)
	if err != nil {
		writeFamilyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"member": familyMemberResponse(&member),
	})
}

// removeMember removes one member from the authenticated family.
func (handler familyHandler) removeMember(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, userID, ok := handler.authenticatedMember(response, request)
	if !ok {
		return
	}
	targetUserID := strings.TrimSpace(request.PathValue("user_id"))
	if err := handler.service.RemoveMember(
		request.Context(),
		familyID,
		userID,
		targetUserID,
	); err != nil {
		writeFamilyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"removed": true,
	})
}

// capability returns the membership summary for the authenticated family.
func (handler familyHandler) capability(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, userID, ok := handler.authenticatedMember(response, request)
	if !ok {
		return
	}
	access, _ := authenticatedFamilyAccess(request)
	capability, err := handler.service.Capability(
		request.Context(),
		familyID,
		userID,
	)
	if err != nil {
		writeFamilyError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"family_id":            capability.FamilyID,
		"owner_user_id":        capability.OwnerUserID,
		"member_count":         capability.MemberCount,
		"pending_invite_count": capability.PendingInvites,
		"schema_version":       capability.SchemaVersion,
		"permissions":          familyActionForReport(access.Role),
	})
}

// authenticatedMember resolves the authenticated account's family scope and
// member id. The family is resolved from stored membership, so a guardian who
// joined by invitation is scoped to that family rather than to its own account
// id.
func (handler familyHandler) authenticatedMember(
	response http.ResponseWriter,
	request *http.Request,
) (string, string, bool) {
	if access, ok := authenticatedFamilyAccess(request); ok {
		return access.FamilyID, access.ActorUserID, true
	}
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return "", "", false
	}
	member, err := handler.service.ResolveFamily(request.Context(), accountID)
	if err != nil {
		writeFamilyError(response, request, err)
		return "", "", false
	}
	return member.FamilyID, member.UserID, true
}

func familyMemberResponse(member *familydomain.Member) map[string]any {
	return map[string]any{
		"id":         member.ID,
		"family_id":  member.FamilyID,
		"user_id":    member.UserID,
		"role":       member.Role,
		"created_at": member.CreatedAt,
		"updated_at": member.UpdatedAt,
	}
}

func familyInvitationResponse(invitation *familydomain.Invitation) map[string]any {
	return map[string]any{
		"id":                 invitation.ID,
		"family_id":          invitation.FamilyID,
		"email":              invitation.Email,
		"role":               invitation.Role,
		"status":             invitation.Status,
		"invited_by_user_id": invitation.InvitedByUserID,
		"created_at":         invitation.CreatedAt,
		"expires_at":         invitation.ExpiresAt,
		"accepted_at":        invitation.AcceptedAt,
	}
}

func writeFamilyError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, familydomain.ErrPermissionDenied):
		writeError(
			response,
			request,
			http.StatusForbidden,
			"insufficient_permission",
			"当前角色没有执行该操作的权限",
		)
	case errors.Is(err, familydomain.ErrMemberNotFound),
		errors.Is(err, familydomain.ErrInvitationNotFound):
		writeError(response, request, http.StatusNotFound, "family_not_found", "没有找到这个成员或邀请")
	case errors.Is(err, familydomain.ErrMemberExists):
		writeError(response, request, http.StatusConflict, "member_exists", "该账号已经是家庭成员")
	case errors.Is(err, familydomain.ErrLastOwner):
		writeError(response, request, http.StatusConflict, "last_owner", "家庭必须保留一位所有者")
	case errors.Is(err, familydomain.ErrInvitationState):
		writeError(response, request, http.StatusConflict, "invitation_state", "该邀请已处理或已存在待处理邀请")
	case errors.Is(err, familydomain.ErrInvitationExpired):
		writeError(response, request, http.StatusUnprocessableEntity, "invitation_expired", "邀请已过期，请重新邀请")
	case errors.Is(err, familydomain.ErrInvalidInvitation):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_invitation", "请填写有效的邮箱和角色")
	case errors.Is(err, familydomain.ErrInvalidMember):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_member", "当前角色没有执行该操作的权限")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

var _ familyService = (*familyservice.Service)(nil)
