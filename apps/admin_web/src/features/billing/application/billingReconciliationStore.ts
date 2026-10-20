import { defineStore } from 'pinia'
import { ref } from 'vue'

import { apiNoticeWarning } from '@/api/apiNotice'
import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createBillingClient,
  type BillingReconciliationInput,
  type BillingReconciliationRun,
} from '@/features/billing/api/billingClient'

/// 对账记录与发起对账。发起前由页面校验渠道与营业日，store 只负责调用与
/// 状态收敛，避免校验规则散落两处。
export const useBillingReconciliationStore = defineStore('admin-billing-reconciliation', () => {
  const client = createBillingClient()
  const runs = ref<BillingReconciliationRun[]>([])
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)
  const isStarting = ref(false)
  const startError = ref<ApiError | null>(null)
  const lastMessage = ref('')
  const lastWarning = ref('')

  let listRequestSequence = 0
  let listLoadingSequence = 0
  let activeListLoadingRequests = 0

  async function load(): Promise<void> {
    const loadingSequence = beginLoading()
    const sequence = ++listRequestSequence
    error.value = null
    try {
      const result = await client.listReconciliationRuns()
      if (sequence === listRequestSequence) {
        runs.value = result
      }
    } catch (caught: unknown) {
      if (sequence === listRequestSequence) {
        error.value = mapApiError(caught)
      }
    } finally {
      finishLoading(loadingSequence)
    }
  }

  async function start(input: BillingReconciliationInput): Promise<boolean> {
    if (isStarting.value) {
      return false
    }
    isStarting.value = true
    startError.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const result = await client.startReconciliation(input)
      upsert(result.value)
      lastWarning.value = apiNoticeWarning(result.notice)
      lastMessage.value = '对账已开始，完成后会显示在下方记录里。'
      return true
    } catch (caught: unknown) {
      startError.value = mapApiError(caught)
      return false
    } finally {
      isStarting.value = false
    }
  }

  function runById(runId: string): BillingReconciliationRun | null {
    return runs.value.find((run) => run.id === runId) ?? null
  }

  function upsert(run: BillingReconciliationRun): void {
    const exists = runs.value.some((item) => item.id === run.id)
    runs.value = exists
      ? runs.value.map((item) => (item.id === run.id ? run : item))
      : [run, ...runs.value]
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
    error,
    isLoading,
    isStarting,
    lastMessage,
    lastWarning,
    load,
    runById,
    runs,
    start,
    startError,
  }
})
