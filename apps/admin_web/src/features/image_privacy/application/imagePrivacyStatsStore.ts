import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import {
  createAdminImagePrivacyClient,
  type AdminImagePrivacyStats,
} from '@/api/adminImagePrivacy'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator image-privacy statistics view. Only aggregate counts
/// are stored; image bytes, thumbnails and face templates never leave the
/// platform here.
export const useImagePrivacyStatsStore = defineStore('admin-image-privacy-stats', () => {
  const client = createAdminImagePrivacyClient()
  const stats = ref<AdminImagePrivacyStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)
  let requestSequence = 0

  const uploadRatio = computed(() => {
    const value = stats.value
    if (!value || value.processedTotal === 0) {
      return 0
    }
    return Math.round((value.uploadedTotal / value.processedTotal) * 100)
  })

  const blockedRatio = computed(() => {
    const value = stats.value
    if (!value || value.processedTotal === 0) {
      return 0
    }
    return Math.round((value.blockedTotal / value.processedTotal) * 100)
  })

  async function load(days = windowDays.value): Promise<void> {
    const sequence = ++requestSequence
    windowDays.value = days
    isLoading.value = true
    error.value = null
    try {
      const nextStats = await client.loadStats(days)
      if (sequence !== requestSequence) {
        return
      }
      stats.value = nextStats
    } catch (caught: unknown) {
      if (sequence !== requestSequence) {
        return
      }
      error.value = mapApiError(caught)
    } finally {
      if (sequence === requestSequence) {
        isLoading.value = false
      }
    }
  }

  return {
    stats,
    windowDays,
    isLoading,
    error,
    uploadRatio,
    blockedRatio,
    load,
  }
})
