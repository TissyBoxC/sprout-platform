import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminScreenClient, type AdminScreenStats } from '@/api/adminScreen'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator screen statistics view. Only aggregate counts are
/// stored; expression names, dictionary text and pixels are never fetched here.
export const useScreenStatsStore = defineStore('admin-screen-stats', () => {
  const client = createAdminScreenClient()
  const stats = ref<AdminScreenStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const hasData = computed(() => stats.value !== null)
  const videoRatio = computed(() => {
    const value = stats.value
    if (!value || value.deviceCount === 0) {
      return 0
    }
    return Math.round((value.videoDeviceCount / value.deviceCount) * 100)
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
    videoRatio,
    load,
  }
})
