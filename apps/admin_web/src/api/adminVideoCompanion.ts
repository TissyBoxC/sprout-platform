import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface VideoBucket {
  label: string
  count: number
}

export interface AdminVideoStats {
  windowDays: number
  callCount: number
  activeCount: number
  endedCount: number
  missedCount: number
  rejectedCount: number
  failedCount: number
  poorQualityCount: number
  stateBuckets: VideoBucket[]
  reasonBuckets: VideoBucket[]
  generatedAt: string
}

export interface AdminVideoClient {
  loadStats(windowDays: number): Promise<AdminVideoStats>
}

/// Creates the administrator video-companionship statistics client. Only
/// aggregate call counts are fetched; no media, contact identity or per-device
/// call detail leaves the platform here.
export function createAdminVideoClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminVideoClient {
  return {
    async loadStats(windowDays: number): Promise<AdminVideoStats> {
      const response = await httpClient.get('/api/v1/admin/video-companion/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminVideoStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    callCount: numberValue(record.call_count ?? record.callCount),
    activeCount: numberValue(record.active_count ?? record.activeCount),
    endedCount: numberValue(record.ended_count ?? record.endedCount),
    missedCount: numberValue(record.missed_count ?? record.missedCount),
    rejectedCount: numberValue(record.rejected_count ?? record.rejectedCount),
    failedCount: numberValue(record.failed_count ?? record.failedCount),
    poorQualityCount: numberValue(record.poor_quality_count ?? record.poorQualityCount),
    stateBuckets: arrayValue(record.state_buckets ?? record.stateBuckets).map(normalizeBucket),
    reasonBuckets: arrayValue(record.reason_buckets ?? record.reasonBuckets).map(normalizeBucket),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): VideoBucket {
  const entry = recordValue(item)
  return {
    label: stringValue(entry.label, 'unknown'),
    count: numberValue(entry.count),
  }
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}

function numberValue(value: unknown): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? Math.max(0, parsed) : 0
}
