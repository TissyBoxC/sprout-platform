import axios from 'axios'
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { apiNoticeWarning } from '@/api/apiNotice'
import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createBillingClient,
  type BillingPlan,
  type BillingPlanInput,
  type BillingPlanUpdateInput,
} from '@/features/billing/api/billingClient'

/// 套餐编辑结果。`conflict` 单独标出，让界面给出「重新加载」的出口，而不是
/// 笼统的失败提示。
export interface BillingPlanMutationResult {
  succeeded: boolean
  conflict: boolean
  error: ApiError | null
  warning: string
}

// 后端在乐观并发冲突时返回 409；部分实现会用版本号错误码表达同一件事。
const CONFLICT_STATUS = 409
const CONFLICT_CODES = new Set([
  'plan_version_conflict',
  'version_conflict',
  'resource_version_conflict',
  'concurrent_modification',
])

export const BILLING_PLAN_CONFLICT_MESSAGE =
  '该套餐已被其他管理员修改，请重新加载。'

/// 套餐列表与单个套餐的编辑草稿。保存时携带版本号，其它管理员已经改过时
/// 服务端拒绝覆盖，界面提示重新加载。
export const useBillingPlanStore = defineStore('admin-billing-plans', () => {
  const client = createBillingClient()
  const plans = ref<BillingPlan[]>([])
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)
  const isSaving = ref(false)
  const saveError = ref<ApiError | null>(null)
  const conflictMessage = ref('')
  const lastMessage = ref('')
  const lastWarning = ref('')

  let listRequestSequence = 0
  let listLoadingSequence = 0
  let activeListLoadingRequests = 0

  const activePlanCount = computed(
    () => plans.value.filter((plan) => plan.status === 'active').length,
  )
  const draftPlanCount = computed(
    () => plans.value.filter((plan) => plan.status === 'draft').length,
  )

  async function load(): Promise<void> {
    const loadingSequence = beginLoading()
    const sequence = ++listRequestSequence
    error.value = null
    try {
      const result = await client.listPlans()
      if (sequence === listRequestSequence) {
        plans.value = result
      }
    } catch (caught: unknown) {
      if (sequence === listRequestSequence) {
        error.value = mapApiError(caught)
      }
    } finally {
      finishLoading(loadingSequence)
    }
  }

  async function create(input: BillingPlanInput): Promise<BillingPlanMutationResult> {
    if (isSaving.value) {
      return idleResult()
    }
    clearFeedback()
    isSaving.value = true
    try {
      const result = await client.createPlan(input)
      upsert(result.value)
      lastWarning.value = apiNoticeWarning(result.notice)
      lastMessage.value = '套餐已保存。'
      return { succeeded: true, conflict: false, error: null, warning: lastWarning.value }
    } catch (caught: unknown) {
      return failWith(caught)
    } finally {
      isSaving.value = false
    }
  }

  async function update(
    input: BillingPlanUpdateInput,
  ): Promise<BillingPlanMutationResult> {
    if (isSaving.value) {
      return idleResult()
    }
    clearFeedback()
    isSaving.value = true
    try {
      const result = await client.updatePlan(input)
      upsert(result.value)
      lastWarning.value = apiNoticeWarning(result.notice)
      lastMessage.value = '套餐已保存。'
      return { succeeded: true, conflict: false, error: null, warning: lastWarning.value }
    } catch (caught: unknown) {
      return failWith(caught)
    } finally {
      isSaving.value = false
    }
  }

  /// 发生冲突后重新读取套餐，让草稿基于最新版本重新编辑。
  async function reloadAfterConflict(): Promise<void> {
    conflictMessage.value = ''
    saveError.value = null
    await load()
  }

  function planById(planId: string): BillingPlan | null {
    return plans.value.find((plan) => plan.id === planId) ?? null
  }

  function upsert(plan: BillingPlan): void {
    const exists = plans.value.some((item) => item.id === plan.id)
    plans.value = exists
      ? plans.value.map((item) => (item.id === plan.id ? plan : item))
      : sortByOrder([...plans.value, plan])
  }

  function clearFeedback(): void {
    saveError.value = null
    conflictMessage.value = ''
    lastMessage.value = ''
    lastWarning.value = ''
  }

  function failWith(caught: unknown): BillingPlanMutationResult {
    if (isVersionConflict(caught)) {
      // 冲突不是普通失败：保留已有列表，只要求重新加载，界面上不能假装已保存。
      conflictMessage.value = BILLING_PLAN_CONFLICT_MESSAGE
      return { succeeded: false, conflict: true, error: null, warning: '' }
    }
    saveError.value = mapApiError(caught)
    return { succeeded: false, conflict: false, error: saveError.value, warning: '' }
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
    activePlanCount,
    conflictMessage,
    create,
    draftPlanCount,
    error,
    isLoading,
    isSaving,
    lastMessage,
    lastWarning,
    load,
    planById,
    plans,
    reloadAfterConflict,
    saveError,
    update,
  }
})

function idleResult(): BillingPlanMutationResult {
  return { succeeded: false, conflict: false, error: null, warning: '' }
}

function sortByOrder(plans: BillingPlan[]): BillingPlan[] {
  return [...plans].sort((left, right) => {
    if (left.sortOrder !== right.sortOrder) {
      return left.sortOrder - right.sortOrder
    }
    return left.code.localeCompare(right.code)
  })
}

export function isVersionConflict(caught: unknown): boolean {
  if (!axios.isAxiosError(caught)) {
    return false
  }
  if (caught.response?.status === CONFLICT_STATUS) {
    return true
  }
  const code = caught.response?.data?.error?.code
  return typeof code === 'string' && CONFLICT_CODES.has(code)
}
