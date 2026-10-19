package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

// familyTestService is an in-memory double so the handler tests exercise the
// real validation and membership invariants without a database.
type familyTestService struct {
	members     map[string]familydomain.Member
	invitations map[string]familydomain.Invitation
	authorize   func(
		userID string,
		action familydomain.Action,
	) (familydomain.Access, error)
}

func newFamilyTestService() *familyTestService {
	return &familyTestService{
		members:     map[string]familydomain.Member{},
		invitations: map[string]familydomain.Invitation{},
	}
}

func (s *familyTestService) BootstrapOwner(
	_ context.Context,
	familyID string,
	userID string,
) (familydomain.Member, error) {
	owner := familydomain.Member{
		ID:        "member-owner",
		FamilyID:  familyID,
		UserID:    userID,
		Role:      familydomain.RoleOwner,
		CreatedAt: time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC),
	}
	s.members[userID] = owner
	return owner, nil
}

func (s *familyTestService) Members(
	_ context.Context,
	familyID string,
	actorUserID string,
) ([]familydomain.Member, error) {
	if _, ok := s.members[actorUserID]; !ok {
		return nil, familydomain.ErrMemberNotFound
	}
	members := make([]familydomain.Member, 0, len(s.members))
	for _, member := range s.members {
		if member.FamilyID == familyID {
			members = append(members, member)
		}
	}
	return members, nil
}

func (s *familyTestService) ResolveFamily(
	_ context.Context,
	userID string,
) (familydomain.Member, error) {
	member, ok := s.members[userID]
	if !ok {
		return familydomain.Member{}, familydomain.ErrMemberNotFound
	}
	return member, nil
}

func (s *familyTestService) Authorize(
	_ context.Context,
	userID string,
	action familydomain.Action,
) (familydomain.Access, error) {
	if s.authorize != nil {
		return s.authorize(userID, action)
	}
	member, ok := s.members[userID]
	if !ok {
		return familydomain.Access{}, familydomain.ErrMemberNotFound
	}
	if !familydomain.Can(member.Role, action) {
		return familydomain.Access{}, familydomain.ErrPermissionDenied
	}
	ownerUserID := ""
	for _, candidate := range s.members {
		if candidate.FamilyID == member.FamilyID &&
			candidate.Role == familydomain.RoleOwner {
			ownerUserID = candidate.UserID
			break
		}
	}
	return familydomain.Access{
		FamilyID:    member.FamilyID,
		ActorUserID: member.UserID,
		OwnerUserID: ownerUserID,
		Role:        member.Role,
	}, nil
}

func (s *familyTestService) Invitations(
	_ context.Context,
	familyID string,
	actorUserID string,
) ([]familydomain.Invitation, error) {
	if _, ok := s.members[actorUserID]; !ok {
		return nil, familydomain.ErrMemberNotFound
	}
	invitations := make([]familydomain.Invitation, 0, len(s.invitations))
	for _, invitation := range s.invitations {
		if invitation.FamilyID == familyID {
			invitations = append(invitations, invitation)
		}
	}
	return invitations, nil
}

func (s *familyTestService) PendingInvitationsForAccount(
	_ context.Context,
	userID string,
	email string,
) ([]familydomain.Invitation, error) {
	normalized := familydomain.NormalizeEmail(email)
	invitations := make([]familydomain.Invitation, 0, len(s.invitations))
	for _, invitation := range s.invitations {
		if familydomain.NormalizeEmail(invitation.Email) == normalized &&
			invitation.Status == familydomain.InvitationPending {
			invitations = append(invitations, invitation)
		}
	}
	return invitations, nil
}

func (s *familyTestService) CreateInvitation(
	_ context.Context,
	familyID string,
	actorUserID string,
	email string,
	role string,
) (familydomain.Invitation, error) {
	actor, ok := s.members[actorUserID]
	if !ok {
		return familydomain.Invitation{}, familydomain.ErrMemberNotFound
	}
	if !familydomain.CanInvite(actor.Role) {
		return familydomain.Invitation{}, familydomain.ErrPermissionDenied
	}
	if err := familydomain.ValidateInviteEmail(email); err != nil {
		return familydomain.Invitation{}, err
	}
	if err := familydomain.ValidateInviteRole(role); err != nil {
		return familydomain.Invitation{}, err
	}
	invitation := familydomain.Invitation{
		ID:       "invitation-1",
		FamilyID: familyID,
		Email:    familydomain.NormalizeEmail(email),
		Role:     role,
		Status:   familydomain.InvitationPending,
	}
	s.invitations[invitation.ID] = invitation
	return invitation, nil
}

func (s *familyTestService) RevokeInvitation(
	_ context.Context,
	familyID string,
	actorUserID string,
	invitationID string,
) error {
	actor, ok := s.members[actorUserID]
	if !ok {
		return familydomain.ErrMemberNotFound
	}
	if !familydomain.CanInvite(actor.Role) {
		return familydomain.ErrPermissionDenied
	}
	invitation, ok := s.invitations[invitationID]
	if !ok || invitation.FamilyID != familyID {
		return familydomain.ErrInvitationNotFound
	}
	return nil
}

func (s *familyTestService) AcceptInvitation(
	_ context.Context,
	familyID string,
	invitationID string,
	userID string,
	email string,
) (familydomain.Member, error) {
	invitation, ok := s.invitations[invitationID]
	if !ok || invitation.FamilyID != familyID {
		return familydomain.Member{}, familydomain.ErrInvitationNotFound
	}
	if familydomain.NormalizeEmail(email) != familydomain.NormalizeEmail(invitation.Email) {
		return familydomain.Member{}, familydomain.ErrInvitationNotFound
	}
	member := familydomain.Member{
		ID:       "member-2",
		FamilyID: familyID,
		UserID:   userID,
		Role:     invitation.Role,
	}
	s.members[userID] = member
	return member, nil
}

func (s *familyTestService) UpdateMemberRole(
	_ context.Context,
	familyID string,
	actorUserID string,
	targetUserID string,
	role string,
) (familydomain.Member, error) {
	actor, ok := s.members[actorUserID]
	if !ok {
		return familydomain.Member{}, familydomain.ErrMemberNotFound
	}
	if !familydomain.CanManageMembers(actor.Role) {
		return familydomain.Member{}, familydomain.ErrPermissionDenied
	}
	target, ok := s.members[targetUserID]
	if !ok {
		return familydomain.Member{}, familydomain.ErrMemberNotFound
	}
	if target.Role == familydomain.RoleOwner {
		return familydomain.Member{}, familydomain.ErrLastOwner
	}
	target.Role = role
	s.members[targetUserID] = target
	return target, nil
}

func (s *familyTestService) RemoveMember(
	_ context.Context,
	familyID string,
	actorUserID string,
	targetUserID string,
) error {
	actor, ok := s.members[actorUserID]
	if !ok {
		return familydomain.ErrMemberNotFound
	}
	target, ok := s.members[targetUserID]
	if !ok {
		return familydomain.ErrMemberNotFound
	}
	if target.Role == familydomain.RoleOwner {
		return familydomain.ErrLastOwner
	}
	if actor.UserID != targetUserID && !familydomain.CanManageMembers(actor.Role) {
		return familydomain.ErrPermissionDenied
	}
	delete(s.members, targetUserID)
	return nil
}

func (s *familyTestService) Capability(
	_ context.Context,
	familyID string,
	actorUserID string,
) (familydomain.Capability, error) {
	if _, ok := s.members[actorUserID]; !ok {
		return familydomain.Capability{}, familydomain.ErrMemberNotFound
	}
	owner := ""
	for _, member := range s.members {
		if member.FamilyID == familyID && member.Role == familydomain.RoleOwner {
			owner = member.UserID
		}
	}
	return familydomain.Capability{
		FamilyID:      familyID,
		OwnerUserID:   owner,
		MemberCount:   len(s.members),
		SchemaVersion: familydomain.SchemaVersion,
	}, nil
}

func (s *familyTestService) AdminMembers(
	_ context.Context,
	familyID string,
) ([]familydomain.AdminMember, int64, error) {
	members := make([]familydomain.AdminMember, 0, len(s.members))
	for _, member := range s.members {
		if member.FamilyID == familyID {
			members = append(members, familydomain.AdminMember{
				Member: member,
				Account: familydomain.AccountIdentity{
					AccountID: member.UserID,
					Role:      "parent",
					Status:    "active",
				},
			})
		}
	}
	return members, 1, nil
}

func (s *familyTestService) AdminInvitations(
	_ context.Context,
	familyID string,
) ([]familydomain.Invitation, int64, error) {
	invitations := make([]familydomain.Invitation, 0, len(s.invitations))
	for _, invitation := range s.invitations {
		if invitation.FamilyID == familyID {
			invitations = append(invitations, invitation)
		}
	}
	return invitations, 1, nil
}

func (s *familyTestService) AdminCreateInvitation(
	_ context.Context,
	familyID string,
	_ string,
	email string,
	role string,
	_ int64,
) (familydomain.Invitation, error) {
	invitation := familydomain.Invitation{
		ID:       "admin-invitation-1",
		FamilyID: familyID,
		Email:    familydomain.NormalizeEmail(email),
		Role:     role,
		Status:   familydomain.InvitationPending,
	}
	s.invitations[invitation.ID] = invitation
	return invitation, nil
}

func (s *familyTestService) AdminRevokeInvitation(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ int64,
) error {
	return nil
}

func (s *familyTestService) AdminUpdateMemberRole(
	_ context.Context,
	familyID string,
	_ string,
	targetUserID string,
	role string,
	_ int64,
) (familydomain.Member, error) {
	member, ok := s.members[targetUserID]
	if !ok || member.FamilyID != familyID {
		return familydomain.Member{}, familydomain.ErrMemberNotFound
	}
	if member.Role == familydomain.RoleOwner {
		return familydomain.Member{}, familydomain.ErrLastOwner
	}
	member.Role = role
	s.members[targetUserID] = member
	return member, nil
}

func (s *familyTestService) AdminRemoveMember(
	_ context.Context,
	familyID string,
	_ string,
	targetUserID string,
	_ int64,
) error {
	member, ok := s.members[targetUserID]
	if !ok || member.FamilyID != familyID {
		return familydomain.ErrMemberNotFound
	}
	delete(s.members, targetUserID)
	return nil
}

type familyTestAccountReader struct {
	emails map[string]string
}

func (r familyTestAccountReader) AccountEmail(_ context.Context, userID string) (string, error) {
	return r.emails[userID], nil
}

func newFamilyHandlerForTest() (familyHandler, *familyTestService) {
	service := newFamilyTestService()
	_, _ = service.BootstrapOwner(context.Background(), "family-1", "owner-1")
	return familyHandler{service: service}, service
}

func TestFamilyMembersRequireAuthentication(t *testing.T) {
	handler, _ := newFamilyHandlerForTest()
	request := englishRequest(http.MethodGet, "/api/v1/family/members", "", "")
	recorder := httptest.NewRecorder()
	handler.listMembers(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestFamilyCreateInvitation(t *testing.T) {
	handler, _ := newFamilyHandlerForTest()
	request := englishRequest(
		http.MethodPost,
		"/api/v1/family/invitations",
		`{"email":"guardian@example.com","role":"guardian"}`,
		"owner-1",
	)
	recorder := httptest.NewRecorder()
	handler.createInvitation(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Invitation familydomain.Invitation `json:"invitation"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Invitation.Role != familydomain.RoleGuardian {
		t.Fatalf("unexpected invitation: %+v", envelope.Data.Invitation)
	}
}

func TestFamilyOwnerRoleInviteRejected(t *testing.T) {
	handler, _ := newFamilyHandlerForTest()
	request := englishRequest(
		http.MethodPost,
		"/api/v1/family/invitations",
		`{"email":"second@example.com","role":"owner"}`,
		"owner-1",
	)
	recorder := httptest.NewRecorder()
	handler.createInvitation(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

func TestFamilyCapabilityReportsOwner(t *testing.T) {
	handler, _ := newFamilyHandlerForTest()
	request := englishRequest(http.MethodGet, "/api/v1/family/capability", "", "owner-1")
	request = request.WithContext(context.WithValue(
		request.Context(),
		familyAccessKey{},
		familydomain.Access{
			FamilyID:    "family-1",
			ActorUserID: "owner-1",
			OwnerUserID: "owner-1",
			Role:        familydomain.RoleOwner,
		},
	))
	recorder := httptest.NewRecorder()
	handler.capability(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			OwnerUserID string          `json:"owner_user_id"`
			MemberCount int             `json:"member_count"`
			Permissions map[string]bool `json:"permissions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.OwnerUserID != "owner-1" || envelope.Data.MemberCount != 1 {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
	}
	if !envelope.Data.Permissions["member_manage"] ||
		!envelope.Data.Permissions["ai_account_manage"] {
		t.Fatalf("owner permissions missing: %+v", envelope.Data.Permissions)
	}
}

func TestFamilyAcceptInvitationMatchesEmail(t *testing.T) {
	handler, service := newFamilyHandlerForTest()
	service.invitations["invitation-1"] = familydomain.Invitation{
		ID:       "invitation-1",
		FamilyID: "family-1",
		Email:    "guardian@example.com",
		Role:     familydomain.RoleGuardian,
		Status:   familydomain.InvitationPending,
	}
	handler.accountReader = familyTestAccountReader{
		emails: map[string]string{"user-2": "guardian@example.com"},
	}
	request := englishRequest(
		http.MethodPost,
		"/api/v1/family/invitations/invitation-1/accept",
		"",
		"user-2",
	)
	request.SetPathValue("family_id", "family-1")
	request.SetPathValue("invitation_id", "invitation-1")
	recorder := httptest.NewRecorder()
	handler.acceptInvitation(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestFamilyListMyInvitationsFindsPendingByEmail(t *testing.T) {
	handler, service := newFamilyHandlerForTest()
	service.invitations["invitation-1"] = familydomain.Invitation{
		ID:       "invitation-1",
		FamilyID: "family-1",
		Email:    "Guardian@Example.com",
		Role:     familydomain.RoleGuardian,
		Status:   familydomain.InvitationPending,
	}
	service.invitations["invitation-2"] = familydomain.Invitation{
		ID:       "invitation-2",
		FamilyID: "family-1",
		Email:    "someone-else@example.com",
		Role:     familydomain.RoleViewer,
		Status:   familydomain.InvitationPending,
	}
	handler.accountReader = familyTestAccountReader{
		emails: map[string]string{"user-2": "guardian@example.com"},
	}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/family/invitations/mine",
		"",
		"user-2",
	)
	recorder := httptest.NewRecorder()
	handler.listMyInvitations(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Invitations []familydomain.Invitation `json:"invitations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.Invitations) != 1 ||
		envelope.Data.Invitations[0].ID != "invitation-1" {
		t.Fatalf("unexpected invitations: %+v", envelope.Data.Invitations)
	}
}

func TestFamilyListMyInvitationsRequiresAuthentication(t *testing.T) {
	handler, _ := newFamilyHandlerForTest()
	request := englishRequest(
		http.MethodGet,
		"/api/v1/family/invitations/mine",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.listMyInvitations(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestFamilyRemoveOwnerRejected(t *testing.T) {
	handler, _ := newFamilyHandlerForTest()
	request := englishRequest(
		http.MethodDelete,
		"/api/v1/family/members/owner-1",
		"",
		"owner-1",
	)
	request.SetPathValue("user_id", "owner-1")
	recorder := httptest.NewRecorder()
	handler.removeMember(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func TestFamilyViewerMemberRoutesAreReadOnly(t *testing.T) {
	handler, service := newFamilyHandlerForTest()
	service.members["viewer-1"] = familydomain.Member{
		ID:       "member-viewer",
		FamilyID: "family-1",
		UserID:   "viewer-1",
		Role:     familydomain.RoleViewer,
	}

	readRequest := englishRequest(
		http.MethodGet,
		"/api/v1/family/members",
		"",
		"viewer-1",
	)
	readRecorder := httptest.NewRecorder()
	handler.requireFamilyAction(
		familydomain.ActionMemberRead,
		handler.listMembers,
	)(readRecorder, readRequest)
	if readRecorder.Code != http.StatusOK {
		t.Fatalf("viewer read status = %d, want 200: %s",
			readRecorder.Code, readRecorder.Body.String())
	}

	writeRequest := englishRequest(
		http.MethodPut,
		"/api/v1/family/members/owner-1",
		`{"role":"viewer"}`,
		"viewer-1",
	)
	writeRequest.SetPathValue("user_id", "owner-1")
	writeRecorder := httptest.NewRecorder()
	handler.requireFamilyAction(
		familydomain.ActionMemberManage,
		handler.updateMemberRole,
	)(writeRecorder, writeRequest)
	if writeRecorder.Code != http.StatusForbidden {
		t.Fatalf("viewer write status = %d, want 403: %s",
			writeRecorder.Code, writeRecorder.Body.String())
	}

	capabilityRequest := englishRequest(
		http.MethodGet,
		"/api/v1/family/capability",
		"",
		"viewer-1",
	)
	capabilityRequest = capabilityRequest.WithContext(context.WithValue(
		capabilityRequest.Context(),
		familyAccessKey{},
		familydomain.Access{
			FamilyID:    "family-1",
			ActorUserID: "viewer-1",
			OwnerUserID: "owner-1",
			Role:        familydomain.RoleViewer,
		},
	))
	capabilityRecorder := httptest.NewRecorder()
	handler.capability(capabilityRecorder, capabilityRequest)
	if capabilityRecorder.Code != http.StatusOK {
		t.Fatalf("viewer capability status = %d, want 200: %s",
			capabilityRecorder.Code, capabilityRecorder.Body.String())
	}
	var capabilityEnvelope struct {
		Data struct {
			Permissions map[string]bool `json:"permissions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(capabilityRecorder.Body.Bytes(), &capabilityEnvelope); err != nil {
		t.Fatalf("decode capability: %v", err)
	}
	if capabilityEnvelope.Data.Permissions["device_manage"] ||
		capabilityEnvelope.Data.Permissions["device_bind"] ||
		capabilityEnvelope.Data.Permissions["vision_execute"] ||
		capabilityEnvelope.Data.Permissions["privacy_manage"] ||
		capabilityEnvelope.Data.Permissions["ai_account_manage"] {
		t.Fatalf("viewer received write permissions: %+v",
			capabilityEnvelope.Data.Permissions)
	}
	if !capabilityEnvelope.Data.Permissions["device_read"] ||
		!capabilityEnvelope.Data.Permissions["vision_read"] {
		t.Fatalf("viewer read permissions missing: %+v",
			capabilityEnvelope.Data.Permissions)
	}
}

func TestFamilyGuardianCannotChangeOwnerOrRemovePeers(t *testing.T) {
	handler, service := newFamilyHandlerForTest()
	service.members["guardian-1"] = familydomain.Member{
		ID:       "member-guardian",
		FamilyID: "family-1",
		UserID:   "guardian-1",
		Role:     familydomain.RoleGuardian,
	}

	updateRequest := englishRequest(
		http.MethodPut,
		"/api/v1/family/members/owner-1",
		`{"role":"viewer"}`,
		"guardian-1",
	)
	updateRequest.SetPathValue("user_id", "owner-1")
	updateRecorder := httptest.NewRecorder()
	handler.requireFamilyAction(
		familydomain.ActionMemberManage,
		handler.updateMemberRole,
	)(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusForbidden {
		t.Fatalf("guardian update status = %d, want 403: %s",
			updateRecorder.Code, updateRecorder.Body.String())
	}

	removeRequest := englishRequest(
		http.MethodDelete,
		"/api/v1/family/members/owner-1",
		"",
		"guardian-1",
	)
	removeRequest.SetPathValue("user_id", "owner-1")
	removeRecorder := httptest.NewRecorder()
	handler.removeMember(removeRecorder, removeRequest)
	if removeRecorder.Code != http.StatusConflict {
		t.Fatalf("guardian remove owner status = %d, want 409: %s",
			removeRecorder.Code, removeRecorder.Body.String())
	}
}

func TestFamilyGuardianCanSelfLeave(t *testing.T) {
	handler, service := newFamilyHandlerForTest()
	service.members["guardian-1"] = familydomain.Member{
		ID:       "member-guardian",
		FamilyID: "family-1",
		UserID:   "guardian-1",
		Role:     familydomain.RoleGuardian,
	}

	request := englishRequest(
		http.MethodDelete,
		"/api/v1/family/members/guardian-1",
		"",
		"guardian-1",
	)
	request.SetPathValue("user_id", "guardian-1")
	recorder := httptest.NewRecorder()
	handler.removeMember(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("guardian self-leave status = %d, want 200: %s",
			recorder.Code, recorder.Body.String())
	}
}
