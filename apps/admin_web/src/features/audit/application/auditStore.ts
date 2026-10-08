import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createAdminAuditClient,
  type AdminAuditEntry,
} from '@/features/audit/api/adminAudit'

export interface AuditFilters {
  action: string
  actorAccountId: string
  targetAccountId: string
  from: string
  to: string
  pageSize: number
}

export const DEFAULT_AUDIT_FILTERS: AuditFilters = {
  action: '',
  actorAccountId: '',
  targetAccountId: '',
  from: '',
  to: '',
  pageSize: 20,
}

export const useAuditStore = defineStore('admin-operation-audit', () => {
  const client = createAdminAuditClient()
  const items = ref<AdminAuditEntry[]>([])
  const actions = ref<string[]>([])
  const filters = ref<AuditFilters>({ ...DEFAULT_AUDIT_FILTERS })
  const page = ref(1)
  const total = ref(0)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const totalPages = computed(() => {
    if (total.value <= 0) {
      return 0
    }
    return Math.max(1, Math.ceil(total.value / filters.value.pageSize))
  })

  const hasFilters = computed(
    () =>
      filters.value.action !== '' ||
      filters.value.actorAccountId.trim() !== '' ||
      filters.value.targetAccountId.trim() !== '' ||
      filters.value.from !== '' ||
      filters.value.to !== '',
  )

  async function load(targetPage = page.value): Promise<void> {
    isLoading.value = true
    error.value = null
    const requestedPage = Math.max(1, targetPage)
    try {
      const result = await client.load({
        action: filters.value.action,
        actorAccountId: filters.value.actorAccountId,
        targetAccountId: filters.value.targetAccountId,
        from: startOfDayIsoTimestamp(filters.value.from),
        to: endOfDayIsoTimestamp(filters.value.to),
        page: requestedPage,
        pageSize: filters.value.pageSize,
      })
      items.value = result.items
      total.value = result.total
      page.value = result.page
      actions.value = normalizeActions(result.actions)
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoading.value = false
    }
  }

  async function applyFilters(): Promise<void> {
    await load(1)
  }

  async function resetFilters(): Promise<void> {
    filters.value = { ...DEFAULT_AUDIT_FILTERS }
    await load(1)
  }

  async function goToPage(nextPage: number): Promise<void> {
    if (nextPage < 1 || (totalPages.value > 0 && nextPage > totalPages.value)) {
      return
    }
    await load(nextPage)
  }

  async function retry(): Promise<void> {
    await load(page.value)
  }

  return {
    actions,
    applyFilters,
    DEFAULT_AUDIT_FILTERS,
    error,
    filters,
    goToPage,
    hasFilters,
    isLoading,
    items,
    load,
    page,
    resetFilters,
    retry,
    total,
    totalPages,
  }
})

function normalizeActions(values: string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))].sort(
    (left, right) => left.localeCompare(right),
  )
}

function startOfDayIsoTimestamp(value: string): string | undefined {
  const normalized = value.trim()
  if (!normalized) {
    return undefined
  }
  const date = parseDateInput(normalized)
  if (date === null) {
    return normalized
  }
  date.setHours(0, 0, 0, 0)
  return date.toISOString()
}

function endOfDayIsoTimestamp(value: string): string | undefined {
  const normalized = value.trim()
  if (!normalized) {
    return undefined
  }
  const date = parseDateInput(normalized)
  if (date === null) {
    return normalized
  }
  date.setHours(23, 59, 59, 999)
  return date.toISOString()
}

function parseDateInput(value: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (match === null) {
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? null : date
  }
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(year, month - 1, day)
  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day
  ) {
    return null
  }
  return date
}
