import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export type NotificationCategory =
  | 'account_security'
  | 'device_status'
  | 'content_release'
  | 'service_update'
  | 'usage_report'
  | 'family_message'
  | 'system_announcement'

export type NotificationSeverity = 'info' | 'success' | 'warning' | 'critical'

export type NotificationAudience = 'parent' | 'all_parents' | 'family_devices'

export type NotificationChannel = 'in_app' | 'push' | 'sms' | 'email' | 'device'

export type NotificationSource = 'administrator' | 'guardian' | 'system'

/// One operator-authored notice plus aggregated delivery counters. The
/// counters never expose per-guardian identity, only totals.
export interface AdminNotification {
  id: string
  category: NotificationCategory
  severity: NotificationSeverity
  title: string
  body: string
  actionPath: string
  actionLabel: string
  audience: NotificationAudience
  parentAccountId: string
  deviceId: string
  displayDurationSeconds: number
  channels: string[]
  source: NotificationSource
  createdBy: string
  publishAt: string
  expiresAt: string
  createdAt: string
  updatedAt: string
  deliveryTotal: number
  deliveryPending: number
  deliveryDelivered: number
  deliveryRead: number
  deliveryFailed: number
}

export interface AdminNotificationStats {
  total: number
  broadcastCount: number
  pendingDeliveryCount: number
  failedDeliveryCount: number
}

export interface AdminNotificationFilters {
  category: NotificationCategory | 'all'
  audience: NotificationAudience | 'all'
  source: NotificationSource | 'all'
  query: string
  page: number
  pageSize: number
}

export interface AdminNotificationListResult {
  items: AdminNotification[]
  total: number
  page: number
  pageSize: number
  totalPages: number
}

export interface CreateAdminNotificationInput {
  category: NotificationCategory
  severity: NotificationSeverity
  title: string
  body: string
  actionPath: string
  actionLabel: string
  audience: NotificationAudience
  channels: NotificationChannel[]
  parentAccountId: string
  deviceId: string
  displayDurationSeconds: number
  expiresAt: string
}

export interface AdminNotificationsClient {
  loadNotifications(
    filters: AdminNotificationFilters,
  ): Promise<AdminNotificationListResult>
  createNotification(input: CreateAdminNotificationInput): Promise<AdminNotification>
  deleteNotification(notificationId: string): Promise<void>
  loadStats(): Promise<AdminNotificationStats>
}

/// Creates the administrator client for platform notices, broadcasts, and
/// family-message delivery counters.
export function createAdminNotificationsClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminNotificationsClient {
  const basePath = '/api/v1/admin/notifications'

  return {
    async loadNotifications(
      filters: AdminNotificationFilters,
    ): Promise<AdminNotificationListResult> {
      const response = await httpClient.get(basePath, {
        params: compactParams({
          category: filters.category === 'all' ? undefined : filters.category,
          audience: filters.audience === 'all' ? undefined : filters.audience,
          source: filters.source === 'all' ? undefined : filters.source,
          query: filters.query.trim(),
          page: String(Math.max(1, filters.page)),
          page_size: String(Math.max(1, filters.pageSize)),
        }),
      })
      const payload = recordValue(response.data?.data)
      const items = arrayValue(payload.items).flatMap((item) => {
        const notification = toNullableNotification(item)
        return notification === null ? [] : [notification]
      })
      return {
        items,
        total: nonNegativeInteger(payload.total, items.length),
        page: positiveInteger(payload.page, filters.page),
        pageSize: positiveInteger(
          payload.page_size ?? payload.pageSize,
          filters.pageSize,
        ),
        totalPages: positiveInteger(payload.total_pages ?? payload.totalPages, 1),
      }
    },

    async createNotification(
      input: CreateAdminNotificationInput,
    ): Promise<AdminNotification> {
      const response = await httpClient.post(basePath, toCreatePayload(input))
      const notification = toNullableNotification(response.data?.data?.notification)
      if (notification === null) {
        throw new Error('notification response is missing a notification')
      }
      return notification
    },

    async deleteNotification(notificationId: string): Promise<void> {
      await httpClient.delete(
        `${basePath}/${encodeURIComponent(notificationId.trim())}`,
      )
    },

    async loadStats(): Promise<AdminNotificationStats> {
      const response = await httpClient.get(`${basePath}/stats`)
      const payload = recordValue(response.data?.data)
      return {
        total: nonNegativeInteger(payload.total, 0),
        broadcastCount: nonNegativeInteger(
          payload.broadcast_count ?? payload.broadcastCount,
          0,
        ),
        pendingDeliveryCount: nonNegativeInteger(
          payload.pending_delivery_count ?? payload.pendingDeliveryCount,
          0,
        ),
        failedDeliveryCount: nonNegativeInteger(
          payload.failed_delivery_count ?? payload.failedDeliveryCount,
          0,
        ),
      }
    },
  }
}

function toCreatePayload(
  input: CreateAdminNotificationInput,
): Record<string, unknown> {
  return {
    category: input.category,
    severity: input.severity,
    title: input.title.trim(),
    body: input.body.trim(),
    action_path: input.actionPath.trim(),
    action_label: input.actionLabel.trim(),
    audience: input.audience,
    channels: input.channels,
    parent_account_id:
      input.audience === 'parent' ? input.parentAccountId.trim() : '',
    device_id:
      input.audience === 'family_devices' ? input.deviceId.trim() : '',
    display_duration_seconds: nonNegativeInteger(input.displayDurationSeconds, 0),
    expires_at: input.expiresAt.trim() === '' ? undefined : input.expiresAt.trim(),
  }
}

function toNullableNotification(value: unknown): AdminNotification | null {
  const record = recordValue(value)
  const id = stringValue(record.id ?? record.notification_id).trim()
  if (id === '') {
    return null
  }
  return {
    id,
    category: categoryValue(record.category),
    severity: severityValue(record.severity),
    title: stringValue(record.title).trim(),
    body: stringValue(record.body).trim(),
    actionPath: stringValue(record.action_path ?? record.actionPath).trim(),
    actionLabel: stringValue(record.action_label ?? record.actionLabel).trim(),
    audience: audienceValue(record.audience),
    parentAccountId: stringValue(
      record.parent_account_id ?? record.parentAccountId,
    ).trim(),
    deviceId: stringValue(record.device_id ?? record.deviceId).trim(),
    displayDurationSeconds: nonNegativeInteger(
      record.display_duration_seconds ?? record.displayDurationSeconds,
      0,
    ),
    channels: stringList(record.channels),
    source: sourceValue(record.source),
    createdBy: stringValue(record.created_by ?? record.createdBy).trim(),
    publishAt: stringValue(record.publish_at ?? record.publishAt),
    expiresAt: stringValue(record.expires_at ?? record.expiresAt),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
    deliveryTotal: nonNegativeInteger(
      record.delivery_total ?? record.deliveryTotal,
      0,
    ),
    deliveryPending: nonNegativeInteger(
      record.delivery_pending ?? record.deliveryPending,
      0,
    ),
    deliveryDelivered: nonNegativeInteger(
      record.delivery_delivered ?? record.deliveryDelivered,
      0,
    ),
    deliveryRead: nonNegativeInteger(
      record.delivery_read ?? record.deliveryRead,
      0,
    ),
    deliveryFailed: nonNegativeInteger(
      record.delivery_failed ?? record.deliveryFailed,
      0,
    ),
  }
}

function categoryValue(value: unknown): NotificationCategory {
  switch (value) {
    case 'account_security':
    case 'device_status':
    case 'content_release':
    case 'service_update':
    case 'usage_report':
    case 'family_message':
    case 'system_announcement':
      return value
    default:
      return 'system_announcement'
  }
}

function severityValue(value: unknown): NotificationSeverity {
  switch (value) {
    case 'info':
    case 'success':
    case 'warning':
    case 'critical':
      return value
    default:
      return 'info'
  }
}

function audienceValue(value: unknown): NotificationAudience {
  switch (value) {
    case 'parent':
    case 'all_parents':
    case 'family_devices':
      return value
    default:
      return 'all_parents'
  }
}

function sourceValue(value: unknown): NotificationSource {
  switch (value) {
    case 'administrator':
    case 'guardian':
    case 'system':
      return value
    default:
      return 'administrator'
  }
}

function compactParams(input: Record<string, string | undefined>): Record<string, string> {
  const params: Record<string, string> = {}
  for (const [key, value] of Object.entries(input)) {
    if (value !== undefined && value !== '') {
      params[key] = value
    }
  }
  return params
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function stringList(value: unknown): string[] {
  return arrayValue(value)
    .map((item) => stringValue(item).trim())
    .filter((item) => item.length > 0)
}

function nonNegativeInteger(value: unknown, fallback: number): number {
  const number = Number(value)
  return Number.isFinite(number) && number >= 0 ? Math.trunc(number) : fallback
}

function positiveInteger(value: unknown, fallback: number): number {
  const number = Number(value)
  return Number.isFinite(number) && number > 0 ? Math.trunc(number) : fallback
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}
