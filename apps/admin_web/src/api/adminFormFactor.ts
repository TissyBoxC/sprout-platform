import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface FormFactorBucket {
  label: string
  count: number
}

export interface AdminFormFactorStats {
  windowDays: number
  deviceCount: number
  locomotionCount: number
  voiceCount: number
  videoCallCount: number
  wearableCount: number
  nameBuckets: FormFactorBucket[]
  capabilityCounts: FormFactorBucket[]
  generatedAt: string
}

export interface AdminFormFactorClient {
  loadStats(windowDays: number): Promise<AdminFormFactorStats>
}

/// Creates the administrator form-factor statistics client. Only aggregate
/// counts are fetched; no device identity or per-device hardware detail leaves
/// the platform here.
export function createAdminFormFactorClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminFormFactorClient {
  return {
    async loadStats(windowDays: number): Promise<AdminFormFactorStats> {
      const response = await httpClient.get('/api/v1/admin/form-factor/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminFormFactorStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    locomotionCount: numberValue(record.locomotion_count ?? record.locomotionCount),
    voiceCount: numberValue(record.voice_count ?? record.voiceCount),
    videoCallCount: numberValue(record.video_call_count ?? record.videoCallCount),
    wearableCount: numberValue(record.wearable_count ?? record.wearableCount),
    nameBuckets: arrayValue(record.name_buckets ?? record.nameBuckets).map(normalizeBucket),
    capabilityCounts: arrayValue(record.capability_counts ?? record.capabilityCounts).map(
      normalizeBucket,
    ),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): FormFactorBucket {
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
