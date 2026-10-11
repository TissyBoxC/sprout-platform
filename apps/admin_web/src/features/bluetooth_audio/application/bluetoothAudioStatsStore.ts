import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { createAdminBluetoothAudioClient, type AdminBluetoothAudioStats } from '@/api/adminBluetoothAudio'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator Bluetooth audio statistics view. Only aggregate counts
/// are stored; pairing keys and audio never leave the platform here.
export const useBluetoothAudioStatsStore = defineStore('admin-bluetooth-audio-stats', () => {
  const client = createAdminBluetoothAudioClient()
  const stats = ref<AdminBluetoothAudioStats | null>(null)
  const windowDays = ref(7)
  const isLoading = ref(false)
  const error = ref<ApiError | null>(null)

  const connectedRatio = computed(() => {
    const value = stats.value
    if (!value || value.deviceCount === 0) {
      return 0
    }
    return Math.round((value.connectedCount / value.deviceCount) * 100)
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
    connectedRatio,
    load,
  }
})
