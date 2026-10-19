import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { apiNoticeWarning } from '@/api/apiNotice'
import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createAdminVersionManagementClient,
  ReleaseCatalogNotReadyError,
  type AdminServiceVersion,
  type AdminServiceVersionOperation,
  type ServiceVersionRelease,
  type ServiceVersionSnapshot,
} from '@/api/adminVersionManagement'
import { createHttpClient } from '@/api/httpClient'

const activeOperationStatuses = new Set(['queued', 'running', 'recovering'])
const VERSION_CHECK_POLL_ATTEMPTS = 10
const VERSION_CHECK_POLL_INTERVAL_MS = 250
const upgradeWorkerDependencyId = 'upgrade_worker'

export function isUpgradeableService(service: AdminServiceVersion): boolean {
  return service.upgradeSupported && service.upgradeExecutable
}

export interface ServiceReleaseState {
  releases: ServiceVersionRelease[]
  selectedVersion: string
  isLoading: boolean
  error: string
}

/// Owns the brand-wide service inventory and the state of upgrade operations.
///
/// The page drives polling while an operation is active and stops it on
/// unmount. This keeps requests scoped to the visible operations page.
export const useVersionManagementStore = defineStore('admin-version-management', () => {
  const client = createAdminVersionManagementClient(createHttpClient())
  const snapshot = ref<ServiceVersionSnapshot | null>(null)
  const operations = ref<AdminServiceVersionOperation[]>([])
  const selectedOperation = ref<AdminServiceVersionOperation | null>(null)
  const isLoading = ref(false)
  const isChecking = ref(false)
  const isUpgradingAll = ref(false)
  const isRefreshingOperations = ref(false)
  const upgradingServiceIds = ref<string[]>([])
  const releaseStates = ref<Record<string, ServiceReleaseState>>({})
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')
  const checkWarning = ref('')
  const lastWarning = ref('')
  let operationRequestGeneration = 0

  const services = computed(() => {
    const reportedServices = snapshot.value?.services ?? []
    if (
      snapshot.value === null ||
      reportedServices.some((service) => service.id === upgradeWorkerDependencyId)
    ) {
      return reportedServices
    }
    return [
      ...reportedServices,
      upgradeWorkerDependencyRow(snapshot.value.checkedAt),
    ]
  })
  const outdatedServices = computed(() =>
    services.value.filter(
      (service) =>
        service.status === 'outdated' &&
        service.canUpgrade &&
        isUpgradeableService(service),
    ),
  )
  const hasActiveOperations = computed(() =>
    operations.value.some((operation) => activeOperationStatuses.has(operation.status)),
  )
  const isUpgrading = computed(() => isUpgradingAll.value || upgradingServiceIds.value.length > 0)

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      const loadedSnapshot = await client.loadServiceVersions()
      snapshot.value = loadedSnapshot
      operations.value = await client.loadServiceVersionOperations()
      await loadReleaseStates(services.value)
      checkWarning.value = hasUncheckedSnapshot(loadedSnapshot)
        ? '检查尚未返回，正在显示上次检查结果。请稍后重新检查。'
        : ''
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoading.value = false
    }
  }

  async function checkForUpdates(): Promise<void> {
    isChecking.value = true
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    checkWarning.value = ''
    try {
      const previousCheckedAt = snapshot.value?.checkedAt ?? ''
      const checkResponse = await client.checkServiceVersions()
      const requestedSnapshot = checkResponse.value
      lastWarning.value = apiNoticeWarning(checkResponse.notice)
      // The worker refreshes asynchronously. A timeout is not a successful
      // check: preserve the previous snapshot and expose a retryable state.
      const checkResult = await waitForCheckedSnapshot(
        requestedSnapshot,
        previousCheckedAt,
        () => client.loadServiceVersions(),
      )
      if (!checkResult.confirmed) {
        // Keep the last confirmed snapshot. Replacing it with an unchanged
        // response would make the old timestamp look like a fresh check.
        checkWarning.value = '检查尚未返回，正在显示上次检查结果。请稍后重新检查。'
        return
      }
      snapshot.value = checkResult.snapshot
      checkWarning.value = ''
      const count = outdatedServices.value.length
      lastMessage.value =
        count === 0 ? '所有服务都已是当前版本。' : `检查完成，有 ${count} 个服务可以升级。`
      void refreshAfterVersionCheck()
    } catch (caught: unknown) {
      const mappedError = mapApiError(caught)
      if (mappedError.retryable && snapshot.value !== null) {
        checkWarning.value = '检查尚未返回，正在显示上次检查结果。请稍后重新检查。'
      } else {
        error.value = mappedError
      }
    } finally {
      isChecking.value = false
    }
  }

  async function refreshAfterVersionCheck(): Promise<void> {
    try {
      operations.value = await client.loadServiceVersionOperations()
      await loadReleaseStates(services.value)
    } catch (caught: unknown) {
      // A release catalog failure must not turn a completed version check
      // into a misleading timeout. It is a secondary refresh failure only.
      void caught
    }
  }

  async function upgradeService(
    service: AdminServiceVersion,
    targetVersion?: string,
  ): Promise<boolean> {
    if (!isUpgradeableService(service)) {
      return false
    }
    const releaseState = releaseStates.value[service.id]
    const requestedVersion = (targetVersion ?? releaseState?.selectedVersion ?? '').trim()
    if (
      requestedVersion === '' ||
      requestedVersion === service.currentVersion ||
      isServiceUpgrading(service.id)
    ) {
      return false
    }
    upgradingServiceIds.value = [...upgradingServiceIds.value, service.id]
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    checkWarning.value = ''
    try {
      const result = await client.upgradeService(service.id, requestedVersion)
      const operation = result.value
      if (operation === null) {
        throw new Error('missing service upgrade operation')
      }
      lastWarning.value = apiNoticeWarning(result.notice)
      upsertOperation(operation)
      selectedOperation.value = operation
      lastMessage.value = `${service.displayName || service.id} 的升级已开始。`
      await refreshOperations()
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      upgradingServiceIds.value = upgradingServiceIds.value.filter(
        (serviceId) => serviceId !== service.id,
      )
    }
  }

  async function upgradeAll(): Promise<boolean> {
    if (outdatedServices.value.length === 0 || isUpgradingAll.value) {
      return false
    }
    isUpgradingAll.value = true
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const result = await client.upgradeAllServices()
      const operation = result.value
      if (operation === null) {
        throw new Error('missing all-service upgrade operation')
      }
      lastWarning.value = apiNoticeWarning(result.notice)
      upsertOperation(operation)
      selectedOperation.value = operation
      lastMessage.value = '全部服务升级已开始，完成后会自动更新状态。'
      await refreshOperations()
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isUpgradingAll.value = false
    }
  }

  /// Refreshes operation rows and reloads service state after completion.
  async function refreshOperations(): Promise<void> {
    if (isRefreshingOperations.value) {
      return
    }
    isRefreshingOperations.value = true
    try {
      const previousStatuses = new Map(
        operations.value.map((operation) => [operation.id, operation.status]),
      )
      const nextOperations = await client.loadServiceVersionOperations()
      operations.value = sortOperations(nextOperations)
      const completedOperation = operations.value.find((operation) => {
        const previousStatus = previousStatuses.get(operation.id)
        return (
          previousStatus !== undefined &&
          activeOperationStatuses.has(previousStatus) &&
          !activeOperationStatuses.has(operation.status)
        )
      })
      if (completedOperation !== undefined) {
        selectedOperation.value = completedOperation
        lastMessage.value =
          completedOperation.status === 'succeeded'
            ? '服务升级已完成。'
            : completedOperation.message || '服务升级没有完成，请查看操作记录后重试。'
      }
      if (!hasActiveOperations.value) {
        snapshot.value = await client.loadServiceVersions()
        await loadReleaseStates(services.value)
      }
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isRefreshingOperations.value = false
    }
  }

  async function selectOperation(operationId: string): Promise<void> {
    const normalizedOperationId = operationId.trim()
    if (normalizedOperationId === '') {
      return
    }
    const requestGeneration = ++operationRequestGeneration
    error.value = null
    try {
      const operation = await client.loadServiceVersionOperation(normalizedOperationId)
      if (requestGeneration !== operationRequestGeneration) {
        return
      }
      if (operation !== null) {
        upsertOperation(operation)
        selectedOperation.value = operation
      }
    } catch (caught: unknown) {
      if (requestGeneration === operationRequestGeneration) {
        error.value = mapApiError(caught)
      }
    }
  }

  function closeOperation(): void {
    operationRequestGeneration += 1
    selectedOperation.value = null
  }

  function isServiceUpgrading(serviceId: string): boolean {
    return upgradingServiceIds.value.includes(serviceId)
  }

  function releaseStateForService(serviceId: string): ServiceReleaseState {
    return (
      releaseStates.value[serviceId] ?? {
        releases: [],
        selectedVersion: '',
        isLoading: false,
        error: '',
      }
    )
  }

  async function loadServiceReleases(service: AdminServiceVersion): Promise<void> {
    if (!isUpgradeableService(service)) {
      return
    }
    releaseStates.value = {
      ...releaseStates.value,
      [service.id]: {
        ...releaseStateForService(service.id),
        isLoading: true,
        error: '',
      },
    }
    try {
      const releases = await client.loadServiceReleases(service.id)
      const selectedVersion = selectableVersion(releases, service)
      releaseStates.value = {
        ...releaseStates.value,
        [service.id]: {
          releases,
          selectedVersion,
          isLoading: false,
          error: '',
        },
      }
    } catch (caught: unknown) {
      const errorMessage =
        caught instanceof ReleaseCatalogNotReadyError ? caught.message : mapApiError(caught).message
      releaseStates.value = {
        ...releaseStates.value,
        [service.id]: {
          ...releaseStateForService(service.id),
          isLoading: false,
          error: errorMessage,
        },
      }
    }
  }

  async function loadReleaseStates(targetServices: AdminServiceVersion[]): Promise<void> {
    const upgradeableServices = targetServices.filter((service) => isUpgradeableService(service))
    await Promise.all(upgradeableServices.map((service) => loadServiceReleases(service)))
  }

  function selectServiceVersion(serviceId: string, version: string): void {
    releaseStates.value = {
      ...releaseStates.value,
      [serviceId]: {
        ...releaseStateForService(serviceId),
        selectedVersion: version,
      },
    }
  }

  function operationForService(serviceId: string): AdminServiceVersionOperation | null {
    return operations.value.find((operation) => operation.targetService === serviceId) ?? null
  }

  function upsertOperation(operation: AdminServiceVersionOperation): void {
    const existingIndex = operations.value.findIndex((item) => item.id === operation.id)
    const nextOperations = [...operations.value]
    if (existingIndex === -1) {
      nextOperations.unshift(operation)
    } else {
      nextOperations[existingIndex] = operation
    }
    operations.value = sortOperations(nextOperations)
  }

  return {
    checkWarning,
    closeOperation,
    error,
    hasActiveOperations,
    isChecking,
    isLoading,
    isServiceUpgrading,
    isUpgrading,
    isUpgradingAll,
    lastMessage,
    lastWarning,
    loadServiceReleases,
    operationForService,
    operations,
    outdatedServices,
    releaseStateForService,
    refreshOperations,
    selectedOperation,
    selectServiceVersion,
    selectOperation,
    services,
    snapshot,
    checkForUpdates,
    load,
    upgradeAll,
    upgradeService,
  }
})

function sortOperations(
  operations: AdminServiceVersionOperation[],
): AdminServiceVersionOperation[] {
  return [...operations].sort((left, right) => right.startedAt.localeCompare(left.startedAt))
}

function selectableVersion(
  releases: ServiceVersionRelease[],
  service: AdminServiceVersion,
): string {
  const latestRelease = releases.find(
    (release) => release.isLatest && release.version !== service.currentVersion,
  )
  if (latestRelease !== undefined) {
    return latestRelease.version
  }

  const newestRelease = releases.find((release) => release.version !== service.currentVersion)
  if (newestRelease !== undefined) {
    return newestRelease.version
  }

  if (releases.length === 0 && service.latestVersion !== service.currentVersion) {
    return service.latestVersion
  }

  return ''
}

interface CheckedSnapshotResult {
  snapshot: ServiceVersionSnapshot
  confirmed: boolean
}

async function waitForCheckedSnapshot(
  requestedSnapshot: ServiceVersionSnapshot,
  previousCheckedAt: string,
  loadSnapshot: () => Promise<ServiceVersionSnapshot>,
): Promise<CheckedSnapshotResult> {
  if (snapshotAdvanced(requestedSnapshot, previousCheckedAt)) {
    return { snapshot: requestedSnapshot, confirmed: true }
  }
  for (let attempt = 0; attempt < VERSION_CHECK_POLL_ATTEMPTS; attempt += 1) {
    const nextSnapshot = await loadSnapshot()
    if (snapshotAdvanced(nextSnapshot, previousCheckedAt)) {
      return { snapshot: nextSnapshot, confirmed: true }
    }
    await delay(VERSION_CHECK_POLL_INTERVAL_MS)
  }
  return { snapshot: requestedSnapshot, confirmed: false }
}

function snapshotAdvanced(snapshot: ServiceVersionSnapshot, previousCheckedAt: string): boolean {
  const previous = Date.parse(previousCheckedAt)
  const current = Date.parse(snapshot.checkedAt)
  return Number.isFinite(current) && (!Number.isFinite(previous) || current > previous)
}

function hasUncheckedSnapshot(snapshot: ServiceVersionSnapshot): boolean {
  return snapshot.checkedAt.trim() === ''
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, milliseconds)
  })
}

/**
 * The upgrade executor is deployed by Compose but is not yet represented in
 * the backend service catalog. Keep it visible as an explicit dependency gap
 * without inventing an image tag, version, or upgrade action.
 */
function upgradeWorkerDependencyRow(checkedAt: string): AdminServiceVersion {
  return {
    id: upgradeWorkerDependencyId,
    displayName: '服务升级执行器',
    role: '执行后台升级任务',
    image: 'ghcr.io/tissyboxc/sprout-upgrade-worker',
    currentVersion: '',
    latestVersion: '',
    status: 'unknown',
    releaseUrl: '',
    isSelf: false,
    upgradeSupported: false,
    upgradeExecutable: false,
    canUpgrade: false,
    lastCheckedAt: checkedAt,
    updatedAt: checkedAt,
    catalogDependencyMissing: true,
  }
}
