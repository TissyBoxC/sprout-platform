import type { AxiosInstance } from 'axios'

import {
  readApiNotice,
  type ApiMutationResult,
  type ApiNotice,
} from '@/api/apiNotice'
import { createHttpClient } from '@/api/httpClient'

// 计费域的界面契约：金额一律是从后端返回的整数「人民币分」，字段名以
// `*_cny_cents` 结尾。此层不做除 100 的换算，也不猜测后端字段；
// 所有换算只发生在 presentation 的金额格式化函数里。

export type BillingOrderStatus =
  | 'pending'
  | 'paid'
  | 'cancelled'
  | 'expired'
  | 'refunded'
  | 'partially_refunded'

export type BillingPeriod = 'monthly' | 'quarterly' | 'yearly' | 'one_time'

export type BillingPlanStatus = 'draft' | 'active' | 'archived'

export type BillingPaymentStatus =
  | 'created'
  | 'processing'
  | 'succeeded'
  | 'failed'
  | 'closed'

export type BillingRefundStatus =
  | 'requested'
  | 'approved'
  | 'processing'
  | 'succeeded'
  | 'failed'
  | 'rejected'

export type BillingReconciliationStatus = 'running' | 'completed' | 'failed'

export interface BillingPagination {
  page: number
  pageSize: number
  total: number
  totalPages: number
}

export interface BillingOrder {
  id: string
  orderNo: string
  parentAccountId: string
  planCode: string
  planDisplayName: string
  billingPeriod: string
  periodMonths: number
  unitPriceCnyCents: number
  discountCnyCents: number
  payableCnyCents: number
  paidCnyCents: number
  refundedCnyCents: number
  status: string
  expiresAt: string
  paidAt: string
  cancelledAt: string
  closedAt: string
  createdAt: string
  remark: string
}

export interface BillingPayment {
  id: string
  channel: string
  channelTradeNo: string
  amountCnyCents: number
  status: string
  failureCode: string
  failureMessage: string
  paidAt: string
  createdAt: string
}

export interface BillingRefund {
  id: string
  refundNo: string
  orderNo: string
  orderId: string
  amountCnyCents: number
  reason: string
  operatorAccountId: string
  status: string
  channelRefundNo: string
  requestedAt: string
  approvedAt: string
  completedAt: string
}

export interface BillingOrderDetail {
  order: BillingOrder
  payments: BillingPayment[]
  refunds: BillingRefund[]
}

export interface BillingPlan {
  id: string
  code: string
  displayName: string
  description: string
  billingPeriod: string
  periodMonths: number
  priceCnyCents: number
  originalPriceCnyCents: number
  status: string
  sortOrder: number
  version: number
  publishedAt: string
  updatedAt: string
}

export interface BillingReconciliationRun {
  id: string
  channel: string
  businessDate: string
  status: string
  channelTotalCnyCents: number
  platformTotalCnyCents: number
  differenceCnyCents: number
  matchedCount: number
  missingCount: number
  extraCount: number
  startedAt: string
  finishedAt: string
}

export interface BillingOrderQuery {
  status: string
  planCode: string
  from: string
  to: string
  page: number
  pageSize: number
}

export interface BillingOrderPage {
  orders: BillingOrder[]
  pagination: BillingPagination
}

export interface BillingRefundPage {
  refunds: BillingRefund[]
  pagination: BillingPagination
}

export interface BillingRefundQuery {
  status: string
  page: number
  pageSize: number
}

export interface BillingPlanInput {
  code: string
  displayName: string
  description: string
  billingPeriod: string
  periodMonths: number
  priceCnyCents: number
  originalPriceCnyCents: number
  status: string
  sortOrder: number
}

export interface BillingPlanUpdateInput extends BillingPlanInput {
  id: string
  version: number
}

export interface BillingReconciliationInput {
  channel: string
  businessDate: string
}

// A registered payment provider and whether it can actually take money. The
// console has to show unconfigured providers explicitly: an operator who cannot
// see that every channel is unavailable will read an empty order list as "no one
// bought anything" rather than "nothing can be sold".
export interface BillingChannel {
  channel: string
  health: string
}

export interface BillingChannelSummary {
  channels: BillingChannel[]
  registeredChannelCount: number
  configuredChannelCount: number
}

export interface BillingClient {
  listOrders(query: BillingOrderQuery): Promise<BillingOrderPage>
  loadOrder(orderNo: string): Promise<BillingOrderDetail>
  listRefunds(query: BillingRefundQuery): Promise<BillingRefundPage>
  listPlans(): Promise<BillingPlan[]>
  createPlan(input: BillingPlanInput): Promise<ApiMutationResult<BillingPlan>>
  updatePlan(input: BillingPlanUpdateInput): Promise<ApiMutationResult<BillingPlan>>
  approveRefund(refundNo: string): Promise<ApiMutationResult<BillingRefund>>
  rejectRefund(refundNo: string): Promise<ApiMutationResult<BillingRefund>>
  listChannels(): Promise<BillingChannelSummary>
  listReconciliationRuns(): Promise<BillingReconciliationRun[]>
  startReconciliation(
    input: BillingReconciliationInput,
  ): Promise<ApiMutationResult<BillingReconciliationRun>>
  loadReconciliationRun(runId: string): Promise<BillingReconciliationRun>
}

const BASE_PATH = '/api/v1/admin/billing'

// 套餐列表与退款列表按页返回；管理端把它们当作完整清单展示，这里取一个
// 足够大的分页，超出部分需要由服务端支持更细的筛选。
const UNPAGED_PAGE_SIZE = 200

/// 计费管理端客户端。读取失败会原样抛出，由 store 统一映射成中文提示；
/// 写入成功时保留后端附带的警示信息，供页面单独展示。
export function createBillingClient(
  httpClient: AxiosInstance = createHttpClient(),
): BillingClient {
  return {
    async listOrders(query: BillingOrderQuery): Promise<BillingOrderPage> {
      const response = await httpClient.get(`${BASE_PATH}/orders`, {
        params: compactParams({
          status: query.status,
          plan_code: query.planCode,
          from: query.from,
          to: query.to,
          page: query.page,
          page_size: query.pageSize,
        }),
      })
      const payload = recordValue(response.data?.data)
      const orders = arrayValue(payload.orders ?? payload.items)
        .map(toOrder)
        .filter((order): order is BillingOrder => order !== null)
      return {
        orders,
        pagination: toPagination(payload.pagination ?? payload, query.page, query.pageSize),
      }
    },

    async loadOrder(orderNo: string): Promise<BillingOrderDetail> {
      const response = await httpClient.get(
        `${BASE_PATH}/orders/${encodeURIComponent(orderNo.trim())}`,
      )
      const payload = recordValue(response.data?.data)
      const order = toOrder(payload.order ?? payload)
      if (order === null) {
        throw new Error('billing order payload is missing the order')
      }
      return {
        order,
        payments: arrayValue(payload.payments)
          .map(toPayment)
          .filter((payment): payment is BillingPayment => payment !== null),
        refunds: arrayValue(payload.refunds)
          .map(toRefund)
          .filter((refund): refund is BillingRefund => refund !== null),
      }
    },

    async listRefunds(query: BillingRefundQuery): Promise<BillingRefundPage> {
      const response = await httpClient.get(`${BASE_PATH}/refunds`, {
        params: compactParams({
          status: query.status,
          page: query.page,
          page_size: query.pageSize,
        }),
      })
      const payload = recordValue(response.data?.data)
      return {
        refunds: arrayValue(payload.refunds ?? payload.items)
          .map(toRefund)
          .filter((refund): refund is BillingRefund => refund !== null),
        pagination: toPagination(payload.pagination ?? payload, query.page, query.pageSize),
      }
    },

    async listPlans(): Promise<BillingPlan[]> {
      const response = await httpClient.get(`${BASE_PATH}/plans`, {
        params: { page: 1, page_size: UNPAGED_PAGE_SIZE },
      })
      const payload = recordValue(response.data?.data)
      return arrayValue(payload.plans ?? payload.items)
        .map(toPlan)
        .filter((plan): plan is BillingPlan => plan !== null)
    },

    async listChannels(): Promise<BillingChannelSummary> {
      const response = await httpClient.get(`${BASE_PATH}/channels`)
      const payload = recordValue(response.data?.data)
      const channels = arrayValue(payload.channels)
        .map((entry) => {
          const item = recordValue(entry)
          const channel = stringValue(item.channel)
          if (!channel) {
            return null
          }
          return { channel, health: stringValue(item.health) || 'unknown' }
        })
        .filter((entry): entry is BillingChannel => entry !== null)
      return {
        channels,
        // The server owns these counts; recomputing them here would drift as
        // soon as a channel is registered but not yet configured.
        registeredChannelCount: nonNegativeInteger(
          payload.registered_channel_count,
          channels.length,
        ),
        configuredChannelCount: nonNegativeInteger(
          payload.configured_channel_count,
          channels.filter((entry) => entry.health === 'healthy').length,
        ),
      }
    },

    async createPlan(input: BillingPlanInput): Promise<ApiMutationResult<BillingPlan>> {
      const response = await httpClient.post(`${BASE_PATH}/plans`, planPayload(input))
      return { value: requirePlan(response.data?.data), notice: readApiNotice(response.data) }
    },

    async updatePlan(
      input: BillingPlanUpdateInput,
    ): Promise<ApiMutationResult<BillingPlan>> {
      const response = await httpClient.put(
        `${BASE_PATH}/plans/${encodeURIComponent(input.id)}`,
        { ...planPayload(input), version: input.version },
      )
      return { value: requirePlan(response.data?.data), notice: readApiNotice(response.data) }
    },

    async approveRefund(refundNo: string): Promise<ApiMutationResult<BillingRefund>> {
      const response = await httpClient.post(
        `${BASE_PATH}/refunds/${encodeURIComponent(refundNo.trim())}/approve`,
        {},
      )
      return {
        value: decidedRefund(response.data?.data, refundNo, 'approved'),
        notice: readApiNotice(response.data),
      }
    },

    async rejectRefund(refundNo: string): Promise<ApiMutationResult<BillingRefund>> {
      const response = await httpClient.post(
        `${BASE_PATH}/refunds/${encodeURIComponent(refundNo.trim())}/reject`,
        {},
      )
      return {
        value: decidedRefund(response.data?.data, refundNo, 'rejected'),
        notice: readApiNotice(response.data),
      }
    },

    async listReconciliationRuns(): Promise<BillingReconciliationRun[]> {
      const response = await httpClient.get(`${BASE_PATH}/reconciliation`)
      const payload = recordValue(response.data?.data)
      return arrayValue(payload.runs ?? payload.items)
        .map(toReconciliationRun)
        .filter((run): run is BillingReconciliationRun => run !== null)
    },

    async startReconciliation(
      input: BillingReconciliationInput,
    ): Promise<ApiMutationResult<BillingReconciliationRun>> {
      const response = await httpClient.post(`${BASE_PATH}/reconciliation`, {
        channel: input.channel,
        business_date: input.businessDate,
      })
      return {
        value: requireRun(response.data?.data),
        notice: readApiNotice(response.data),
      }
    },

    async loadReconciliationRun(runId: string): Promise<BillingReconciliationRun> {
      const response = await httpClient.get(
        `${BASE_PATH}/reconciliation/${encodeURIComponent(runId)}`,
      )
      return requireRun(response.data?.data)
    },
  }
}

// 审批接口可能只回传状态，甚至不回传退款单，此时用已知单号与决定结果补全，
// 避免界面出现空白或凭空成功。
function decidedRefund(value: unknown, refundNo: string, status: string): BillingRefund {
  const payload = recordValue(value)
  return (
    toRefund(payload.refund ?? payload) ?? {
      ...emptyRefund(),
      refundNo: refundNo.trim(),
      status,
    }
  )
}

function requirePlan(value: unknown): BillingPlan {
  const payload = recordValue(value)
  const plan = toPlan(payload.plan ?? payload)
  if (plan === null) {
    throw new Error('billing plan payload is missing the plan')
  }
  return plan
}

function requireRun(value: unknown): BillingReconciliationRun {
  const payload = recordValue(value)
  const run = toReconciliationRun(payload.run ?? payload)
  if (run === null) {
    throw new Error('billing reconciliation payload is missing the run')
  }
  return run
}

function planPayload(input: BillingPlanInput): Record<string, unknown> {
  return {
    code: input.code.trim(),
    display_name: input.displayName.trim(),
    description: input.description.trim(),
    billing_period: input.billingPeriod,
    period_months: input.periodMonths,
    price_cny_cents: input.priceCnyCents,
    original_price_cny_cents: input.originalPriceCnyCents,
    status: input.status,
    sort_order: input.sortOrder,
  }
}

function emptyRefund(): BillingRefund {
  return {
    id: '',
    refundNo: '',
    orderNo: '',
    orderId: '',
    amountCnyCents: 0,
    reason: '',
    operatorAccountId: '',
    status: '',
    channelRefundNo: '',
    requestedAt: '',
    approvedAt: '',
    completedAt: '',
  }
}

function compactParams(params: Record<string, string | number>): Record<string, string | number> {
  const compacted: Record<string, string | number> = {}
  for (const [key, value] of Object.entries(params)) {
    if (typeof value === 'string' && value.trim() === '') {
      continue
    }
    compacted[key] = value
  }
  return compacted
}

function toPagination(
  value: unknown,
  fallbackPage: number,
  fallbackPageSize: number,
): BillingPagination {
  const record = recordValue(value)
  const total = nonNegativeInteger(record.total)
  const pageSize = positiveInteger(record.page_size, fallbackPageSize)
  const totalPages = nonNegativeInteger(
    record.total_pages,
    total === 0 ? 0 : Math.ceil(total / pageSize),
  )
  return {
    page: positiveInteger(record.page, fallbackPage),
    pageSize,
    total,
    totalPages,
  }
}

function toOrder(value: unknown): BillingOrder | null {
  const record = recordValue(value)
  const orderNo = stringValue(record.order_no)
  if (orderNo === '') {
    return null
  }
  return {
    id: stringValue(record.id),
    orderNo,
    parentAccountId: stringValue(record.parent_account_id),
    planCode: stringValue(record.plan_code),
    planDisplayName: stringValue(record.plan_display_name),
    billingPeriod: stringValue(record.billing_period),
    periodMonths: nonNegativeInteger(record.period_months),
    unitPriceCnyCents: nonNegativeInteger(record.unit_price_cny_cents),
    discountCnyCents: nonNegativeInteger(record.discount_cny_cents),
    payableCnyCents: nonNegativeInteger(record.payable_cny_cents),
    paidCnyCents: nonNegativeInteger(record.paid_cny_cents),
    refundedCnyCents: nonNegativeInteger(record.refunded_cny_cents),
    status: stringValue(record.status),
    expiresAt: stringValue(record.expires_at),
    paidAt: stringValue(record.paid_at),
    cancelledAt: stringValue(record.cancelled_at),
    closedAt: stringValue(record.closed_at),
    createdAt: stringValue(record.created_at),
    remark: stringValue(record.remark),
  }
}

function toPayment(value: unknown): BillingPayment | null {
  const record = recordValue(value)
  const id = stringValue(record.id) || stringValue(record.channel_trade_no)
  if (id === '') {
    return null
  }
  return {
    id,
    channel: stringValue(record.channel),
    channelTradeNo: stringValue(record.channel_trade_no),
    amountCnyCents: nonNegativeInteger(record.amount_cny_cents),
    status: stringValue(record.status),
    failureCode: stringValue(record.failure_code),
    failureMessage: stringValue(record.failure_message),
    paidAt: stringValue(record.paid_at),
    createdAt: stringValue(record.created_at),
  }
}

function toRefund(value: unknown): BillingRefund | null {
  const record = recordValue(value)
  const refundNo = stringValue(record.refund_no)
  if (refundNo === '') {
    return null
  }
  const order = recordValue(record.order)
  return {
    id: stringValue(record.id),
    refundNo,
    orderNo: stringValue(record.order_no, stringValue(order.order_no)),
    orderId: stringValue(record.order_id),
    amountCnyCents: nonNegativeInteger(record.amount_cny_cents),
    reason: stringValue(record.reason),
    operatorAccountId: stringValue(record.operator_account_id),
    status: stringValue(record.status),
    channelRefundNo: stringValue(record.channel_refund_no),
    requestedAt: stringValue(record.requested_at),
    approvedAt: stringValue(record.approved_at),
    completedAt: stringValue(record.completed_at),
  }
}

function toPlan(value: unknown): BillingPlan | null {
  const record = recordValue(value)
  const id = stringValue(record.id)
  const code = stringValue(record.code)
  if (id === '' && code === '') {
    return null
  }
  return {
    id: id || code,
    code,
    displayName: stringValue(record.display_name),
    description: stringValue(record.description),
    billingPeriod: stringValue(record.billing_period),
    periodMonths: nonNegativeInteger(record.period_months),
    priceCnyCents: nonNegativeInteger(record.price_cny_cents),
    originalPriceCnyCents: nonNegativeInteger(record.original_price_cny_cents),
    status: stringValue(record.status, 'draft'),
    sortOrder: nonNegativeInteger(record.sort_order),
    version: positiveInteger(record.version, 1),
    publishedAt: stringValue(record.published_at),
    updatedAt: stringValue(record.updated_at),
  }
}

function toReconciliationRun(value: unknown): BillingReconciliationRun | null {
  const record = recordValue(value)
  const id = stringValue(record.id) || stringValue(record.run_id)
  if (id === '') {
    return null
  }
  return {
    id,
    channel: stringValue(record.channel),
    businessDate: stringValue(record.business_date),
    status: stringValue(record.status),
    channelTotalCnyCents: nonNegativeInteger(record.channel_total_cny_cents),
    platformTotalCnyCents: nonNegativeInteger(record.platform_total_cny_cents),
    differenceCnyCents: signedInteger(record.difference_cny_cents),
    matchedCount: nonNegativeInteger(record.matched_count),
    missingCount: nonNegativeInteger(record.missing_count),
    extraCount: nonNegativeInteger(record.extra_count),
    startedAt: stringValue(record.started_at),
    finishedAt: stringValue(record.finished_at),
  }
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}

function nonNegativeInteger(value: unknown, fallback = 0): number {
  return integerValue(value, fallback, 0)
}

function positiveInteger(value: unknown, fallback: number): number {
  return integerValue(value, fallback, 1)
}

// 对账差额可以为负，其余金额字段必须是非负整数；不符合契约的值按默认值
// 处理，界面永远不会拿到小数或非法数字。
function integerValue(value: unknown, fallback: number, minimum: number): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= minimum ? parsed : fallback
}

function signedInteger(value: unknown, fallback = 0): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) ? parsed : fallback
}

export type { ApiNotice, ApiMutationResult }
