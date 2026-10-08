import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

/// One aggregated day of device usage. The report carries counters and
/// durations only, never conversation text, media, tokens, or child identity.
export interface UsageCategoryBreakdown {
  category: string
  playCount: number
  minutes: number
}

export interface UsageBlockedCounts {
  disabledPeriod: number
  dailyLimit: number
  categoryDenied: number
  timeUntrusted: number
}

export interface UsageDeviceBreakdown {
  deviceId: string
  deviceName: string
  activeMinutes: number
  conversationCount: number
  contentPlayCount: number
}

export interface UsageReportDay {
  schemaVersion: string
  reportDate: string
  timezoneOffsetMinutes: number
  activeMinutes: number
  conversationCount: number
  conversationMinutes: number
  contentPlayCount: number
  contentMinutes: number
  dailyLimitMinutes: number
  remainingMinutes: number
  limitReached: boolean
  categories: UsageCategoryBreakdown[]
  blocked: UsageBlockedCounts
  devices: UsageDeviceBreakdown[]
  updatedAt: string
}

/// Loads read-only usage reports for one family from the brand console API.
export interface AdminUsageReportsClient {
  loadUsageReports(parentAccountId: string, days: number): Promise<UsageReportDay[]>
}

export function createAdminUsageReportsClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminUsageReportsClient {
  return {
    async loadUsageReports(parentAccountId: string, days: number): Promise<UsageReportDay[]> {
      const response = await httpClient.get(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/usage-reports`,
        { params: { days } },
      )
      const records = response.data?.data?.reports
      if (!Array.isArray(records)) {
        return []
      }
      const reports = records
        .map((record) => parseUsageReportDay(record))
        .filter((report): report is UsageReportDay => report !== null)
      if (reports.length !== records.length) {
        throw new Error('usage_report_contract_invalid')
      }
      return reports
    },
  }
}

export function parseUsageReportDay(value: unknown): UsageReportDay | null {
  if (!isRecord(value)) {
    return null
  }
  const reportDate = stringValue(value.report_date)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(reportDate)) {
    return null
  }

  return {
    schemaVersion: stringValue(value.schema_version),
    reportDate,
    timezoneOffsetMinutes: numberValue(value.timezone_offset_minutes),
    activeMinutes: nonNegativeNumber(value.active_minutes),
    conversationCount: nonNegativeNumber(value.conversation_count),
    conversationMinutes: nonNegativeNumber(value.conversation_minutes),
    contentPlayCount: nonNegativeNumber(value.content_play_count),
    contentMinutes: nonNegativeNumber(value.content_minutes),
    dailyLimitMinutes: nonNegativeNumber(value.daily_limit_minutes),
    remainingMinutes: nonNegativeNumber(value.remaining_minutes),
    limitReached: value.limit_reached === true,
    categories: parseCategories(value.categories),
    blocked: parseBlocked(value.blocked),
    devices: parseDevices(value.devices),
    updatedAt: stringValue(value.updated_at),
  }
}

function parseCategories(value: unknown): UsageCategoryBreakdown[] {
  return arrayValue(value)
    .map((item): UsageCategoryBreakdown | null => {
      if (!isRecord(item)) {
        return null
      }
      const category = stringValue(item.category)
      if (!category) {
        return null
      }
      return {
        category,
        playCount: nonNegativeNumber(item.play_count),
        minutes: nonNegativeNumber(item.minutes),
      }
    })
    .filter((item): item is UsageCategoryBreakdown => item !== null)
}

function parseBlocked(value: unknown): UsageBlockedCounts {
  const record = recordValue(value)
  return {
    disabledPeriod: nonNegativeNumber(record.disabled_period),
    dailyLimit: nonNegativeNumber(record.daily_limit),
    categoryDenied: nonNegativeNumber(record.category_denied),
    timeUntrusted: nonNegativeNumber(record.time_untrusted),
  }
}

function parseDevices(value: unknown): UsageDeviceBreakdown[] {
  return arrayValue(value)
    .map((item): UsageDeviceBreakdown | null => {
      if (!isRecord(item)) {
        return null
      }
      const deviceId = stringValue(item.device_id)
      if (!deviceId) {
        return null
      }
      return {
        deviceId,
        deviceName: stringValue(item.device_name),
        activeMinutes: nonNegativeNumber(item.active_minutes),
        conversationCount: nonNegativeNumber(item.conversation_count),
        contentPlayCount: nonNegativeNumber(item.content_play_count),
      }
    })
    .filter((item): item is UsageDeviceBreakdown => item !== null)
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function recordValue(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' && value.length > 0 ? value : fallback
}

function numberValue(value: unknown, fallback = 0): number {
  if (value === null || value === undefined || value === '') {
    return fallback
  }
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function nonNegativeNumber(value: unknown): number {
  return Math.max(0, numberValue(value))
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
