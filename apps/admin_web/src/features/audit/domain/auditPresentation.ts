import type {
  AdminAuditDetailValue,
  AdminAuditEntry,
} from '@/features/audit/api/adminAudit'

const ACTION_LABELS: Record<string, string> = {
  'auth.login.succeeded': '管理员登录',
  'auth.login.failed': '管理员登录失败',
  'auth.logout': '管理员退出',
  'privacy.deletion.requested': '申请注销账号',
  'privacy.deletion.cancelled': '撤销注销申请',
  'privacy.deletion.completed': '完成账号注销',
  'privacy.consent.withdrawn': '撤回监护人授权',
  'privacy.export.requested': '导出个人数据',
  'admin.family.created': '创建家长账号',
  'admin.family.profile.updated': '修改家长资料',
  'admin.family.password.reset': '重置家长密码',
  'admin.family.ai_account.created': '开通家长 AI 服务',
  'admin.family.ai_account.updated': '调整家长 AI 服务',
  'admin.family.device_binding.revoked': '解绑家长设备',
  'admin.settings.updated': '修改系统设置',
  'admin.feature_center.config.updated': '修改功能配置',
  'admin.feature_center.health.checked': '检查功能状态',
  'admin.release.created': '登记新版本',
  'admin.release.updated': '调整版本信息',
  'admin.release.deleted': '删除版本记录',
  'admin.download.uploaded': '上传发布文件',
  'admin.download.deleted': '删除发布文件',
  'admin.service.upgrade.started': '开始升级服务',
  'admin.service.upgrade.completed': '完成升级服务',
  'admin.service.upgrade.failed': '升级服务失败',
}

const DETAIL_LABELS: Record<string, string> = {
  action: '操作类型',
  affected_count: '影响数量',
  channel: '更新渠道',
  connection_state: '连接状态',
  consent_type: '授权类型',
  current_version: '当前版本',
  deleted_counts: '已删除内容',
  file_name: '文件名称',
  firmware_version: '设备版本',
  kind: '文件类型',
  latest_version: '最新版本',
  message: '结果说明',
  model: '模型名称',
  new_status: '新状态',
  previous_status: '原状态',
  reason: '原因',
  release_notes: '版本说明',
  scope: '影响范围',
  service: '服务名称',
  status: '处理结果',
  target_version: '目标版本',
  transport: '连接方式',
  version: '版本',
}

const GENERIC_ACTION_LABEL = '其他操作'

export interface AuditDetailEntry {
  key: string
  label: string
  value: string
}

export function auditActionLabel(action: string): string {
  return ACTION_LABELS[action.trim()] ?? GENERIC_ACTION_LABEL
}

export function auditActionDescription(action: string): string {
  const label = auditActionLabel(action)
  return label === GENERIC_ACTION_LABEL ? '已记录一次系统操作。' : label
}

export function auditDetailEntries(
  detail: Record<string, AdminAuditDetailValue>,
): AuditDetailEntry[] {
  return Object.entries(detail)
    .flatMap(([key, value]) => {
      const formatted = formatDetailValue(value)
      if (!formatted) {
        return []
      }
      return [
        {
          key,
          label: DETAIL_LABELS[key] ?? '操作信息',
          value: formatted,
        },
      ]
    })
    .slice(0, 8)
}

export function auditActorLabel(entry: AdminAuditEntry): string {
  return entry.actorDisplayName || entry.actorAccountId || '系统'
}

export function auditTargetLabel(entry: AdminAuditEntry): string {
  return entry.targetDisplayName || entry.targetAccountId || '未指定'
}

function formatDetailValue(value: AdminAuditDetailValue): string {
  if (value === null || value === undefined || value === '') {
    return ''
  }
  if (typeof value === 'string') {
    return value
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  return ''
}
