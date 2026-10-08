import axios, { type AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export type ServiceVersionStatus = 'current' | 'outdated' | 'unknown' | 'updating' | 'failed'

export type ServiceVersionOperationStatus =
  'queued' | 'running' | 'succeeded' | 'failed' | 'recovering'

export interface AdminServiceVersion {
  id: string
  displayName: string
  role: string
  image: string
  currentVersion: string
  latestVersion: string
  status: ServiceVersionStatus
  releaseUrl: string
  isSelf: boolean
  canUpgrade: boolean
  lastCheckedAt: string
  updatedAt: string
}

export interface ServiceVersionSnapshot {
  services: AdminServiceVersion[]
  checkedAt: string
  allUpToDate: boolean
}

export interface ServiceVersionRelease {
  version: string
  releaseUrl: string
  publishedAt: string
  isCurrent: boolean
  isLatest: boolean
}

export interface AdminServiceVersionOperation {
  id: string
  targetService: string
  currentVersion: string
  targetVersion: string
  status: ServiceVersionOperationStatus
  message: string
  startedAt: string
  finishedAt: string
  logTail: string
}

export interface AdminVersionManagementClient {
  loadServiceVersions(): Promise<ServiceVersionSnapshot>
  checkServiceVersions(): Promise<ServiceVersionSnapshot>
  loadServiceReleases(serviceId: string): Promise<ServiceVersionRelease[]>
  upgradeService(
    serviceId: string,
    targetVersion?: string,
  ): Promise<AdminServiceVersionOperation | null>
  upgradeAllServices(): Promise<AdminServiceVersionOperation | null>
  loadServiceVersionOperations(): Promise<AdminServiceVersionOperation[]>
  loadServiceVersionOperation(operationId: string): Promise<AdminServiceVersionOperation | null>
}

export class ReleaseCatalogNotReadyError extends Error {
  readonly retryable = true

  constructor() {
    super('版本目录正在准备，稍后刷新即可')
    this.name = 'ReleaseCatalogNotReadyError'
  }
}

/// Creates the client for the brand service inventory and its upgrade operations.
export function createAdminVersionManagementClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminVersionManagementClient {
  return {
    async loadServiceVersions(): Promise<ServiceVersionSnapshot> {
      const response = await httpClient.get('/api/v1/admin/service-versions')
      return toSnapshot(response.data?.data)
    },

    async checkServiceVersions(): Promise<ServiceVersionSnapshot> {
      const response = await httpClient.post('/api/v1/admin/service-versions/check')
      return toSnapshot(response.data?.data)
    },

    async loadServiceReleases(serviceId: string): Promise<ServiceVersionRelease[]> {
      let response
      try {
        response = await httpClient.get(
          `/api/v1/admin/service-versions/${encodeURIComponent(serviceId)}/releases`,
          { params: { page_size: 100 } },
        )
      } catch (caught: unknown) {
        if (releaseCatalogNotReady(caught)) {
          throw new ReleaseCatalogNotReadyError()
        }
        throw caught
      }
      return toReleases(response.data?.data)
    },

    async upgradeService(
      serviceId: string,
      targetVersion?: string,
    ): Promise<AdminServiceVersionOperation | null> {
      const normalizedTargetVersion = targetVersion?.trim() ?? ''
      const response = await httpClient.post(
        `/api/v1/admin/service-versions/${encodeURIComponent(serviceId)}/upgrade`,
        normalizedTargetVersion === '' ? undefined : { target_version: normalizedTargetVersion },
      )
      return toNullableOperation(response.data?.data?.operation)
    },

    async upgradeAllServices(): Promise<AdminServiceVersionOperation | null> {
      const response = await httpClient.post('/api/v1/admin/service-versions/upgrade')
      return toNullableOperation(response.data?.data?.operation)
    },

    async loadServiceVersionOperations(): Promise<AdminServiceVersionOperation[]> {
      const response = await httpClient.get('/api/v1/admin/service-version-operations')
      const records = response.data?.data?.operations
      return Array.isArray(records)
        ? records.flatMap((record: unknown) => {
            const operation = toNullableOperation(record)
            return operation === null ? [] : [operation]
          })
        : []
    },

    async loadServiceVersionOperation(
      operationId: string,
    ): Promise<AdminServiceVersionOperation | null> {
      const response = await httpClient.get(
        `/api/v1/admin/service-version-operations/${encodeURIComponent(operationId)}`,
      )
      return toNullableOperation(response.data?.data?.operation)
    },
  }
}

function releaseCatalogNotReady(error: unknown): boolean {
  if (!axios.isAxiosError(error)) {
    return false
  }
  const data = error.response?.data as
    | { error?: { code?: unknown } }
    | undefined
  return data?.error?.code === 'release_catalog_not_ready'
}

function toReleases(value: unknown): ServiceVersionRelease[] {
  const record = recordValue(value)
  const source = Array.isArray(record.releases)
    ? record.releases
    : Array.isArray(value)
      ? value
      : []

  return source.flatMap((release: unknown) => {
    const parsedRelease = toNullableRelease(release)
    return parsedRelease === null ? [] : [parsedRelease]
  })
}

function toNullableRelease(value: unknown): ServiceVersionRelease | null {
  const record = recordValue(value)
  const version = stringValue(record.version).trim()
  if (!version) {
    return null
  }

  return {
    version,
    releaseUrl: stringValue(record.release_url),
    publishedAt: stringValue(record.published_at),
    isCurrent: record.is_current === true,
    isLatest: record.is_latest === true,
  }
}

function toSnapshot(value: unknown): ServiceVersionSnapshot {
  const record = recordValue(value)
  const services = Array.isArray(record.services)
    ? record.services.flatMap((service: unknown) => {
        const parsedService = toNullableServiceVersion(service)
        return parsedService === null ? [] : [parsedService]
      })
    : []

  return {
    services,
    checkedAt: stringValue(record.checked_at),
    allUpToDate:
      typeof record.all_up_to_date === 'boolean'
        ? record.all_up_to_date
        : services.length > 0 && services.every((service) => service.status === 'current'),
  }
}

function toNullableServiceVersion(value: unknown): AdminServiceVersion | null {
  const record = recordValue(value)
  const id = stringValue(record.id).trim()
  if (!id) {
    return null
  }

  return {
    id,
    displayName: stringValue(record.display_name),
    role: stringValue(record.role),
    image: stringValue(record.image),
    currentVersion: stringValue(record.current_version),
    latestVersion: stringValue(record.latest_version),
    status: serviceVersionStatus(record.status),
    releaseUrl: stringValue(record.release_url),
    isSelf: record.is_self === true,
    canUpgrade: record.can_upgrade === true,
    lastCheckedAt: stringValue(record.last_checked_at),
    updatedAt: stringValue(record.updated_at),
  }
}

function toNullableOperation(value: unknown): AdminServiceVersionOperation | null {
  const record = recordValue(value)
  const id = stringValue(record.id).trim()
  if (!id) {
    return null
  }

  return {
    id,
    targetService: stringValue(record.target_service),
    currentVersion: stringValue(record.current_version),
    targetVersion: stringValue(record.target_version),
    status: operationStatus(record.status),
    message: stringValue(record.message),
    startedAt: stringValue(record.started_at),
    finishedAt: stringValue(record.finished_at),
    logTail: stringValue(record.log_tail),
  }
}

function serviceVersionStatus(value: unknown): ServiceVersionStatus {
  switch (value) {
    case 'current':
    case 'outdated':
    case 'unknown':
    case 'updating':
    case 'failed':
      return value
    default:
      return 'unknown'
  }
}

function operationStatus(value: unknown): ServiceVersionOperationStatus {
  switch (value) {
    case 'queued':
    case 'running':
    case 'succeeded':
    case 'failed':
    case 'recovering':
      return value
    default:
      return 'queued'
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
