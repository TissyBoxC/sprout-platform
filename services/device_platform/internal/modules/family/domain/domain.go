// Package domain defines the family membership and invitation contract for the
// family module.
//
// A family is the guardian account that owns children, devices and policies.
// The owner is a member row like any other, so the module can enforce that a
// family always keeps exactly one owner. Additional guardians and viewers join
// through an email invitation and only become members after accepting it.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidMember reports a malformed membership change.
	ErrInvalidMember = errors.New("invalid family member")
	// ErrMemberNotFound reports a missing member.
	ErrMemberNotFound = errors.New("family member not found")
	// ErrMemberExists reports a duplicate membership.
	ErrMemberExists = errors.New("family member already exists")
	// ErrLastOwner reports a change that would leave the family without an owner.
	ErrLastOwner = errors.New("family must keep exactly one owner")
	// ErrInvalidInvitation reports a malformed invitation.
	ErrInvalidInvitation = errors.New("invalid family invitation")
	// ErrInvitationNotFound reports a missing invitation.
	ErrInvitationNotFound = errors.New("family invitation not found")
	// ErrInvitationExpired reports an invitation past its expiry.
	ErrInvitationExpired = errors.New("family invitation expired")
	// ErrInvitationState reports an invitation that is no longer pending.
	ErrInvitationState = errors.New("family invitation is not pending")
)

// SchemaVersion is the family contract version shared with the clients.
const SchemaVersion = "1.0.0"

// The member roles the platform recognises. They mirror the shared family
// contract exactly, and "owner" is unique per family.
const (
	RoleOwner    = "owner"
	RoleGuardian = "guardian"
	RoleViewer   = "viewer"
)

// Roles is the canonical ordered role catalogue shared by clients and
// validation.
var Roles = []string{RoleOwner, RoleGuardian, RoleViewer}

// The invitation lifecycle states.
const (
	InvitationPending  = "pending"
	InvitationAccepted = "accepted"
	InvitationRevoked  = "revoked"
)

// FamilyNameMaxLength bounds a family display name.
const FamilyNameMaxLength = 64

// InvitationTTLHours bounds how long a pending invitation stays usable.
const InvitationTTLHours = 72

// Member is one guardian or viewer inside a family.
type Member struct {
	ID        string
	FamilyID  string
	UserID    string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Invitation is one pending or resolved request to join a family.
type Invitation struct {
	ID              string
	FamilyID        string
	Email           string
	Role            string
	Status          string
	InvitedByUserID string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	AcceptedAt      *time.Time
}

// Capability summarizes a family's membership surface for the guardian view.
type Capability struct {
	FamilyID       string `json:"family_id"`
	FamilyName     string `json:"family_name"`
	OwnerUserID    string `json:"owner_user_id"`
	MemberCount    int    `json:"member_count"`
	PendingInvites int    `json:"pending_invite_count"`
	SchemaVersion  string `json:"schema_version"`
}

// IsRole reports whether a role is in the fixed catalogue.
func IsRole(role string) bool {
	for _, candidate := range Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

// CanInvite reports whether a member role may create invitations. Owners and
// guardians may; viewers are read-only.
func CanInvite(role string) bool {
	return role == RoleOwner || role == RoleGuardian
}

// CanManageMembers reports whether a member role may change or remove other
// members. Only the owner may, so a guardian cannot remove a peer.
func CanManageMembers(role string) bool {
	return role == RoleOwner
}

// NormalizeEmail lowercases and trims an invitation email.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateFamilyName normalizes and validates one family display name.
func ValidateFamilyName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len([]rune(trimmed)) > FamilyNameMaxLength {
		return "", ErrInvalidMember
	}
	return trimmed, nil
}

// ValidateInviteRole reports whether a role may be granted by invitation. The
// unique owner role is never handed out through an invitation.
func ValidateInviteRole(role string) error {
	if role != RoleGuardian && role != RoleViewer {
		return ErrInvalidInvitation
	}
	return nil
}

// ValidateInviteEmail reports whether an invitation email is well formed. The
// check is intentionally small: the platform does not own address parsing, it
// only rejects obviously unusable values.
func ValidateInviteEmail(email string) error {
	normalized := NormalizeEmail(email)
	at := strings.IndexByte(normalized, '@')
	if at <= 0 || at == len(normalized)-1 {
		return ErrInvalidInvitation
	}
	if strings.ContainsAny(normalized, " \t\r\n") {
		return ErrInvalidInvitation
	}
	return nil
}

// InvitationExpired reports whether a pending invitation is past its expiry at
// the given instant.
func InvitationExpired(invitation Invitation, now time.Time) bool {
	return !invitation.ExpiresAt.IsZero() && !now.Before(invitation.ExpiresAt)
}
