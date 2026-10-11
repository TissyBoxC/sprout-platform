import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface MotionBucket {
  label: string
  count: number
}

export interface AdminMotionStats {
  windowDays: number
  deviceCount: number
  runningCount: number
  faultedCount: number
  idleCount: number
  actionBuckets: MotionBucket[]
  stateBuckets: MotionBucket[]
  generatedAt: string
}

export interface AdminMotionClient {
  loadStats(windowDays: number): Promise<AdminMotionStats>
}

/// Creates the administrator motion statistics client. Only aggregate counts
/// are fetched; no raw motor telemetry or per-device movement detail leaves the
/// platform here.
export function createAdminMotionClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminMotionClient {
  return {
    async loadStats(windowDays: number): Promise<AdminMotionStats> {
      const response = await httpClient.get('/api/v1/admin/motion/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminMotionStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    runningCount: numberValue(record.running_count ?? record.runningCount),
    faultedCount: numberValue(record.faulted_count ?? record.faultedCount),
    idleCount: numberValue(record.idle_count ?? record.idleCount),
    actionBuckets: arrayValue(record.action_buckets ?? record.actionBuckets).map(normalizeBucket),
    stateBuckets: arrayValue(record.state_buckets ?? record.stateBuckets).map(normalizeBucket),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): MotionBucket {
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
