import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminDeviceMeshClient, type AdminDeviceMeshStats } from '@/api/adminDeviceMesh'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator multi-device statistics view. Only aggregate counts
/// are stored; membership, messages and per-device content state are never
/// fetched here.
export const useMultiDeviceStatsStore = defineStore('admin-multi-device-stats', () => {
  const client = createAdminDeviceMeshClient()
  const stats = ref<AdminDeviceMeshStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const hasData = computed(() => stats.value !== null)
  const conflictRatio = computed(() => {
    const value = stats.value
    if (!value || value.syncedContentCount === 0) {
      return 0
    }
    return Math.round((value.conflictCount / value.syncedContentCount) * 100)
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
    conflictRatio,
    load,
  }
})
