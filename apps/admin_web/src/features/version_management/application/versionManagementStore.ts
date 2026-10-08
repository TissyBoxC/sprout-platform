import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

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
export const UPGRADEABLE_SERVICE_IDS = [
  'sub2api',
  'device_platform',
  'voice_gateway',
  'admin_web',
] as const

const upgradeableServiceIds = new Set<string>(UPGRADEABLE_SERVICE_IDS)

export function isUpgradeableService(serviceId: string): boolean {
  return upgradeableServiceIds.has(serviceId)
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

  const services = computed(() => snapshot.value?.services ?? [])
  const outdatedServices = computed(() =>
    services.value.filter((service) => service.status === 'outdated' && service.canUpgrade),
  )
  const hasActiveOperations = computed(() =>
    operations.value.some((operation) => activeOperationStatuses.has(operation.status)),
  )
  const isUpgrading = computed(() => isUpgradingAll.value || upgradingServiceIds.value.length > 0)

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      snapshot.value = await client.loadServiceVersions()
      operations.value = await client.loadServiceVersionOperations()
      await loadReleaseStates(services.value)
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
    try {
      const requestedSnapshot = await client.checkServiceVersions()
      // The worker refreshes asynchronously. Poll briefly for a newer
      // checked_at instead of presenting the old snapshot as the result.
      snapshot.value = await waitForCheckedSnapshot(
        requestedSnapshot,
        () => client.loadServiceVersions(),
      )
      operations.value = await client.loadServiceVersionOperations()
      await loadReleaseStates(services.value)
      const count = outdatedServices.value.length
      lastMessage.value =
        count === 0 ? '所有服务都已是当前版本。' : `检查完成，有 ${count} 个服务可以升级。`
    } catch (caught: unknown) {
      const mappedError = mapApiError(caught)
      // A check request can fail while the last complete snapshot is still
      // useful. Keep the page readable and tell the operator what to retry.
      if (mappedError.retryable && snapshot.value !== null) {
        lastMessage.value = '暂时无法请求新的检查结果，正在显示上次检查结果。'
      } else {
        error.value = mappedError
      }
    } finally {
      isChecking.value = false
    }
  }

  async function upgradeService(
    service: AdminServiceVersion,
    targetVersion?: string,
  ): Promise<boolean> {
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
    try {
      const operation = await client.upgradeService(service.id, requestedVersion)
      if (operation === null) {
        throw new Error('missing service upgrade operation')
      }
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
    try {
      const operation = await client.upgradeAllServices()
      if (operation === null) {
        throw new Error('missing all-service upgrade operation')
      }
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
    error.value = null
    try {
      const operation = await client.loadServiceVersionOperation(operationId)
      if (operation !== null) {
        upsertOperation(operation)
        selectedOperation.value = operation
      }
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    }
  }

  function closeOperation(): void {
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
    if (!isUpgradeableService(service.id)) {
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
        caught instanceof ReleaseCatalogNotReadyError
          ? caught.message
          : mapApiError(caught).message
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
    const upgradeableServices = targetServices.filter((service) =>
      isUpgradeableService(service.id),
    )
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
    closeOperation,
    error,
    hasActiveOperations,
    isChecking,
    isLoading,
    isServiceUpgrading,
    isUpgrading,
    isUpgradingAll,
    lastMessage,
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
  const latestRelease = releases.find((release) => release.isLatest)
  if (latestRelease !== undefined) {
    return latestRelease.version
  }

  const newestRelease = releases.find((release) => release.version !== service.currentVersion)
  if (newestRelease !== undefined) {
    return newestRelease.version
  }

  return releases[0]?.version ?? service.latestVersion
}

async function waitForCheckedSnapshot(
  requestedSnapshot: ServiceVersionSnapshot,
  loadSnapshot: () => Promise<ServiceVersionSnapshot>,
): Promise<ServiceVersionSnapshot> {
  const requestedAt = Date.parse(requestedSnapshot.checkedAt)
  let latestSnapshot = requestedSnapshot
  for (let attempt = 0; attempt < 6; attempt += 1) {
    const nextSnapshot = await loadSnapshot()
    latestSnapshot = nextSnapshot
    const nextCheckedAt = Date.parse(nextSnapshot.checkedAt)
    if (
      Number.isFinite(nextCheckedAt) &&
      (!Number.isFinite(requestedAt) || nextCheckedAt > requestedAt)
    ) {
      return nextSnapshot
    }
    await delay(500)
  }
  return latestSnapshot
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, milliseconds)
  })
}
