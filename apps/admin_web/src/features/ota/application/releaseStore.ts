import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createAdminOperationsClient,
  type ReleaseArtifact,
} from '@/api/adminOperations'
import { createHttpClient } from '@/api/httpClient'
import { getRuntimeConfig } from '@/config/runtimeConfig'

export type ReleaseKind = 'resource' | 'client' | 'firmware'
export type ReleaseChannel = 'stable' | 'beta' | 'canary'
export type ReleasePlatform = 'android' | 'ios' | 'esp32_s3' | 'all'

export interface AdminRelease {
  version: string
  channel: string
  kind: string
  platform: string
  downloadUrl: string
  sha256: string
  releaseNotes: string
  isMandatory: boolean
  minimumSupportedVersion: string
  status: string
  publishedAt: string
  createdAt: string
}

export interface CreateReleaseInput {
  version: string
  channel: ReleaseChannel
  kind: ReleaseKind
  platform: ReleasePlatform
  downloadUrl: string
  sha256: string
  releaseNotes: string
  isMandatory: boolean
  minimumSupportedVersion: string
}

export interface ReleaseArtifactLookup {
  version: string
  kind: ReleaseKind
  platform: ReleasePlatform
}

export interface ReleasePagination {
  page: number
  pageSize: number
  pendingPageSize: number | null
  total: number
  totalPages: number
  hasPrevious: boolean
  hasMore: boolean
}

const DEFAULT_PAGE_SIZE = 20
const MAX_PAGE_SIZE = 100

// 管理端只操作版本清单；文件本身由发布流程上传并校验后再登记。
export const useReleaseStore = defineStore('admin-releases', () => {
  const httpClient = createHttpClient()
  const operationsClient = createAdminOperationsClient(httpClient)
  const runtimeConfig = getRuntimeConfig()
  const releases = ref<AdminRelease[]>([])
  const isLoading = ref(false)
  const isResolvingArtifact = ref(false)
  const isSubmitting = ref(false)
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')
  const pagination = ref<ReleasePagination>({
    page: 1,
    pageSize: DEFAULT_PAGE_SIZE,
    pendingPageSize: null,
    total: 0,
    totalPages: 0,
    hasPrevious: false,
    hasMore: false,
  })
  let requestSequence = 0
  let loadingSequence = 0
  let activeLoadingRequests = 0
  const requestInFlight = ref(false)
  const pageStart = computed(() =>
    pagination.value.total === 0
      ? 0
      : (pagination.value.page - 1) * pagination.value.pageSize + 1,
  )
  const pageEnd = computed(() =>
    Math.min(
      pagination.value.page * pagination.value.pageSize,
      pagination.value.total,
    ),
  )

  async function load(page = pagination.value.page): Promise<void> {
    const requestedPage = Math.max(1, Math.trunc(page))
    if (isLoading.value) {
      return
    }
    const sequence = ++requestSequence
    const loadSequence = beginLoading()
    error.value = null
    try {
      const response = await httpClient.get('/api/v1/admin/releases', {
        params: {
          page: requestedPage,
          page_size: pagination.value.pageSize,
        },
      })
      if (sequence !== requestSequence) {
        return
      }
      const payload = response.data.data ?? {}
      const records = payload.releases ?? []
      releases.value = Array.isArray(records) ? records.map(toRelease) : []
      pagination.value = toPagination(
        payload.pagination,
        requestedPage,
        pagination.value.pageSize,
        releases.value.length,
      )
      if (
        releases.value.length === 0 &&
        pagination.value.total > 0 &&
        requestedPage > pagination.value.totalPages
      ) {
        const lastPage = Math.max(1, pagination.value.totalPages)
        pagination.value = { ...pagination.value, page: lastPage }
        const retryResponse = await httpClient.get('/api/v1/admin/releases', {
          params: {
            page: lastPage,
            page_size: pagination.value.pageSize,
          },
        })
        if (sequence !== requestSequence) {
          return
        }
        const retryPayload = retryResponse.data.data ?? {}
        const retryRecords = retryPayload.releases ?? []
        releases.value = Array.isArray(retryRecords)
          ? retryRecords.map(toRelease)
          : []
        pagination.value = toPagination(
          retryPayload.pagination,
          lastPage,
          pagination.value.pageSize,
          releases.value.length,
        )
      }
    } catch (caught: unknown) {
      if (sequence === requestSequence) {
        error.value = mapApiError(caught)
      }
    } finally {
      finishLoading(loadSequence)
    }
  }

  async function goToPage(page: number): Promise<void> {
    const nextPage = Math.max(1, Math.trunc(page))
    if (isLoading.value) {
      return
    }
    if (
      nextPage === pagination.value.page ||
      (pagination.value.totalPages > 0 &&
        nextPage > pagination.value.totalPages)
    ) {
      return
    }
    await load(nextPage)
  }

  async function setPageSize(pageSize: number): Promise<void> {
    if (isLoading.value) {
      return
    }
    const nextPageSize = clampPageSize(pageSize)
    setPendingPageSize(nextPageSize)
    const sequence = ++requestSequence
    const loadSequence = beginLoading()
    error.value = null
    try {
      const response = await httpClient.get('/api/v1/admin/releases', {
        params: {
          page: 1,
          page_size: nextPageSize,
        },
      })
      if (sequence !== requestSequence) {
        return
      }
      const payload = response.data.data ?? {}
      const records = payload.releases ?? []
      releases.value = Array.isArray(records) ? records.map(toRelease) : []
      pagination.value = toPagination(
        payload.pagination,
        1,
        nextPageSize,
        releases.value.length,
      )
    } catch (caught: unknown) {
      if (sequence === requestSequence) {
        error.value = mapApiError(caught)
      }
    } finally {
      clearPendingPageSize()
      finishLoading(loadSequence)
    }
  }

  async function retryCurrentPage(): Promise<void> {
    if (requestInFlight.value) {
      return
    }
    const requestedPage = Math.max(1, Math.trunc(pagination.value.page))
    const sequence = ++requestSequence
    const loadSequence = beginLoading()
    error.value = null
    try {
      const response = await httpClient.get('/api/v1/admin/releases', {
        params: {
          page: requestedPage,
          page_size: pagination.value.pageSize,
        },
      })
      if (sequence !== requestSequence) {
        return
      }
      const payload = response.data.data ?? {}
      const records = payload.releases ?? []
      releases.value = Array.isArray(records) ? records.map(toRelease) : []
      pagination.value = toPagination(
        payload.pagination,
        requestedPage,
        pagination.value.pageSize,
        releases.value.length,
      )
      if (
        releases.value.length === 0 &&
        pagination.value.total > 0 &&
        requestedPage > pagination.value.totalPages
      ) {
        const lastPage = Math.max(1, pagination.value.totalPages)
        pagination.value = { ...pagination.value, page: lastPage }
        const retryResponse = await httpClient.get('/api/v1/admin/releases', {
          params: {
            page: lastPage,
            page_size: pagination.value.pageSize,
          },
        })
        if (sequence !== requestSequence) {
          return
        }
        const retryPayload = retryResponse.data.data ?? {}
        const retryRecords = retryPayload.releases ?? []
        releases.value = Array.isArray(retryRecords)
          ? retryRecords.map(toRelease)
          : []
        pagination.value = toPagination(
          retryPayload.pagination,
          lastPage,
          pagination.value.pageSize,
          releases.value.length,
        )
      }
    } catch (caught: unknown) {
      if (sequence === requestSequence) {
        error.value = mapApiError(caught)
      }
    } finally {
      finishLoading(loadSequence)
    }
  }

  async function create(input: CreateReleaseInput): Promise<boolean> {
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    try {
      await httpClient.post('/api/v1/admin/releases', {
        version: input.version.trim(),
        channel: input.channel,
        kind: input.kind,
        platform: input.platform,
        download_url: input.downloadUrl.trim(),
        sha256: input.sha256.trim(),
        release_notes: input.releaseNotes.trim(),
        is_mandatory: input.isMandatory,
        min_supported_version: input.minimumSupportedVersion.trim(),
      })
      await load(1)
      lastMessage.value = '更新版本已登记。'
      return true
    } catch (caught: unknown) {
      error.value = mapReleaseError(caught)
      return false
    } finally {
      isSubmitting.value = false
    }
  }

  async function resolveArtifact(
    input: ReleaseArtifactLookup,
  ): Promise<CreateReleaseInput | null> {
    isResolvingArtifact.value = true
    error.value = null
    lastMessage.value = ''
    try {
      const artifact = await operationsClient.loadReleaseArtifact(
        input.version,
        input.kind,
        input.platform,
      )
      if (artifact === null) {
        error.value = {
          kind: 'not_found',
          message: '这个版本还没有可用的更新文件，请确认文件已经上传。',
          retryable: false,
        }
        return null
      }
      const releaseInput = artifactToCreateInput(artifact, input)
      if (releaseInput === null) {
        error.value = {
          kind: 'unexpected',
          message: '更新文件的信息不完整，请确认下载地址和校验值已经生成。',
          retryable: false,
        }
        return null
      }
      lastMessage.value = `已找到 ${artifact.version} 的更新文件。`
      return releaseInput
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return null
    } finally {
      isResolvingArtifact.value = false
    }
  }

  async function publish(version: string): Promise<boolean> {
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    try {
      await httpClient.put(
        `/api/v1/admin/releases/${encodeURIComponent(version)}/publish`,
      )
      await load(pagination.value.page)
      lastMessage.value = `版本 ${version} 已发布。`
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isSubmitting.value = false
    }
  }

  async function remove(version: string): Promise<boolean> {
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    try {
      await httpClient.delete(
        `/api/v1/admin/releases/${encodeURIComponent(version)}`,
      )
      const nextPage =
        releases.value.length === 1 && pagination.value.page > 1
          ? pagination.value.page - 1
          : pagination.value.page
      await load(nextPage)
      lastMessage.value = `版本 ${version} 已删除。`
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isSubmitting.value = false
    }
  }

  return {
    adminBuildVersion: runtimeConfig.adminBuildVersion,
    error,
    goToPage,
    isLoading,
    isSubmitting,
    isResolvingArtifact,
    lastMessage,
    pageEnd,
    pageStart,
    pagination,
    releases,
    create,
    load,
    publish,
    resolveArtifact,
    retryCurrentPage,
    setPageSize,
    remove,
  }

  function beginLoading(): number {
    const loadSequence = ++loadingSequence
    activeLoadingRequests += 1
    requestInFlight.value = true
    isLoading.value = true
    return loadSequence
  }

  function finishLoading(loadSequence: number): void {
    if (loadSequence !== loadingSequence) {
      return
    }
    activeLoadingRequests = Math.max(0, activeLoadingRequests - 1)
    if (activeLoadingRequests === 0) {
      requestInFlight.value = false
      isLoading.value = false
    }
  }

  function setPendingPageSize(pageSize: number): void {
    pagination.value = {
      ...pagination.value,
      pendingPageSize: pageSize,
    }
  }

  function clearPendingPageSize(): void {
    pagination.value = {
      ...pagination.value,
      pendingPageSize: null,
    }
  }
})

function toPagination(
  value: unknown,
  fallbackPage: number,
  fallbackPageSize: number,
  recordCount: number,
): ReleasePagination {
  const record = recordValue(value)
  const total = nonNegativeInteger(
    record.total,
    recordCount,
  )
  const page = positiveInteger(record.page, fallbackPage)
  const pageSize = clampPageSize(
    positiveInteger(record.page_size ?? record.pageSize, fallbackPageSize),
  )
  const totalPages = nonNegativeInteger(
    record.total_pages ?? record.totalPages,
    total === 0 ? 0 : Math.ceil(total / pageSize),
  )
  return {
    page,
    pageSize,
    pendingPageSize: null,
    total,
    totalPages,
    hasPrevious: page > 1,
    hasMore: record.has_more === true || (totalPages > 0 && page < totalPages),
  }
}

function mapReleaseError(caught: unknown): ApiError {
  if (
    typeof caught === 'object' &&
    caught !== null &&
    'response' in caught &&
    typeof caught.response === 'object' &&
    caught.response !== null &&
    'data' in caught.response &&
    typeof caught.response.data === 'object' &&
    caught.response.data !== null &&
    'error' in caught.response.data &&
    typeof caught.response.data.error === 'object' &&
    caught.response.data.error !== null &&
    'code' in caught.response.data.error &&
    caught.response.data.error.code === 'release_exists'
  ) {
    return {
      kind: 'validation',
      message: '这个版本已经登记，请提高版本号后再登记。',
      retryable: false,
    }
  }
  return mapApiError(caught)
}

function clampPageSize(value: number): number {
  return Math.min(MAX_PAGE_SIZE, Math.max(1, Math.trunc(value)))
}

function positiveInteger(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

function nonNegativeInteger(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= 0 ? parsed : fallback
}

function artifactToCreateInput(
  artifact: ReleaseArtifact,
  lookup: ReleaseArtifactLookup,
): CreateReleaseInput | null {
  if (!artifact.downloadUrl || !artifact.sha256) {
    return null
  }
  return {
    version: artifact.version || lookup.version,
    channel: 'stable',
    kind: normalizeReleaseKind(artifact.kind, lookup.kind),
    platform: normalizeReleasePlatform(artifact.platform, lookup.platform),
    downloadUrl: artifact.downloadUrl,
    sha256: artifact.sha256,
    releaseNotes: '',
    isMandatory: false,
    minimumSupportedVersion: '',
  }
}

function normalizeReleaseKind(
  value: string,
  fallback: ReleaseKind,
): ReleaseKind {
  return value === 'resource' || value === 'client' || value === 'firmware'
    ? value
    : fallback
}

function normalizeReleasePlatform(
  value: string,
  fallback: ReleasePlatform,
): ReleasePlatform {
  return value === 'android' ||
    value === 'ios' ||
    value === 'esp32_s3' ||
    value === 'all'
    ? value
    : fallback
}

function toRelease(value: Record<string, unknown>): AdminRelease {
  return {
    version: stringValue(value.version),
    channel: stringValue(value.channel, 'stable'),
    kind: stringValue(value.kind, 'resource'),
    platform: stringValue(value.platform, 'all'),
    downloadUrl: stringValue(value.download_url),
    sha256: stringValue(value.sha256),
    releaseNotes: stringValue(value.release_notes),
    isMandatory: value.is_mandatory === true,
    minimumSupportedVersion: stringValue(value.min_supported_version),
    status: stringValue(value.status, 'draft'),
    publishedAt: stringValue(value.published_at),
    createdAt: stringValue(value.created_at),
  }
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}
