import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createAdminNotificationsClient,
  type AdminNotification,
  type AdminNotificationFilters,
  type AdminNotificationStats,
  type CreateAdminNotificationInput,
  type NotificationAudience,
  type NotificationCategory,
  type NotificationSeverity,
  type NotificationSource,
} from '@/api/adminNotifications'
import { createHttpClient } from '@/api/httpClient'
import {
  parseParentAccountOptions,
  type ParentAccountOption,
} from '@/features/child/domain/childProfile'

export interface NotificationFormState {
  category: NotificationCategory
  severity: NotificationSeverity
  title: string
  body: string
  actionPath: string
  actionLabel: string
  audience: NotificationAudience
  channels: string[]
  parentAccountId: string
  deviceId: string
  displayDurationSeconds: number
  expiresAt: string
}

/// Drives the brand console notification center: authoring platform notices,
/// reviewing delivery counters, and removing withdrawn copy.
export const useNotificationCenterStore = defineStore('admin-notifications', () => {
  const httpClient = createHttpClient()
  const client = createAdminNotificationsClient(httpClient)
  const notifications = ref<AdminNotification[]>([])
  const stats = ref<AdminNotificationStats | null>(null)
  const families = ref<ParentAccountOption[]>([])
  const filters = ref<AdminNotificationFilters>({
    category: 'all',
    audience: 'all',
    source: 'all',
    query: '',
    page: 1,
    pageSize: 20,
  })
  const total = ref(0)
  const totalPages = ref(1)
  const isLoading = ref(false)
  const isLoadingFamilies = ref(false)
  const isSubmitting = ref(false)
  const deletingId = ref('')
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')

  const hasNotifications = computed(() => notifications.value.length > 0)
  const canGoPrevious = computed(() => filters.value.page > 1)
  const canGoNext = computed(() => filters.value.page < totalPages.value)

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    await Promise.all([loadPage(), loadStats()])
    isLoading.value = false
  }

  async function loadPage(): Promise<void> {
    error.value = null
    try {
      const result = await client.loadNotifications(filters.value)
      notifications.value = result.items
      total.value = result.total
      totalPages.value = result.totalPages
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      notifications.value = []
      total.value = 0
      totalPages.value = 1
    }
  }

  async function loadStats(): Promise<void> {
    try {
      stats.value = await client.loadStats()
    } catch {
      stats.value = null
    }
  }

  async function loadFamilies(): Promise<void> {
    if (families.value.length > 0 || isLoadingFamilies.value) {
      return
    }
    isLoadingFamilies.value = true
    try {
      const response = await httpClient.get('/api/v1/admin/families')
      families.value = parseParentAccountOptions(response.data?.data)
    } catch {
      families.value = []
    } finally {
      isLoadingFamilies.value = false
    }
  }

  async function setFilters(patch: Partial<AdminNotificationFilters>): Promise<void> {
    filters.value = { ...filters.value, ...patch, page: patch.page ?? 1 }
    await loadPage()
  }

  async function goToPage(page: number): Promise<void> {
    const bounded = Math.min(Math.max(1, page), Math.max(1, totalPages.value))
    filters.value = { ...filters.value, page: bounded }
    await loadPage()
  }

  async function create(input: CreateAdminNotificationInput): Promise<boolean> {
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    try {
      await client.createNotification(input)
      lastMessage.value = '通知已发布，家长会收到这条消息。'
      await load()
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isSubmitting.value = false
    }
  }

  async function remove(notificationId: string): Promise<boolean> {
    deletingId.value = notificationId
    error.value = null
    lastMessage.value = ''
    try {
      await client.deleteNotification(notificationId)
      lastMessage.value = '通知已删除。'
      await load()
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      deletingId.value = ''
    }
  }

  return {
    canGoNext,
    canGoPrevious,
    create,
    deletingId,
    error,
    families,
    filters,
    goToPage,
    hasNotifications,
    isLoading,
    isLoadingFamilies,
    isSubmitting,
    lastMessage,
    load,
    loadFamilies,
    loadPage,
    notifications,
    remove,
    setFilters,
    stats,
    total,
    totalPages,
  }
})
