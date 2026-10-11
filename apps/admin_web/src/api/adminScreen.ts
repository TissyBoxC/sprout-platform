import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface ScreenBucket {
  label: string
  count: number
}

export interface AdminScreenStats {
  windowDays: number
  deviceCount: number
  videoDeviceCount: number
  pointReadDeviceCount: number
  outdatedCount: number
  stateBuckets: ScreenBucket[]
  generatedAt: string
}

export interface AdminScreenClient {
  loadStats(windowDays: number): Promise<AdminScreenStats>
}

/// Creates the administrator screen statistics client. Only aggregate counts
/// are fetched; expressions, dictionary text and pixels never leave the
/// platform here.
export function createAdminScreenClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminScreenClient {
  return {
    async loadStats(windowDays: number): Promise<AdminScreenStats> {
      const response = await httpClient.get('/api/v1/admin/screen/stats', {
        params: { window_days: windowDays },
      })
      return normalizeStats(response.data?.data)
    },
  }
}

function normalizeStats(value: unknown): AdminScreenStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    videoDeviceCount: numberValue(record.video_device_count ?? record.videoDeviceCount),
    pointReadDeviceCount: numberValue(
      record.point_read_device_count ?? record.pointReadDeviceCount,
    ),
    outdatedCount: numberValue(record.outdated_count ?? record.outdatedCount),
    stateBuckets: arrayValue(record.state_buckets ?? record.stateBuckets).map(normalizeBucket),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): ScreenBucket {
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
