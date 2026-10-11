import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface VideoSessionBucket {
  label: string
  count: number
}

export interface AdminVideoSessionStats {
  windowDays: number
  deviceCount: number
  supportedCount: number
  captureAllowedCount: number
  outdatedCount: number
  poorQualityCount: number
  stateBuckets: VideoSessionBucket[]
  reasonBuckets: VideoSessionBucket[]
  generatedAt: string
}

export interface AdminVideoSessionClient {
  loadStats(windowDays: number): Promise<AdminVideoSessionStats>
}

/// Creates the administrator video-session statistics client. Only aggregate
/// counts are fetched; no captured audio or video ever leaves the platform
/// here.
export function createAdminVideoSessionClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminVideoSessionClient {
  return {
    async loadStats(windowDays: number): Promise<AdminVideoSessionStats> {
      const response = await httpClient.get('/api/v1/admin/video-session/stats', {
        params: { window_days: windowDays },
      })
      return normalizeStats(response.data?.data)
    },
  }
}

function normalizeStats(value: unknown): AdminVideoSessionStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    supportedCount: numberValue(record.supported_count ?? record.supportedCount),
    captureAllowedCount: numberValue(record.capture_allowed_count ?? record.captureAllowedCount),
    outdatedCount: numberValue(record.outdated_count ?? record.outdatedCount),
    poorQualityCount: numberValue(record.poor_quality_count ?? record.poorQualityCount),
    stateBuckets: arrayValue(record.state_buckets ?? record.stateBuckets).map(normalizeBucket),
    reasonBuckets: arrayValue(record.reason_buckets ?? record.reasonBuckets).map(normalizeBucket),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): VideoSessionBucket {
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
