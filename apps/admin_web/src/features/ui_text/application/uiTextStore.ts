import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createAdminUiTextClient,
  type AdminUiTextClient,
  type UiTextDetail,
  type UiTextDraftInput,
  type UiTextPlatform,
  type UiTextStatus,
  type UiTextVersionRef,
  type UiTextVersionSummary,
} from '@/api/adminUiText'

export const UI_TEXT_PAGE_SIZE = 20

export type UiTextStatusFilter = 'all' | UiTextStatus
export type UiTextPlatformFilter = 'all' | UiTextPlatform

export interface UiTextFilters {
  keyword: string
  language: string
  platform: UiTextPlatformFilter
  status: UiTextStatusFilter
}

export interface UiTextActionResult {
  succeeded: boolean
  error: ApiError | null
}

/// Owns the remote UI text list, filters, and the selected version detail. The
/// page treats this store as the single source of truth so the list and detail
/// never drift apart after a lifecycle decision.
export const useUiTextStore = defineStore('admin-ui-text', () => {
  const client: AdminUiTextClient = createAdminUiTextClient()

  const versions = ref<UiTextVersionSummary[]>([])
  const filters = ref<UiTextFilters>({
    keyword: '',
    language: '',
    platform: 'all',
    status: 'all',
  })
  const page = ref(1)
  const total = ref(0)
  const isLoading = ref(false)
  const isSubmitting = ref(false)
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')

  const selectedRef = ref<UiTextVersionRef | null>(null)
  const selectedDetail = ref<UiTextDetail | null>(null)
  const isDetailLoading = ref(false)

  const pageCount = computed(() =>
    total.value <= 0 ? 1 : Math.ceil(total.value / UI_TEXT_PAGE_SIZE),
  )

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      const result = await client.loadVersions({
        // The backend filters by exact package id, so the keyword is treated as
        // a package-id lookup rather than a fuzzy search.
        packageId: filters.value.keyword.trim() || undefined,
        language: filters.value.language.trim() || undefined,
        platform: filters.value.platform === 'all' ? undefined : filters.value.platform,
        status: filters.value.status === 'all' ? undefined : filters.value.status,
        page: page.value,
        pageSize: UI_TEXT_PAGE_SIZE,
      })
      versions.value = result.versions
      total.value = result.total
      if (page.value > pageCount.value) {
        page.value = pageCount.value
        await load()
      }
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoading.value = false
    }
  }

  async function openVersion(ref: UiTextVersionRef): Promise<void> {
    selectedRef.value = ref
    selectedDetail.value = null
    isDetailLoading.value = true
    error.value = null
    try {
      selectedDetail.value = await client.loadVersion(ref)
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isDetailLoading.value = false
    }
  }

  function closeVersion(): void {
    selectedRef.value = null
    selectedDetail.value = null
    isDetailLoading.value = false
  }

  async function createDraft(input: UiTextDraftInput): Promise<UiTextActionResult> {
    const result = await runMutation(async () => {
      const created = await client.createDraft(input)
      await load()
      await openVersion(refFromDetail(created))
    })
    if (result.succeeded) {
      lastMessage.value = '文案草稿已创建。'
    }
    return result
  }

  async function updateDraft(
    ref: UiTextVersionRef,
    input: UiTextDraftInput,
  ): Promise<UiTextActionResult> {
    const result = await runMutation(async () => {
      await client.updateDraft(ref, input)
      await load()
      await openVersion(ref)
    })
    if (result.succeeded) {
      lastMessage.value = '文案草稿已保存。'
    }
    return result
  }

  async function submitVersion(ref: UiTextVersionRef): Promise<UiTextActionResult> {
    return runVersionAction(() => client.submitVersion(ref), ref, '已提交审核。')
  }

  async function approveVersion(ref: UiTextVersionRef): Promise<UiTextActionResult> {
    return runVersionAction(() => client.approveVersion(ref), ref, '审核已通过。')
  }

  async function rejectVersion(ref: UiTextVersionRef, reason: string): Promise<UiTextActionResult> {
    return runVersionAction(() => client.rejectVersion(ref, reason), ref, '已驳回该版本。')
  }

  async function publishVersion(
    ref: UiTextVersionRef,
    signatureKeyId: string,
    signature: string,
  ): Promise<UiTextActionResult> {
    return runVersionAction(
      () => client.publishVersion(ref, signatureKeyId, signature),
      ref,
      '文案已发布。',
    )
  }

  async function withdrawVersion(ref: UiTextVersionRef): Promise<UiTextActionResult> {
    return runVersionAction(() => client.withdrawVersion(ref), ref, '文案已撤回。')
  }

  function setFilters(nextFilters: Partial<UiTextFilters>): void {
    filters.value = { ...filters.value, ...nextFilters }
    page.value = 1
  }

  function resetFilters(): void {
    filters.value = { keyword: '', language: '', platform: 'all', status: 'all' }
    page.value = 1
  }

  function setPage(nextPage: number): void {
    page.value = Math.min(Math.max(1, Math.trunc(nextPage)), pageCount.value)
  }

  function clearMessages(): void {
    error.value = null
    lastMessage.value = ''
  }

  async function runVersionAction(
    action: () => Promise<unknown>,
    ref: UiTextVersionRef,
    successMessage: string,
  ): Promise<UiTextActionResult> {
    const result = await runMutation(async () => {
      await action()
      await load()
      await openVersion(ref)
    })
    if (result.succeeded) {
      lastMessage.value = successMessage
    }
    return result
  }

  async function runMutation(operation: () => Promise<void>): Promise<UiTextActionResult> {
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    try {
      await operation()
      return { succeeded: true, error: null }
    } catch (caught: unknown) {
      const mappedError = mapApiError(caught)
      error.value = mappedError
      return { succeeded: false, error: mappedError }
    } finally {
      isSubmitting.value = false
    }
  }

  return {
    clearMessages,
    closeVersion,
    createDraft,
    error,
    filters,
    isLoading,
    isDetailLoading,
    isSubmitting,
    lastMessage,
    load,
    openVersion,
    page,
    pageCount,
    resetFilters,
    setFilters,
    setPage,
    selectedDetail,
    selectedRef,
    total,
    updateDraft,
    approveVersion,
    publishVersion,
    rejectVersion,
    submitVersion,
    versions,
    withdrawVersion,
  }
})

export function refFromDetail(detail: UiTextDetail): UiTextVersionRef {
  return {
    packageId: detail.package.packageId,
    language: detail.package.language,
    platform: detail.package.platform,
    packageVersion: detail.package.packageVersion,
  }
}

export function uiTextStatusLabel(value: UiTextStatus): string {
  return (
    {
      draft: '草稿',
      in_review: '待审核',
      published: '已发布',
      withdrawn: '已撤回',
    }[value] ?? value
  )
}

export function uiTextPlatformLabel(value: UiTextPlatform): string {
  return (
    {
      parent_app: '家长端',
      admin_web: '管理后台',
      device_firmware: '设备固件',
    }[value] ?? value
  )
}

export function uiTextLanguageLabel(value: string): string {
  return (
    {
      'zh-CN': '简体中文',
      'en-US': '英语（美国）',
    }[value] ?? value
  )
}
