import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminPowerClient, type AdminPowerStats } from '@/api/adminPower'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator battery statistics view. Only aggregate counts are
/// stored; raw gauge telemetry and per-device detail are never fetched here.
export const usePowerStatsStore = defineStore('admin-power-stats', () => {
  const client = createAdminPowerClient()
  const stats = ref<AdminPowerStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const hasData = computed(() => stats.value !== null)
  const lowBatteryRatio = computed(() => {
    const value = stats.value
    if (!value || value.deviceCount === 0) {
      return 0
    }
    return Math.round((value.lowBatteryCount / value.deviceCount) * 100)
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
    lowBatteryRatio,
    load,
  }
})
