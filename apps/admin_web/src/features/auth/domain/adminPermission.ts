/// Admin permission identifiers shared by routing and navigation.
///
/// These values mirror the backend catalog in
/// services/device_platform/internal/modules/auth/domain/permission.go. The
/// router uses them to reject direct URL access and the layout uses them to
/// hide pages an administrator cannot open.
export const ADMIN_PERMISSIONS = {
  adminsRead: 'admins.read',
  adminsManage: 'admins.manage',
  devicesRead: 'devices.read',
  devicesManage: 'devices.manage',
  familyRead: 'family.read',
  familyManage: 'family.manage',
  aiAccountsRead: 'ai_accounts.read',
  aiAccountsManage: 'ai_accounts.manage',
  otaRead: 'ota.read',
  otaManage: 'ota.manage',
  storageRead: 'storage.read',
  storageManage: 'storage.manage',
  contentRead: 'content.read',
  contentReview: 'content.review',
  serviceRead: 'service.read',
  serviceUpgrade: 'service.upgrade',
  featureCenterRead: 'feature_center.read',
  featureCenterManage: 'feature_center.manage',
  settingsRead: 'settings.read',
  settingsManage: 'settings.manage',
  notificationsRead: 'notifications.read',
  notificationsManage: 'notifications.manage',
  statsRead: 'stats.read',
  auditRead: 'audit.read',
  billingRead: 'billing.read',
  billingManage: 'billing.manage',
} as const

export type AdminPermission =
  (typeof ADMIN_PERMISSIONS)[keyof typeof ADMIN_PERMISSIONS]

/// Returns true when the granted permission set includes the required
/// permission. The wildcard grants every capability.
export function hasAdminPermission(
  granted: readonly string[],
  required: AdminPermission,
): boolean {
  return granted.includes('*') || granted.includes(required)
}
