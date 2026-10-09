import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export type OtaReleaseChannel = 'stable' | 'canary' | 'internal'
export type OtaReleaseStatus = 'draft' | 'published' | 'paused' | 'withdrawn'
export type OtaTargetType = 'all' | 'device' | 'group'
export type OtaSignatureStatus = 'pending' | 'verified' | 'failed' | 'missing'
export type OtaSignatureAlgorithm = 'ed25519'
export type OtaDeploymentStatus =
  | 'queued'
  | 'offered'
  | 'downloading'
  | 'validating'
  | 'installing'
  | 'pending_verify'
  | 'succeeded'
  | 'failed'
  | 'rolled_back'

export interface OtaRelease {
  releaseId: string
  firmwareVersion: string
  hardwareRevision: string
  channel: OtaReleaseChannel
  status: OtaReleaseStatus
  artifactKey: string
  artifactUrl: string
  sha256: string
  sizeBytes: number
  signatureKeyId: string
  signatureAlgorithm: OtaSignatureAlgorithm
  signature: string
  signatureStatus: OtaSignatureStatus
  rollbackAllowed: boolean
  minSourceVersion: string
  publishedAt: string
  canaryPercent: number
  targetType: OtaTargetType
  targetId: string
  releaseNotes: string
  recordVersion: number
  createdAt: string
  updatedAt: string
}

export interface OtaDeployment {
  deploymentId: string
  releaseId: string
  deviceId: string
  deviceName: string
  hardwareRevision: string
  status: OtaDeploymentStatus
  progressPercent: number
  errorCode: string
  errorMessage: string
  retryCount: number
  createdAt: string
  updatedAt: string
  completedAt: string
}

export interface OtaRollbackTarget {
  allowed: boolean
  targetVersion: string
}

export interface OtaReleaseDetail {
  release: OtaRelease
  versions: OtaRelease[]
  rollback: OtaRollbackTarget
  deployments: OtaDeployment[]
  deploymentsTotal: number
}

export interface OtaStatistics {
  publishedReleaseCount: number
  canaryReleaseCount: number
  activeDeploymentCount: number
  succeededDeploymentCount: number
  failedDeploymentCount: number
  rollbackCount: number
  failureRate: number
  rollbackRate: number
}

export interface OtaReleaseFilters {
  status: OtaReleaseStatus | 'all'
  channel: OtaReleaseChannel | 'all'
  firmwareVersion: string
  page: number
  pageSize: number
}

export interface OtaReleaseListResult {
  releases: OtaRelease[]
  total: number
  page: number
  pageSize: number
}

export interface OtaDeploymentListFilters {
  status: OtaDeploymentStatus | 'all'
  page: number
  pageSize: number
}

export interface OtaDeploymentListResult {
  deployments: OtaDeployment[]
  total: number
  page: number
  pageSize: number
}

export interface CreateOtaReleaseInput {
  firmwareVersion: string
  hardwareRevision: string
  channel: OtaReleaseChannel
  artifactKey: string
  artifactUrl: string
  sha256: string
  sizeBytes: number
  signatureKeyId: string
  signatureAlgorithm: OtaSignatureAlgorithm
  signature: string
  rollbackAllowed: boolean
  minSourceVersion: string
  releaseNotes: string
  targetType: OtaTargetType
  targetId: string
  canaryPercent: number
}

export interface UpdateOtaReleaseInput extends CreateOtaReleaseInput {
  expectedVersion: number
}

export interface AdminOtaClient {
  loadReleases(filters: OtaReleaseFilters): Promise<OtaReleaseListResult>
  createRelease(input: CreateOtaReleaseInput): Promise<OtaRelease>
  loadRelease(releaseId: string): Promise<OtaReleaseDetail>
  updateRelease(releaseId: string, input: UpdateOtaReleaseInput): Promise<OtaRelease>
  publishRelease(releaseId: string): Promise<OtaRelease>
  pauseRelease(releaseId: string): Promise<OtaRelease>
  withdrawRelease(releaseId: string): Promise<OtaRelease>
  rollbackRelease(releaseId: string): Promise<OtaRelease>
  loadDeployments(
    releaseId: string,
    filters: OtaDeploymentListFilters,
  ): Promise<OtaDeploymentListResult>
  retryDeployment(deploymentId: string): Promise<OtaDeployment>
  loadStatistics(): Promise<OtaStatistics>
}

/// Creates the administrator client for device firmware OTA releases,
/// rollout state, deployment progress, and rollback operations.
export function createAdminOtaClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminOtaClient {
  const basePath = '/api/v1/admin/ota'

  return {
    async loadReleases(filters: OtaReleaseFilters): Promise<OtaReleaseListResult> {
      const response = await httpClient.get(`${basePath}/releases`, {
        params: compactParams({
          status: filters.status === 'all' ? undefined : filters.status,
          channel: filters.channel === 'all' ? undefined : filters.channel,
          version: filters.firmwareVersion.trim(),
          page: String(Math.max(1, filters.page)),
          page_size: String(Math.max(1, filters.pageSize)),
        }),
      })
      const payload = recordValue(response.data?.data)
      const releases = Array.isArray(payload.releases)
        ? payload.releases.flatMap((item: unknown) => {
            const release = toNullableRelease(item)
            return release === null ? [] : [release]
          })
        : []
      return {
        releases,
        total: nonNegativeInteger(payload.total, releases.length),
        page: positiveInteger(payload.page, filters.page),
        pageSize: positiveInteger(payload.page_size ?? payload.pageSize, filters.pageSize),
      }
    },

    async createRelease(input: CreateOtaReleaseInput): Promise<OtaRelease> {
      const response = await httpClient.post(`${basePath}/releases`, toCreatePayload(input))
      return expectRelease(response.data?.data)
    },

    async loadRelease(releaseId: string): Promise<OtaReleaseDetail> {
      const response = await httpClient.get(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}`,
      )
      const payload = recordValue(response.data?.data)
      const release = toNullableRelease(payload.release)
      if (release === null) {
        throw new Error('ota release response is missing a release')
      }
      const versions = Array.isArray(payload.versions)
        ? payload.versions.flatMap((item: unknown) => {
            const version = toNullableRelease(item)
            return version === null ? [] : [version]
          })
        : []
      const rollback = recordValue(payload.rollback)
      return {
        release,
        versions,
        rollback: {
          allowed: rollback.allowed === true,
          targetVersion: stringValue(rollback.target_version ?? rollback.targetVersion).trim(),
        },
        deployments: [],
        deploymentsTotal: 0,
      }
    },

    async updateRelease(releaseId: string, input: UpdateOtaReleaseInput): Promise<OtaRelease> {
      const response = await httpClient.put(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}`,
        toUpdatePayload(input),
      )
      return expectRelease(response.data?.data)
    },

    async publishRelease(releaseId: string): Promise<OtaRelease> {
      const response = await httpClient.post(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/publish`,
      )
      return expectRelease(response.data?.data)
    },

    async pauseRelease(releaseId: string): Promise<OtaRelease> {
      const response = await httpClient.post(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/pause`,
      )
      return expectRelease(response.data?.data)
    },

    async withdrawRelease(releaseId: string): Promise<OtaRelease> {
      const response = await httpClient.post(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/withdraw`,
      )
      return expectRelease(response.data?.data)
    },

    async rollbackRelease(releaseId: string): Promise<OtaRelease> {
      const response = await httpClient.post(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/rollback`,
      )
      return expectRelease(response.data?.data)
    },

    async loadDeployments(
      releaseId: string,
      filters: OtaDeploymentListFilters,
    ): Promise<OtaDeploymentListResult> {
      const response = await httpClient.get(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/deployments`,
        {
          params: compactParams({
            status: filters.status === 'all' ? undefined : filters.status,
            page: String(Math.max(1, filters.page)),
            page_size: String(Math.max(1, filters.pageSize)),
          }),
        },
      )
      const payload = recordValue(response.data?.data)
      const deployments = toDeployments(payload.deployments)
      return {
        deployments,
        total: nonNegativeInteger(payload.total, deployments.length),
        page: positiveInteger(payload.page, filters.page),
        pageSize: positiveInteger(payload.page_size ?? payload.pageSize, filters.pageSize),
      }
    },

    async retryDeployment(deploymentId: string): Promise<OtaDeployment> {
      const response = await httpClient.post(
        `${basePath}/deployments/${encodeURIComponent(deploymentId.trim())}/retry`,
      )
      const deployment = toNullableDeployment(response.data?.data)
      if (deployment === null) {
        throw new Error('ota deployment response is missing a deployment')
      }
      return deployment
    },

    async loadStatistics(): Promise<OtaStatistics> {
      const response = await httpClient.get(`${basePath}/statistics`)
      return toStatistics(response.data?.data)
    },
  }
}

function toCreatePayload(input: CreateOtaReleaseInput): Record<string, unknown> {
  return {
    firmware_version: input.firmwareVersion.trim(),
    hardware_revision: input.hardwareRevision.trim(),
    channel: input.channel,
    artifact_key: input.artifactKey.trim(),
    artifact_url: input.artifactUrl.trim(),
    sha256: input.sha256.trim().toLowerCase(),
    size_bytes: nonNegativeInteger(input.sizeBytes, 0),
    signature_key_id: input.signatureKeyId.trim(),
    signature_algorithm: input.signatureAlgorithm,
    signature: input.signature.trim(),
    rollback_allowed: input.rollbackAllowed,
    min_source_version: input.minSourceVersion.trim(),
    release_notes: input.releaseNotes.trim(),
    target_type: input.targetType,
    target_id: input.targetType === 'all' ? '' : input.targetId.trim(),
    canary_percent: boundedPercentage(input.canaryPercent),
  }
}

function toUpdatePayload(input: UpdateOtaReleaseInput): Record<string, unknown> {
  return {
    ...toCreatePayload(input),
    expected_version: positiveInteger(input.expectedVersion, 1),
  }
}

function expectRelease(value: unknown): OtaRelease {
  const release = toNullableRelease(value)
  if (release === null) {
    throw new Error('ota release response is missing a release')
  }
  return release
}

function toNullableRelease(value: unknown): OtaRelease | null {
  const record = recordValue(value)
  const target = recordValue(record.target)
  const releaseId = stringValue(record.release_id ?? record.releaseId ?? record.id).trim()
  const firmwareVersion = stringValue(
    record.firmware_version ?? record.firmwareVersion ?? record.version,
  ).trim()
  if (!releaseId && !firmwareVersion) {
    return null
  }
  const targetType = otaTargetType(
    record.target_type ?? record.targetType ?? target.scope ?? target.type,
  )
  return {
    releaseId: releaseId || firmwareVersion,
    firmwareVersion,
    hardwareRevision: stringValue(record.hardware_revision ?? record.hardwareRevision).trim(),
    channel: releaseChannel(record.channel),
    status: releaseStatus(record.status),
    artifactKey: stringValue(record.artifact_key ?? record.artifactKey).trim(),
    artifactUrl: stringValue(record.artifact_url ?? record.artifactUrl ?? record.url).trim(),
    sha256: stringValue(record.sha256).trim().toLowerCase(),
    sizeBytes: nonNegativeInteger(record.size_bytes ?? record.sizeBytes ?? record.size, 0),
    signatureKeyId: stringValue(
      record.signature_key_id ?? record.signatureKeyId ?? record.signing_key_id,
    ).trim(),
    signatureAlgorithm: signatureAlgorithm(
      record.signature_algorithm ?? record.signatureAlgorithm,
    ),
    signature: stringValue(record.signature).trim(),
    signatureStatus: signatureStatus(record.signature_status ?? record.signatureStatus),
    rollbackAllowed: record.rollback_allowed === true || record.rollbackAllowed === true,
    minSourceVersion: stringValue(
      record.min_source_version ??
        record.minSourceVersion ??
        record.minimum_supported_version ??
        record.minimumSourceVersion,
    ).trim(),
    publishedAt: stringValue(record.published_at ?? record.publishedAt),
    canaryPercent: boundedPercentage(
      record.canary_percent ??
        record.canaryPercent ??
        target.canary_percent ??
        target.canaryPercent,
    ),
    targetType,
    targetId: stringValue(
      record.target_id ??
        record.targetId ??
        target.device_id ??
        target.deviceId ??
        target.group_id ??
        target.groupId,
    ).trim(),
    releaseNotes: stringValue(record.release_notes ?? record.releaseNotes).trim(),
    recordVersion: positiveInteger(record.record_version ?? record.recordVersion, 1),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
  }
}

function toDeployments(value: unknown): OtaDeployment[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value.flatMap((item: unknown) => {
    const deployment = toNullableDeployment(item)
    return deployment === null ? [] : [deployment]
  })
}

function toNullableDeployment(value: unknown): OtaDeployment | null {
  const record = recordValue(value)
  const deploymentId = stringValue(record.deployment_id ?? record.deploymentId ?? record.id).trim()
  const deviceId = stringValue(record.device_id ?? record.deviceId).trim()
  if (!deploymentId && !deviceId) {
    return null
  }
  return {
    deploymentId: deploymentId || deviceId,
    releaseId: stringValue(record.release_id ?? record.releaseId).trim(),
    deviceId,
    deviceName: stringValue(record.device_name ?? record.deviceName).trim(),
    hardwareRevision: stringValue(record.hardware_revision ?? record.hardwareRevision).trim(),
    status: deploymentStatus(record.status),
    progressPercent: boundedPercentage(
      record.progress_percent ?? record.progressPercent ?? record.progress,
    ),
    errorCode: stringValue(record.error_code ?? record.errorCode).trim(),
    errorMessage: stringValue(record.error_message ?? record.errorMessage).trim(),
    retryCount: nonNegativeInteger(record.retry_count ?? record.retryCount, 0),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
    completedAt: stringValue(record.completed_at ?? record.completedAt),
  }
}

function toStatistics(value: unknown): OtaStatistics {
  const record = recordValue(value)
  return {
    publishedReleaseCount: nonNegativeInteger(
      record.published_release_count ?? record.publishedReleaseCount,
      0,
    ),
    canaryReleaseCount: nonNegativeInteger(
      record.canary_release_count ?? record.canaryReleaseCount,
      0,
    ),
    activeDeploymentCount: nonNegativeInteger(
      record.active_deployment_count ?? record.activeDeploymentCount,
      0,
    ),
    succeededDeploymentCount: nonNegativeInteger(
      record.succeeded_deployment_count ?? record.succeededDeploymentCount,
      0,
    ),
    failedDeploymentCount: nonNegativeInteger(
      record.failed_deployment_count ?? record.failedDeploymentCount,
      0,
    ),
    rollbackCount: nonNegativeInteger(record.rollback_count ?? record.rollbackCount, 0),
    failureRate: boundedRatio(record.failure_rate ?? record.failureRate),
    rollbackRate: boundedRatio(record.rollback_rate ?? record.rollbackRate),
  }
}

function compactParams(input: Record<string, string | undefined>): Record<string, string> {
  const params: Record<string, string> = {}
  for (const [key, value] of Object.entries(input)) {
    if (value !== undefined && value !== '') {
      params[key] = value
    }
  }
  return params
}

function releaseChannel(value: unknown): OtaReleaseChannel {
  switch (value) {
    case 'canary':
    case 'internal':
    case 'stable':
      return value
    default:
      return 'stable'
  }
}

function releaseStatus(value: unknown): OtaReleaseStatus {
  switch (value) {
    case 'published':
    case 'paused':
    case 'withdrawn':
    case 'draft':
      return value
    default:
      return 'draft'
  }
}

function otaTargetType(value: unknown): OtaTargetType {
  switch (value) {
    case 'device':
    case 'group':
    case 'all':
      return value
    case 'canary':
      return 'all'
    default:
      return 'all'
  }
}

function signatureStatus(value: unknown): OtaSignatureStatus {
  switch (value) {
    case 'verified':
    case 'failed':
    case 'missing':
    case 'pending':
      return value
    default:
      return 'missing'
  }
}

function signatureAlgorithm(_value: unknown): OtaSignatureAlgorithm {
  return 'ed25519'
}

function deploymentStatus(value: unknown): OtaDeploymentStatus {
  switch (value) {
    case 'queued':
    case 'offered':
    case 'downloading':
    case 'validating':
    case 'installing':
    case 'pending_verify':
    case 'succeeded':
    case 'failed':
    case 'rolled_back':
      return value
    default:
      return 'queued'
  }
}

function boundedPercentage(value: unknown): number {
  const number = Number(value)
  if (!Number.isFinite(number)) {
    return 0
  }
  return Math.min(100, Math.max(0, Math.round(number)))
}

function boundedRatio(value: unknown): number {
  const number = Number(value)
  if (!Number.isFinite(number)) {
    return 0
  }
  return Math.min(1, Math.max(0, number))
}

function nonNegativeInteger(value: unknown, fallback: number): number {
  const number = Number(value)
  return Number.isFinite(number) && number >= 0 ? Math.trunc(number) : fallback
}

function positiveInteger(value: unknown, fallback: number): number {
  const number = Number(value)
  return Number.isFinite(number) && number > 0 ? Math.trunc(number) : fallback
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}
