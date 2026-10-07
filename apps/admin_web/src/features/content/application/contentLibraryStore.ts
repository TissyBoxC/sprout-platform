import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import {
  createAdminContentClient,
  type AdminContentClient,
  type ContentAgeTier,
  type ContentCategory,
  type ContentDraftInput,
  type ContentPackage,
  type ContentPackageCreateInput,
  type ContentStatus,
  type ContentVersion,
} from '@/api/adminContent'
import {
  createAdminDownloadFilesClient,
  type DownloadFile,
} from '@/api/adminDownloadFiles'
import { mapApiError, type ApiError } from '@/api/apiError'
import { createHttpClient } from '@/api/httpClient'

export type ContentCategoryFilter = 'all' | ContentCategory
export type ContentStatusFilter = 'all' | ContentStatus
export type ContentAgeTierFilter = 'all' | ContentAgeTier

export interface ContentLibraryFilters {
  category: ContentCategoryFilter
  ageTier: ContentAgeTierFilter
  status: ContentStatusFilter
  keyword: string
}

export interface ContentStatusResult {
  succeeded: boolean
  error: ApiError | null
}

export const CONTENT_PAGE_SIZE = 20

/// Owns the content library list, filters, pagination, and the selected package
/// detail. The page treats this store as the single source of truth so list and
/// detail never drift apart after a status change.
export const useContentLibraryStore = defineStore('admin-content-library', () => {
  const client: AdminContentClient = createAdminContentClient()
  const downloadFilesClient = createAdminDownloadFilesClient(createHttpClient())

  const packages = ref<ContentPackage[]>([])
  const filters = ref<ContentLibraryFilters>({
    category: 'all',
    ageTier: 'all',
    status: 'all',
    keyword: '',
  })
  const page = ref(1)
  const total = ref(0)
  const isLoading = ref(false)
  const isSubmitting = ref(false)
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')

  const selectedPackageId = ref('')
  const selectedPackage = ref<ContentPackage | null>(null)
  const isDetailLoading = ref(false)

  const downloadFiles = ref<DownloadFile[]>([])
  const isLoadingDownloadFiles = ref(false)
  const downloadFilesError = ref<ApiError | null>(null)

  const pageCount = computed(() =>
    total.value <= 0 ? 1 : Math.ceil(total.value / CONTENT_PAGE_SIZE),
  )

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      const result = await client.loadPackages({
        category: filters.value.category === 'all' ? undefined : filters.value.category,
        ageTier: filters.value.ageTier === 'all' ? undefined : filters.value.ageTier,
        status: filters.value.status === 'all' ? undefined : filters.value.status,
        keyword: filters.value.keyword,
        page: page.value,
        pageSize: CONTENT_PAGE_SIZE,
      })
      packages.value = result.packages
      total.value = result.total
      if (page.value > pageCount.value) {
        page.value = pageCount.value
        await load()
        return
      }
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoading.value = false
    }
  }

  async function openPackage(packageId: string): Promise<void> {
    selectedPackageId.value = packageId
    selectedPackage.value =
      packages.value.find((item) => item.packageId === packageId) ?? null
    isDetailLoading.value = true
    error.value = null
    try {
      selectedPackage.value = await client.loadPackage(packageId)
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isDetailLoading.value = false
    }
  }

  function closePackage(): void {
    selectedPackageId.value = ''
    selectedPackage.value = null
    isDetailLoading.value = false
  }

  async function createPackage(input: ContentPackageCreateInput): Promise<ContentStatusResult> {
    const result = await runMutation(async () => {
      const created = await client.createPackage(input)
      await load()
      if (created !== null) {
        await openPackage(created.packageId)
      }
    })
    if (result.succeeded) {
      lastMessage.value = '内容草稿已创建。'
    }
    return result
  }

  async function updateDraft(
    packageId: string,
    packageVersion: number,
    input: ContentDraftInput,
  ): Promise<ContentStatusResult> {
    const result = await runMutation(async () => {
      const updated = await client.updateVersion(packageId, packageVersion, input)
      await applyUpdatedPackage(packageId, updated)
    })
    if (result.succeeded) {
      lastMessage.value = '内容草稿已保存。'
    }
    return result
  }

  async function submitVersion(packageId: string, packageVersion: number): Promise<ContentStatusResult> {
    return runVersionAction(() => client.submitVersion(packageId, packageVersion), packageId, '已提交审核。')
  }

  async function approveVersion(packageId: string, packageVersion: number): Promise<ContentStatusResult> {
    return runVersionAction(() => client.approveVersion(packageId, packageVersion), packageId, '审核已通过。')
  }

  async function rejectVersion(
    packageId: string,
    packageVersion: number,
    reason: string,
  ): Promise<ContentStatusResult> {
    return runVersionAction(
      () => client.rejectVersion(packageId, packageVersion, reason),
      packageId,
      '已驳回该版本。',
    )
  }

  async function publishVersion(packageId: string, packageVersion: number): Promise<ContentStatusResult> {
    return runVersionAction(() => client.publishVersion(packageId, packageVersion), packageId, '内容已发布。')
  }

  async function withdrawVersion(packageId: string, packageVersion: number): Promise<ContentStatusResult> {
    return runVersionAction(() => client.withdrawVersion(packageId, packageVersion), packageId, '内容已撤回。')
  }

  async function archiveVersion(packageId: string, packageVersion: number): Promise<ContentStatusResult> {
    return runVersionAction(() => client.archiveVersion(packageId, packageVersion), packageId, '内容已归档。')
  }

  async function loadDownloadFiles(): Promise<void> {
    isLoadingDownloadFiles.value = true
    downloadFilesError.value = null
    try {
      downloadFiles.value = await downloadFilesClient.loadFiles()
    } catch (caught: unknown) {
      downloadFilesError.value = mapApiError(caught)
    } finally {
      isLoadingDownloadFiles.value = false
    }
  }

  function setFilters(nextFilters: Partial<ContentLibraryFilters>): void {
    filters.value = { ...filters.value, ...nextFilters }
    page.value = 1
  }

  function resetFilters(): void {
    filters.value = { category: 'all', ageTier: 'all', status: 'all', keyword: '' }
    page.value = 1
  }

  function setPage(nextPage: number): void {
    page.value = Math.min(Math.max(1, Math.trunc(nextPage)), pageCount.value)
  }

  function clearMessages(): void {
    error.value = null
    lastMessage.value = ''
  }

  /// A content version points at a download file; the address is resolved from
  /// the download inventory so operators see the real link without retyping it.
  function downloadUrlForVersion(version: ContentVersion): string {
    const matched = downloadFileForVersion(version)
    return matched?.downloadUrl ?? ''
  }

  function downloadFileForVersion(version: ContentVersion): DownloadFile | null {
    const byKey = downloadFiles.value.find(
      (file) => file.relativePath === version.assetKey,
    )
    if (byKey !== undefined) {
      return byKey
    }
    if (!version.sha256) {
      return null
    }
    return (
      downloadFiles.value.find((file) => file.sha256 === version.sha256) ?? null
    )
  }

  /// A mutation returns only the changed version, so the drawer refreshes from
  /// the canonical detail endpoint rather than merging partial state by hand.
  async function applyUpdatedPackage(
    packageId: string,
    updated: ContentPackage | null,
  ): Promise<void> {
    const detail = await client.loadPackage(packageId)
    selectedPackage.value =
      updated === null || updated.history.length === 0
        ? detail
        : { ...detail, history: updated.history }
    await load()
  }

  async function runVersionAction(
    action: () => Promise<ContentPackage | null>,
    packageId: string,
    successMessage: string,
  ): Promise<ContentStatusResult> {
    const result = await runMutation(async () => {
      const updated = await action()
      await applyUpdatedPackage(packageId, updated)
    })
    if (result.succeeded) {
      lastMessage.value = successMessage
    }
    return result
  }

  async function runMutation(operation: () => Promise<void>): Promise<ContentStatusResult> {
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
    closePackage,
    createPackage,
    downloadFileForVersion,
    downloadFiles,
    downloadFilesError,
    downloadUrlForVersion,
    error,
    filters,
    isLoading,
    isLoadingDownloadFiles,
    isDetailLoading,
    isSubmitting,
    lastMessage,
    load,
    loadDownloadFiles,
    openPackage,
    packages,
    page,
    pageCount,
    resetFilters,
    setFilters,
    setPage,
    selectedPackage,
    selectedPackageId,
    total,
    updateDraft,
    approveVersion,
    archiveVersion,
    publishVersion,
    rejectVersion,
    submitVersion,
    withdrawVersion,
  }
})

export function latestVersion(contentPackage: ContentPackage): ContentVersion | null {
  if (contentPackage.versions.length === 0) {
    return null
  }
  return (
    [...contentPackage.versions].sort((left, right) =>
      right.packageVersion - left.packageVersion,
    )[0] ?? null
  )
}

/// A package has no top-level title or category; the table and drawer header
/// read them from the newest version so they stay correct after a version edit.
export function packageTitle(contentPackage: ContentPackage): string {
  return latestVersion(contentPackage)?.title ?? ''
}

export function packageCategory(contentPackage: ContentPackage): ContentCategory {
  return latestVersion(contentPackage)?.category ?? 'story'
}

export function packageAgeTiers(contentPackage: ContentPackage): ContentAgeTier[] {
  return latestVersion(contentPackage)?.ageTiers ?? []
}

export function contentCategoryLabel(value: ContentCategory): string {
  return (
    {
      story: '故事',
      nursery_rhyme: '儿歌',
      poetry: '古诗',
      english: '英语',
      encyclopedia: '百科',
      bedtime: '睡前',
    }[value] ?? value
  )
}

export function contentAgeTierLabel(value: ContentAgeTier): string {
  return (
    {
      age_3_4: '3-4 岁',
      age_5_6: '5-6 岁',
      age_7_8: '7-8 岁',
    }[value] ?? value
  )
}

export function contentStatusLabel(value: ContentStatus): string {
  return (
    {
      draft: '草稿',
      in_review: '待审核',
      published: '已发布',
      withdrawn: '已撤回',
      archived: '已归档',
    }[value] ?? value
  )
}

export function contentReviewActionLabel(action: string): string {
  return (
    {
      create: '创建草稿',
      update: '更新草稿',
      submit: '提交审核',
      approve: '审核通过',
      reject: '审核驳回',
      publish: '发布',
      withdraw: '撤回',
      archive: '归档',
    }[action] ?? action
  )
}

export function isPackageIdValid(packageId: string, category: ContentCategory): boolean {
  const pattern = new RegExp(`^content_${category}_\\d{3}$`)
  return pattern.test(packageId.trim())
}

export function isSha256Valid(value: string): boolean {
  return /^[0-9a-f]{64}$/.test(value.trim())
}

export const MAX_CONTENT_SIZE_BYTES = 4 * 1024 * 1024 * 1024
