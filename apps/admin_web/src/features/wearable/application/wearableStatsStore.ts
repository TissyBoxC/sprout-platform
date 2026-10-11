import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminWearableClient, type AdminWearableStats } from '@/api/adminWearable'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator wearable-location statistics view. Only aggregate
/// counts are stored; positions and contact identity are never fetched here.
export const useWearableStatsStore = defineStore('admin-wearable-stats', () => {
  const client = createAdminWearableClient()
  const stats = ref<AdminWearableStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const hasData = computed(() => stats.value !== null)
  const geofenceOutsideRatio = computed(() => {
    const value = stats.value
    if (!value || value.deviceCount === 0) {
      return 0
    }
    return Math.round((value.geofenceOutsideCount / value.deviceCount) * 100)
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
    geofenceOutsideRatio,
    load,
  }
})
