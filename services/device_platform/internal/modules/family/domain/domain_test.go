package domain

import (
	"testing"
	"time"
)

func TestIsRoleCoversCatalogue(t *testing.T) {
	for _, role := range Roles {
		if !IsRole(role) {
			t.Fatalf("catalogue role %q rejected", role)
		}
	}
	if IsRole("admin") {
		t.Fatal("admin is not a family member role")
	}
	if IsRole("") {
		t.Fatal("the empty role is not a family member role")
	}
}

func TestInviteAndManagePermissions(t *testing.T) {
	// Owners and guardians may invite; viewers may not.
	if !CanInvite(RoleOwner) || !CanInvite(RoleGuardian) {
		t.Fatal("owners and guardians must be able to invite")
	}
	if CanInvite(RoleViewer) {
		t.Fatal("viewers must not be able to invite")
	}
	// Only the owner may manage other members.
	if !CanManageMembers(RoleOwner) {
		t.Fatal("the owner must be able to manage members")
	}
	if CanManageMembers(RoleGuardian) || CanManageMembers(RoleViewer) {
		t.Fatal("non-owners must not manage members")
	}
}

func TestPermissionMatrixEnforcesRoleBoundaries(t *testing.T) {
	// Viewers are strictly read-only.
	for _, action := range []Action{
		ActionMemberManage,
		ActionInvitationManage,
		ActionChildManage,
		ActionDeviceBind,
		ActionDeviceManage,
		ActionVisionExecute,
		ActionPrivacyManage,
		ActionAIAccountManage,
		ActionFamilyProfileWrite,
	} {
		if Can(RoleViewer, action) {
			t.Fatalf("viewer must not be allowed to perform %q", action)
		}
	}
	for _, action := range []Action{
		ActionFamilyRead,
		ActionMemberRead,
		ActionInvitationRead,
		ActionChildRead,
		ActionDeviceRead,
		ActionVisionRead,
		ActionPrivacyRead,
		ActionAIAccountRead,
	} {
		if !Can(RoleViewer, action) {
			t.Fatalf("viewer must be allowed to read %q", action)
		}
	}

	// Guardians can operate family devices, run Vision and edit child/privacy
	// policy, but cannot manage members or the owner's AI account.
	for _, action := range []Action{
		ActionDeviceBind,
		ActionDeviceManage,
		ActionVisionExecute,
		ActionPrivacyManage,
		ActionChildManage,
		ActionInvitationManage,
	} {
		if !Can(RoleGuardian, action) {
			t.Fatalf("guardian must be allowed to perform %q", action)
		}
	}
	for _, action := range []Action{
		ActionMemberManage,
		ActionAIAccountManage,
		ActionFamilyProfileWrite,
	} {
		if Can(RoleGuardian, action) {
			t.Fatalf("guardian must not be allowed to perform %q", action)
		}
	}
	for _, action := range []Action{
		ActionMemberManage,
		ActionInvitationManage,
		ActionDeviceBind,
		ActionDeviceManage,
		ActionVisionExecute,
		ActionPrivacyManage,
		ActionAIAccountManage,
		ActionFamilyProfileWrite,
	} {
		if !Can(RoleOwner, action) {
			t.Fatalf("owner must be allowed to perform %q", action)
		}
	}
}

func TestPermissionMatrixFailsClosed(t *testing.T) {
	if Can("admin", ActionFamilyRead) {
		t.Fatal("unknown roles must not receive family permissions")
	}
	if Can(RoleOwner, Action("unknown_action")) {
		t.Fatal("unknown actions must be denied")
	}
}

func TestActionCatalogueCoversEveryKnownAction(t *testing.T) {
	if len(Actions) == 0 {
		t.Fatal("action catalogue is empty")
	}
	seen := make(map[Action]struct{}, len(Actions))
	for _, action := range Actions {
		if action == "" {
			t.Fatal("action catalogue contains an empty action")
		}
		if _, exists := seen[action]; exists {
			t.Fatalf("action catalogue repeats %q", action)
		}
		seen[action] = struct{}{}
		if !Can(RoleOwner, action) {
			t.Fatalf("owner does not cover catalogue action %q", action)
		}
	}
}

func TestValidateFamilyName(t *testing.T) {
	name, err := ValidateFamilyName("  初芽之家  ")
	if err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
	if name != "初芽之家" {
		t.Fatalf("unexpected normalization: %q", name)
	}
	if _, err := ValidateFamilyName("   "); err == nil {
		t.Fatal("blank name must be rejected")
	}
	long := make([]rune, FamilyNameMaxLength+1)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := ValidateFamilyName(string(long)); err == nil {
		t.Fatal("over-long name must be rejected")
	}
}

func TestValidateInviteRoleNeverGrantsOwner(t *testing.T) {
	if err := ValidateInviteRole(RoleGuardian); err != nil {
		t.Fatalf("guardian invite rejected: %v", err)
	}
	if err := ValidateInviteRole(RoleViewer); err != nil {
		t.Fatalf("viewer invite rejected: %v", err)
	}
	if err := ValidateInviteRole(RoleOwner); err == nil {
		t.Fatal("the owner role must never be handed out by invitation")
	}
}

func TestValidateInviteEmail(t *testing.T) {
	if err := ValidateInviteEmail("  Guardian@Example.COM "); err != nil {
		t.Fatalf("valid email rejected: %v", err)
	}
	for _, bad := range []string{"", "no-at-sign", "@example.com", "user@", "a b@example.com"} {
		if err := ValidateInviteEmail(bad); err == nil {
			t.Fatalf("email %q must be rejected", bad)
		}
	}
	if NormalizeEmail("  Guardian@Example.COM ") != "guardian@example.com" {
		t.Fatal("email must normalize to lower case without surrounding space")
	}
}

func TestInvitationExpired(t *testing.T) {
	now := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	pending := Invitation{ExpiresAt: now.Add(time.Hour)}
	if InvitationExpired(pending, now) {
		t.Fatal("an invitation before its expiry is not expired")
	}
	expired := Invitation{ExpiresAt: now}
	if !InvitationExpired(expired, now) {
		t.Fatal("an invitation exactly at its expiry is expired")
	}
	zero := Invitation{}
	if InvitationExpired(zero, now) {
		t.Fatal("an invitation without an expiry is never treated as expired")
	}
}
