import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminVisionClient, type AdminVisionStats } from '@/api/adminVision'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator vision statistics view. Only aggregate counts are
/// stored; individual tasks, labels, and summaries are never fetched here.
export const useVisionStatsStore = defineStore('admin-vision-stats', () => {
  const client = createAdminVisionClient()
  const stats = ref<AdminVisionStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const hasData = computed(() => stats.value !== null)
  const failureRate = computed(() => {
    const value = stats.value
    if (!value || value.taskCount === 0) {
      return 0
    }
    return Math.round((value.failedCount / value.taskCount) * 100)
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
    failureRate,
    load,
  }
})
