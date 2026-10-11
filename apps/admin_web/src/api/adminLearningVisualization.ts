import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface LearningVisualizationBucket {
  label: string
  count: number
}

export interface AdminLearningVisualizationStats {
  windowDays: number
  deviceCount: number
  supportedCount: number
  touchCount: number
  lessonLinkedCount: number
  outdatedCount: number
  feedbackEvents: number
  stateBuckets: LearningVisualizationBucket[]
  styleBuckets: LearningVisualizationBucket[]
  generatedAt: string
}

export interface AdminLearningVisualizationClient {
  loadStats(windowDays: number): Promise<AdminLearningVisualizationStats>
}

/// Creates the administrator learning-visualization statistics client. Only
/// aggregate counts are fetched; no frame buffers or touch traces ever leave
/// the platform here.
export function createAdminLearningVisualizationClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminLearningVisualizationClient {
  return {
    async loadStats(windowDays: number): Promise<AdminLearningVisualizationStats> {
      const response = await httpClient.get(
        '/api/v1/admin/learning-visualization/stats',
        { params: { window_days: windowDays } },
      )
      return normalizeStats(response.data?.data)
    },
  }
}

function normalizeStats(value: unknown): AdminLearningVisualizationStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    supportedCount: numberValue(record.supported_count ?? record.supportedCount),
    touchCount: numberValue(record.touch_count ?? record.touchCount),
    lessonLinkedCount: numberValue(record.lesson_linked_count ?? record.lessonLinkedCount),
    outdatedCount: numberValue(record.outdated_count ?? record.outdatedCount),
    feedbackEvents: numberValue(record.feedback_events ?? record.feedbackEvents),
    stateBuckets: arrayValue(record.state_buckets ?? record.stateBuckets).map(normalizeBucket),
    styleBuckets: arrayValue(record.style_buckets ?? record.styleBuckets).map(normalizeBucket),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): LearningVisualizationBucket {
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
