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

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	CreateMember(ctx context.Context, member domain.Member) error
	UpdateMemberRole(ctx context.Context, member domain.Member) error
	DeleteMember(ctx context.Context, familyID string, userID string) error
	Members(ctx context.Context, familyID string) ([]domain.Member, error)
	Member(ctx context.Context, familyID string, userID string) (domain.Member, bool, error)
	MemberByUser(ctx context.Context, userID string) (domain.Member, bool, error)

	CreateInvitation(ctx context.Context, invitation domain.Invitation) error
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
}

// Service owns family membership and invitations.
type Service struct {
	repository Repository
	clock      clock.Clock
}

// Options contains family service dependencies.
type Options struct {
	Repository Repository
	Clock      clock.Clock
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
	return &Service{repository: options.Repository, clock: timeSource}, nil
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
		return domain.Invitation{}, domain.ErrInvalidMember
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
	if err := s.repository.CreateInvitation(ctx, invitation); err != nil {
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
		return domain.ErrInvalidMember
	}
	invitationID = strings.TrimSpace(invitationID)
	if invitationID == "" {
		return domain.ErrInvalidInvitation
	}
	return s.repository.UpdateInvitationState(
		ctx,
		familyID,
		invitationID,
		domain.InvitationRevoked,
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
	if err := s.repository.CreateMember(ctx, member); err != nil {
		return domain.Member{}, err
	}
	if err := s.repository.UpdateInvitationState(
		ctx,
		familyID,
		invitationID,
		domain.InvitationAccepted,
	); err != nil {
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
		return domain.Member{}, domain.ErrInvalidMember
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
	if err := s.repository.UpdateMemberRole(ctx, target); err != nil {
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
		return domain.ErrInvalidMember
	}
	return s.repository.DeleteMember(ctx, familyID, targetUserID)
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
