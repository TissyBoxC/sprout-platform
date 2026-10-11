import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export type VisionTaskType =
  'object_recognition' | 'picture_book_recognition' | 'photo_qa' | 'capture'

export interface VisionTaskTypeCount {
  taskType: VisionTaskType
  count: number
}

export interface AdminVisionStats {
  windowDays: number
  taskCount: number
  completedCount: number
  failedCount: number
  expiredCount: number
  resultCount: number
  activeDeviceCount: number
  taskTypes: VisionTaskTypeCount[]
  generatedAt: string
}

export interface AdminVisionClient {
  loadStats(windowDays: number): Promise<AdminVisionStats>
}

/// Creates the administrator vision statistics client.
export function createAdminVisionClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminVisionClient {
  return {
    async loadStats(windowDays: number): Promise<AdminVisionStats> {
      const response = await httpClient.get('/api/v1/admin/vision/stats', {
        params: { window_days: windowDays },
      })
      const data = response.data?.data
      return normalizeStats(data)
    },
  }
}

function normalizeStats(value: unknown): AdminVisionStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    taskCount: numberValue(record.task_count ?? record.taskCount),
    completedCount: numberValue(record.completed_count ?? record.completedCount),
    failedCount: numberValue(record.failed_count ?? record.failedCount),
    expiredCount: numberValue(record.expired_count ?? record.expiredCount),
    resultCount: numberValue(record.result_count ?? record.resultCount),
    activeDeviceCount: numberValue(record.active_device_count ?? record.activeDeviceCount),
    taskTypes: arrayValue(record.task_types ?? record.taskTypes).map((item) => {
      const entry = recordValue(item)
      return {
        taskType: taskTypeValue(entry.task_type ?? entry.taskType),
        count: numberValue(entry.count),
      }
    }),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function taskTypeValue(value: unknown): VisionTaskType {
  switch (value) {
    case 'object_recognition':
    case 'picture_book_recognition':
    case 'photo_qa':
    case 'capture':
      return value
    default:
      return 'capture'
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
