import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import {
  createAdminLearningVisualizationClient,
  type AdminLearningVisualizationStats,
} from '@/api/adminLearningVisualization'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator learning-visualization statistics view. Only
/// aggregate counts are stored; frame buffers and touch traces never leave the
/// platform here.
export const useLearningVisualizationStatsStore = defineStore(
  'admin-learning-visualization-stats',
  () => {
    const client = createAdminLearningVisualizationClient()
    const stats = ref<AdminLearningVisualizationStats | null>(null)
    const windowDays = ref(7)
    const isLoading = ref(false)
    const error = ref<ApiError | null>(null)

    const runningRatio = computed(() => {
      const value = stats.value
      if (!value || value.deviceCount === 0) {
        return 0
      }
      const running = value.stateBuckets
        .filter((bucket) => bucket.label === 'lesson' || bucket.label === 'feedback')
        .reduce((total, bucket) => total + bucket.count, 0)
      return Math.round((running / value.deviceCount) * 100)
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
      runningRatio,
      load,
    }
  },
)
