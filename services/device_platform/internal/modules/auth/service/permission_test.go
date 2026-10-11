package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
)

func TestHasAdminPermissionHonorsExplicitGrant(t *testing.T) {
	repository := &memoryRepository{account: &domain.ParentAccount{
		ID:               "admin-1",
		Role:             domain.RoleAdmin,
		Status:           accountStatusActive,
		AdminPermissions: []string{domain.AdminPermissionDevicesManage},
	}}
	service := newPermissionTestService(t, repository)

	granted, err := service.HasAdminPermission(
		context.Background(),
		"admin-1",
		domain.AdminPermissionDevicesManage,
	)
	if err != nil {
		t.Fatalf("check permission: %v", err)
	}
	if !granted {
		t.Fatal("expected the explicit grant to pass")
	}

	denied, err := service.HasAdminPermission(
		context.Background(),
		"admin-1",
		domain.AdminPermissionContentReview,
	)
	if err != nil {
		t.Fatalf("check denied permission: %v", err)
	}
	if denied {
		t.Fatal("an unrelated permission must be denied")
	}
}

func TestHasAdminPermissionHonorsWildcard(t *testing.T) {
	repository := &memoryRepository{account: &domain.ParentAccount{
		ID:               "admin-1",
		Role:             domain.RoleAdmin,
		Status:           accountStatusActive,
		AdminPermissions: []string{domain.AdminPermissionWildcard},
	}}
	service := newPermissionTestService(t, repository)

	granted, err := service.HasAdminPermission(
		context.Background(),
		"admin-1",
		domain.AdminPermissionServiceUpgrade,
	)
	if err != nil {
		t.Fatalf("check wildcard permission: %v", err)
	}
	if !granted {
		t.Fatal("the wildcard must grant every capability")
	}
}

func TestHasAdminPermissionDeniesNonAdmin(t *testing.T) {
	repository := &memoryRepository{account: &domain.ParentAccount{
		ID:               "parent-1",
		Role:             domain.RoleParent,
		Status:           accountStatusActive,
		AdminPermissions: []string{domain.AdminPermissionWildcard},
	}}
	service := newPermissionTestService(t, repository)

	granted, err := service.HasAdminPermission(
		context.Background(),
		"parent-1",
		domain.AdminPermissionDevicesRead,
	)
	if err != nil {
		t.Fatalf("check guardian permission: %v", err)
	}
	if granted {
		t.Fatal("a guardian must never hold administrator capability")
	}
}

func TestUpdateAdminPermissionsRejectsGuardianTarget(t *testing.T) {
	repository := &memoryRepository{account: &domain.ParentAccount{
		ID:     "parent-1",
		Role:   domain.RoleParent,
		Status: accountStatusActive,
	}}
	service := newPermissionTestService(t, repository)

	_, err := service.UpdateAdminPermissions(
		context.Background(),
		"parent-1",
		[]string{domain.AdminPermissionDevicesRead},
	)
	if !errors.Is(err, domain.ErrInsufficientPrivilege) {
		t.Fatalf("expected insufficient privilege, got %v", err)
	}
}

func TestUpdateAdminPermissionsRejectsUnknownValue(t *testing.T) {
	repository := &memoryRepository{account: &domain.ParentAccount{
		ID:     "admin-1",
		Role:   domain.RoleAdmin,
		Status: accountStatusActive,
	}}
	service := newPermissionTestService(t, repository)

	_, err := service.UpdateAdminPermissions(
		context.Background(),
		"admin-1",
		[]string{"devices.destroy"},
	)
	if !errors.Is(err, domain.ErrUnknownAdminPermission) {
		t.Fatalf("expected unknown permission, got %v", err)
	}
}

func TestUpdateAdminPermissionsPersistsNormalizedSet(t *testing.T) {
	repository := &memoryRepository{account: &domain.ParentAccount{
		ID:     "admin-1",
		Role:   domain.RoleAdmin,
		Status: accountStatusActive,
	}}
	service := newPermissionTestService(t, repository)

	updated, err := service.UpdateAdminPermissions(
		context.Background(),
		"admin-1",
		[]string{domain.AdminPermissionAuditRead, domain.AdminPermissionDevicesRead},
	)
	if err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	if len(updated.Permissions) != 2 ||
		updated.Permissions[0] != domain.AdminPermissionDevicesRead {
		t.Fatalf("expected normalized permissions, got %v", updated.Permissions)
	}
	if len(repository.account.AdminPermissions) != 2 {
		t.Fatalf("expected the repository write, got %v", repository.account.AdminPermissions)
	}
}

func newPermissionTestService(
	t *testing.T,
	repository *memoryRepository,
) *Service {
	t.Helper()
	service, err := New(Options{
		Repository:  repository,
		TokenIssuer: mustTokenIssuer(t),
		AccessTTL:   time.Minute,
		RefreshTTL:  time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}
	return service
}
