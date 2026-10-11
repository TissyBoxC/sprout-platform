import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface ImagePrivacyBucket {
  label: string
  count: number
}

export interface AdminImagePrivacyStats {
  windowDays: number
  deviceCount: number
  cameraCount: number
  localOnlyCount: number
  uploadAllowedCount: number
  outdatedCount: number
  processedTotal: number
  uploadedTotal: number
  blockedTotal: number
  modeBuckets: ImagePrivacyBucket[]
  reasonBuckets: ImagePrivacyBucket[]
  generatedAt: string
}

export interface AdminImagePrivacyClient {
  loadStats(windowDays: number): Promise<AdminImagePrivacyStats>
}

/// Creates the administrator image-privacy statistics client. Only aggregate
/// counts are fetched; image bytes, thumbnails and face templates never leave
/// the platform here.
export function createAdminImagePrivacyClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminImagePrivacyClient {
  return {
    async loadStats(windowDays: number): Promise<AdminImagePrivacyStats> {
      const response = await httpClient.get('/api/v1/admin/image-privacy/stats', {
        params: { window_days: windowDays },
      })
      return normalizeStats(response.data?.data)
    },
  }
}

function normalizeStats(value: unknown): AdminImagePrivacyStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    cameraCount: numberValue(record.camera_count ?? record.cameraCount),
    localOnlyCount: numberValue(record.local_only_count ?? record.localOnlyCount),
    uploadAllowedCount: numberValue(
      record.upload_allowed_count ?? record.uploadAllowedCount,
    ),
    outdatedCount: numberValue(record.outdated_count ?? record.outdatedCount),
    processedTotal: numberValue(record.processed_total ?? record.processedTotal),
    uploadedTotal: numberValue(record.uploaded_total ?? record.uploadedTotal),
    blockedTotal: numberValue(record.blocked_total ?? record.blockedTotal),
    modeBuckets: arrayValue(record.mode_buckets ?? record.modeBuckets).map(
      normalizeBucket,
    ),
    reasonBuckets: arrayValue(record.reason_buckets ?? record.reasonBuckets).map(
      normalizeBucket,
    ),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): ImagePrivacyBucket {
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
