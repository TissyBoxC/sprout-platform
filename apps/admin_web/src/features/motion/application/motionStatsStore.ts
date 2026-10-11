import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminMotionClient, type AdminMotionStats } from '@/api/adminMotion'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator motion statistics view. Only aggregate counts are
/// stored; raw motor telemetry and per-device movement detail are never fetched
/// here.
export const useMotionStatsStore = defineStore('admin-motion-stats', () => {
  const client = createAdminMotionClient()
  const stats = ref<AdminMotionStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const hasData = computed(() => stats.value !== null)
  const faultedRatio = computed(() => {
    const value = stats.value
    if (!value || value.deviceCount === 0) {
      return 0
    }
    return Math.round((value.faultedCount / value.deviceCount) * 100)
  })

  async function load(days = windowDays.value): Promise<void> {
    windowDays.value = days
    isLoading.value = true
    error.value = null
    try {
      stats.value = await client.loadStats(days)
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoading.value = false
    }
  }

  return {
    stats,
    windowDays,
    isLoading,
    error,
    hasData,
    faultedRatio,
    load,
  }
})
