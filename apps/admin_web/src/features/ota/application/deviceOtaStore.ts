import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { apiNoticeWarning, readApiNotice } from '@/api/apiNotice'
import {
  createAdminDownloadFilesClient,
  type DownloadFile,
  type DownloadFileQuery,
} from '@/api/adminDownloadFiles'
import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createAdminOtaClient,
  type CreateOtaReleaseInput,
  type OtaDeployment,
  type OtaDeploymentStatus,
  type OtaRelease,
  type OtaReleaseChannel,
  type OtaReleaseDetail,
  type OtaReleaseStatus,
  type OtaStatistics,
  type OtaTargetType,
  type UpdateOtaReleaseInput,
} from '@/api/adminOta'
import { createHttpClient } from '@/api/httpClient'

export type OtaReleaseStatusFilter = OtaReleaseStatus | 'all'
export type OtaReleaseChannelFilter = OtaReleaseChannel | 'all'
export type OtaDeploymentStatusFilter = OtaDeploymentStatus | 'all'

export interface OtaReleaseFilters {
  status: OtaReleaseStatusFilter
  channel: OtaReleaseChannelFilter
  firmwareVersion: string
  page: number
  pageSize: number
}

export interface OtaFirmwareUploadInput {
  file: File
  filename: string
  firmwareVersion: string
  channel: OtaReleaseChannel
  hardwareRevision: string
  overwrite?: boolean
}

export const emptyOtaStatistics: OtaStatistics = {
  publishedReleaseCount: 0,
  canaryReleaseCount: 0,
  activeDeploymentCount: 0,
  succeededDeploymentCount: 0,
  failedDeploymentCount: 0,
  rollbackCount: 0,
  failureRate: 0,
  rollbackRate: 0,
}

/// Owns the complete device firmware OTA management surface: release
/// inventory and filters, release details, deployment pages, live statistics,
/// download-server firmware selection, and all rollout actions.
export const useDeviceOtaStore = defineStore('admin-device-ota', () => {
  const client = createAdminOtaClient(createHttpClient())
  const downloadFilesClient = createAdminDownloadFilesClient(createHttpClient())
  const releases = ref<OtaRelease[]>([])
  const releasesTotal = ref(0)
  const filters = ref<OtaReleaseFilters>({
    status: 'all',
    channel: 'all',
    firmwareVersion: '',
    page: 1,
    pageSize: 20,
  })
  const statistics = ref<OtaStatistics>({ ...emptyOtaStatistics })
  const detail = ref<OtaReleaseDetail | null>(null)
  const selectedReleaseId = ref('')
  const deploymentStatus = ref<OtaDeploymentStatusFilter>('all')
  const deploymentPage = ref(1)
  const deploymentTotal = ref(0)
  const firmwareFiles = ref<DownloadFile[]>([])
  const isLoading = ref(false)
  const isLoadingDetail = ref(false)
  const isLoadingFiles = ref(false)
  const isSubmitting = ref(false)
  const uploading = ref(false)
  const uploadProgress = ref(0)
  const statisticsDegraded = ref(false)
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')
  const lastWarning = ref('')
  let releaseRequestGeneration = 0
  let deploymentRequestGeneration = 0

  const pageCount = computed(() =>
    releasesTotal.value <= 0
      ? 1
      : Math.max(1, Math.ceil(releasesTotal.value / filters.value.pageSize)),
  )
  const deploymentPageCount = computed(() =>
    deploymentTotal.value <= 0
      ? 1
      : Math.max(1, Math.ceil(deploymentTotal.value / filters.value.pageSize)),
  )
  const firmwareDownloadFiles = computed(() =>
    firmwareFiles.value.filter((file) => file.kind === 'firmware'),
  )

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      const [releaseResult, statisticsResult] = await Promise.allSettled([
        client.loadReleases({
          status: filters.value.status,
          channel: filters.value.channel,
          firmwareVersion: filters.value.firmwareVersion,
          page: filters.value.page,
          pageSize: filters.value.pageSize,
        }),
        client.loadStatistics(),
      ])
      if (releaseResult.status === 'rejected') {
        throw releaseResult.reason
      }
      releases.value = releaseResult.value.releases
      releasesTotal.value = releaseResult.value.total
      filters.value = {
        ...filters.value,
        page: releaseResult.value.page,
        pageSize: releaseResult.value.pageSize,
      }
      if (statisticsResult.status === 'fulfilled') {
        statistics.value = statisticsResult.value
        statisticsDegraded.value = false
      } else {
        statisticsDegraded.value = true
      }
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoading.value = false
    }
  }

  async function loadReleaseDetail(releaseId: string): Promise<void> {
    const normalizedReleaseId = releaseId.trim()
    if (!normalizedReleaseId) {
      return
    }
    const requestGeneration = ++releaseRequestGeneration
    selectedReleaseId.value = normalizedReleaseId
    isLoadingDetail.value = true
    error.value = null
    try {
      const result = await client.loadRelease(normalizedReleaseId)
      if (
        requestGeneration !== releaseRequestGeneration ||
        selectedReleaseId.value !== normalizedReleaseId
      ) {
        return
      }
      detail.value = result
      deploymentStatus.value = 'all'
      deploymentPage.value = 1
      deploymentTotal.value = 0
      await loadDeploymentPage(1)
    } catch (caught: unknown) {
      if (
        requestGeneration === releaseRequestGeneration &&
        selectedReleaseId.value === normalizedReleaseId
      ) {
        error.value = mapApiError(caught)
      }
    } finally {
      if (
        requestGeneration === releaseRequestGeneration &&
        selectedReleaseId.value === normalizedReleaseId
      ) {
        isLoadingDetail.value = false
      }
    }
  }

  async function refreshSelectedRelease(): Promise<void> {
    if (selectedReleaseId.value) {
      await loadReleaseDetail(selectedReleaseId.value)
    }
  }

  async function loadDeploymentPage(page: number): Promise<void> {
    const releaseId = selectedReleaseId.value
    if (!releaseId) {
      return
    }
    const requestGeneration = ++deploymentRequestGeneration
    const nextPage = Math.min(Math.max(1, page), deploymentPageCount.value)
    isLoadingDetail.value = true
    error.value = null
    try {
      const result = await client.loadDeployments(releaseId, {
        status: deploymentStatus.value,
        page: nextPage,
        pageSize: filters.value.pageSize,
      })
      if (
        requestGeneration !== deploymentRequestGeneration ||
        selectedReleaseId.value !== releaseId
      ) {
        return
      }
      if (detail.value !== null) {
        detail.value = {
          ...detail.value,
          deployments: result.deployments,
          deploymentsTotal: result.total,
        }
      }
      deploymentPage.value = result.page
      deploymentTotal.value = result.total
    } catch (caught: unknown) {
      if (
        requestGeneration === deploymentRequestGeneration &&
        selectedReleaseId.value === releaseId
      ) {
        error.value = mapApiError(caught)
      }
    } finally {
      if (
        requestGeneration === deploymentRequestGeneration &&
        selectedReleaseId.value === releaseId
      ) {
        isLoadingDetail.value = false
      }
    }
  }

  async function applyDeploymentFilter(): Promise<void> {
    deploymentPage.value = 1
    await loadDeploymentPage(1)
  }

  async function createRelease(input: CreateOtaReleaseInput): Promise<boolean> {
    return submitAction('固件版本已创建。', async () => {
      const result = await client.createRelease(input)
      await load()
      return apiNoticeWarning(result.notice)
    })
  }

  async function updateRelease(releaseId: string, input: UpdateOtaReleaseInput): Promise<boolean> {
    return submitAction('固件版本已更新。', async () => {
      const result = await client.updateRelease(releaseId, input)
      await load()
      await loadReleaseDetail(releaseId)
      return apiNoticeWarning(result.notice)
    })
  }

  async function publishRelease(release: OtaRelease): Promise<boolean> {
    return submitReleaseAction(
      release,
      () => client.publishRelease(release.releaseId),
      '固件版本已发布。',
    )
  }

  async function pauseRelease(release: OtaRelease): Promise<boolean> {
    return submitReleaseAction(
      release,
      () => client.pauseRelease(release.releaseId),
      '发布已暂停。',
    )
  }

  async function withdrawRelease(release: OtaRelease): Promise<boolean> {
    return submitReleaseAction(
      release,
      () => client.withdrawRelease(release.releaseId),
      '固件版本已撤回。',
    )
  }

  async function rollbackRelease(release: OtaRelease): Promise<boolean> {
    return submitReleaseAction(
      release,
      () => client.rollbackRelease(release.releaseId),
      '回滚任务已创建。',
    )
  }

  async function retryDeployment(deployment: OtaDeployment): Promise<boolean> {
    return submitAction('设备升级任务已重新下发。', async () => {
      const result = await client.retryDeployment(deployment.deploymentId)
      await refreshSelectedRelease()
      await load()
      return apiNoticeWarning(result.notice)
    })
  }

  async function sendFirmwareToDevice(
    release: OtaRelease,
    deviceId: string,
  ): Promise<boolean> {
    const normalizedDeviceID = deviceId.trim()
    if (!normalizedDeviceID) {
      error.value = {
        kind: 'validation',
        message: '请输入设备的唯一标识',
        retryable: false,
      }
      return false
    }
    return submitAction('固件更新已下发，设备联网后会开始下载。', async () => {
      const deploymentResult = await client.createDeployment(
        release.releaseId,
        normalizedDeviceID,
      )
      // The OTA deployment is the durable authorization record. The runtime
      // command re-checks that exact record before delivery, so a failure here
      // is surfaced and the deployment remains visible for an explicit retry.
      const commandResponse = await createHttpClient().post(
        `/api/v1/admin/devices/${encodeURIComponent(normalizedDeviceID)}/commands`,
        {
          command_type: 'firmware_update',
          release_id: release.releaseId,
        },
      )
      await refreshSelectedRelease()
      await load()
      return joinWarnings(
        apiNoticeWarning(deploymentResult.notice),
        apiNoticeWarning(readApiNotice(commandResponse.data)),
      )
    })
  }

  async function loadFirmwareFiles(query: DownloadFileQuery = {}): Promise<void> {
    isLoadingFiles.value = true
    error.value = null
    try {
      firmwareFiles.value = await downloadFilesClient.loadFiles({
        ...query,
      })
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoadingFiles.value = false
    }
  }

  async function uploadFirmware(input: OtaFirmwareUploadInput): Promise<DownloadFile | null> {
    uploading.value = true
    uploadProgress.value = 0
    error.value = null
    lastMessage.value = ''
    try {
      const result = await downloadFilesClient.uploadFile(
        {
          file: input.file,
          filename: input.filename,
          version: input.firmwareVersion,
          kind: 'firmware',
          platform: 'esp32_s3',
          channel: input.channel,
          overwrite: input.overwrite === true,
        },
        (progress) => {
          uploadProgress.value = progress
        },
      )
      const file = result.value
      uploadProgress.value = 100
      await loadFirmwareFiles()
      lastWarning.value = apiNoticeWarning(result.notice)
      lastMessage.value = `${input.filename} 已上传到固件发布目录。`
      return file
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return null
    } finally {
      uploading.value = false
    }
  }

  function selectRelease(release: OtaRelease): void {
    selectedReleaseId.value = release.releaseId
    detail.value = null
    void loadReleaseDetail(release.releaseId)
  }

  function closeRelease(): void {
    releaseRequestGeneration += 1
    deploymentRequestGeneration += 1
    selectedReleaseId.value = ''
    detail.value = null
    isLoadingDetail.value = false
    deploymentStatus.value = 'all'
    deploymentPage.value = 1
    deploymentTotal.value = 0
  }

  async function applyFilters(): Promise<void> {
    filters.value = { ...filters.value, page: 1 }
    await load()
  }

  async function clearFilters(): Promise<void> {
    filters.value = {
      status: 'all',
      channel: 'all',
      firmwareVersion: '',
      page: 1,
      pageSize: filters.value.pageSize,
    }
    await load()
  }

  async function goToPage(page: number): Promise<void> {
    const nextPage = Math.min(Math.max(1, page), pageCount.value)
    if (nextPage === filters.value.page) {
      return
    }
    filters.value = { ...filters.value, page: nextPage }
    await load()
  }

  function clearMessages(): void {
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
  }

  async function submitReleaseAction(
    release: OtaRelease,
    action: () => Promise<{ value: OtaRelease; notice: import('@/api/apiNotice').ApiNotice | null }>,
    successMessage: string,
  ): Promise<boolean> {
    return submitAction(successMessage, async () => {
      const result = await action()
      await load()
      await loadReleaseDetail(release.releaseId)
      return apiNoticeWarning(result.notice)
    })
  }

  async function submitAction(
    successMessage: string,
    action: () => Promise<string | void>,
  ): Promise<boolean> {
    if (isSubmitting.value) {
      return false
    }
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      lastWarning.value = (await action()) ?? ''
      lastMessage.value = successMessage
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isSubmitting.value = false
    }
  }

  return {
    applyDeploymentFilter,
    applyFilters,
    clearFilters,
    clearMessages,
    closeRelease,
    createRelease,
    deploymentStatus,
    deploymentPage,
    deploymentPageCount,
    deploymentTotal,
    detail,
    error,
    filters,
    firmwareDownloadFiles,
    firmwareFiles,
    goToPage,
    isLoading,
    isLoadingDetail,
    isLoadingFiles,
    isSubmitting,
    lastMessage,
    lastWarning,
    load,
    loadDeploymentPage,
    loadFirmwareFiles,
    loadReleaseDetail,
    pageCount,
    pauseRelease,
    publishRelease,
    releases,
    releasesTotal,
    refreshSelectedRelease,
    retryDeployment,
    rollbackRelease,
    selectRelease,
    sendFirmwareToDevice,
    selectedReleaseId,
    statistics,
    statisticsDegraded,
    updateRelease,
    uploadFirmware,
    uploadProgress,
    uploading,
    visibleDeployments: computed(() => detail.value?.deployments ?? []),
    withdrawRelease,
  }
})

function joinWarnings(...warnings: string[]): string {
  return [...new Set(warnings.map((warning) => warning.trim()).filter(Boolean))].join(
    '\n',
  )
}

export function otaReleaseStatusLabel(value: OtaReleaseStatus): string {
  return {
    draft: '草稿',
    published: '已发布',
    paused: '已暂停',
    withdrawn: '已撤回',
  }[value]
}

export function otaReleaseChannelLabel(value: OtaReleaseChannel): string {
  return {
    stable: '稳定渠道',
    canary: '灰度渠道',
    internal: '内部渠道',
  }[value]
}

export function otaTargetTypeLabel(value: OtaTargetType): string {
  return {
    all: '全部设备',
    device: '指定设备',
    group: '设备分组',
  }[value]
}

export function otaDeploymentStatusLabel(value: string): string {
  return (
    {
      queued: '等待下发',
      offered: '已通知设备',
      downloading: '正在下载',
      validating: '正在校验',
      installing: '正在安装',
      pending_verify: '等待确认',
      succeeded: '升级成功',
      failed: '升级失败',
      rolled_back: '已回滚',
    }[value] ?? '状态待确认'
  )
}

export function otaSignatureStatusLabel(value: string): string {
  return (
    {
      pending: '等待验证',
      verified: '签名有效',
      failed: '签名无效',
      missing: '缺少签名',
    }[value] ?? '签名待确认'
  )
}

export function otaReleaseRailClass(value: OtaReleaseStatus): string {
  return value === 'withdrawn' ? 'rolled-back' : value
}

export function otaDeploymentRailClass(value: OtaDeploymentStatus): string {
  return value === 'rolled_back' ? 'rolled-back' : value
}

export function formatOtaRate(value: number): string {
  return `${Math.round(Math.min(1, Math.max(0, value)) * 100)}%`
}
