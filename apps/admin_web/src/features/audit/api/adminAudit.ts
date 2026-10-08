import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface AdminAuditEntry {
  id: string
  actorAccountId: string
  actorDisplayName: string
  targetAccountId: string
  targetDisplayName: string
  action: string
  detail: Record<string, AdminAuditDetailValue>
  createdAt: string
}

export type AdminAuditDetailValue = string | number | boolean | null

export interface AdminAuditQuery {
  action?: string
  actorAccountId?: string
  targetAccountId?: string
  from?: string
  to?: string
  page?: number
  pageSize?: number
}

export interface AdminAuditPage {
  items: AdminAuditEntry[]
  total: number
  page: number
  pageSize: number
  actions: string[]
}

export interface AdminAuditClient {
  load(query: AdminAuditQuery): Promise<AdminAuditPage>
}

/// Reads the administrator operation audit log. Detail objects are reduced to
/// a scalar allowlist here so a malformed upstream response cannot put tokens,
/// request bodies, or child media into the page state.
export function createAdminAuditClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminAuditClient {
  return {
    async load(query: AdminAuditQuery): Promise<AdminAuditPage> {
      const response = await httpClient.get('/api/v1/admin/audit', {
        params: {
          action: normalizedQueryValue(query.action),
          actor_account_id: normalizedQueryValue(query.actorAccountId),
          target_account_id: normalizedQueryValue(query.targetAccountId),
          from: normalizedQueryValue(query.from),
          to: normalizedQueryValue(query.to),
          page: positiveInteger(query.page, 1),
          page_size: positiveInteger(query.pageSize, 20),
        },
      })
      const payload = recordValue(response.data?.data ?? response.data)
      const items = arrayValue(payload.items).flatMap((value) => {
        const entry = toNullableAuditEntry(value)
        return entry === null ? [] : [entry]
      })
      const responsePage = recordValue(payload)
      const page = positiveInteger(
        responsePage.page ?? responsePage.page_number,
        positiveInteger(query.page, 1),
      )
      const pageSize = positiveInteger(
        responsePage.page_size ?? responsePage.pageSize,
        positiveInteger(query.pageSize, 20),
      )
      const actions = arrayValue(payload.actions).flatMap((value) => {
        const action = stringValue(value).trim()
        return action ? [action] : []
      })

      return {
        items,
        total: nonNegativeInteger(payload.total),
        page,
        pageSize,
        actions,
      }
    },
  }
}

function toNullableAuditEntry(value: unknown): AdminAuditEntry | null {
  const record = recordValue(value)
  const id = stringValue(record.id ?? record.event_id).trim()
  if (!id) {
    return null
  }

  return {
    id,
    actorAccountId: stringValue(record.actor_account_id ?? record.actorAccountId).trim(),
    actorDisplayName: stringValue(
      record.actor_display_name ?? record.actorDisplayName,
    ).trim(),
    targetAccountId: stringValue(
      record.target_account_id ?? record.targetAccountId,
    ).trim(),
    targetDisplayName: stringValue(
      record.target_display_name ?? record.targetDisplayName,
    ).trim(),
    action: stringValue(record.action).trim(),
    detail: safeDetailRecord(record.detail),
    createdAt: stringValue(record.created_at ?? record.createdAt).trim(),
  }
}

function safeDetailRecord(
  value: unknown,
): Record<string, AdminAuditDetailValue> {
  const record = recordValue(value)
  const safe: Record<string, AdminAuditDetailValue> = {}
  for (const [key, item] of Object.entries(record)) {
    if (isSensitiveDetailKey(key)) {
      continue
    }
    const primitive = primitiveDetailValue(item)
    if (primitive !== undefined) {
      safe[normalizedDetailKey(key)] = primitive
    }
  }
  return safe
}

function primitiveDetailValue(value: unknown): AdminAuditDetailValue | undefined {
  if (typeof value === 'string') {
    const normalized = value.trim()
    if (
      normalized === '' ||
      normalized.length > 240 ||
      isSensitiveDetailValue(normalized)
    ) {
      return undefined
    }
    return normalized
  }
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value
  }
  if (typeof value === 'boolean' || value === null) {
    return value
  }
  return undefined
}

function normalizedDetailKey(key: string): string {
  return key.trim().toLowerCase()
}

function isSensitiveDetailKey(key: string): boolean {
  const normalized = key.toLowerCase().replace(/[^a-z0-9]/g, '')
  return (
    normalized.includes('password') ||
    normalized.includes('token') ||
    normalized.includes('secret') ||
    normalized.includes('apikey') ||
    normalized.includes('authorization') ||
    normalized.includes('credential') ||
    normalized.includes('cookie') ||
    normalized.includes('body') ||
    normalized.includes('payload') ||
    normalized.includes('chat') ||
    normalized.includes('conversation') ||
    normalized.includes('transcript') ||
    normalized.includes('audio') ||
    normalized.includes('image') ||
    normalized.includes('video') ||
    normalized.includes('media') ||
    normalized.includes('recording') ||
    normalized.includes('attachment') ||
    normalized.includes('content') ||
    normalized.includes('prompt') ||
    normalized.includes('requestbody') ||
    normalized.includes('responsebody')
  )
}

function isSensitiveDetailValue(value: string): boolean {
  const normalized = value.trim().toLowerCase()
  return (
    /^bearer\s+/.test(normalized) ||
    /^sk-[a-z0-9_-]+$/i.test(normalized) ||
    /eyj[a-z0-9_-]{20,}\.[a-z0-9_-]{20,}/i.test(normalized) ||
    /^data:(?:audio|image|video)\//i.test(normalized) ||
    /^blob:/i.test(normalized) ||
    /"?(password|token|authorization|api[_-]?key|secret)"?\s*[:=]/.test(normalized)
  )
}

function normalizedQueryValue(value: unknown): string | undefined {
  const normalized = stringValue(value).trim()
  return normalized ? normalized : undefined
}

function positiveInteger(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

function nonNegativeInteger(value: unknown): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= 0 ? parsed : 0
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
