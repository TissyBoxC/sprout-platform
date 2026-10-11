import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminVideoClient, type AdminVideoStats } from '@/api/adminVideoCompanion'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator video-companionship statistics view. Only aggregate
/// call counts are stored; media and contact identity are never fetched here.
export const useVideoStatsStore = defineStore('admin-video-stats', () => {
  const client = createAdminVideoClient()
  const stats = ref<AdminVideoStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const hasData = computed(() => stats.value !== null)
  const completedRatio = computed(() => {
    const value = stats.value
    if (!value || value.callCount === 0) {
      return 0
    }
    return Math.round((value.endedCount / value.callCount) * 100)
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
    completedRatio,
    load,
  }
})
