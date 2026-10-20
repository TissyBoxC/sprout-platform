package domain

import (
	"strings"
	"time"
)

// Admin permission identifiers.
//
// One identifier represents one management capability on the admin console.
// They are stable contract values: HTTP middleware and the management UI both
// reference them, so a rename must be treated as a breaking change. The SQL
// check constraint in migration 0045 mirrors this catalog.
const (
	AdminPermissionWildcard           = "*"
	AdminPermissionAdminsRead         = "admins.read"
	AdminPermissionAdminsManage       = "admins.manage"
	AdminPermissionDevicesRead        = "devices.read"
	AdminPermissionDevicesManage      = "devices.manage"
	AdminPermissionFamilyRead         = "family.read"
	AdminPermissionFamilyManage       = "family.manage"
	AdminPermissionAIAccountsRead     = "ai_accounts.read"
	AdminPermissionAIAccountsManage   = "ai_accounts.manage"
	AdminPermissionOTARead            = "ota.read"
	AdminPermissionOTAManage          = "ota.manage"
	AdminPermissionStorageRead        = "storage.read"
	AdminPermissionStorageManage      = "storage.manage"
	AdminPermissionContentRead        = "content.read"
	AdminPermissionContentReview      = "content.review"
	AdminPermissionServiceRead        = "service.read"
	AdminPermissionServiceUpgrade     = "service.upgrade"
	AdminPermissionFeatureCenterRead  = "feature_center.read"
	AdminPermissionFeatureCenterWrite = "feature_center.manage"
	AdminPermissionSettingsRead       = "settings.read"
	AdminPermissionSettingsManage     = "settings.manage"
	AdminPermissionNotificationsRead  = "notifications.read"
	AdminPermissionNotificationsWrite = "notifications.manage"
	AdminPermissionStatsRead          = "stats.read"
	AdminPermissionAuditRead          = "audit.read"
	AdminPermissionBillingRead        = "billing.read"
	AdminPermissionBillingManage      = "billing.manage"
)

// AdminPermissionCatalog is the closed, ordered list of assignable
// permissions. The sequence is the order the management console renders, and
// the membership test backs both input validation and the SQL constraint.
var AdminPermissionCatalog = []string{
	AdminPermissionAdminsRead,
	AdminPermissionAdminsManage,
	AdminPermissionDevicesRead,
	AdminPermissionDevicesManage,
	AdminPermissionFamilyRead,
	AdminPermissionFamilyManage,
	AdminPermissionAIAccountsRead,
	AdminPermissionAIAccountsManage,
	AdminPermissionOTARead,
	AdminPermissionOTAManage,
	AdminPermissionStorageRead,
	AdminPermissionStorageManage,
	AdminPermissionContentRead,
	AdminPermissionContentReview,
	AdminPermissionServiceRead,
	AdminPermissionServiceUpgrade,
	AdminPermissionFeatureCenterRead,
	AdminPermissionFeatureCenterWrite,
	AdminPermissionSettingsRead,
	AdminPermissionSettingsManage,
	AdminPermissionNotificationsRead,
	AdminPermissionNotificationsWrite,
	AdminPermissionStatsRead,
	AdminPermissionAuditRead,
	AdminPermissionBillingRead,
	AdminPermissionBillingManage,
}

// IsKnownAdminPermission reports whether one identifier is assignable. The
// wildcard is intentionally excluded because it is granted, not chosen.
func IsKnownAdminPermission(permission string) bool {
	permission = strings.TrimSpace(permission)
	for _, known := range AdminPermissionCatalog {
		if known == permission {
			return true
		}
	}
	return false
}

// HasAdminPermission reports whether a permission set grants the required
// capability. The wildcard grants everything. A guardian or an administrator
// whose wildcard was revoked receives no implicit access.
func HasAdminPermission(
	granted []string,
	required string,
) bool {
	required = strings.TrimSpace(required)
	for _, permission := range granted {
		trimmed := strings.TrimSpace(permission)
		if trimmed == AdminPermissionWildcard {
			return true
		}
		if required != "" && trimmed == required {
			return true
		}
	}
	return false
}

// NormalizeAdminPermissions returns a validated, de-duplicated permission set
// in catalog order. An unknown identifier makes the whole set invalid so a
// partial typo cannot be stored. An empty result is a valid "no capability"
// set. A wildcard collapses the set to the wildcard alone: dropping it would
// silently narrow an existing grant.
func NormalizeAdminPermissions(permissions []string) ([]string, bool) {
	selected := make(map[string]bool, len(permissions))
	for _, permission := range permissions {
		trimmed := strings.TrimSpace(permission)
		if trimmed == "" {
			continue
		}
		if trimmed == AdminPermissionWildcard || IsKnownAdminPermission(trimmed) {
			selected[trimmed] = true
			continue
		}
		return nil, false
	}
	if selected[AdminPermissionWildcard] {
		return []string{AdminPermissionWildcard}, true
	}
	normalized := make([]string, 0, len(selected))
	for _, permission := range AdminPermissionCatalog {
		if selected[permission] {
			normalized = append(normalized, permission)
		}
	}
	return normalized, true
}

// AdminPermissionAccount is the management-console projection of one
// administrator. It deliberately omits the password hash, phone and consent
// state so the permission screen never receives data it does not need.
type AdminPermissionAccount struct {
	AccountID   string
	Email       string
	DisplayName string
	Status      string
	CreatedAt   time.Time
	Permissions []string
}
