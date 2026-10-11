import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminVideoSessionClient, type AdminVideoSessionStats } from '@/api/adminVideoSession'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator video-session statistics view. Only aggregate counts
/// are stored; captured media never leaves the platform here.
export const useVideoSessionStatsStore = defineStore('admin-video-session-stats', () => {
  const client = createAdminVideoSessionClient()
  const stats = ref<AdminVideoSessionStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const captureRatio = computed(() => {
    const value = stats.value
    if (!value || value.deviceCount === 0) {
      return 0
    }
    return Math.round((value.captureAllowedCount / value.deviceCount) * 100)
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
    captureRatio,
    load,
  }
})
