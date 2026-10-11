import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface TouchPresetCount {
  preset: string
  count: number
}

export interface AdminTouchStats {
  windowDays: number
  deviceCount: number
  touchDeviceCount: number
  hapticDeviceCount: number
  outdatedCount: number
  averageSensitivity: number
  presetCoverage: TouchPresetCount[]
  generatedAt: string
}

export interface AdminTouchClient {
  loadStats(windowDays: number): Promise<AdminTouchStats>
}

/// Creates the administrator touch statistics client. Only aggregate counts and
/// averages are fetched; no coordinates, samples, or per-device detail leaves
/// the platform here.
export function createAdminTouchClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminTouchClient {
  return {
    async loadStats(windowDays: number): Promise<AdminTouchStats> {
      const response = await httpClient.get('/api/v1/admin/touch/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminTouchStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    touchDeviceCount: numberValue(record.touch_device_count ?? record.touchDeviceCount),
    hapticDeviceCount: numberValue(record.haptic_device_count ?? record.hapticDeviceCount),
    outdatedCount: numberValue(record.outdated_count ?? record.outdatedCount),
    averageSensitivity: numberValue(record.average_sensitivity ?? record.averageSensitivity),
    presetCoverage: arrayValue(record.preset_coverage ?? record.presetCoverage).map((item) => {
      const entry = recordValue(item)
      return {
        preset: stringValue(entry.preset, 'unknown'),
        count: numberValue(entry.count),
      }
    }),
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
