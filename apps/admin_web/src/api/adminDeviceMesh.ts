import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface DeviceMeshBucket {
  label: string
  count: number
}

export interface AdminDeviceMeshStats {
  windowDays: number
  groupCount: number
  deviceCount: number
  messageCount: number
  syncedContentCount: number
  conflictCount: number
  scopeBuckets: DeviceMeshBucket[]
  stateBuckets: DeviceMeshBucket[]
  generatedAt: string
}

export interface AdminDeviceMeshClient {
  loadStats(windowDays: number): Promise<AdminDeviceMeshStats>
}

/// Creates the administrator multi-device statistics client. Only aggregate
/// counts are fetched; group membership, message bodies and per-device content
/// state never leave the platform here.
export function createAdminDeviceMeshClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminDeviceMeshClient {
  return {
    async loadStats(windowDays: number): Promise<AdminDeviceMeshStats> {
      const response = await httpClient.get('/api/v1/admin/device-mesh/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminDeviceMeshStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    groupCount: numberValue(record.group_count ?? record.groupCount),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    messageCount: numberValue(record.message_count ?? record.messageCount),
    syncedContentCount: numberValue(record.synced_content_count ?? record.syncedContentCount),
    conflictCount: numberValue(record.conflict_count ?? record.conflictCount),
    scopeBuckets: arrayValue(record.scope_buckets ?? record.scopeBuckets).map(normalizeBucket),
    stateBuckets: arrayValue(record.state_buckets ?? record.stateBuckets).map(normalizeBucket),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): DeviceMeshBucket {
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
