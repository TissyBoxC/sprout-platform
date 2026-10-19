import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { apiNoticeWarning } from '@/api/apiNotice'
import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createAdminPermissionsClient,
  type AdminPermissionAccount,
  type AdminPermissionOption,
} from '@/api/adminPermissions'
import { createHttpClient } from '@/api/httpClient'

export interface AdminPermissionGroup {
  group: string
  options: AdminPermissionOption[]
}

/// Owns the permission screen state: the catalog, every administrator, the
/// per-account draft, and the save result. Drafts are keyed by account so two
/// administrators can be edited in sequence without losing changes.
export const useAdminPermissionStore = defineStore('admin-permissions', () => {
  const httpClient = createHttpClient()
  const client = createAdminPermissionsClient(httpClient)
  const catalog = ref<AdminPermissionOption[]>([])
  const accounts = ref<AdminPermissionAccount[]>([])
  const drafts = ref<Record<string, string[]>>({})
  const isLoading = ref(false)
  const savingAccountId = ref('')
  const error = ref<ApiError | null>(null)
  const saveErrors = ref<Record<string, ApiError | null>>({})
  const lastMessage = ref('')
  const lastWarning = ref('')
  const saveWarnings = ref<Record<string, string>>({})

  const groups = computed<AdminPermissionGroup[]>(() => {
    const grouped: AdminPermissionGroup[] = []
    for (const option of catalog.value) {
      if (option.id === '*') {
        continue
      }
      const existing = grouped.find((entry) => entry.group === option.group)
      if (existing === undefined) {
        grouped.push({ group: option.group, options: [option] })
      } else {
        existing.options.push(option)
      }
    }
    return grouped
  })

  const wildcardOption = computed<AdminPermissionOption | null>(
    () => catalog.value.find((option) => option.id === '*') ?? null,
  )

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const overview = await client.load()
      catalog.value = overview.catalog
      accounts.value = overview.accounts
      const nextDrafts: Record<string, string[]> = {}
      for (const account of overview.accounts) {
        nextDrafts[account.accountId] = [...account.permissions]
      }
      drafts.value = nextDrafts
      saveErrors.value = {}
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoading.value = false
    }
  }

  function draftFor(accountId: string): string[] {
    return drafts.value[accountId] ?? []
  }

  function isWildcard(accountId: string): boolean {
    return draftFor(accountId).includes('*')
  }

  function hasPermission(accountId: string, permissionId: string): boolean {
    return draftFor(accountId).includes(permissionId)
  }

  function togglePermission(accountId: string, permissionId: string): void {
    if (isWildcard(accountId)) {
      return
    }
    const current = draftFor(accountId)
    const next = current.includes(permissionId)
      ? current.filter((item) => item !== permissionId)
      : [...current, permissionId]
    drafts.value = { ...drafts.value, [accountId]: next }
  }

  function setWildcard(accountId: string, enabled: boolean): void {
    drafts.value = {
      ...drafts.value,
      [accountId]: enabled ? ['*'] : [],
    }
  }

  function isDirty(accountId: string): boolean {
    const account = accounts.value.find((item) => item.accountId === accountId)
    if (account === undefined) {
      return false
    }
    return !sameSet(draftFor(accountId), account.permissions)
  }

  async function save(accountId: string): Promise<boolean> {
    savingAccountId.value = accountId
    lastMessage.value = ''
    lastWarning.value = ''
    saveWarnings.value = { ...saveWarnings.value, [accountId]: '' }
    saveErrors.value = { ...saveErrors.value, [accountId]: null }
    try {
      const result = await client.update(accountId, sortPermissions(draftFor(accountId)))
      const updated = result.value
      accounts.value = accounts.value.map((account) =>
        account.accountId === accountId ? updated : account,
      )
      drafts.value = { ...drafts.value, [accountId]: [...updated.permissions] }
      lastWarning.value = apiNoticeWarning(result.notice)
      saveWarnings.value = {
        ...saveWarnings.value,
        [accountId]: lastWarning.value,
      }
      lastMessage.value = '权限已保存。'
      return true
    } catch (caught: unknown) {
      saveErrors.value = {
        ...saveErrors.value,
        [accountId]: mapApiError(caught),
      }
      saveWarnings.value = { ...saveWarnings.value, [accountId]: '' }
      return false
    } finally {
      savingAccountId.value = ''
    }
  }

  return {
    accounts,
    catalog,
    draftFor,
    error,
    groups,
    hasPermission,
    isDirty,
    isLoading,
    isWildcard,
    lastMessage,
    lastWarning,
    load,
    save,
    saveErrors,
    saveWarnings,
    savingAccountId,
    setWildcard,
    togglePermission,
    wildcardOption,
  }
})

/// Compares permission sets order-independently so a reordering does not look
/// like an unsaved change.
function sameSet(left: string[], right: string[]): boolean {
  if (left.length !== right.length) {
    return false
  }
  const rightSet = new Set(right)
  return left.every((item) => rightSet.has(item))
}

/// Presents the wildcard alone and otherwise keeps the catalog order the server
/// returned, so the payload matches what the backend normalizes.
function sortPermissions(permissions: string[]): string[] {
  if (permissions.includes('*')) {
    return ['*']
  }
  return [...new Set(permissions)].sort()
}
