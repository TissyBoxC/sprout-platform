import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createBillingClient,
  type BillingChannel,
} from '@/features/billing/api/billingClient'

/// 支付渠道状态。渠道列表只读，但必须能被单独读到：所有渠道都不可用时，
/// 运营看到的应该是一句明确的「暂不可收款」，而不是一个空订单表。
export const useBillingChannelStore = defineStore('billingChannels', () => {
  const client = createBillingClient()

  const channels = ref<BillingChannel[]>([])
  const registeredCount = ref(0)
  const configuredCount = ref(0)
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref<ApiError | null>(null)

  const hasConfiguredChannel = computed(() => configuredCount.value > 0)

  /// 没有任何渠道可收款。未加载完成前不算作不可用，避免界面先闪一句错误的结论。
  const isSellingUnavailable = computed(
    () => loaded.value && registeredCount.value > 0 && configuredCount.value === 0,
  )

  async function load(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const summary = await client.listChannels()
      channels.value = summary.channels
      registeredCount.value = summary.registeredChannelCount
      configuredCount.value = summary.configuredChannelCount
      loaded.value = true
    } catch (caught) {
      error.value = mapApiError(caught)
    } finally {
      loading.value = false
    }
  }

  function $reset(): void {
    channels.value = []
    registeredCount.value = 0
    configuredCount.value = 0
    loading.value = false
    loaded.value = false
    error.value = null
  }

  return {
    channels,
    registeredCount,
    configuredCount,
    loading,
    loaded,
    error,
    hasConfiguredChannel,
    isSellingUnavailable,
    load,
    $reset,
  }
})
