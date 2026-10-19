import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { apiNoticeWarning, readApiNotice, type ApiNotice } from '@/api/apiNotice'
import {
  canonicalArtifactRelativePath,
  createAdminDownloadFilesClient,
  type AdminDownloadFilesClient,
  type DownloadArtifactKind,
  type DownloadArtifactPathInput,
  type DownloadArtifactPlatform,
  type DownloadFile,
  type DownloadFileUpload,
  type DownloadIndexStatus,
} from '@/api/adminDownloadFiles'
import { mapApiError, type ApiError } from '@/api/apiError'
import { createHttpClient } from '@/api/httpClient'

export type DownloadFileStatusFilter = 'all' | 'indexed' | 'pending'
export type DownloadArtifactKindFilter = 'all' | DownloadArtifactKind

export interface DownloadFileFilters {
  directory: string
  version: string
  kind: DownloadArtifactKindFilter
  status: DownloadFileStatusFilter
}

export interface DownloadFileUploadState {
  isUploading: boolean
  isRegistering: boolean
  progress: number
  fileName: string
  uploadedFilePath: string
  registrationState:
    | 'idle'
    | 'registered'
    | 'failed'
    | 'skipped'
    | 'notes_missing'
    | 'conflict'
  indexState: 'idle' | 'refreshed' | 'failed'
  listState: 'idle' | 'refreshed' | 'failed'
  registrationNotice: string
  indexNotice: string
  listNotice: string
  canRetryRegistration: boolean
  error: string
}

export interface DownloadFileUploadReview {
  summary: string
  actionLabel: string
  action:
    | 'retry_registration'
    | 'refresh_index'
    | 'reload_files'
    | 'manage_release'
    | 'none'
  canManageRelease: boolean
}

export type DownloadUploadStage =
  | 'uploading'
  | 'registering'
  | 'registration_failed'
  | 'registration_required'
  | 'registered'
  | 'conflict'

export interface DownloadUploadStageState {
  upload: 'idle' | 'pending' | 'done' | 'failed'
  registration:
    | 'idle'
    | 'pending'
    | 'done'
    | 'failed'
    | 'blocked'
    | 'conflict'
  index: 'idle' | 'pending' | 'done' | 'failed' | 'blocked'
  list: 'idle' | 'pending' | 'done' | 'failed' | 'blocked'
}

export const emptyDownloadIndexStatus: DownloadIndexStatus = {
  isAvailable: false,
  refreshedAt: '',
  indexedFileCount: 0,
  pendingFileCount: 0,
  errorCode: '',
  errorMessage: '',
}

export interface DownloadUploadResult {
  uploaded: boolean
  registered: boolean
  indexRefreshed: boolean
  listRefreshed: boolean
  uploadedFile: DownloadFile | null
  error: ApiError | null
  warning: string
}

export interface PendingUploadMetadata {
  filename: string
  version: string
  channel: string
  kind: DownloadArtifactKind
  platform: DownloadArtifactPlatform
  releaseNotes: string
}

export interface UploadConflict {
  message: string
}

/// Owns the download-server inventory, filters, upload progress, and the
/// publishing index state. The page keeps this store as the single source of
/// truth so refreshes and uploads stay consistent.
export const useDownloadFilesStore = defineStore('admin-download-files', () => {
  const client: AdminDownloadFilesClient = createAdminDownloadFilesClient()
  const httpClient = createHttpClient()
  const files = ref<DownloadFile[]>([])
  const indexStatus = ref<DownloadIndexStatus>({ ...emptyDownloadIndexStatus })
  const filters = ref<DownloadFileFilters>({
    directory: '',
    version: '',
    kind: 'all',
    status: 'all',
  })
  const isLoading = ref(false)
  const isRefreshingIndex = ref(false)
  const deletingPaths = ref<string[]>([])
  const uploadState = ref<DownloadFileUploadState>({
    isUploading: false,
    isRegistering: false,
    progress: 0,
    fileName: '',
    uploadedFilePath: '',
    registrationState: 'idle',
    indexState: 'idle',
    listState: 'idle',
    registrationNotice: '',
    indexNotice: '',
    listNotice: '',
    canRetryRegistration: false,
    error: '',
  })
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')
  const lastWarning = ref('')
  let pendingUpload: { input: DownloadFileUpload; file: DownloadFile } | null = null
  let completionSequence = 0
  let filesRequestSequence = 0
  let activeFilesRequests = 0
  let conflictCheckInput: string | null = null
  let conflictCheckResult: UploadConflict | null = null

  const directories = computed(() => {
    const values = new Set<string>()
    for (const file of files.value) {
      if (file.directory) {
        values.add(file.directory)
      }
    }
    return [...values].sort((left, right) => left.localeCompare(right))
  })
  const uploadReview = computed<DownloadFileUploadReview>(() =>
    buildUploadReview(uploadState.value),
  )
  const uploadStages = computed<DownloadUploadStageState>(() =>
    buildUploadStages(uploadState.value),
  )
  const uploadStage = computed<DownloadUploadStage>(() =>
    currentUploadStage(uploadState.value),
  )

  const versions = computed(() => {
    const values = new Set<string>()
    for (const file of files.value) {
      if (file.version) {
        values.add(file.version)
      }
    }
    return [...values].sort((left, right) => right.localeCompare(left, undefined, { numeric: true }))
  })

  const visibleFiles = computed(() =>
    files.value.filter((file) => {
      if (filters.value.directory && file.directory !== filters.value.directory) {
        return false
      }
      if (filters.value.version && file.version !== filters.value.version) {
        return false
      }
      if (filters.value.kind !== 'all' && file.kind !== filters.value.kind) {
        return false
      }
      if (filters.value.status === 'indexed' && !file.isIndexed) {
        return false
      }
      if (filters.value.status === 'pending' && file.isIndexed) {
        return false
      }
      return true
    }),
  )

  const indexedFileCount = computed(
    () => files.value.filter((file) => file.isIndexed).length,
  )
  const pendingFileCount = computed(
    () => files.value.filter((file) => !file.isIndexed).length,
  )
  const totalSizeBytes = computed(() =>
    files.value.reduce((sum, file) => sum + file.sizeBytes, 0),
  )
  const versionArtifacts = computed(() => {
    const grouped = new Map<string, DownloadFile[]>()
    for (const file of files.value) {
      const version = file.version.trim()
      if (!version) {
        continue
      }
      const groupKey = `${version}\u0000${file.channel}`
      const existing = grouped.get(groupKey) ?? []
      existing.push(file)
      grouped.set(groupKey, existing)
    }
    return [...grouped.entries()]
      .map(([groupKey, artifacts]) => {
        const separator = groupKey.indexOf('\u0000')
        return {
          version: groupKey.slice(0, separator),
          channel: groupKey.slice(separator + 1),
          artifacts: [...artifacts].sort((left, right) =>
            left.relativePath.localeCompare(right.relativePath),
          ),
        }
      })
      .sort((left, right) => {
        const versionOrder = right.version.localeCompare(left.version, undefined, {
          numeric: true,
        })
        return versionOrder !== 0 ? versionOrder : left.channel.localeCompare(right.channel)
      })
  })

  async function load(): Promise<void> {
    const requestSequence = ++filesRequestSequence
    activeFilesRequests += 1
    isLoading.value = true
    error.value = null
    try {
      const [loadedFiles, loadedIndexStatus] = await Promise.all([
        client.loadFiles(),
        client.loadIndexStatus().catch((caught: unknown) => ({
          ...emptyDownloadIndexStatus,
          errorCode: indexErrorCode(caught),
        })),
      ])
      if (requestSequence === filesRequestSequence) {
        files.value = loadedFiles
        indexStatus.value = loadedIndexStatus
      }
    } catch (caught: unknown) {
      if (requestSequence === filesRequestSequence) {
        error.value = mapApiError(caught)
      }
    } finally {
      activeFilesRequests = Math.max(0, activeFilesRequests - 1)
      if (activeFilesRequests === 0) {
        isLoading.value = false
      }
    }
  }

  async function upload(input: DownloadFileUpload): Promise<DownloadUploadResult> {
    if (
      uploadState.value.isUploading ||
      uploadState.value.isRegistering ||
      isRefreshingIndex.value
    ) {
      return cancelledUploadResult()
    }
    uploadState.value = {
      isUploading: true,
      isRegistering: false,
      progress: 0,
      fileName: input.filename,
      uploadedFilePath: '',
      registrationState: 'idle',
      indexState: 'idle',
      listState: 'idle',
      registrationNotice: '',
      indexNotice: '',
      listNotice: '',
      canRetryRegistration: false,
      error: '',
    }
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    const requestSequence = ++completionSequence
    pendingUpload = null
    try {
      const existingConflict = await existingReleaseConflict(input)
      if (requestSequence !== completionSequence) {
        return cancelledUploadResult()
      }
      if (existingConflict !== null) {
        uploadState.value = {
          ...uploadState.value,
          isUploading: false,
          registrationState: 'conflict',
          error: existingConflict.message,
        }
        error.value = existingConflict
        return {
          uploaded: false,
          registered: false,
          indexRefreshed: false,
          listRefreshed: false,
          uploadedFile: null,
          error: existingConflict,
          warning: '',
        }
      }
      const uploadResult = await client.uploadFile(input, (progress) => {
        if (requestSequence === completionSequence) {
          uploadState.value = { ...uploadState.value, progress }
        }
      })
      const uploadedFile = uploadResult.value
      if (requestSequence !== completionSequence) {
        return cancelledUploadResult()
      }
      uploadState.value = {
        ...uploadState.value,
        isUploading: false,
        progress: 100,
        fileName: uploadedFile.name,
        uploadedFilePath: uploadedFile.relativePath,
      }
      const resolvedInput = { ...input, channel: uploadedFile.channel }
      pendingUpload = { input: resolvedInput, file: uploadedFile }
      lastWarning.value = apiNoticeWarning(uploadResult.notice)
      return await completeUploadCompletion(
        uploadedFile,
        resolvedInput,
        requestSequence,
        uploadResult.notice,
      )
    } catch (caught: unknown) {
      const mappedError = mapApiError(caught)
      if (requestSequence !== completionSequence) {
        return cancelledUploadResult()
      }
      if (pendingUpload !== null) {
        uploadState.value = {
          ...uploadState.value,
          isUploading: false,
          isRegistering: false,
          registrationState: 'failed',
          registrationNotice: '',
          canRetryRegistration: true,
          error: `文件已经上传，但后续处理失败：${mappedError.message}`,
        }
        error.value = mappedError
        return {
          uploaded: true,
          registered: false,
          indexRefreshed: false,
          listRefreshed: false,
          uploadedFile: pendingUpload.file,
          error: mappedError,
          warning: lastWarning.value,
        }
      }
      uploadState.value = {
        ...uploadState.value,
        isUploading: false,
        error: mappedError.message,
      }
      error.value = mappedError
      return {
        uploaded: false,
        registered: false,
        indexRefreshed: false,
        listRefreshed: false,
        uploadedFile: null,
        error: mappedError,
        warning: '',
      }
    } finally {
      if (requestSequence === completionSequence) {
        uploadState.value = { ...uploadState.value, isUploading: false }
      }
    }
  }

  async function retryUploadCompletion(): Promise<DownloadUploadResult> {
    if (uploadState.value.isUploading || uploadState.value.isRegistering) {
      return cancelledUploadResult()
    }
    const pending = pendingUpload
    if (pending === null) {
      const mappedError: ApiError = {
        kind: 'unexpected',
        message: '没有可以继续处理的文件，请重新选择文件上传。',
        retryable: false,
      }
      uploadState.value = { ...uploadState.value, error: mappedError.message }
      return {
        uploaded: false,
        registered: false,
        indexRefreshed: false,
        listRefreshed: false,
        uploadedFile: null,
        error: mappedError,
        warning: '',
      }
    }
    return completeUploadCompletion(pending.file, pending.input)
  }

  async function checkUploadConflict(
    input: DownloadFileUpload,
  ): Promise<UploadConflict | null> {
    const inputKey = uploadConflictKey(input)
    if (inputKey === conflictCheckInput) {
      return conflictCheckResult
    }
    const version = input.version.trim()
    const targetPath = fileUploadRelativePath(input)
    if (files.value.some((file) => file.relativePath === targetPath)) {
      conflictCheckInput = inputKey
      conflictCheckResult = {
        message:
          '这个版本目录下已经有同名文件。为避免已校验文件被替换，请调整渠道、平台或文件名。',
      }
      return conflictCheckResult
    }
    if (version === '') {
      conflictCheckInput = inputKey
      conflictCheckResult = null
      return null
    }
    const release = await findRegisteredRelease(
      version,
      input.kind,
      input.platform,
      (input.channel ?? 'stable').trim() || 'stable',
    )
    conflictCheckInput = inputKey
    conflictCheckResult = release
      ? {
        message:
          '同版本、渠道、类型和平台已经登记过，不能用新文件覆盖。请调整其中一项后重试。',
      }
      : null
    return conflictCheckResult
  }

  /// Returns the exact path the download server will use for an upload so the
  /// form can show the destination before any file is sent.
  function previewUploadPath(input: DownloadArtifactPathInput): string {
    return fileUploadRelativePath(input)
  }

  async function completeUploadCompletion(
    uploadedFile: DownloadFile,
    input: DownloadFileUpload,
    requestSequence = ++completionSequence,
    uploadNotice: ApiNotice | null = null,
  ): Promise<DownloadUploadResult> {
    uploadState.value = {
      ...uploadState.value,
      isRegistering: true,
      error: '',
    }
    error.value = null
    lastMessage.value = ''
    let registration: 'registered' | 'skipped' | 'conflict' | 'notes_missing'
    try {
      registration = await registerDraftRelease(
        uploadedFile,
        input.releaseNotes ?? '',
      )
    } catch (caught: unknown) {
      const mappedError = mapApiError(caught)
      if (requestSequence !== completionSequence) {
        return incompleteUploadResult(uploadedFile)
      }
      uploadState.value = {
        ...uploadState.value,
        isRegistering: false,
        registrationState: 'failed',
        registrationNotice: '',
        canRetryRegistration: true,
        error: `文件已经上传，但版本登记失败：${mappedError.message}`,
      }
      error.value = mappedError
      return {
        uploaded: true,
        registered: false,
        indexRefreshed: false,
        listRefreshed: false,
        uploadedFile,
        error: mappedError,
        warning: apiNoticeWarning(uploadNotice),
      }
    }
    if (requestSequence !== completionSequence) {
      return incompleteUploadResult(uploadedFile)
    }

    uploadState.value = {
      ...uploadState.value,
      isRegistering: false,
      registrationState: registration,
      canRetryRegistration: false,
    }

    if (registration === 'conflict') {
      const mappedError: ApiError = {
        kind: 'validation',
        message:
          '同版本、渠道、类型和平台已经登记过，不能用新文件覆盖。请调整其中一项后重试。',
        retryable: false,
      }
      uploadState.value = { ...uploadState.value, error: mappedError.message }
      error.value = mappedError
      return {
        uploaded: true,
        registered: false,
        indexRefreshed: false,
        listRefreshed: false,
        uploadedFile,
        error: mappedError,
        warning: apiNoticeWarning(uploadNotice),
      }
    }
    if (registration === 'skipped' || registration === 'notes_missing') {
      const mappedError: ApiError = {
        kind: 'validation',
        message:
          '文件已经上传，但版本登记所需的信息不完整，请补充更新说明后重试登记。',
        retryable: false,
      }
      uploadState.value = { ...uploadState.value, error: mappedError.message }
      error.value = mappedError
      return {
        uploaded: true,
        registered: false,
        indexRefreshed: false,
        listRefreshed: false,
        uploadedFile,
        error: mappedError,
        warning: apiNoticeWarning(uploadNotice),
      }
    }

    const registered = registration === 'registered'
    const indexRefreshed = await refreshIndexAfterUpload(requestSequence)
    const listRefreshed = await reloadFilesAfterUpload(requestSequence)
    if (requestSequence !== completionSequence) {
      return incompleteUploadResult(uploadedFile)
    }
    if (registered) {
      lastMessage.value = `${input.filename} 已上传，并登记为待发布版本。`
    }
    return {
      uploaded: true,
      registered,
      indexRefreshed,
      listRefreshed,
      uploadedFile,
      error: null,
      warning: latestUploadWarning(),
    }
  }

  async function refreshIndexAfterUpload(requestSequence: number): Promise<boolean> {
    if (requestSequence === completionSequence) {
      uploadState.value = {
        ...uploadState.value,
        indexState: 'idle',
      }
    }
    try {
      const result = await client.refreshIndex()
      const nextStatus = result.value
      if (requestSequence !== completionSequence) {
        return false
      }
      indexStatus.value = nextStatus
      uploadState.value = {
        ...uploadState.value,
        indexNotice: joinWarnings(
          uploadState.value.indexNotice,
          apiNoticeWarning(result.notice),
        ),
      }
      uploadState.value = {
        ...uploadState.value,
        indexState: 'refreshed',
        error:
          uploadState.value.registrationState === 'skipped' ||
          uploadState.value.registrationState === 'notes_missing'
            ? '文件已经上传并更新索引，但还没有登记为待发布版本。'
            : '',
      }
      return true
    } catch (caught: unknown) {
      const mappedError = mapApiError(caught)
      if (requestSequence !== completionSequence) {
        return false
      }
      uploadState.value = {
        ...uploadState.value,
        indexState: 'failed',
        error: `版本已经登记，但发布索引刷新失败：${mappedError.message}。可以重新刷新索引。`,
      }
      return false
    }
  }

  async function reloadFilesAfterUpload(requestSequence: number): Promise<boolean> {
    if (requestSequence === completionSequence) {
      uploadState.value = {
        ...uploadState.value,
        listState: 'idle',
      }
    }
    try {
      const nextFiles = await client.loadFiles()
      if (requestSequence !== completionSequence) {
        return false
      }
      files.value = nextFiles
      uploadState.value = {
        ...uploadState.value,
        listState: 'refreshed',
      }
      return true
    } catch (caught: unknown) {
      const mappedError = mapApiError(caught)
      if (requestSequence !== completionSequence) {
        return false
      }
      uploadState.value = {
        ...uploadState.value,
        listState: 'failed',
        error: `版本已经完成，但文件列表刷新失败：${mappedError.message}。可以重新读取文件。`,
      }
      return false
    }
  }

  /// A manual upload is useful only when it becomes publishable. Register the
  /// draft with the same metadata the download server wrote, and let a later
  /// "publish" action decide whether clients may receive it.
  async function registerDraftRelease(
    file: DownloadFile,
    releaseNotes: string,
  ): Promise<'registered' | 'skipped' | 'conflict' | 'notes_missing'> {
    if (
      !file.version ||
      !file.sha256 ||
      !file.downloadUrl ||
      file.kind === 'unknown' ||
      file.platform === 'unknown'
    ) {
      return 'skipped'
    }
    const existing = await findRegisteredRelease(
      file.version,
      file.kind,
      file.platform,
      file.channel,
    )
    if (existing) {
      return 'conflict'
    }
    if (releaseNotes.trim() === '') {
      return 'notes_missing'
    }
    try {
      const response = await httpClient.post('/api/v1/admin/releases', {
        version: file.version,
        channel: file.channel,
        kind: file.kind,
        platform: file.platform,
        download_url: downloadUrlForFile(file),
        sha256: file.sha256,
        release_notes: releaseNotes.trim(),
        is_mandatory: false,
        min_supported_version: '',
      })
      lastWarning.value = joinWarnings(
        lastWarning.value,
        apiNoticeWarning(readApiNotice(response.data)),
      )
    } catch (caught: unknown) {
      if (releaseAlreadyExists(caught)) {
        return 'conflict'
      }
      throw caught
    }
    return 'registered'
  }

  async function findRegisteredRelease(
    version: string,
    kind: DownloadArtifactKind,
    platform: DownloadArtifactPlatform,
    channel: string,
  ): Promise<Record<string, unknown> | null> {
    const pageSize = 100
    for (let page = 1; ; page += 1) {
      const response = await httpClient.get('/api/v1/admin/releases', {
        params: { page, page_size: pageSize },
      })
      const payload = recordValue(response.data?.data)
      const records = Array.isArray(payload.releases) ? payload.releases : []
      const matched = records.find((item: unknown) => {
        const record = recordValue(item)
        return (
          stringValue(record.version) === version &&
          stringValue(record.kind) === kind &&
          stringValue(record.platform) === platform &&
          stringValue(record.channel, 'stable') === channel
        )
      })
      if (matched) {
        return recordValue(matched)
      }
      const pagination = recordValue(payload.pagination)
      const totalPages = Number(pagination.total_pages)
      const hasNextPage =
        pagination.has_more === true ||
        (Number.isInteger(totalPages) && page < totalPages)
      if (!hasNextPage || records.length === 0) {
        return null
      }
    }
  }

  async function existingReleaseConflict(
    input: DownloadFileUpload,
  ): Promise<ApiError | null> {
    const version = input.version.trim()
    if (
      version === '' ||
      input.kind === 'unknown' ||
      input.platform === 'unknown'
    ) {
      return null
    }
    if (
      await findRegisteredRelease(
        version,
        input.kind,
        input.platform,
        (input.channel ?? 'stable').trim() || 'stable',
      )
    ) {
      return {
        kind: 'validation',
        message:
          '同版本、渠道、类型和平台已经登记过，不能用新文件覆盖。请调整其中一项后重试。',
        retryable: false,
      }
    }
    return null
  }

  function downloadUrlForFile(file: DownloadFile): string {
    return file.downloadUrl
  }

  function latestUploadWarning(): string {
    return joinWarnings(
      lastWarning.value,
      uploadState.value.registrationNotice,
      uploadState.value.indexNotice,
      uploadState.value.listNotice,
    )
  }

  async function remove(file: DownloadFile): Promise<boolean> {
    deletingPaths.value = [...deletingPaths.value, file.relativePath]
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const deletion = await client.deleteFile(file.relativePath)
      lastWarning.value = apiNoticeWarning(deletion.notice)
      files.value = files.value.filter((item) => item.relativePath !== file.relativePath)
      const refreshedIndex = await client.refreshIndex().catch(() => null)
      if (refreshedIndex === null) {
        indexStatus.value = {
          ...emptyDownloadIndexStatus,
          isAvailable: false,
        }
      } else {
        indexStatus.value = refreshedIndex.value
        lastWarning.value = joinWarnings(
          lastWarning.value,
          apiNoticeWarning(refreshedIndex.notice),
        )
      }
      await reloadFiles()
      lastMessage.value = `${file.name} 已从下载服务器删除。`
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      deletingPaths.value = deletingPaths.value.filter(
        (relativePath) => relativePath !== file.relativePath,
      )
    }
  }

  async function refreshIndex(): Promise<boolean> {
    if (isRefreshingIndex.value) {
      return false
    }
    isRefreshingIndex.value = true
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const result = await client.refreshIndex()
      indexStatus.value = result.value
      lastWarning.value = apiNoticeWarning(result.notice)
      lastMessage.value =
        indexStatus.value.pendingFileCount === 0
          ? '发布索引已刷新，所有文件都可以登记版本。'
          : `发布索引已刷新，还有 ${indexStatus.value.pendingFileCount} 个文件未登记版本。`
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isRefreshingIndex.value = false
    }
  }

  async function reloadFiles(): Promise<void> {
    if (isLoading.value) {
      return
    }
    const requestSequence = ++filesRequestSequence
    activeFilesRequests += 1
    isLoading.value = true
    try {
      const nextFiles = await client.loadFiles()
      if (requestSequence !== filesRequestSequence) {
        return
      }
      files.value = nextFiles
      if (uploadState.value.listState === 'failed') {
        uploadState.value = {
          ...uploadState.value,
          listState: 'refreshed',
        }
      }
      error.value = null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (requestSequence !== filesRequestSequence) {
        return
      }
      error.value = mapped
      if (uploadState.value.listState === 'failed') {
        uploadState.value = {
          ...uploadState.value,
          error: `文件列表仍然没有刷新：${mapped.message}。可以重新读取文件。`,
        }
      }
    } finally {
      activeFilesRequests = Math.max(0, activeFilesRequests - 1)
      if (activeFilesRequests === 0) {
        isLoading.value = false
      }
    }
  }

  async function refreshUploadedIndex(): Promise<boolean> {
    if (
      uploadState.value.registrationState !== 'registered' ||
      uploadState.value.indexState !== 'failed'
    ) {
      return refreshIndex()
    }
    const requestSequence = completionSequence
    isRefreshingIndex.value = true
    try {
      const refreshed = await refreshIndexAfterUpload(requestSequence)
      if (!refreshed || requestSequence !== completionSequence) {
        return false
      }
      await reloadFilesAfterUpload(requestSequence)
      if (requestSequence === completionSequence) {
        lastMessage.value = `${uploadState.value.fileName} 已登记，发布索引也已刷新。`
        uploadState.value = { ...uploadState.value, error: '' }
        error.value = null
      }
      return refreshed
    } finally {
      isRefreshingIndex.value = false
    }
  }

  function clearUploadError(): void {
    uploadState.value = { ...uploadState.value, error: '' }
    if (error.value?.kind !== 'unexpected') {
      error.value = null
    }
  }

  function isDeleting(relativePath: string): boolean {
    return deletingPaths.value.includes(relativePath)
  }

  function setFilters(nextFilters: Partial<DownloadFileFilters>): void {
    filters.value = { ...filters.value, ...nextFilters }
  }

  function resetFilters(): void {
    filters.value = { directory: '', version: '', kind: 'all', status: 'all' }
  }

  function clearMessages(): void {
    if (!uploadState.value.isUploading && !uploadState.value.isRegistering) {
      ++completionSequence
    }
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    uploadState.value = {
      isUploading: false,
      isRegistering: false,
      progress: 0,
      fileName: '',
      uploadedFilePath: '',
      registrationState: 'idle',
      indexState: 'idle',
      listState: 'idle',
      registrationNotice: '',
      indexNotice: '',
      listNotice: '',
      canRetryRegistration: false,
      error: '',
    }
    if (!uploadState.value.isUploading && !uploadState.value.isRegistering) {
      pendingUpload = null
      conflictCheckInput = null
      conflictCheckResult = null
    }
  }

  function applyReleaseNotes(releaseNotes: string): void {
    if (pendingUpload === null) {
      return
    }
    pendingUpload = {
      ...pendingUpload,
      input: {
        ...pendingUpload.input,
        releaseNotes,
      },
    }
    uploadState.value = { ...uploadState.value, error: '' }
    error.value = null
  }

  function pendingUploadMetadata(): PendingUploadMetadata | null {
    if (pendingUpload === null) {
      return null
    }
    return {
      filename: pendingUpload.input.filename,
      version: pendingUpload.input.version,
      channel: pendingUpload.file.channel,
      kind: pendingUpload.input.kind,
      platform: pendingUpload.input.platform,
      releaseNotes: pendingUpload.input.releaseNotes ?? '',
    }
  }

  return {
    applyReleaseNotes,
    checkUploadConflict,
    clearMessages,
    clearUploadError,
    deletingPaths,
    directories,
    error,
    files,
    filters,
    indexStatus,
    indexedFileCount,
    isLoading,
    isRefreshingIndex,
    isDeleting,
    lastMessage,
    lastWarning,
    pendingUploadMetadata,
    pendingFileCount,
    previewUploadPath,
    reloadFiles,
    refreshIndex,
    refreshUploadedIndex,
    remove,
    retryUploadCompletion,
    resetFilters,
    setFilters,
    totalSizeBytes,
    upload,
    uploadState,
    uploadStage,
    uploadStages,
    uploadReview,
    versionArtifacts,
    versions,
    visibleFiles,
    load,
  }
})

export function artifactKindLabel(value: DownloadArtifactKind): string {
  return (
    {
      resource: '资源更新',
      client: '客户端更新',
      firmware: '设备固件',
      unknown: '未标注',
    }[value] ?? '未标注'
  )
}

export function artifactPlatformLabel(value: DownloadArtifactPlatform): string {
  return (
    {
      android: 'Android',
      ios: 'iOS',
      esp32_s3: '初芽设备',
      all: '全部平台',
      unknown: '未标注',
    }[value] ?? '未标注'
  )
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}

function fileUploadRelativePath(input: DownloadArtifactPathInput): string {
  return canonicalArtifactRelativePath({
    version: input.version,
    channel: input.channel,
    kind: input.kind,
    platform: input.platform,
    filename: input.filename,
  })
}

function uploadConflictKey(input: DownloadFileUpload): string {
  return [
    input.version.trim(),
    (input.channel ?? 'stable').trim(),
    input.kind,
    input.platform,
    input.filename.trim(),
  ].join('\u0000')
}

function indexErrorCode(caught: unknown): string {
  void caught
  return 'download_index_unavailable'
}

function incompleteUploadResult(
  uploadedFile: DownloadFile | null = null,
): DownloadUploadResult {
  return {
    uploaded: uploadedFile !== null,
    registered: false,
    indexRefreshed: false,
    listRefreshed: false,
    uploadedFile,
    error: null,
    warning: '',
  }
}

function cancelledUploadResult(): DownloadUploadResult {
  return {
    uploaded: false,
    registered: false,
    indexRefreshed: false,
    listRefreshed: false,
    uploadedFile: null,
    error: null,
    warning: '',
  }
}

function joinWarnings(...warnings: string[]): string {
  return [...new Set(warnings.map((warning) => warning.trim()).filter(Boolean))].join(
    '\n',
  )
}

function releaseAlreadyExists(caught: unknown): boolean {
  if (typeof caught !== 'object' || caught === null || !('response' in caught)) {
    return false
  }
  const response = caught.response
  if (typeof response !== 'object' || response === null || !('data' in response)) {
    return false
  }
  const data = response.data
  if (typeof data !== 'object' || data === null || !('error' in data)) {
    return false
  }
  const error = data.error
  return (
    typeof error === 'object' &&
    error !== null &&
    'code' in error &&
    error.code === 'release_exists'
  )
}

export function buildUploadReview(
  state: DownloadFileUploadState,
): DownloadFileUploadReview {
  if (state.isUploading) {
    return {
      summary: `正在上传 ${state.progress}%`,
      actionLabel: '',
      action: 'none',
      canManageRelease: false,
    }
  }
  if (state.isRegistering) {
    return {
      summary: '文件已上传，正在登记版本。',
      actionLabel: '',
      action: 'none',
      canManageRelease: false,
    }
  }
  if (
    state.indexState === 'failed' &&
    (state.registrationState === 'registered' ||
      state.registrationState === 'idle')
  ) {
    return {
      summary: '版本已经登记，但发布索引同步失败。',
      actionLabel: '重试刷新索引',
      action: 'refresh_index',
      canManageRelease: false,
    }
  }
  if (state.registrationState === 'failed') {
    return {
      summary: '文件已上传，但版本登记失败。文件不会丢失，可以重试登记。',
      actionLabel: '重试登记版本',
      action: 'retry_registration',
      canManageRelease: false,
    }
  }
  if (state.registrationState === 'conflict') {
    return {
      summary: '文件已上传，但同版本、渠道、类型和平台的更新已经存在。请调整其中一项后重试。',
      actionLabel: '前往版本管理',
      action: 'manage_release',
      canManageRelease: true,
    }
  }
  if (
    state.registrationState === 'skipped' ||
    state.registrationState === 'notes_missing' ||
    state.canRetryRegistration
  ) {
    return {
      summary: '文件已上传，但缺少可发布信息，尚未登记版本。',
      actionLabel: '补全并重试登记',
      action: 'retry_registration',
      canManageRelease: false,
    }
  }
  if (state.listState === 'failed') {
    return {
      summary: '版本和索引已经完成，但文件列表刷新失败。',
      actionLabel: '重新读取文件',
      action: 'reload_files',
      canManageRelease: false,
    }
  }
  if (state.registrationState === 'registered') {
    return {
      summary: '文件已上传并登记为待发布版本，可以直接前往内容发布。',
      actionLabel: '',
      action: 'manage_release',
      canManageRelease: true,
    }
  }
  return {
    summary: '',
    actionLabel: '',
    action: 'none',
    canManageRelease: false,
  }
}

export function buildUploadStages(
  state: DownloadFileUploadState,
): DownloadUploadStageState {
  const upload: DownloadUploadStageState['upload'] = state.isUploading
    ? 'pending'
    : state.uploadedFilePath
      ? 'done'
      : state.error
        ? 'failed'
        : 'idle'
  const registration: DownloadUploadStageState['registration'] =
    state.isRegistering
      ? 'pending'
      : state.registrationState === 'registered'
        ? 'done'
        : state.registrationState === 'conflict'
          ? 'conflict'
          : state.registrationState === 'failed'
            ? 'failed'
            : state.registrationState === 'skipped' ||
                state.registrationState === 'notes_missing'
              ? 'blocked'
              : upload === 'done'
                ? 'pending'
                : 'idle'
  const registrationDone = registration === 'done'
  const index: DownloadUploadStageState['index'] = !registrationDone
    ? 'blocked'
    : state.indexState === 'refreshed'
      ? 'done'
      : state.indexState === 'failed'
        ? 'failed'
        : 'pending'
  const list: DownloadUploadStageState['list'] = !registrationDone
    ? 'blocked'
    : state.listState === 'refreshed'
      ? 'done'
      : state.listState === 'failed'
        ? 'failed'
        : 'pending'
  return { upload, registration, index, list }
}

export function currentUploadStage(
  state: DownloadFileUploadState,
): DownloadUploadStage {
  if (state.isUploading) {
    return 'uploading'
  }
  if (state.isRegistering) {
    return 'registering'
  }
  if (state.registrationState === 'conflict') {
    return 'conflict'
  }
  if (state.registrationState === 'failed') {
    return 'registration_failed'
  }
  if (
    state.registrationState === 'skipped' ||
    state.registrationState === 'notes_missing'
  ) {
    return 'registration_required'
  }
  return state.registrationState === 'registered' ? 'registered' : 'uploading'
}
