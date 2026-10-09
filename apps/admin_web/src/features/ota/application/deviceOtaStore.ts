import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

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
  type OtaRelease,
  type OtaReleaseChannel,
  type OtaReleaseDetail,
  type OtaReleaseStatus,
  type OtaStatistics,
  type UpdateOtaReleaseInput,
} from '@/api/adminOta'
import { createHttpClient } from '@/api/httpClient'

export type OtaReleaseStatusFilter = OtaReleaseStatus | 'all'
export type OtaReleaseChannelFilter = OtaReleaseChannel | 'all'

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
  inProgressDeploymentCount: 0,
  succeededDeploymentCount: 0,
  failedDeploymentCount: 0,
  rolledBackDeploymentCount: 0,
  rollbackRatePercent: 0,
  generatedAt: '',
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
  const deploymentStatus = ref('all')
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

  const pageCount = computed(() =>
    releasesTotal.value <= 0
      ? 1
      : Math.max(1, Math.ceil(releasesTotal.value / filters.value.pageSize)),
  )
  const visibleDeployments = computed(() => {
    const deployments = detail.value?.deployments ?? []
    if (deploymentStatus.value === 'all') {
      return deployments
    }
    return deployments.filter((deployment) => deployment.status === deploymentStatus.value)
  })
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
    isLoadingDetail.value = true
    error.value = null
    try {
      detail.value = await client.loadRelease(normalizedReleaseId)
      selectedReleaseId.value = normalizedReleaseId
      deploymentStatus.value = 'all'
      deploymentPage.value = 1
      deploymentTotal.value = detail.value.deploymentsTotal
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoadingDetail.value = false
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
    const nextPage = Math.min(Math.max(1, page), deploymentPageCount.value)
    isLoadingDetail.value = true
    error.value = null
    try {
      const result = await client.loadDeployments(releaseId, {
        page: nextPage,
        pageSize: filters.value.pageSize,
      })
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
      error.value = mapApiError(caught)
    } finally {
      isLoadingDetail.value = false
    }
  }

  async function createRelease(input: CreateOtaReleaseInput): Promise<boolean> {
    return submitAction('固件版本已创建。', async () => {
      await client.createRelease(input)
      await load()
    })
  }

  async function updateRelease(releaseId: string, input: UpdateOtaReleaseInput): Promise<boolean> {
    return submitAction('固件版本已更新。', async () => {
      await client.updateRelease(releaseId, input)
      await load()
      await loadReleaseDetail(releaseId)
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

  async function rollbackRelease(release: OtaRelease, reason: string): Promise<boolean> {
    return submitReleaseAction(
      release,
      () => client.rollbackRelease(release.releaseId, reason),
      '回滚任务已创建。',
    )
  }

  async function deleteRelease(release: OtaRelease): Promise<boolean> {
    return submitAction('固件版本已删除。', async () => {
      await client.deleteRelease(release.releaseId)
      if (selectedReleaseId.value === release.releaseId) {
        detail.value = null
        selectedReleaseId.value = ''
      }
      await load()
    })
  }

  async function retryDeployment(deployment: OtaDeployment): Promise<boolean> {
    return submitAction('设备升级任务已重新下发。', async () => {
      await client.retryDeployment(deployment.deploymentId)
      await refreshSelectedRelease()
      await load()
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
      const file = await downloadFilesClient.uploadFile(
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
      uploadProgress.value = 100
      await loadFirmwareFiles()
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
    selectedReleaseId.value = ''
    detail.value = null
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
  }

  async function submitReleaseAction(
    release: OtaRelease,
    action: () => Promise<OtaRelease>,
    successMessage: string,
  ): Promise<boolean> {
    return submitAction(successMessage, async () => {
      await action()
      await load()
      await loadReleaseDetail(release.releaseId)
    })
  }

  async function submitAction(
    successMessage: string,
    action: () => Promise<void>,
  ): Promise<boolean> {
    if (isSubmitting.value) {
      return false
    }
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    try {
      await action()
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
    applyFilters,
    clearFilters,
    clearMessages,
    closeRelease,
    createRelease,
    deleteRelease,
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
    selectedReleaseId,
    statistics,
    statisticsDegraded,
    updateRelease,
    uploadFirmware,
    uploadProgress,
    uploading,
    visibleDeployments,
    withdrawRelease,
  }
})

export function otaReleaseStatusLabel(value: OtaReleaseStatus): string {
  return {
    draft: '草稿',
    published: '已发布',
    paused: '已暂停',
    withdrawn: '已撤回',
    rolled_back: '已回滚',
  }[value]
}

export function otaReleaseChannelLabel(value: OtaReleaseChannel): string {
  return {
    stable: '稳定渠道',
    canary: '灰度渠道',
    internal: '内部渠道',
  }[value]
}

export function otaDeploymentStatusLabel(value: string): string {
  return (
    {
      pending: '等待设备',
      downloading: '正在下载',
      installing: '正在安装',
      succeeded: '升级成功',
      failed: '升级失败',
      rolled_back: '已回滚',
      cancelled: '已取消',
    }[value] ?? '状态待确认'
  )
}

export function otaSignatureStatusLabel(value: string): string {
  return (
    {
      pending: '等待校验',
      verified: '签名有效',
      failed: '签名无效',
      missing: '缺少签名',
    }[value] ?? '签名待确认'
  )
}

export function otaAuditActionLabel(value: string): string {
  return (
    {
      create: '创建版本',
      update: '更新配置',
      publish: '发布版本',
      pause: '暂停发布',
      withdraw: '撤回版本',
      rollback: '回滚版本',
      delete: '删除版本',
    }[value] ?? '版本操作'
  )
}
