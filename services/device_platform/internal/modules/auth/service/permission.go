package service

import (
	"context"
	"errors"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
)

// AdminPermissionCatalog returns the assignable administrator permissions in
// the order the management console renders them. The service owns this read so
// the HTTP layer and the UI share one authoritative list.
func (s *Service) AdminPermissionCatalog() []string {
	catalog := make([]string, len(domain.AdminPermissionCatalog))
	copy(catalog, domain.AdminPermissionCatalog)
	return catalog
}

// ListAdminPermissions returns every administrator account with its current
// permission set for the permission-management screen.
func (s *Service) ListAdminPermissions(
	ctx context.Context,
) ([]domain.AdminPermissionAccount, error) {
	accounts, err := s.repository.ListAdminAccounts(ctx)
	if err != nil {
		return nil, err
	}
	projection := make([]domain.AdminPermissionAccount, 0, len(accounts))
	for _, account := range accounts {
		permissions := account.AdminPermissions
		if permissions == nil {
			permissions = []string{}
		}
		projection = append(projection, domain.AdminPermissionAccount{
			AccountID:   account.ID,
			Email:       account.Email,
			DisplayName: account.DisplayName,
			Status:      account.Status,
			CreatedAt:   account.CreatedAt,
			Permissions: permissions,
		})
	}
	return projection, nil
}

// UpdateAdminPermissions replaces one administrator's permission set.
//
// Authorization is enforced by the HTTP layer through requireAdminPermission;
// this method only validates the target and the requested values. A guardian
// id, an unknown permission, or a disabled account is rejected so a partial
// write can never widen authority.
func (s *Service) UpdateAdminPermissions(
	ctx context.Context,
	accountID string,
	permissions []string,
) (domain.AdminPermissionAccount, error) {
	account, err := s.GetIdentity(ctx, accountID)
	if err != nil {
		return domain.AdminPermissionAccount{}, err
	}
	if account.Role != domain.RoleAdmin {
		return domain.AdminPermissionAccount{}, domain.ErrInsufficientPrivilege
	}
	normalized, ok := domain.NormalizeAdminPermissions(permissions)
	if !ok {
		return domain.AdminPermissionAccount{}, domain.ErrUnknownAdminPermission
	}
	if err := s.repository.UpdateAdminPermissions(ctx, accountID, normalized); err != nil {
		return domain.AdminPermissionAccount{}, err
	}
	return domain.AdminPermissionAccount{
		AccountID:   account.ID,
		Email:       account.Email,
		DisplayName: account.DisplayName,
		Status:      account.Status,
		CreatedAt:   account.CreatedAt,
		Permissions: normalized,
	}, nil
}

// HasAdminPermission reports whether the account currently holds one
// capability. A missing, disabled, or non-admin account has no permission.
func (s *Service) HasAdminPermission(
	ctx context.Context,
	accountID string,
	permission string,
) (bool, error) {
	account, err := s.GetIdentity(ctx, accountID)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) ||
			errors.Is(err, domain.ErrAccountDisabled) {
			return false, nil
		}
		return false, err
	}
	if account.Role != domain.RoleAdmin {
		return false, nil
	}
	return domain.HasAdminPermission(account.AdminPermissions, permission), nil
}
