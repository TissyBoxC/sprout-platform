// 计费页共用的展示换算。后端只传整数「人民币分」，界面只展示 `¥X.XX`；
// 任何页面都不得直接打印分值或使用美元符号。

/// 把整数人民币分格式化为 `¥9.90` 形式的展示金额。
///
/// 入参必须是非负整数分；负数（例如对账差额）用 formatSignedCents 展示。
/// 非法值按 0 处理，界面永远不会出现 NaN 或 undefined。
export function formatCents(value: number): string {
  const cents = toWholeCents(value)
  if (cents === null) {
    return '¥0.00'
  }
  return `¥${(cents / 100).toFixed(2)}`
}

/// 展示对账差额等可能为负的金额，`-¥3.50` 表示渠道比平台多收。
export function formatSignedCents(value: number): string {
  const cents = toWholeCents(value)
  if (cents === null || cents === 0) {
    return '¥0.00'
  }
  const absolute = Math.abs(cents)
  const formatted = `¥${(absolute / 100).toFixed(2)}`
  return cents < 0 ? `-${formatted}` : `+${formatted}`
}

/// 把后台状态值翻译成家长与运营都能看懂的中文。
export function billingStatusLabel(value: string): string {
  return BILLING_STATUS_LABELS[value] ?? '状态未知'
}

export function billingPeriodLabel(value: string, periodMonths: number): string {
  if (value === 'one_time') {
    return '一次性'
  }
  const known = BILLING_PERIOD_LABELS[value]
  if (known !== undefined) {
    return known
  }
  return periodMonths > 0 ? `${periodMonths} 个月` : '周期未知'
}

export function paymentChannelLabel(value: string): string {
  return PAYMENT_CHANNEL_LABELS[value] ?? '其他渠道'
}

export function paymentStatusLabel(value: string): string {
  return PAYMENT_STATUS_LABELS[value] ?? '状态未知'
}

export function refundStatusLabel(value: string): string {
  return REFUND_STATUS_LABELS[value] ?? '状态未知'
}

export function planStatusLabel(value: string): string {
  return PLAN_STATUS_LABELS[value] ?? '状态未知'
}

export function reconciliationStatusLabel(value: string): string {
  return RECONCILIATION_STATUS_LABELS[value] ?? '状态未知'
}

/// 判断状态值是否属于套餐自己的三种状态，用于下拉框回显。
export function isKnownPlanStatus(value: string): boolean {
  return value in PLAN_STATUS_LABELS
}

const BILLING_STATUS_LABELS: Record<string, string> = {
  pending: '待支付',
  paid: '已支付',
  cancelled: '已取消',
  expired: '已过期',
  refunded: '已退款',
  partially_refunded: '部分退款',
  requested: '待审批',
  approved: '已批准',
  processing: '退款中',
  succeeded: '退款成功',
  failed: '处理失败',
  rejected: '已拒绝',
  created: '已创建',
  closed: '已关闭',
}

const BILLING_PERIOD_LABELS: Record<string, string> = {
  monthly: '按月',
  quarterly: '按季',
  yearly: '按年',
}

const PAYMENT_CHANNEL_LABELS: Record<string, string> = {
  wechat: '微信支付',
  alipay: '支付宝',
  manual: '人工登记',
}

const PAYMENT_STATUS_LABELS: Record<string, string> = {
  created: '已创建',
  processing: '支付中',
  succeeded: '支付成功',
  failed: '支付失败',
  closed: '已关闭',
}

const REFUND_STATUS_LABELS: Record<string, string> = {
  requested: '待审批',
  approved: '已批准',
  processing: '退款中',
  succeeded: '退款成功',
  failed: '退款失败',
  rejected: '已拒绝',
}

const PLAN_STATUS_LABELS: Record<string, string> = {
  draft: '草稿',
  active: '已上架',
  archived: '已下架',
}

const RECONCILIATION_STATUS_LABELS: Record<string, string> = {
  running: '进行中',
  completed: '已完成',
  failed: '未能完成',
}

function toWholeCents(value: number): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return null
  }
  return Math.round(value)
}
