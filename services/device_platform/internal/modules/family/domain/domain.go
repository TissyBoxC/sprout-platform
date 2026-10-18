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
	// ErrPermissionDenied reports an action the member's role cannot perform.
	ErrPermissionDenied = errors.New("family permission denied")
	// ErrInvitationConcurrent reports an invitation changed under a concurrent
	// accept. The caller must reload the invitation before deciding the next
	// user-visible state.
	ErrInvitationConcurrent = errors.New("family invitation changed concurrently")
)

// Security audit action names. They are stable operator-facing identifiers and
// never contain invitation emails, credentials, or child data.
const (
	AuditInvitationCreated  = "family.invitation.created"
	AuditInvitationAccepted = "family.invitation.accepted"
	AuditMemberRoleUpdated  = "family.member.role_updated"
	AuditMemberRemoved      = "family.member.removed"
)

// AuditEvent is one safe family security audit record. Detail must contain
// only stable identifiers, roles, and timestamps; never an invitee email or
// child data.
type AuditEvent struct {
	ID              string
	ActorAccountID  string
	TargetAccountID string
	Action          string
	Detail          map[string]any
	CreatedAt       time.Time
}

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

// Action identifies one family-scoped permission check. Callers must use the
// canonical action constants below so HTTP routes and service use cases cannot
// drift into different privilege rules.
type Action string

// Actions is the complete, ordered permission catalogue. Client capability
// reports and tests use the same list as the policy evaluator.
var Actions = []Action{
	ActionFamilyRead,
	ActionMemberRead,
	ActionMemberManage,
	ActionInvitationRead,
	ActionInvitationManage,
	ActionChildRead,
	ActionChildManage,
	ActionDeviceRead,
	ActionDeviceBind,
	ActionDeviceManage,
	ActionVisionRead,
	ActionVisionExecute,
	ActionPrivacyRead,
	ActionPrivacyManage,
	ActionAIAccountRead,
	ActionAIAccountManage,
	ActionFamilyProfileWrite,
}

const (
	ActionFamilyRead         Action = "family_read"
	ActionMemberRead         Action = "member_read"
	ActionMemberManage       Action = "member_manage"
	ActionInvitationRead     Action = "invitation_read"
	ActionInvitationManage   Action = "invitation_manage"
	ActionChildRead          Action = "child_read"
	ActionChildManage        Action = "child_manage"
	ActionDeviceRead         Action = "device_read"
	ActionDeviceBind         Action = "device_bind"
	ActionDeviceManage       Action = "device_manage"
	ActionVisionRead         Action = "vision_read"
	ActionVisionExecute      Action = "vision_execute"
	ActionPrivacyRead        Action = "privacy_read"
	ActionPrivacyManage      Action = "privacy_manage"
	ActionAIAccountRead      Action = "ai_account_read"
	ActionAIAccountManage    Action = "ai_account_manage"
	ActionFamilyProfileWrite Action = "family_profile_write"
)

// Access is the resolved authorization context for one authenticated family
// member. OwnerUserID is the account under which family devices, children and
// AI data are stored.
type Access struct {
	FamilyID    string
	ActorUserID string
	OwnerUserID string
	Role        string
}

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

// Can reports whether role may perform action. Unknown roles and actions fail
// closed so adding a route without a policy cannot accidentally grant access.
func Can(role string, action Action) bool {
	switch role {
	case RoleOwner:
		return ownerCan(action)
	case RoleGuardian:
		return guardianCan(action)
	case RoleViewer:
		return viewerCan(action)
	default:
		return false
	}
}

func ownerCan(action Action) bool {
	switch action {
	case ActionFamilyRead,
		ActionMemberRead,
		ActionMemberManage,
		ActionInvitationRead,
		ActionInvitationManage,
		ActionChildRead,
		ActionChildManage,
		ActionDeviceRead,
		ActionDeviceBind,
		ActionDeviceManage,
		ActionVisionRead,
		ActionVisionExecute,
		ActionPrivacyRead,
		ActionPrivacyManage,
		ActionAIAccountRead,
		ActionAIAccountManage,
		ActionFamilyProfileWrite:
		return true
	default:
		return false
	}
}

func guardianCan(action Action) bool {
	switch action {
	case ActionFamilyRead,
		ActionMemberRead,
		ActionInvitationRead,
		ActionInvitationManage,
		ActionChildRead,
		ActionChildManage,
		ActionDeviceRead,
		ActionDeviceBind,
		ActionDeviceManage,
		ActionVisionRead,
		ActionVisionExecute,
		ActionPrivacyRead,
		ActionPrivacyManage,
		ActionAIAccountRead:
		return true
	default:
		return false
	}
}

func viewerCan(action Action) bool {
	switch action {
	case ActionFamilyRead,
		ActionMemberRead,
		ActionInvitationRead,
		ActionChildRead,
		ActionDeviceRead,
		ActionVisionRead,
		ActionPrivacyRead,
		ActionAIAccountRead:
		return true
	default:
		return false
	}
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
