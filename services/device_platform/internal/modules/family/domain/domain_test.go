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
