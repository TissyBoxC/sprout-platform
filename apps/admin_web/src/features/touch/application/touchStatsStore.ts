import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminTouchClient, type AdminTouchStats } from '@/api/adminTouch'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator touch statistics view. Only aggregate counts are
/// stored; coordinates, samples, and per-device payloads are never fetched
/// here.
export const useTouchStatsStore = defineStore('admin-touch-stats', () => {
  const client = createAdminTouchClient()
  const stats = ref<AdminTouchStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const hasData = computed(() => stats.value !== null)
  const touchRatio = computed(() => {
    const value = stats.value
    if (!value || value.deviceCount === 0) {
      return 0
    }
    return Math.round((value.touchDeviceCount / value.deviceCount) * 100)
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
    touchRatio,
    load,
  }
})
