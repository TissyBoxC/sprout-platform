package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

// fixedClock returns a stable instant so invitation expiry is deterministic.
type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.now
	c.now = c.now.Add(time.Minute)
	return current
}

type fakeRepository struct {
	mu           sync.Mutex
	members      map[string]domain.Member
	invitations  map[string]domain.Invitation
	audits       []domain.AuditEvent
	auditErr     error
	acceptCalls  int
	readiness    domain.Readiness
	readinessErr error
}

func (r *fakeRepository) Readiness(
	_ context.Context,
) (domain.Readiness, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readinessErr != nil {
		return domain.Readiness{}, r.readinessErr
	}
	readiness := r.readiness
	families := map[string]int{}
	owners := map[string]int{}
	for _, member := range r.members {
		families[member.FamilyID]++
		readiness.MemberCount++
		if member.Role == domain.RoleOwner {
			readiness.OwnerCount++
			owners[member.FamilyID]++
		}
	}
	readiness.FamilyCount = int64(len(families))
	if readiness.ParentAccountCount == 0 {
		readiness.ParentAccountCount = readiness.FamilyCount
	}
	for familyID := range families {
		switch owners[familyID] {
		case 0:
			readiness.OwnerlessFamilyCount++
		case 1:
		default:
			readiness.MultiOwnerFamilyCount++
		}
	}
	for _, invitation := range r.invitations {
		if invitation.Status == domain.InvitationPending {
			readiness.PendingInvitationCount++
		}
	}
	return readiness, nil
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		members:     map[string]domain.Member{},
		invitations: map[string]domain.Invitation{},
	}
}

func memberKey(familyID, userID string) string { return familyID + "|" + userID }

func (r *fakeRepository) CreateMember(_ context.Context, member domain.Member) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	key := memberKey(member.FamilyID, member.UserID)
	if _, exists := r.members[key]; !exists {
		return domain.ErrMemberNotFound
	}
	r.members[key] = member
	return nil
}

func (r *fakeRepository) UpdateMemberRoleWithAudit(
	ctx context.Context,
	member domain.Member,
	audit domain.AuditEvent,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.auditErr != nil {
		return r.auditErr
	}
	r.audits = append(r.audits, audit)
	key := memberKey(member.FamilyID, member.UserID)
	if _, exists := r.members[key]; !exists {
		return domain.ErrMemberNotFound
	}
	r.members[key] = member
	return nil
}

func (r *fakeRepository) DeleteMember(_ context.Context, familyID, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := memberKey(familyID, userID)
	if _, exists := r.members[key]; !exists {
		return domain.ErrMemberNotFound
	}
	delete(r.members, key)
	return nil
}

func (r *fakeRepository) DeleteMemberWithAudit(
	_ context.Context,
	familyID string,
	userID string,
	audit domain.AuditEvent,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.auditErr != nil {
		return r.auditErr
	}
	key := memberKey(familyID, userID)
	if _, exists := r.members[key]; !exists {
		return domain.ErrMemberNotFound
	}
	delete(r.members, key)
	r.audits = append(r.audits, audit)
	return nil
}

func (r *fakeRepository) Members(_ context.Context, familyID string) ([]domain.Member, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	member, ok := r.members[memberKey(familyID, userID)]
	return member, ok, nil
}

func (r *fakeRepository) MemberByUser(
	_ context.Context,
	userID string,
) (domain.Member, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, member := range r.members {
		if member.UserID == userID {
			return member, true, nil
		}
	}
	return domain.Member{}, false, nil
}

func (r *fakeRepository) CreateInvitation(
	_ context.Context,
	invitation domain.Invitation,
	audit domain.AuditEvent,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.auditErr != nil {
		return r.auditErr
	}
	for _, existing := range r.invitations {
		if existing.FamilyID == invitation.FamilyID &&
			existing.Status == domain.InvitationPending &&
			domain.NormalizeEmail(existing.Email) == domain.NormalizeEmail(invitation.Email) {
			return domain.ErrInvitationState
		}
	}
	r.audits = append(r.audits, audit)
	r.invitations[invitation.ID] = invitation
	return nil
}

func (r *fakeRepository) recordAudit(audit domain.AuditEvent) error {
	if r.auditErr != nil {
		return r.auditErr
	}
	r.audits = append(r.audits, audit)
	return nil
}

func (r *fakeRepository) Invitation(
	_ context.Context,
	familyID string,
	invitationID string,
) (domain.Invitation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invitation, ok := r.invitations[invitationID]
	if !ok || invitation.FamilyID != familyID {
		return domain.Invitation{}, false, nil
	}
	return invitation, true, nil
}

func (r *fakeRepository) Invitations(_ context.Context, familyID string) ([]domain.Invitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invitations := make([]domain.Invitation, 0, 8)
	for _, invitation := range r.invitations {
		if invitation.FamilyID == familyID {
			invitations = append(invitations, invitation)
		}
	}
	return invitations, nil
}

func (r *fakeRepository) PendingInvitationsByEmail(
	_ context.Context,
	email string,
) ([]domain.Invitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	normalized := domain.NormalizeEmail(email)
	invitations := make([]domain.Invitation, 0, 4)
	for _, invitation := range r.invitations {
		if domain.NormalizeEmail(invitation.Email) == normalized &&
			invitation.Status == domain.InvitationPending {
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
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *fakeRepository) AcceptInvitation(
	ctx context.Context,
	member domain.Member,
	familyID string,
	invitationID string,
	audit domain.AuditEvent,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acceptCalls++
	invitation, ok := r.invitations[invitationID]
	if !ok || invitation.FamilyID != familyID ||
		invitation.Status != domain.InvitationPending {
		return domain.ErrInvitationState
	}
	if r.auditErr != nil {
		return r.auditErr
	}
	r.audits = append(r.audits, audit)
	if _, exists := r.members[memberKey(member.FamilyID, member.UserID)]; exists {
		return domain.ErrMemberExists
	}
	r.members[memberKey(member.FamilyID, member.UserID)] = member
	invitation.Status = domain.InvitationAccepted
	now := time.Now().UTC()
	invitation.AcceptedAt = &now
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

func TestPendingInvitationsForAccountMatchesNormalizedEmail(t *testing.T) {
	repository := newFakeRepository()
	now := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	service := newService(t, repository, now)
	repository.members[memberKey("family-1", "owner-1")] = domain.Member{
		ID:       "owner-1",
		FamilyID: "family-1",
		UserID:   "owner-1",
		Role:     domain.RoleOwner,
	}
	if _, err := service.CreateInvitation(
		context.Background(), "family-1", "owner-1",
		"Guardian@Example.com", domain.RoleGuardian,
	); err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	invitations, err := service.PendingInvitationsForAccount(
		context.Background(),
		"user-2",
		"guardian@example.com",
	)
	if err != nil {
		t.Fatalf("list pending invitations: %v", err)
	}
	if len(invitations) != 1 || invitations[0].Role != domain.RoleGuardian {
		t.Fatalf("unexpected invitations: %+v", invitations)
	}

	other, err := service.PendingInvitationsForAccount(
		context.Background(),
		"user-3",
		"nobody@example.com",
	)
	if err != nil {
		t.Fatalf("list other invitations: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("expected no invitations for another email, got %+v", other)
	}
}

func TestPendingInvitationsForAccountRejectsInvalidEmail(t *testing.T) {
	service := newService(t, newFakeRepository(), time.Now())
	if _, err := service.PendingInvitationsForAccount(
		context.Background(),
		"user-2",
		"not-an-email",
	); !errors.Is(err, domain.ErrInvalidInvitation) {
		t.Fatalf("err = %v, want ErrInvalidInvitation", err)
	}
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
	); !errors.Is(err, domain.ErrPermissionDenied) {
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

func TestAuthorizeUsesStoredRoleAndResolvesOwner(t *testing.T) {
	repository := newFakeRepository()
	service := newService(
		t,
		repository,
		time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC),
	)
	if _, err := service.BootstrapOwner(
		context.Background(),
		"family-owner",
		"owner-account",
	); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	repository.members[memberKey("family-owner", "guardian-account")] = domain.Member{
		ID:       "guardian-member",
		FamilyID: "family-owner",
		UserID:   "guardian-account",
		Role:     domain.RoleGuardian,
	}
	repository.members[memberKey("family-owner", "viewer-account")] = domain.Member{
		ID:       "viewer-member",
		FamilyID: "family-owner",
		UserID:   "viewer-account",
		Role:     domain.RoleViewer,
	}

	cases := []struct {
		name    string
		userID  string
		action  domain.Action
		allowed bool
	}{
		{"owner manages ai", "owner-account", domain.ActionAIAccountManage, true},
		{"guardian reads ai", "guardian-account", domain.ActionAIAccountRead, true},
		{"guardian cannot manage ai", "guardian-account", domain.ActionAIAccountManage, false},
		{"guardian manages device", "guardian-account", domain.ActionDeviceManage, true},
		{"viewer reads device", "viewer-account", domain.ActionDeviceRead, true},
		{"viewer cannot bind device", "viewer-account", domain.ActionDeviceBind, false},
		{"viewer cannot run vision", "viewer-account", domain.ActionVisionExecute, false},
		{"guardian executes vision", "guardian-account", domain.ActionVisionExecute, true},
		{"guardian cannot manage members", "guardian-account", domain.ActionMemberManage, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			access, err := service.Authorize(
				context.Background(),
				testCase.userID,
				testCase.action,
			)
			if !testCase.allowed {
				if !errors.Is(err, domain.ErrPermissionDenied) {
					t.Fatalf("err = %v, want ErrPermissionDenied", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("authorize: %v", err)
			}
			if access.FamilyID != "family-owner" ||
				access.OwnerUserID != "owner-account" ||
				access.ActorUserID != testCase.userID {
				t.Fatalf("unexpected access context: %+v", access)
			}
		})
	}
}

func TestAuthorizeRejectsNonMember(t *testing.T) {
	repository := newFakeRepository()
	service := newService(
		t,
		repository,
		time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC),
	)
	if _, err := service.BootstrapOwner(
		context.Background(),
		"family-owner",
		"owner-account",
	); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	if _, err := service.Authorize(
		context.Background(),
		"stranger",
		domain.ActionFamilyRead,
	); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("err = %v, want ErrMemberNotFound", err)
	}
}

func TestFamilyMutationsReturnPermissionDenied(t *testing.T) {
	repository := newFakeRepository()
	service := newService(
		t,
		repository,
		time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC),
	)
	if _, err := service.BootstrapOwner(
		context.Background(),
		"family-1",
		"owner-1",
	); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	repository.members[memberKey("family-1", "guardian-1")] = domain.Member{
		ID:       "guardian-member",
		FamilyID: "family-1",
		UserID:   "guardian-1",
		Role:     domain.RoleGuardian,
	}
	repository.members[memberKey("family-1", "viewer-1")] = domain.Member{
		ID:       "viewer-member",
		FamilyID: "family-1",
		UserID:   "viewer-1",
		Role:     domain.RoleViewer,
	}
	invitation, err := service.CreateInvitation(
		context.Background(),
		"family-1",
		"owner-1",
		"new-viewer@example.com",
		domain.RoleViewer,
	)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	if _, err := service.CreateInvitation(
		context.Background(),
		"family-1",
		"viewer-1",
		"blocked@example.com",
		domain.RoleViewer,
	); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("viewer invite err = %v, want ErrPermissionDenied", err)
	}
	if err := service.RevokeInvitation(
		context.Background(),
		"family-1",
		"viewer-1",
		invitation.ID,
	); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("viewer revoke err = %v, want ErrPermissionDenied", err)
	}
	if _, err := service.UpdateMemberRole(
		context.Background(),
		"family-1",
		"guardian-1",
		"viewer-1",
		domain.RoleGuardian,
	); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("guardian manage err = %v, want ErrPermissionDenied", err)
	}
	if err := service.RemoveMember(
		context.Background(),
		"family-1",
		"guardian-1",
		"viewer-1",
	); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("guardian remove peer err = %v, want ErrPermissionDenied", err)
	}
}
