import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { apiNoticeWarning } from '@/api/apiNotice'
import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createBillingClient,
  type BillingRefund,
} from '@/features/billing/api/billingClient'

export type BillingRefundDecision = 'approve' | 'reject'

export interface BillingRefundDecisionResult {
  succeeded: boolean
  error: ApiError | null
  warning: string
}

const PAGE_SIZE = 50

/// 待审批的退款申请。每个退款单单独记录处理中状态与错误，审批一个不会
/// 锁住其它退款单；分页游标同样按请求序号保护，旧结果不会覆盖新页。
export const useBillingRefundStore = defineStore('admin-billing-refunds', () => {
  const client = createBillingClient()
  const refunds = ref<BillingRefund[]>([])
  const page = ref(1)
  const total = ref(0)
  const totalPages = ref(0)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)
  const decidingRefundNo = ref('')
  const decisionErrors = ref<Record<string, ApiError | null>>({})
  const lastMessage = ref('')
  const lastWarning = ref('')

  let listRequestSequence = 0
  let listLoadingSequence = 0
  let activeListLoadingRequests = 0

  const hasPendingOnOtherPages = computed(() => page.value < totalPages.value)

  async function load(targetPage = page.value): Promise<void> {
    const loadingSequence = beginLoading()
    const sequence = ++listRequestSequence
    const requestedPage = Math.max(1, targetPage)
    error.value = null
    try {
      // 只读待审批的退款单；已批准或已拒绝的记录在订单详情里查看。
      const result = await client.listRefunds({
        status: 'requested',
        page: requestedPage,
        pageSize: PAGE_SIZE,
      })
      if (sequence !== listRequestSequence) {
        return
      }
      refunds.value = result.refunds
      page.value = result.pagination.page
      total.value = result.pagination.total
      totalPages.value = result.pagination.totalPages
      decisionErrors.value = {}
    } catch (caught: unknown) {
      if (sequence === listRequestSequence) {
        error.value = mapApiError(caught)
      }
    } finally {
      finishLoading(loadingSequence)
    }
  }

  async function goToPage(targetPage: number): Promise<void> {
    if (isLoading.value || targetPage < 1 || targetPage > Math.max(1, totalPages.value)) {
      return
    }
    await load(targetPage)
  }

  async function decide(
    refundNo: string,
    decision: BillingRefundDecision,
  ): Promise<BillingRefundDecisionResult> {
    if (decidingRefundNo.value !== '') {
      return { succeeded: false, error: null, warning: '' }
    }
    decidingRefundNo.value = refundNo
    lastMessage.value = ''
    lastWarning.value = ''
    decisionErrors.value = { ...decisionErrors.value, [refundNo]: null }
    try {
      const result =
        decision === 'approve'
          ? await client.approveRefund(refundNo)
          : await client.rejectRefund(refundNo)
      // 审批后该退款单不再待处理，先本地移除；服务端结果可能进入「退款中」
      // 或「已拒绝」，两种情况都不属于待审批列表。
      refunds.value = refunds.value.filter((refund) => refund.refundNo !== refundNo)
      total.value = Math.max(0, total.value - 1)
      lastWarning.value = apiNoticeWarning(result.notice)
      // 以服务端返回的状态为准：批准后可能进入「退款中」，拒绝后是「已拒绝」。
      lastMessage.value =
        result.value.status === 'rejected' ? '退款申请已拒绝。' : '退款申请已批准。'
      return { succeeded: true, error: null, warning: lastWarning.value }
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      decisionErrors.value = { ...decisionErrors.value, [refundNo]: mapped }
      return { succeeded: false, error: mapped, warning: '' }
    } finally {
      decidingRefundNo.value = ''
    }
  }

  function decisionErrorFor(refundNo: string): ApiError | null {
    return decisionErrors.value[refundNo] ?? null
  }

  /** 处理完成后重新读取，让列表与后端保持一致。 */
  async function refresh(): Promise<void> {
    await load()
  }

  function beginLoading(): number {
    const loadingSequence = ++listLoadingSequence
    activeListLoadingRequests += 1
    isLoading.value = true
    return loadingSequence
  }

  function finishLoading(loadingSequence: number): void {
    if (loadingSequence !== listLoadingSequence) {
      return
    }
    activeListLoadingRequests = Math.max(0, activeListLoadingRequests - 1)
    if (activeListLoadingRequests === 0) {
      isLoading.value = false
    }
  }

  return {
    decide,
    decidingRefundNo,
    decisionErrorFor,
    error,
    goToPage,
    hasPendingOnOtherPages,
    isLoading,
    lastMessage,
    lastWarning,
    load,
    page,
    refresh,
    refunds,
    total,
    totalPages,
  }
})
