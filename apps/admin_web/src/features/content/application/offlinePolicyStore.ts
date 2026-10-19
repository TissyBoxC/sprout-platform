import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { apiNoticeWarning } from '@/api/apiNotice'
import {
  createAdminContentClient,
  type AdminContentClient,
  type OfflinePolicy,
  type OfflinePolicyAcknowledgement,
  type OfflinePolicyInput,
} from '@/api/adminContent'
import { mapApiError, type ApiError } from '@/api/apiError'

export interface OfflinePolicyStatusResult {
  succeeded: boolean
  error: ApiError | null
}

/// Bounds mirrored from the platform domain so the form never submits a value
/// the backend will reject. The device keeps one fleet-wide policy, so this
/// store is the single source of truth for the console.
export const OFFLINE_POLICY_LIMITS = {
  minCachedPackages: 1,
  maxCachedPackages: 200,
  minCacheMegabytes: 16,
  maxCacheMegabytes: 8 * 1024,
  minCacheTtlHours: 1,
  maxCacheTtlHours: 24 * 365,
} as const

export const BYTES_PER_MEGABYTE = 1024 * 1024

export const useOfflinePolicyStore = defineStore('admin-content-offline-policy', () => {
  const client: AdminContentClient = createAdminContentClient()

  const policy = ref<OfflinePolicy | null>(null)
  const acknowledgements = ref<OfflinePolicyAcknowledgement[]>([])
  const isLoading = ref(false)
  const isSubmitting = ref(false)
  const isLoadingAcknowledgements = ref(false)
  const error = ref<ApiError | null>(null)
  const acknowledgementsError = ref<ApiError | null>(null)
  const lastMessage = ref('')
  const lastWarning = ref('')

  const cacheMegabytes = computed(() =>
    policy.value === null ? 0 : Math.round(policy.value.maxCacheBytes / BYTES_PER_MEGABYTE),
  )
  const behindCount = computed(
    () => acknowledgements.value.filter((entry) => !entry.upToDate).length,
  )

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      policy.value = await client.loadOfflinePolicy()
    } catch (loadError) {
      error.value = mapApiError(loadError)
    } finally {
      isLoading.value = false
    }
  }

  async function save(input: OfflinePolicyInput): Promise<OfflinePolicyStatusResult> {
    isSubmitting.value = true
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const result = await client.saveOfflinePolicy(input)
      const saved = result.value
      policy.value = saved
      lastWarning.value = apiNoticeWarning(result.notice)
      lastMessage.value = `离线下载策略已更新到版本 ${saved.revision}`
      return { succeeded: true, error: null }
    } catch (saveError) {
      return { succeeded: false, error: mapApiError(saveError) }
    } finally {
      isSubmitting.value = false
    }
  }

  async function loadAcknowledgements(familyId: string): Promise<void> {
    const trimmed = familyId.trim()
    if (trimmed === '') {
      acknowledgements.value = []
      acknowledgementsError.value = null
      return
    }
    isLoadingAcknowledgements.value = true
    acknowledgementsError.value = null
    try {
      acknowledgements.value = await client.loadOfflinePolicyAcknowledgements(trimmed)
    } catch (loadError) {
      acknowledgements.value = []
      acknowledgementsError.value = mapApiError(loadError)
    } finally {
      isLoadingAcknowledgements.value = false
    }
  }

  return {
    policy,
    acknowledgements,
    isLoading,
    isSubmitting,
    isLoadingAcknowledgements,
    error,
    acknowledgementsError,
    lastMessage,
    lastWarning,
    cacheMegabytes,
    behindCount,
    load,
    save,
    loadAcknowledgements,
  }
})
