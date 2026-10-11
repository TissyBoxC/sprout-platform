import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface WearableBucket {
  label: string
  count: number
}

export interface AdminWearableStats {
  windowDays: number
  deviceCount: number
  locationEnabledCount: number
  permissionCount: number
  sosTriggeredCount: number
  sosCancelledCount: number
  geofenceOutsideCount: number
  precisionBuckets: WearableBucket[]
  stateBuckets: WearableBucket[]
  generatedAt: string
}

export interface AdminWearableClient {
  loadStats(windowDays: number): Promise<AdminWearableStats>
}

/// Creates the administrator wearable-location statistics client. Only
/// aggregate counts are fetched; no position, contact identity or per-device
/// history leaves the platform here.
export function createAdminWearableClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminWearableClient {
  return {
    async loadStats(windowDays: number): Promise<AdminWearableStats> {
      const response = await httpClient.get('/api/v1/admin/wearable/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminWearableStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    locationEnabledCount: numberValue(record.location_enabled_count ?? record.locationEnabledCount),
    permissionCount: numberValue(record.permission_count ?? record.permissionCount),
    sosTriggeredCount: numberValue(record.sos_triggered_count ?? record.sosTriggeredCount),
    sosCancelledCount: numberValue(record.sos_cancelled_count ?? record.sosCancelledCount),
    geofenceOutsideCount: numberValue(record.geofence_outside_count ?? record.geofenceOutsideCount),
    precisionBuckets: arrayValue(record.precision_buckets ?? record.precisionBuckets).map(
      normalizeBucket,
    ),
    stateBuckets: arrayValue(record.state_buckets ?? record.stateBuckets).map(normalizeBucket),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): WearableBucket {
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
