import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface PowerBucket {
  label: string
  count: number
}

export interface AdminPowerStats {
  windowDays: number
  deviceCount: number
  chargingCount: number
  lowBatteryCount: number
  lowPowerCount: number
  deepSleepCount: number
  averagePercent: number
  percentageBuckets: PowerBucket[]
  generatedAt: string
}

export interface AdminPowerClient {
  loadStats(windowDays: number): Promise<AdminPowerStats>
}

/// Creates the administrator battery statistics client. Only aggregate counts
/// and averages are fetched; no raw gauge telemetry or per-device power detail
/// leaves the platform here.
export function createAdminPowerClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminPowerClient {
  return {
    async loadStats(windowDays: number): Promise<AdminPowerStats> {
      const response = await httpClient.get('/api/v1/admin/power/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminPowerStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    chargingCount: numberValue(record.charging_count ?? record.chargingCount),
    lowBatteryCount: numberValue(record.low_battery_count ?? record.lowBatteryCount),
    lowPowerCount: numberValue(record.low_power_count ?? record.lowPowerCount),
    deepSleepCount: numberValue(record.deep_sleep_count ?? record.deepSleepCount),
    averagePercent: numberValue(record.average_percent ?? record.averagePercent),
    percentageBuckets: arrayValue(record.percentage_buckets ?? record.percentageBuckets).map(
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
