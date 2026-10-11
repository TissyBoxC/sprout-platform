import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface CellularBucket {
  label: string
  count: number
}

export interface AdminCellularStats {
  windowDays: number
  deviceCount: number
  cellularCount: number
  roamingCount: number
  quotaExhaustedCount: number
  averageSignal: number
  bearerBuckets: CellularBucket[]
  esimBuckets: CellularBucket[]
  generatedAt: string
}

export interface AdminCellularClient {
  loadStats(windowDays: number): Promise<AdminCellularStats>
}

/// Creates the administrator connectivity statistics client. Only aggregate
/// counts are fetched; no subscriber identifiers, call detail, or per-device
/// traffic detail leaves the platform here.
export function createAdminCellularClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminCellularClient {
  return {
    async loadStats(windowDays: number): Promise<AdminCellularStats> {
      const response = await httpClient.get('/api/v1/admin/connectivity/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminCellularStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    cellularCount: numberValue(record.cellular_count ?? record.cellularCount),
    roamingCount: numberValue(record.roaming_count ?? record.roamingCount),
    quotaExhaustedCount: numberValue(record.quota_exhausted_count ?? record.quotaExhaustedCount),
    averageSignal: numberValue(record.average_signal ?? record.averageSignal),
    bearerBuckets: bucketList(record.bearer_buckets ?? record.bearerBuckets),
    esimBuckets: bucketList(record.esim_buckets ?? record.esimBuckets),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function bucketList(value: unknown): CellularBucket[] {
  return arrayValue(value).map((item) => {
    const entry = recordValue(item)
    return {
      label: stringValue(entry.label, 'unknown'),
      count: numberValue(entry.count),
    }
  })
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
