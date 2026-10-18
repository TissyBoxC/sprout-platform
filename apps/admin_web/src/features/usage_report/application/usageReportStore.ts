import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import {
  createAdminUsageReportsClient,
  type UsageReportDay,
} from '@/api/adminUsageReports'
import { mapApiError, type ApiError } from '@/api/apiError'
import { createHttpClient } from '@/api/httpClient'
import {
  parseParentAccountOptions,
  parentAccountPageHasMore,
  type ParentAccountOption,
} from '@/features/child/domain/childProfile'

export type UsageReportRange = 7 | 30 | 90

/// Keeps the support view read-only: families and their report days are
/// fetched on demand and never mutated from the brand console.
export const useUsageReportStore = defineStore('admin-usage-reports', () => {
  const httpClient = createHttpClient()
  const client = createAdminUsageReportsClient(httpClient)
  const families = ref<ParentAccountOption[]>([])
  const reports = ref<UsageReportDay[]>([])
  const selectedParentAccountId = ref('')
  const selectedDays = ref<UsageReportRange>(7)
  const isLoadingFamilies = ref(false)
  const isLoadingReports = ref(false)
  const error = ref<ApiError | null>(null)
  const reportError = ref<ApiError | null>(null)
  const lastLoadedAt = ref('')
  let reportRequestSequence = 0
  let familiesRequestSequence = 0

  const selectedFamily = computed(
    () =>
      families.value.find(
        (family) => family.parentAccountId === selectedParentAccountId.value,
      ) ?? null,
  )
  const hasUsage = computed(() =>
    reports.value.some(
      (report) =>
        report.activeMinutes > 0 ||
        report.conversationCount > 0 ||
        report.contentPlayCount > 0 ||
        blockedTotal(report) > 0,
    ),
  )
  const totalActiveMinutes = computed(() =>
    reports.value.reduce((sum, report) => sum + report.activeMinutes, 0),
  )
  const totalConversationCount = computed(() =>
    reports.value.reduce((sum, report) => sum + report.conversationCount, 0),
  )
  const totalContentPlayCount = computed(() =>
    reports.value.reduce((sum, report) => sum + report.contentPlayCount, 0),
  )
  const totalBlockedCount = computed(() =>
    reports.value.reduce((sum, report) => sum + blockedTotal(report), 0),
  )
  const categoryTotals = computed(() => {
    const totals = new Map<string, { playCount: number; minutes: number }>()
    for (const report of reports.value) {
      for (const category of report.categories) {
        const existing = totals.get(category.category) ?? { playCount: 0, minutes: 0 }
        existing.playCount += category.playCount
        existing.minutes += category.minutes
        totals.set(category.category, existing)
      }
    }
    return [...totals.entries()]
      .map(([category, value]) => ({ category, ...value }))
      .sort((left, right) => right.minutes - left.minutes)
  })
  const blockedTotals = computed(() => ({
    disabledPeriod: reports.value.reduce((sum, report) => sum + report.blocked.disabledPeriod, 0),
    dailyLimit: reports.value.reduce((sum, report) => sum + report.blocked.dailyLimit, 0),
    categoryDenied: reports.value.reduce((sum, report) => sum + report.blocked.categoryDenied, 0),
    timeUntrusted: reports.value.reduce((sum, report) => sum + report.blocked.timeUntrusted, 0),
  }))
  const deviceTotals = computed(() => {
    const totals = new Map<
      string,
      {
        deviceId: string
        deviceName: string
        activeMinutes: number
        conversationCount: number
        contentPlayCount: number
      }
    >()
    for (const report of reports.value) {
      for (const device of report.devices) {
        const existing =
          totals.get(device.deviceId) ??
          {
            deviceId: device.deviceId,
            deviceName: device.deviceName,
            activeMinutes: 0,
            conversationCount: 0,
            contentPlayCount: 0,
          }
        existing.deviceName = existing.deviceName || device.deviceName
        existing.activeMinutes += device.activeMinutes
        existing.conversationCount += device.conversationCount
        existing.contentPlayCount += device.contentPlayCount
        totals.set(device.deviceId, existing)
      }
    }
    return [...totals.values()].sort(
      (left, right) => right.activeMinutes - left.activeMinutes,
    )
  })

  async function loadFamilies(): Promise<void> {
    const requestSequence = ++familiesRequestSequence
    isLoadingFamilies.value = true
    error.value = null
    try {
      const options: ParentAccountOption[] = []
      for (let page = 1; ; page += 1) {
        const response = await httpClient.get('/api/v1/admin/families', {
          params: { page, page_size: 100 },
        })
        const payload = response.data?.data
        options.push(...parseParentAccountOptions(payload))
        if (!parentAccountPageHasMore(payload)) {
          break
        }
      }
      if (requestSequence !== familiesRequestSequence) {
        return
      }
      families.value = options
      const firstFamily = families.value[0]
      if (firstFamily === undefined) {
        selectedParentAccountId.value = ''
        reports.value = []
        return
      }
      if (
        !families.value.some(
          (family) => family.parentAccountId === selectedParentAccountId.value,
        )
      ) {
        selectedParentAccountId.value = firstFamily.parentAccountId
      }
      await loadReports({
        parentAccountId: selectedParentAccountId.value,
        days: selectedDays.value,
      })
    } catch (caught: unknown) {
      if (requestSequence === familiesRequestSequence) {
        error.value = mapApiError(caught)
        families.value = []
        reports.value = []
      }
    } finally {
      if (requestSequence === familiesRequestSequence) {
        isLoadingFamilies.value = false
      }
    }
  }

  async function selectFamily(parentAccountId: string): Promise<void> {
    if (
      selectedParentAccountId.value === parentAccountId &&
      reports.value.length > 0
    ) {
      return
    }
    selectedParentAccountId.value = parentAccountId
    reportError.value = null
    await loadReports({
      parentAccountId,
      days: selectedDays.value,
    })
  }

  async function setDays(days: UsageReportRange): Promise<void> {
    selectedDays.value = days
    if (!selectedParentAccountId.value) {
      return
    }
    reportError.value = null
    await loadReports({
      parentAccountId: selectedParentAccountId.value,
      days,
    })
  }

  async function loadReports(request?: {
    parentAccountId?: string
    days?: UsageReportRange
  }): Promise<void> {
    const parentAccountId = request?.parentAccountId ?? selectedParentAccountId.value
    const days = request?.days ?? selectedDays.value
    if (!parentAccountId) {
      reports.value = []
      return
    }
    const sequence = ++reportRequestSequence
    isLoadingReports.value = true
    reportError.value = null
    try {
      const nextReports = await client.loadUsageReports(parentAccountId, days)
      if (sequence !== reportRequestSequence) {
        return
      }
      reports.value = nextReports
      lastLoadedAt.value = new Date().toISOString()
    } catch (caught: unknown) {
      if (sequence === reportRequestSequence) {
        reportError.value = mapApiError(caught)
        reports.value = []
      }
    } finally {
      if (sequence === reportRequestSequence) {
        isLoadingReports.value = false
      }
    }
  }

  async function refresh(): Promise<void> {
    await loadFamilies()
  }

  return {
    blockedTotals,
    categoryTotals,
    deviceTotals,
    error,
    families,
    hasUsage,
    isLoadingFamilies,
    isLoadingReports,
    lastLoadedAt,
    loadFamilies,
    loadReports,
    reportError,
    reports,
    refresh,
    selectedDays,
    selectedFamily,
    selectedParentAccountId,
    selectFamily,
    setDays,
    totalActiveMinutes,
    totalBlockedCount,
    totalContentPlayCount,
    totalConversationCount,
  }
})

function blockedTotal(report: UsageReportDay): number {
  return (
    report.blocked.disabledPeriod +
    report.blocked.dailyLimit +
    report.blocked.categoryDenied +
    report.blocked.timeUntrusted
  )
}
