package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

// fixedClock returns a stable instant so invitation expiry is deterministic.
type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

type fakeRepository struct {
	members     map[string]domain.Member
	invitations map[string]domain.Invitation
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		members:     map[string]domain.Member{},
		invitations: map[string]domain.Invitation{},
	}
}

func memberKey(familyID, userID string) string { return familyID + "|" + userID }

func (r *fakeRepository) CreateMember(_ context.Context, member domain.Member) error {
	key := memberKey(member.FamilyID, member.UserID)
	if _, exists := r.members[key]; exists {
		return domain.ErrMemberExists
	}
	if member.Role == domain.RoleOwner {
		for _, existing := range r.members {
			if existing.FamilyID == member.FamilyID && existing.Role == domain.RoleOwner {
				return domain.ErrLastOwner
			}
		}
	}
	r.members[key] = member
	return nil
}

func (r *fakeRepository) UpdateMemberRole(_ context.Context, member domain.Member) error {
	key := memberKey(member.FamilyID, member.UserID)
	if _, exists := r.members[key]; !exists {
		return domain.ErrMemberNotFound
	}
	r.members[key] = member
	return nil
}

func (r *fakeRepository) DeleteMember(_ context.Context, familyID, userID string) error {
	key := memberKey(familyID, userID)
	if _, exists := r.members[key]; !exists {
		return domain.ErrMemberNotFound
	}
	delete(r.members, key)
	return nil
}

func (r *fakeRepository) Members(_ context.Context, familyID string) ([]domain.Member, error) {
	members := make([]domain.Member, 0, 8)
	for _, member := range r.members {
		if member.FamilyID == familyID {
			members = append(members, member)
		}
	}
	return members, nil
}

func (r *fakeRepository) Member(
	_ context.Context,
	familyID string,
	userID string,
) (domain.Member, bool, error) {
	member, ok := r.members[memberKey(familyID, userID)]
	return member, ok, nil
}

func (r *fakeRepository) CreateInvitation(_ context.Context, invitation domain.Invitation) error {
	for _, existing := range r.invitations {
		if existing.FamilyID == invitation.FamilyID &&
			existing.Status == domain.InvitationPending &&
			domain.NormalizeEmail(existing.Email) == domain.NormalizeEmail(invitation.Email) {
			return domain.ErrInvitationState
		}
	}
	r.invitations[invitation.ID] = invitation
	return nil
}

func (r *fakeRepository) Invitation(
	_ context.Context,
	familyID string,
	invitationID string,
) (domain.Invitation, bool, error) {
	invitation, ok := r.invitations[invitationID]
	if !ok || invitation.FamilyID != familyID {
		return domain.Invitation{}, false, nil
	}
	return invitation, true, nil
}

func (r *fakeRepository) Invitations(_ context.Context, familyID string) ([]domain.Invitation, error) {
	invitations := make([]domain.Invitation, 0, 8)
	for _, invitation := range r.invitations {
		if invitation.FamilyID == familyID {
			invitations = append(invitations, invitation)
		}
	}
	return invitations, nil
}

func (r *fakeRepository) UpdateInvitationState(
	_ context.Context,
	familyID string,
	invitationID string,
	status string,
) error {
	invitation, ok := r.invitations[invitationID]
	if !ok || invitation.FamilyID != familyID || invitation.Status != domain.InvitationPending {
		return domain.ErrInvitationState
	}
	invitation.Status = status
	if status == domain.InvitationAccepted {
		now := time.Now().UTC()
		invitation.AcceptedAt = &now
	}
	r.invitations[invitationID] = invitation
	return nil
}

func newService(t *testing.T, repository *fakeRepository, now time.Time) *Service {
	t.Helper()
	service, err := New(Options{
		Repository: repository,
		Clock:      fixedClock{now: now},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	return service
}

func TestBootstrapOwnerIsIdempotent(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository, time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC))

	owner, err := service.BootstrapOwner(context.Background(), "family-1", "user-1")
	if err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	if owner.Role != domain.RoleOwner {
		t.Fatalf("unexpected owner role: %+v", owner)
	}
	again, err := service.BootstrapOwner(context.Background(), "family-1", "user-1")
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if again.ID != owner.ID {
		t.Fatalf("bootstrap must be idempotent: %+v vs %+v", again, owner)
	}
	members, err := repository.Members(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected exactly one member, got %d", len(members))
	}
}

func TestCreateInvitationRejectsViewerAndOwnerRole(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	service := newService(t, repository, now)
	if _, err := service.BootstrapOwner(context.Background(), "family-1", "owner-1"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// A guardian may invite, but never with the unique owner role.
	invite, err := service.CreateInvitation(
		context.Background(), "family-1", "owner-1", "Guardian@Example.com", domain.RoleGuardian,
	)
	if err != nil {
		t.Fatalf("owner invite: %v", err)
	}
	if invite.Role != domain.RoleGuardian || invite.Status != domain.InvitationPending {
		t.Fatalf("unexpected invitation: %+v", invite)
	}
	if invite.Email != "guardian@example.com" {
		t.Fatalf("email must normalize: %q", invite.Email)
	}
	if _, err := service.CreateInvitation(
		context.Background(), "family-1", "owner-1", "second@example.com", domain.RoleOwner,
	); !errors.Is(err, domain.ErrInvalidInvitation) {
		t.Fatalf("owner-role invite must be rejected, got %v", err)
	}
	// A non-member cannot invite.
	if _, err := service.CreateInvitation(
		context.Background(), "family-1", "stranger", "x@example.com", domain.RoleViewer,
	); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("non-member invite must be rejected, got %v", err)
	}
}

func TestAcceptInvitationAddsMemberAndBindsEmail(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	service := newService(t, repository, now)
	if _, err := service.BootstrapOwner(context.Background(), "family-1", "owner-1"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	invite, err := service.CreateInvitation(
		context.Background(), "family-1", "owner-1", "guardian@example.com", domain.RoleGuardian,
	)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	// A different login cannot redeem the invitation.
	if _, err := service.AcceptInvitation(
		context.Background(), "family-1", invite.ID, "user-2", "someone-else@example.com",
	); !errors.Is(err, domain.ErrInvitationNotFound) {
		t.Fatalf("mismatched email must be rejected, got %v", err)
	}
	member, err := service.AcceptInvitation(
		context.Background(), "family-1", invite.ID, "user-2", "guardian@example.com",
	)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if member.Role != domain.RoleGuardian || member.UserID != "user-2" {
		t.Fatalf("unexpected member: %+v", member)
	}
	// The invitation is no longer pending and cannot be replayed.
	if _, err := service.AcceptInvitation(
		context.Background(), "family-1", invite.ID, "user-3", "guardian@example.com",
	); !errors.Is(err, domain.ErrInvitationState) {
		t.Fatalf("replayed invitation must be rejected, got %v", err)
	}
}

func TestAcceptInvitationRejectsExpired(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	service := newService(t, repository, now)
	if _, err := service.BootstrapOwner(context.Background(), "family-1", "owner-1"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	invite, err := service.CreateInvitation(
		context.Background(), "family-1", "owner-1", "guardian@example.com", domain.RoleGuardian,
	)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	// Move past the invitation TTL and try to accept.
	later := newService(t, repository, now.Add((domain.InvitationTTLHours+1)*time.Hour))
	if _, err := later.AcceptInvitation(
		context.Background(), "family-1", invite.ID, "user-2", "guardian@example.com",
	); !errors.Is(err, domain.ErrInvitationExpired) {
		t.Fatalf("expired invitation must be rejected, got %v", err)
	}
}

func TestOwnerInvariantOnRoleAndRemoval(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	service := newService(t, repository, now)
	if _, err := service.BootstrapOwner(context.Background(), "family-1", "owner-1"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	invite, err := service.CreateInvitation(
		context.Background(), "family-1", "owner-1", "guardian@example.com", domain.RoleGuardian,
	)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := service.AcceptInvitation(
		context.Background(), "family-1", invite.ID, "user-2", "guardian@example.com",
	); err != nil {
		t.Fatalf("accept: %v", err)
	}
	// The owner cannot demote or remove itself, so a family always has an owner.
	if _, err := service.UpdateMemberRole(
		context.Background(), "family-1", "owner-1", "owner-1", domain.RoleViewer,
	); !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("owner self-demotion must be rejected, got %v", err)
	}
	if err := service.RemoveMember(
		context.Background(), "family-1", "owner-1", "owner-1",
	); !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("owner self-removal must be rejected, got %v", err)
	}
	// A guardian cannot manage a peer, but may leave the family.
	if _, err := service.UpdateMemberRole(
		context.Background(), "family-1", "user-2", "owner-1", domain.RoleViewer,
	); !errors.Is(err, domain.ErrInvalidMember) {
		t.Fatalf("guardian must not manage peers, got %v", err)
	}
	if err := service.RemoveMember(
		context.Background(), "family-1", "user-2", "user-2",
	); err != nil {
		t.Fatalf("a member must be able to leave: %v", err)
	}
	// The owner promotes the remaining guardian and then removes it.
	if _, err := service.UpdateMemberRole(
		context.Background(), "family-1", "owner-1", "owner-1", domain.RoleGuardian,
	); !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("owner role change must be rejected, got %v", err)
	}
}

func TestCapabilitySummarizesMembership(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	service := newService(t, repository, now)
	if _, err := service.BootstrapOwner(context.Background(), "family-1", "owner-1"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := service.CreateInvitation(
		context.Background(), "family-1", "owner-1", "guardian@example.com", domain.RoleGuardian,
	); err != nil {
		t.Fatalf("invite: %v", err)
	}
	capability, err := service.Capability(context.Background(), "family-1", "owner-1")
	if err != nil {
		t.Fatalf("capability: %v", err)
	}
	if capability.OwnerUserID != "owner-1" {
		t.Fatalf("unexpected owner: %+v", capability)
	}
	if capability.MemberCount != 1 || capability.PendingInvites != 1 {
		t.Fatalf("unexpected capability counts: %+v", capability)
	}
	if capability.SchemaVersion != domain.SchemaVersion {
		t.Fatalf("unexpected schema version: %+v", capability)
	}
	// A non-member cannot read the capability.
	if _, err := service.Capability(
		context.Background(), "family-1", "stranger",
	); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("non-member capability must be rejected, got %v", err)
	}
}
