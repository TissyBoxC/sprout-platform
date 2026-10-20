import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { apiNoticeWarning } from '@/api/apiNotice'
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
  parentAccountPageHasMore,
  type ParentAccountOption,
} from '@/features/child/domain/childProfile'
import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'

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
  const authStore = useAuthStore()
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
  const hasLoadedPage = ref(false)
  const isCurrentPageOutOfRange = ref(false)
  const isLoadingFamilies = ref(false)
  const isSubmitting = ref(false)
  const deletingId = ref('')
  const error = ref<ApiError | null>(null)
  const statsError = ref<ApiError | null>(null)
  const familiesError = ref<ApiError | null>(null)
  const lastMessage = ref('')
  const lastWarning = ref('')
  let pageRequestGeneration = 0
  let activePageRequests = 0
  let statsRequestGeneration = 0
  let familiesRequestGeneration = 0

  const hasNotifications = computed(() => notifications.value.length > 0)
  const canManageNotifications = computed(() =>
    authStore.can(ADMIN_PERMISSIONS.notificationsManage),
  )
  const canReadFamilies = computed(() =>
    authStore.can(ADMIN_PERMISSIONS.familyRead),
  )
  const canGoPrevious = computed(() => filters.value.page > 1)
  const canGoNext = computed(() => filters.value.page < totalPages.value)
  const hasActiveFilters = computed(
    () =>
      filters.value.category !== 'all' ||
      filters.value.audience !== 'all' ||
      filters.value.source !== 'all' ||
      filters.value.query.trim() !== '',
  )

  async function load(): Promise<void> {
    await Promise.all([loadPage(), loadStats()])
  }

  async function loadPage(): Promise<void> {
    const requestGeneration = ++pageRequestGeneration
    const requestFilters = { ...filters.value }
    activePageRequests += 1
    isLoading.value = true
    error.value = null
    try {
      const result = await client.loadNotifications(requestFilters)
      if (requestGeneration !== pageRequestGeneration) {
        return
      }
      notifications.value = result.items
      total.value = result.total
      totalPages.value = result.totalPages
      isCurrentPageOutOfRange.value =
        result.items.length === 0 &&
        result.total > 0 &&
        requestFilters.page > Math.max(1, result.totalPages)
      if (
        result.items.length === 0 &&
        result.total > 0 &&
        requestFilters.page > result.totalPages
      ) {
        const lastPage = Math.max(1, result.totalPages)
        filters.value = { ...filters.value, page: lastPage }
        await loadPage()
        return
      }
      hasLoadedPage.value = true
    } catch (caught: unknown) {
      if (requestGeneration === pageRequestGeneration) {
        error.value = mapApiError(caught)
        notifications.value = []
        total.value = 0
        totalPages.value = 1
        isCurrentPageOutOfRange.value = false
        hasLoadedPage.value = true
      }
    } finally {
      // Only the newest request owns the visible loading state. An older
      // request may finish after a fast filter change, but it must not keep the
      // page locked when the newest request has already completed.
      activePageRequests = Math.max(0, activePageRequests - 1)
      if (activePageRequests === 0) {
        isLoading.value = false
      }
    }
  }

  async function loadStats(): Promise<void> {
    const requestGeneration = ++statsRequestGeneration
    statsError.value = null
    try {
      const nextStats = await client.loadStats()
      if (requestGeneration === statsRequestGeneration) {
        stats.value = nextStats
        statsError.value = null
      }
    } catch (caught: unknown) {
      if (requestGeneration === statsRequestGeneration) {
        statsError.value = mapApiError(caught)
      }
    }
  }

  async function loadFamilies(): Promise<void> {
    if (!canReadFamilies.value) {
      familiesRequestGeneration += 1
      families.value = []
      familiesError.value = null
      isLoadingFamilies.value = false
      return
    }
    if (families.value.length > 0 || isLoadingFamilies.value) {
      return
    }
    const requestGeneration = ++familiesRequestGeneration
    isLoadingFamilies.value = true
    familiesError.value = null
    try {
      const options: ParentAccountOption[] = []
      for (let page = 1; ; page += 1) {
        const response = await httpClient.get('/api/v1/admin/families', {
          params: { page, page_size: 100 },
        })
        const payload = response.data?.data
        options.push(...parseParentAccountOptions(payload))
        if (!parentAccountPageHasMore(payload)) {
          break
        }
      }
      if (requestGeneration === familiesRequestGeneration) {
        families.value = options
      }
    } catch (caught: unknown) {
      if (requestGeneration === familiesRequestGeneration) {
        families.value = []
        familiesError.value = mapApiError(caught)
      }
    } finally {
      if (requestGeneration === familiesRequestGeneration) {
        isLoadingFamilies.value = false
      }
    }
  }

  async function setFilters(patch: Partial<AdminNotificationFilters>): Promise<void> {
    filters.value = { ...filters.value, ...patch, page: patch.page ?? 1 }
    await loadPage()
  }

  async function goToPage(page: number): Promise<void> {
    if (isLoading.value) {
      return
    }
    const bounded = Math.min(Math.max(1, Math.trunc(page)), Math.max(1, totalPages.value))
    if (bounded === filters.value.page) {
      return
    }
    filters.value = { ...filters.value, page: bounded }
    await loadPage()
  }

  async function create(input: CreateAdminNotificationInput): Promise<boolean> {
    if (!canManageNotifications.value) {
      error.value = {
        kind: 'insufficient_permission',
        message: '你没有权限发布通知',
        retryable: false,
      }
      return false
    }
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const result = await client.createNotification(input)
      lastMessage.value = '通知已发布，家长会收到这条消息。'
      lastWarning.value = apiNoticeWarning(result.notice)
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
    if (!canManageNotifications.value) {
      error.value = {
        kind: 'insufficient_permission',
        message: '你没有权限删除通知',
        retryable: false,
      }
      return false
    }
    deletingId.value = notificationId
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const result = await client.deleteNotification(notificationId)
      lastMessage.value = '通知已删除。'
      lastWarning.value = apiNoticeWarning(result.notice)
      await loadAfterDelete()
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      deletingId.value = ''
    }
  }

  async function loadAfterDelete(): Promise<void> {
    const nextPage =
      notifications.value.length === 1 && filters.value.page > 1
        ? filters.value.page - 1
        : filters.value.page
    if (nextPage !== filters.value.page) {
      filters.value = { ...filters.value, page: nextPage }
    }
    await Promise.all([loadPage(), loadStats()])
  }

  return {
    canGoNext,
    canGoPrevious,
    canManageNotifications,
    canReadFamilies,
    create,
    deletingId,
    error,
    families,
    familiesError,
    filters,
    goToPage,
    hasLoadedPage,
    hasActiveFilters,
    hasNotifications,
    isCurrentPageOutOfRange,
    isLoading,
    isLoadingFamilies,
    isSubmitting,
    lastMessage,
    lastWarning,
    load,
    loadFamilies,
    loadPage,
    loadStats,
    notifications,
    remove,
    setFilters,
    stats,
    statsError,
    total,
    totalPages,
  }
})
