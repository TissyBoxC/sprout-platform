/// Pure capability checks shared by management pages and their unit tests.
///
/// A page may be reachable with a read capability while a mutation requires a
/// separate manage capability. Keeping that distinction here prevents a page
/// from treating "can view" as "can write".
export function canManageFamilyAccount(canManageFamily: boolean): boolean {
  return canManageFamily
}

export function canManageFamilyAIAccount(
  canManageAIAccounts: boolean,
): boolean {
  return canManageAIAccounts
}

/// Creating a parent also provisions the dependent AI account in one backend
/// operation. Both capabilities must be present before offering that action.
export function canCreateFamilyAccount(
  canManageFamily: boolean,
  canManageAIAccounts: boolean,
): boolean {
  return canManageFamilyAccount(canManageFamily) && canManageFamilyAIAccount(canManageAIAccounts)
}

export function canManageStorage(storageManage: boolean): boolean {
  return storageManage
}

export function canManageOTA(otaManage: boolean): boolean {
  return otaManage
}

export function canReadStorage(storageRead: boolean): boolean {
  return storageRead
}

export function canUploadStorage(storageManage: boolean): boolean {
  return canManageStorage(storageManage)
}

export function canManageNotifications(notificationsManage: boolean): boolean {
  return notificationsManage
}
