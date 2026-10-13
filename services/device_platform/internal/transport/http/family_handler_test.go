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
		return familydomain.Invitation{}, familydomain.ErrInvalidMember
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
		return familydomain.ErrInvalidMember
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
		return familydomain.Member{}, familydomain.ErrInvalidMember
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
		return familydomain.ErrInvalidMember
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
	recorder := httptest.NewRecorder()
	handler.capability(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			OwnerUserID string `json:"owner_user_id"`
			MemberCount int    `json:"member_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.OwnerUserID != "owner-1" || envelope.Data.MemberCount != 1 {
		t.Fatalf("unexpected capability: %+v", envelope.Data)
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
