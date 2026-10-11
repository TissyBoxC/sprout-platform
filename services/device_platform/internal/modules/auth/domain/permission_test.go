package domain

import "testing"

func TestHasAdminPermissionGrantsWildcard(t *testing.T) {
	if !HasAdminPermission([]string{AdminPermissionWildcard}, AdminPermissionDevicesManage) {
		t.Fatal("wildcard must grant every capability")
	}
}

func TestHasAdminPermissionRequiresExactGrant(t *testing.T) {
	granted := []string{AdminPermissionContentRead}
	if HasAdminPermission(granted, AdminPermissionContentRead) != true {
		t.Fatal("expected the granted read capability")
	}
	if HasAdminPermission(granted, AdminPermissionContentReview) {
		t.Fatal("review must not be implied by content read")
	}
}

func TestHasAdminPermissionDeniesEmptySet(t *testing.T) {
	if HasAdminPermission(nil, AdminPermissionAuditRead) {
		t.Fatal("an empty permission set must grant nothing")
	}
}

func TestNormalizeAdminPermissionsRejectsUnknownValue(t *testing.T) {
	if _, ok := NormalizeAdminPermissions([]string{"devices.destroy"}); ok {
		t.Fatal("an unknown permission must invalidate the whole set")
	}
}

func TestNormalizeAdminPermissionsCollapsesWildcard(t *testing.T) {
	normalized, ok := NormalizeAdminPermissions(
		[]string{AdminPermissionWildcard, AdminPermissionDevicesRead},
	)
	if !ok {
		t.Fatal("wildcard with a specific grant must be valid")
	}
	if len(normalized) != 1 || normalized[0] != AdminPermissionWildcard {
		t.Fatalf("expected a collapsed wildcard, got %v", normalized)
	}
}

func TestNormalizeAdminPermissionsOrdersByCatalog(t *testing.T) {
	normalized, ok := NormalizeAdminPermissions(
		[]string{AdminPermissionAuditRead, AdminPermissionDevicesRead},
	)
	if !ok {
		t.Fatal("known permissions must be valid")
	}
	if len(normalized) != 2 ||
		normalized[0] != AdminPermissionDevicesRead ||
		normalized[1] != AdminPermissionAuditRead {
		t.Fatalf("expected catalog order, got %v", normalized)
	}
}

func TestNormalizeAdminPermissionsAllowsEmptySet(t *testing.T) {
	normalized, ok := NormalizeAdminPermissions(nil)
	if !ok {
		t.Fatal("an empty grant is valid")
	}
	if len(normalized) != 0 {
		t.Fatalf("expected an empty grant, got %v", normalized)
	}
}
