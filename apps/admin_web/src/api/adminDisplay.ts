import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface DisplayScreenBucket {
  label: string
  count: number
}

export interface AdminDisplayStats {
  windowDays: number
  deviceCount: number
  touchDeviceCount: number
  outdatedCount: number
  averageBrightness: number
  screenSizeBuckets: DisplayScreenBucket[]
  generatedAt: string
}

export interface AdminDisplayClient {
  loadStats(windowDays: number): Promise<AdminDisplayStats>
}

/// Creates the administrator display statistics client. Only aggregate counts
/// are fetched; no screenshots, pixels, or per-device policy detail leaves the
/// platform here.
export function createAdminDisplayClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminDisplayClient {
  return {
    async loadStats(windowDays: number): Promise<AdminDisplayStats> {
      const response = await httpClient.get('/api/v1/admin/display/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminDisplayStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    touchDeviceCount: numberValue(record.touch_device_count ?? record.touchDeviceCount),
    outdatedCount: numberValue(record.outdated_count ?? record.outdatedCount),
    averageBrightness: numberValue(record.average_brightness ?? record.averageBrightness),
    screenSizeBuckets: arrayValue(record.screen_size_buckets ?? record.screenSizeBuckets).map(
      (item) => {
        const entry = recordValue(item)
        return {
          label: stringValue(entry.label, 'unknown'),
          count: numberValue(entry.count),
        }
      },
    ),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
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
