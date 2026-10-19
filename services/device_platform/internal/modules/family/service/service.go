// Package service contains the family membership and invitation use cases.
//
// Every operation is family-scoped: the caller passes the authenticated family
// id, and the acting member's role is resolved from the stored membership
// instead of a client claim. The module keeps two invariants: a family always
// has exactly one owner, and the owner role is never granted by invitation.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	CreateMember(ctx context.Context, member domain.Member) error
	UpdateMemberRole(ctx context.Context, member domain.Member) error
	DeleteMember(ctx context.Context, familyID string, userID string) error
	UpdateMemberRoleWithAudit(
		ctx context.Context,
		member domain.Member,
		audit domain.AuditEvent,
	) error
	UpdateMemberRoleWithAuditRevision(
		ctx context.Context,
		member domain.Member,
		audit domain.AuditEvent,
		expectedRevision int64,
	) error
	DeleteMemberWithAudit(
		ctx context.Context,
		familyID string,
		userID string,
		audit domain.AuditEvent,
	) error
	DeleteMemberWithAuditRevision(
		ctx context.Context,
		familyID string,
		userID string,
		audit domain.AuditEvent,
		expectedRevision int64,
	) error
	Members(ctx context.Context, familyID string) ([]domain.Member, error)
	Member(ctx context.Context, familyID string, userID string) (domain.Member, bool, error)
	MemberByUser(ctx context.Context, userID string) (domain.Member, bool, error)

	CreateInvitation(
		ctx context.Context,
		invitation domain.Invitation,
		audit domain.AuditEvent,
	) error
	CreateInvitationRevision(
		ctx context.Context,
		invitation domain.Invitation,
		audit domain.AuditEvent,
		expectedRevision int64,
	) error
	Invitation(
		ctx context.Context,
		familyID string,
		invitationID string,
	) (domain.Invitation, bool, error)
	Invitations(ctx context.Context, familyID string) ([]domain.Invitation, error)
	PendingInvitationsByEmail(
		ctx context.Context,
		email string,
	) ([]domain.Invitation, error)
	UpdateInvitationState(
		ctx context.Context,
		familyID string,
		invitationID string,
		status string,
	) error
	UpdateInvitationStateWithAudit(
		ctx context.Context,
		familyID string,
		invitationID string,
		status string,
		audit domain.AuditEvent,
	) error
	UpdateInvitationStateWithAuditRevision(
		ctx context.Context,
		familyID string,
		invitationID string,
		status string,
		audit domain.AuditEvent,
		expectedRevision int64,
	) error
	AcceptInvitation(
		ctx context.Context,
		member domain.Member,
		familyID string,
		invitationID string,
		audit domain.AuditEvent,
	) error
	ParentIdentity(
		ctx context.Context,
		accountID string,
	) (domain.AccountIdentity, error)
	AccountIdentities(
		ctx context.Context,
		accountIDs []string,
	) (map[string]domain.AccountIdentity, error)
	MembershipRevision(
		ctx context.Context,
		familyID string,
	) (int64, error)
}

// Service owns family membership and invitations.
type Service struct {
	repository      Repository
	readinessReader ReadinessReader
	clock           clock.Clock
}

// Options contains family service dependencies.
type Options struct {
	Repository      Repository
	ReadinessReader ReadinessReader
	Clock           clock.Clock
}

// New creates the family service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("family repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	readinessReader := options.ReadinessReader
	if readinessReader == nil {
		if reader, ok := options.Repository.(ReadinessReader); ok {
			readinessReader = reader
		}
	}
	return &Service{
		repository:      options.Repository,
		readinessReader: readinessReader,
		clock:           timeSource,
	}, nil
}

// BootstrapOwner makes userID the owner of familyID when the family has no
// members yet. It is idempotent: once a member exists it returns the owner
// unchanged, so it is safe to call from registration and every authenticated
// request that may race.
func (s *Service) BootstrapOwner(
	ctx context.Context,
	familyID string,
	userID string,
) (domain.Member, error) {
	familyID = strings.TrimSpace(familyID)
	userID = strings.TrimSpace(userID)
	if familyID == "" || userID == "" {
		return domain.Member{}, domain.ErrInvalidMember
	}
	members, err := s.repository.Members(ctx, familyID)
	if err != nil {
		return domain.Member{}, err
	}
	if len(members) > 0 {
		for _, member := range members {
			if member.Role == domain.RoleOwner {
				return member, nil
			}
		}
		// A family always has an owner; if the stored rows disagree, surface it
		// rather than silently granting a second owner.
		return domain.Member{}, domain.ErrLastOwner
	}
	now := s.clock.Now().UTC()
	owner := domain.Member{
		ID:        uuid.NewString(),
		FamilyID:  familyID,
		UserID:    userID,
		Role:      domain.RoleOwner,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repository.CreateMember(ctx, owner); err != nil {
		return domain.Member{}, err
	}
	return owner, nil
}

// Members lists every member. Any member of the family may read the roster.
func (s *Service) Members(
	ctx context.Context,
	familyID string,
	actorUserID string,
) ([]domain.Member, error) {
	if _, err := s.requireMember(ctx, familyID, actorUserID); err != nil {
		return nil, err
	}
	return s.repository.Members(ctx, familyID)
}

// ResolveFamily returns the acting membership for an authenticated account.
// The HTTP layer uses it to scope a request that names no family id, so a
// guardian who joined by invitation reads the family they actually belong to.
func (s *Service) ResolveFamily(
	ctx context.Context,
	userID string,
) (domain.Member, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return domain.Member{}, domain.ErrInvalidMember
	}
	member, ok, err := s.repository.MemberByUser(ctx, userID)
	if err != nil {
		return domain.Member{}, err
	}
	if !ok {
		return domain.Member{}, domain.ErrMemberNotFound
	}
	return member, nil
}

// Authorize resolves the caller's stored membership and checks one canonical
// family action. The returned Access carries the real family id and the
// owner's account id, which may differ from an invited member's account id.
func (s *Service) Authorize(
	ctx context.Context,
	userID string,
	action domain.Action,
) (domain.Access, error) {
	member, err := s.ResolveFamily(ctx, userID)
	if err != nil {
		return domain.Access{}, err
	}
	if !domain.Can(member.Role, action) {
		return domain.Access{}, domain.ErrPermissionDenied
	}
	ownerUserID, err := s.ownerUserID(ctx, member.FamilyID)
	if err != nil {
		return domain.Access{}, err
	}
	return domain.Access{
		FamilyID:    member.FamilyID,
		ActorUserID: member.UserID,
		OwnerUserID: ownerUserID,
		Role:        member.Role,
	}, nil
}

// PendingInvitationsForAccount lists the pending invitations addressed to the
// authenticated account's email. It is how an invitee discovers an invitation
// before joining, when it has no family id to scope a request with. The email
// is normalized and only pending, unexpired rows are returned.
func (s *Service) PendingInvitationsForAccount(
	ctx context.Context,
	userID string,
	email string,
) ([]domain.Invitation, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, domain.ErrInvalidMember
	}
	normalized := domain.NormalizeEmail(email)
	if err := domain.ValidateInviteEmail(normalized); err != nil {
		return nil, err
	}
	return s.repository.PendingInvitationsByEmail(ctx, normalized)
}

// Invitations lists a family's invitations. Any member may read them.
func (s *Service) Invitations(
	ctx context.Context,
	familyID string,
	actorUserID string,
) ([]domain.Invitation, error) {
	if _, err := s.requireMember(ctx, familyID, actorUserID); err != nil {
		return nil, err
	}
	return s.repository.Invitations(ctx, familyID)
}

// CreateInvitation stores one pending invitation. The actor must be an owner or
// guardian, and the invited role may never be the unique owner role.
func (s *Service) CreateInvitation(
	ctx context.Context,
	familyID string,
	actorUserID string,
	email string,
	role string,
) (domain.Invitation, error) {
	actor, err := s.requireMember(ctx, familyID, actorUserID)
	if err != nil {
		return domain.Invitation{}, err
	}
	if !domain.CanInvite(actor.Role) {
		return domain.Invitation{}, domain.ErrPermissionDenied
	}
	normalizedEmail := domain.NormalizeEmail(email)
	if err := domain.ValidateInviteEmail(normalizedEmail); err != nil {
		return domain.Invitation{}, err
	}
	if err := domain.ValidateInviteRole(role); err != nil {
		return domain.Invitation{}, err
	}
	now := s.clock.Now().UTC()
	invitation := domain.Invitation{
		ID:              uuid.NewString(),
		FamilyID:        familyID,
		Email:           normalizedEmail,
		Role:            role,
		Status:          domain.InvitationPending,
		InvitedByUserID: actor.UserID,
		CreatedAt:       now,
		ExpiresAt:       now.Add(domain.InvitationTTLHours * time.Hour),
	}
	if err := s.repository.CreateInvitation(
		ctx,
		invitation,
		domain.AuditEvent{
			ID:             uuid.NewString(),
			ActorAccountID: actor.UserID,
			Action:         domain.AuditInvitationCreated,
			Detail: map[string]any{
				"family_id":     invitation.FamilyID,
				"invitation_id": invitation.ID,
				"role":          invitation.Role,
				"expires_at":    invitation.ExpiresAt.UTC().Format(time.RFC3339),
			},
			CreatedAt: now,
		},
	); err != nil {
		return domain.Invitation{}, err
	}
	return invitation, nil
}

// RevokeInvitation cancels one pending invitation. Owners and guardians may
// revoke; a viewer may not.
func (s *Service) RevokeInvitation(
	ctx context.Context,
	familyID string,
	actorUserID string,
	invitationID string,
) error {
	actor, err := s.requireMember(ctx, familyID, actorUserID)
	if err != nil {
		return err
	}
	if !domain.CanInvite(actor.Role) {
		return domain.ErrPermissionDenied
	}
	invitationID = strings.TrimSpace(invitationID)
	if invitationID == "" {
		return domain.ErrInvalidInvitation
	}
	return s.repository.UpdateInvitationStateWithAudit(
		ctx,
		familyID,
		invitationID,
		domain.InvitationRevoked,
		domain.AuditEvent{
			ID:             uuid.NewString(),
			ActorAccountID: actor.UserID,
			Action:         domain.AuditInvitationRevoked,
			Detail: map[string]any{
				"family_id":     familyID,
				"invitation_id": invitationID,
			},
			CreatedAt: s.clock.Now().UTC(),
		},
	)
}

// AcceptInvitation turns a pending, unexpired invitation into a membership for
// the authenticated user. The invited email must match the accepting account,
// so an invitation cannot be redeemed by a different login.
func (s *Service) AcceptInvitation(
	ctx context.Context,
	familyID string,
	invitationID string,
	userID string,
	email string,
) (domain.Member, error) {
	familyID = strings.TrimSpace(familyID)
	invitationID = strings.TrimSpace(invitationID)
	userID = strings.TrimSpace(userID)
	if familyID == "" || invitationID == "" || userID == "" {
		return domain.Member{}, domain.ErrInvalidInvitation
	}
	invitation, ok, err := s.repository.Invitation(ctx, familyID, invitationID)
	if err != nil {
		return domain.Member{}, err
	}
	if !ok {
		return domain.Member{}, domain.ErrInvitationNotFound
	}
	if invitation.Status != domain.InvitationPending {
		return domain.Member{}, domain.ErrInvitationState
	}
	now := s.clock.Now().UTC()
	if domain.InvitationExpired(invitation, now) {
		return domain.Member{}, domain.ErrInvitationExpired
	}
	if domain.NormalizeEmail(email) != domain.NormalizeEmail(invitation.Email) {
		return domain.Member{}, domain.ErrInvitationNotFound
	}
	if _, exists, err := s.repository.Member(ctx, familyID, userID); err != nil {
		return domain.Member{}, err
	} else if exists {
		return domain.Member{}, domain.ErrMemberExists
	}
	member := domain.Member{
		ID:        uuid.NewString(),
		FamilyID:  familyID,
		UserID:    userID,
		Role:      invitation.Role,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repository.AcceptInvitation(
		ctx,
		member,
		familyID,
		invitationID,
		domain.AuditEvent{
			ID:              uuid.NewString(),
			ActorAccountID:  userID,
			TargetAccountID: userID,
			Action:          domain.AuditInvitationAccepted,
			Detail: map[string]any{
				"family_id":     familyID,
				"invitation_id": invitationID,
				"member_id":     member.ID,
				"role":          member.Role,
			},
			CreatedAt: now,
		},
	); err != nil {
		if errors.Is(err, domain.ErrInvitationConcurrent) {
			return domain.Member{}, domain.ErrInvitationConcurrent
		}
		return domain.Member{}, err
	}
	return member, nil
}

// UpdateMemberRole changes one member's role. Only the owner may, and the owner
// role itself cannot be reassigned or duplicated through this path.
func (s *Service) UpdateMemberRole(
	ctx context.Context,
	familyID string,
	actorUserID string,
	targetUserID string,
	role string,
) (domain.Member, error) {
	actor, err := s.requireMember(ctx, familyID, actorUserID)
	if err != nil {
		return domain.Member{}, err
	}
	if !domain.CanManageMembers(actor.Role) {
		return domain.Member{}, domain.ErrPermissionDenied
	}
	if role != domain.RoleGuardian && role != domain.RoleViewer {
		return domain.Member{}, domain.ErrInvalidMember
	}
	targetUserID = strings.TrimSpace(targetUserID)
	target, ok, err := s.repository.Member(ctx, familyID, targetUserID)
	if err != nil {
		return domain.Member{}, err
	}
	if !ok {
		return domain.Member{}, domain.ErrMemberNotFound
	}
	if target.Role == domain.RoleOwner {
		return domain.Member{}, domain.ErrLastOwner
	}
	target.Role = role
	target.UpdatedAt = s.clock.Now().UTC()
	if err := s.repository.UpdateMemberRoleWithAudit(
		ctx,
		target,
		domain.AuditEvent{
			ID:              uuid.NewString(),
			ActorAccountID:  actor.UserID,
			TargetAccountID: target.UserID,
			Action:          domain.AuditMemberRoleUpdated,
			Detail: map[string]any{
				"family_id":   target.FamilyID,
				"member_id":   target.ID,
				"target_role": target.Role,
			},
			CreatedAt: target.UpdatedAt,
		},
	); err != nil {
		return domain.Member{}, err
	}
	return target, nil
}

// RemoveMember removes one member. Only the owner may, and the owner can never
// remove itself through this path, so a family is never left ownerless. A
// member may remove itself to leave the family.
func (s *Service) RemoveMember(
	ctx context.Context,
	familyID string,
	actorUserID string,
	targetUserID string,
) error {
	actor, err := s.requireMember(ctx, familyID, actorUserID)
	if err != nil {
		return err
	}
	targetUserID = strings.TrimSpace(targetUserID)
	if targetUserID == "" {
		return domain.ErrInvalidMember
	}
	target, ok, err := s.repository.Member(ctx, familyID, targetUserID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrMemberNotFound
	}
	if target.Role == domain.RoleOwner {
		return domain.ErrLastOwner
	}
	if actor.UserID != targetUserID && !domain.CanManageMembers(actor.Role) {
		return domain.ErrPermissionDenied
	}
	return s.repository.DeleteMemberWithAudit(
		ctx,
		familyID,
		targetUserID,
		domain.AuditEvent{
			ID:              uuid.NewString(),
			ActorAccountID:  actor.UserID,
			TargetAccountID: targetUserID,
			Action:          domain.AuditMemberRemoved,
			Detail: map[string]any{
				"family_id":   familyID,
				"member_id":   target.ID,
				"self_leave":  actor.UserID == targetUserID,
				"target_role": target.Role,
			},
			CreatedAt: s.clock.Now().UTC(),
		},
	)
}

// Capability summarizes the membership surface for the guardian view.
func (s *Service) Capability(
	ctx context.Context,
	familyID string,
	actorUserID string,
) (domain.Capability, error) {
	if _, err := s.requireMember(ctx, familyID, actorUserID); err != nil {
		return domain.Capability{}, err
	}
	members, err := s.repository.Members(ctx, familyID)
	if err != nil {
		return domain.Capability{}, err
	}
	invitations, err := s.repository.Invitations(ctx, familyID)
	if err != nil {
		return domain.Capability{}, err
	}
	pending := 0
	owner := ""
	for _, invitation := range invitations {
		if invitation.Status == domain.InvitationPending {
			pending++
		}
	}
	for _, member := range members {
		if member.Role == domain.RoleOwner {
			owner = member.UserID
			break
		}
	}
	return domain.Capability{
		FamilyID:       familyID,
		OwnerUserID:    owner,
		MemberCount:    len(members),
		PendingInvites: pending,
		SchemaVersion:  domain.SchemaVersion,
	}, nil
}

// requireMember loads the acting member and rejects a non-member.
func (s *Service) requireMember(
	ctx context.Context,
	familyID string,
	userID string,
) (domain.Member, error) {
	familyID = strings.TrimSpace(familyID)
	userID = strings.TrimSpace(userID)
	if familyID == "" || userID == "" {
		return domain.Member{}, domain.ErrInvalidMember
	}
	member, ok, err := s.repository.Member(ctx, familyID, userID)
	if err != nil {
		return domain.Member{}, err
	}
	if !ok {
		return domain.Member{}, domain.ErrMemberNotFound
	}
	return member, nil
}

func (s *Service) ownerUserID(
	ctx context.Context,
	familyID string,
) (string, error) {
	members, err := s.repository.Members(ctx, familyID)
	if err != nil {
		return "", err
	}
	for _, member := range members {
		if member.Role == domain.RoleOwner {
			return member.UserID, nil
		}
	}
	return "", domain.ErrLastOwner
}

// AdminMembers lists one target family's roster. It is separate from Members
// because administrators are not members of the target family and therefore
// must never pass through the acting-member authorization path.
func (s *Service) AdminMembers(
	ctx context.Context,
	familyID string,
) ([]domain.AdminMember, int64, error) {
	if _, err := s.requireAdminFamily(ctx, familyID); err != nil {
		return nil, 0, err
	}
	revision, err := s.repository.MembershipRevision(ctx, familyID)
	if err != nil {
		return nil, 0, err
	}
	members, err := s.repository.Members(ctx, familyID)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.UserID)
	}
	identities, err := s.repository.AccountIdentities(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	result := make([]domain.AdminMember, 0, len(members))
	for _, member := range members {
		identity, ok := identities[member.UserID]
		if !ok {
			identity = domain.AccountIdentity{AccountID: member.UserID}
		}
		result = append(result, domain.AdminMember{
			Member:  member,
			Account: identity,
		})
	}
	return result, revision, nil
}

// AdminInvitations lists one target family's invitations. The email is only
// returned to an administrator with the family read capability.
func (s *Service) AdminInvitations(
	ctx context.Context,
	familyID string,
) ([]domain.Invitation, int64, error) {
	if _, err := s.requireAdminFamily(ctx, familyID); err != nil {
		return nil, 0, err
	}
	revision, err := s.repository.MembershipRevision(ctx, familyID)
	if err != nil {
		return nil, 0, err
	}
	invitations, err := s.repository.Invitations(ctx, familyID)
	if err != nil {
		return nil, 0, err
	}
	return invitations, revision, nil
}

// AdminCreateInvitation creates an invitation on behalf of a target family's
// owner. The audit records the administrator as actor, not the owner, and the
// invitation's invited_by_user_id stays a valid family account.
func (s *Service) AdminCreateInvitation(
	ctx context.Context,
	familyID string,
	adminAccountID string,
	email string,
	role string,
	expectedRevision int64,
) (domain.Invitation, error) {
	adminAccountID = strings.TrimSpace(adminAccountID)
	if adminAccountID == "" {
		return domain.Invitation{}, domain.ErrInvalidMember
	}
	if _, err := s.requireAdminFamily(ctx, familyID); err != nil {
		return domain.Invitation{}, err
	}
	ownerUserID, err := s.ownerUserID(ctx, familyID)
	if err != nil {
		return domain.Invitation{}, err
	}
	normalizedEmail := domain.NormalizeEmail(email)
	if err := domain.ValidateInviteEmail(normalizedEmail); err != nil {
		return domain.Invitation{}, err
	}
	if err := domain.ValidateInviteRole(role); err != nil {
		return domain.Invitation{}, err
	}
	now := s.clock.Now().UTC()
	invitation := domain.Invitation{
		ID:              uuid.NewString(),
		FamilyID:        familyID,
		Email:           normalizedEmail,
		Role:            role,
		Status:          domain.InvitationPending,
		InvitedByUserID: ownerUserID,
		CreatedAt:       now,
		ExpiresAt:       now.Add(domain.InvitationTTLHours * time.Hour),
	}
	if err := s.repository.CreateInvitationRevision(
		ctx,
		invitation,
		domain.AuditEvent{
			ID:              uuid.NewString(),
			ActorAccountID:  adminAccountID,
			TargetAccountID: familyID,
			Action:          domain.AuditAdminInvitationCreated,
			Detail: map[string]any{
				"family_id":     familyID,
				"invitation_id": invitation.ID,
				"role":          invitation.Role,
				"expires_at":    invitation.ExpiresAt.UTC().Format(time.RFC3339),
				"scope":         "administrator",
			},
			CreatedAt: now,
		},
		expectedRevision,
	); err != nil {
		return domain.Invitation{}, err
	}
	return invitation, nil
}

// AdminRevokeInvitation revokes one invitation for a target family and writes
// the operator audit in the repository transaction.
func (s *Service) AdminRevokeInvitation(
	ctx context.Context,
	familyID string,
	adminAccountID string,
	invitationID string,
	expectedRevision int64,
) error {
	adminAccountID = strings.TrimSpace(adminAccountID)
	invitationID = strings.TrimSpace(invitationID)
	if adminAccountID == "" || invitationID == "" {
		return domain.ErrInvalidInvitation
	}
	if _, err := s.requireAdminFamily(ctx, familyID); err != nil {
		return err
	}
	return s.repository.UpdateInvitationStateWithAuditRevision(
		ctx,
		familyID,
		invitationID,
		domain.InvitationRevoked,
		domain.AuditEvent{
			ID:              uuid.NewString(),
			ActorAccountID:  adminAccountID,
			TargetAccountID: familyID,
			Action:          domain.AuditAdminInvitationRevoked,
			Detail: map[string]any{
				"family_id":     familyID,
				"invitation_id": invitationID,
				"scope":         "administrator",
			},
			CreatedAt: s.clock.Now().UTC(),
		},
		expectedRevision,
	)
}

// AdminUpdateMemberRole changes one non-owner member role in a target family.
func (s *Service) AdminUpdateMemberRole(
	ctx context.Context,
	familyID string,
	adminAccountID string,
	targetUserID string,
	role string,
	expectedRevision int64,
) (domain.Member, error) {
	adminAccountID = strings.TrimSpace(adminAccountID)
	targetUserID = strings.TrimSpace(targetUserID)
	if adminAccountID == "" || targetUserID == "" {
		return domain.Member{}, domain.ErrInvalidMember
	}
	if role != domain.RoleGuardian && role != domain.RoleViewer {
		return domain.Member{}, domain.ErrInvalidMember
	}
	if _, err := s.requireAdminFamily(ctx, familyID); err != nil {
		return domain.Member{}, err
	}
	target, ok, err := s.repository.Member(ctx, familyID, targetUserID)
	if err != nil {
		return domain.Member{}, err
	}
	if !ok {
		return domain.Member{}, domain.ErrMemberNotFound
	}
	if target.Role == domain.RoleOwner {
		return domain.Member{}, domain.ErrLastOwner
	}
	target.Role = role
	target.UpdatedAt = s.clock.Now().UTC()
	if err := s.repository.UpdateMemberRoleWithAuditRevision(
		ctx,
		target,
		domain.AuditEvent{
			ID:              uuid.NewString(),
			ActorAccountID:  adminAccountID,
			TargetAccountID: target.UserID,
			Action:          domain.AuditAdminMemberRoleUpdated,
			Detail: map[string]any{
				"family_id":   familyID,
				"member_id":   target.ID,
				"target_role": target.Role,
				"scope":       "administrator",
			},
			CreatedAt: target.UpdatedAt,
		},
		expectedRevision,
	); err != nil {
		return domain.Member{}, err
	}
	return target, nil
}

// AdminRemoveMember removes one non-owner member from a target family. The
// owner row is immutable through the management path so the family cannot be
// left without an owner.
func (s *Service) AdminRemoveMember(
	ctx context.Context,
	familyID string,
	adminAccountID string,
	targetUserID string,
	expectedRevision int64,
) error {
	adminAccountID = strings.TrimSpace(adminAccountID)
	targetUserID = strings.TrimSpace(targetUserID)
	if adminAccountID == "" || targetUserID == "" {
		return domain.ErrInvalidMember
	}
	if _, err := s.requireAdminFamily(ctx, familyID); err != nil {
		return err
	}
	target, ok, err := s.repository.Member(ctx, familyID, targetUserID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrMemberNotFound
	}
	if target.Role == domain.RoleOwner {
		return domain.ErrLastOwner
	}
	return s.repository.DeleteMemberWithAuditRevision(
		ctx,
		familyID,
		targetUserID,
		domain.AuditEvent{
			ID:              uuid.NewString(),
			ActorAccountID:  adminAccountID,
			TargetAccountID: target.UserID,
			Action:          domain.AuditAdminMemberRemoved,
			Detail: map[string]any{
				"family_id":   familyID,
				"member_id":   target.ID,
				"target_role": target.Role,
				"scope":       "administrator",
			},
			CreatedAt: s.clock.Now().UTC(),
		},
		expectedRevision,
	)
}

func (s *Service) requireAdminFamily(
	ctx context.Context,
	familyID string,
) (domain.AccountIdentity, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.AccountIdentity{}, domain.ErrAccountNotFound
	}
	identity, err := s.repository.ParentIdentity(ctx, familyID)
	if err != nil {
		return domain.AccountIdentity{}, err
	}
	if identity.Role != authdomain.RoleParent {
		return domain.AccountIdentity{}, domain.ErrAccountNotParent
	}
	return identity, nil
}
