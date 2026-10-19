import type { AxiosInstance } from 'axios'

import {
  readApiNotice,
  type ApiMutationResult,
  type ApiNotice,
} from '@/api/apiNotice'
import { createHttpClient } from '@/api/httpClient'

/// One administrator account as the permission screen needs it. It never
/// contains credentials, phone numbers, or child data.
export interface AdminPermissionAccount {
  accountId: string
  email: string
  displayName: string
  status: string
  createdAt: string
  permissions: string[]
}

/// One assignable capability with a stable identifier and a plain-language
/// name for operators.
export interface AdminPermissionOption {
  id: string
  label: string
  group: string
}

export interface AdminPermissionOverview {
  catalog: AdminPermissionOption[]
  accounts: AdminPermissionAccount[]
}

export interface AdminPermissionsClient {
  load(): Promise<AdminPermissionOverview>
  update(
    accountId: string,
    permissions: string[],
  ): Promise<ApiMutationResult<AdminPermissionAccount>>
}

/// The assignable catalog is closed and must stay in sync with the platform
/// constants. Unknown identifiers returned by the backend are ignored so the
/// screen never renders a capability it cannot describe.
export const ADMIN_PERMISSION_OPTIONS: AdminPermissionOption[] = [
  { id: 'admins.read', label: '查看管理员权限', group: '管理员与权限' },
  { id: 'admins.manage', label: '调整管理员权限', group: '管理员与权限' },
  { id: 'devices.read', label: '查看设备', group: '设备' },
  { id: 'devices.manage', label: '管理设备与指令', group: '设备' },
  { id: 'family.read', label: '查看家长账号', group: '家长账号' },
  { id: 'family.manage', label: '管理家长账号', group: '家长账号' },
  { id: 'ai_accounts.read', label: '查看 AI 服务', group: 'AI 服务' },
  { id: 'ai_accounts.manage', label: '开通与调整 AI 服务', group: 'AI 服务' },
  { id: 'ota.read', label: '查看设备系统更新', group: '设备系统更新' },
  { id: 'ota.manage', label: '发布设备系统更新', group: '设备系统更新' },
  { id: 'storage.read', label: '查看下载文件', group: '下载文件' },
  { id: 'storage.manage', label: '上传与删除下载文件', group: '下载文件' },
  { id: 'content.read', label: '查看内容库', group: '内容与主题' },
  { id: 'content.review', label: '审核与发布内容', group: '内容与主题' },
  { id: 'service.read', label: '查看服务版本', group: '服务管理' },
  { id: 'service.upgrade', label: '升级服务版本', group: '服务管理' },
  { id: 'feature_center.read', label: '查看功能中心', group: '服务管理' },
  { id: 'feature_center.manage', label: '调整功能配置', group: '服务管理' },
  { id: 'settings.read', label: '查看系统设置', group: '系统设置' },
  { id: 'settings.manage', label: '修改系统设置', group: '系统设置' },
  { id: 'notifications.read', label: '查看通知', group: '消息通知' },
  { id: 'notifications.manage', label: '创建与删除通知', group: '消息通知' },
  { id: 'stats.read', label: '查看运行统计', group: '运行概览' },
  { id: 'audit.read', label: '查看操作记录', group: '运行概览' },
]

const WILDCARD_PERMISSION = '*'

export const PERMISSION_WILDCARD_OPTION: AdminPermissionOption = {
  id: WILDCARD_PERMISSION,
  label: '全部管理权限',
  group: '全部管理权限',
}

/// Creates the administrator client for permission review and assignment.
export function createAdminPermissionsClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminPermissionsClient {
  const basePath = '/api/v1/admin/permissions'

  return {
    async load(): Promise<AdminPermissionOverview> {
      const response = await httpClient.get(basePath)
      const payload = recordValue(response.data?.data)
      const catalog = catalogFromServer(payload.catalog)
      const accounts = arrayValue(payload.accounts).flatMap((item) => {
        const account = toAccount(item)
        return account === null ? [] : [account]
      })
      return { catalog, accounts }
    },

    async update(
      accountId: string,
      permissions: string[],
    ): Promise<ApiMutationResult<AdminPermissionAccount>> {
      const response = await httpClient.put(`${basePath}/${encodeURIComponent(accountId.trim())}`, {
        permissions,
      })
      const account = toAccount(response.data?.data?.account)
      if (account === null) {
        throw new Error('permission response is missing an account')
      }
      return {
        value: account,
        notice: readApiNotice(response.data),
      }
    },
  }
}

/// The server owns the authoritative catalog. When it returns a usable list we
/// prefer it and translate each id through the local label table; otherwise we
/// fall back to the full local catalog so the screen still renders.
function catalogFromServer(value: unknown): AdminPermissionOption[] {
  const ids = stringList(value)
  if (ids.length === 0) {
    return [PERMISSION_WILDCARD_OPTION, ...ADMIN_PERMISSION_OPTIONS]
  }
  const labelled = ids.flatMap((id) => {
    const known = ADMIN_PERMISSION_OPTIONS.find((option) => option.id === id)
    return known === undefined ? [] : [known]
  })
  if (labelled.length === 0) {
    return [PERMISSION_WILDCARD_OPTION, ...ADMIN_PERMISSION_OPTIONS]
  }
  return [PERMISSION_WILDCARD_OPTION, ...labelled]
}

function toAccount(value: unknown): AdminPermissionAccount | null {
  const record = recordValue(value)
  const accountId = stringValue(record.account_id ?? record.accountId).trim()
  if (accountId === '') {
    return null
  }
  return {
    accountId,
    email: stringValue(record.email).trim(),
    displayName: stringValue(record.display_name ?? record.displayName).trim(),
    status: stringValue(record.status, 'active'),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    permissions: stringList(record.permissions),
  }
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function stringList(value: unknown): string[] {
  return arrayValue(value)
    .map((item) => stringValue(item).trim())
    .filter((item) => item.length > 0)
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}
