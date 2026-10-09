import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export type OtaReleaseChannel = 'stable' | 'canary' | 'internal'
export type OtaReleaseStatus = 'draft' | 'published' | 'paused' | 'withdrawn' | 'rolled_back'
export type OtaSignatureStatus = 'pending' | 'verified' | 'failed' | 'missing'
export type OtaDeploymentStatus =
  'pending' | 'downloading' | 'installing' | 'succeeded' | 'failed' | 'rolled_back' | 'cancelled'

export interface OtaRelease {
  releaseId: string
  firmwareVersion: string
  hardwareRevision: string
  channel: OtaReleaseChannel
  status: OtaReleaseStatus
  rolloutPercentage: number
  targetGroup: string
  targetDeviceCount: number
  artifactUrl: string
  artifactFileName: string
  sha256: string
  signatureKeyId: string
  signatureStatus: OtaSignatureStatus
  sizeBytes: number
  rollbackAllowed: boolean
  minimumSourceVersion: string
  releaseNotes: string
  manifest: string
  createdBy: string
  createdAt: string
  publishedAt: string
  pausedAt: string
  rolledBackAt: string
  updatedAt: string
}

export interface OtaReleaseAuditEntry {
  id: string
  action: string
  status: string
  message: string
  actorAccountId: string
  createdAt: string
}

export interface OtaDeployment {
  deploymentId: string
  releaseId: string
  deviceId: string
  deviceName: string
  currentVersion: string
  targetVersion: string
  status: OtaDeploymentStatus
  progressPercent: number
  failureCode: string
  failureMessage: string
  lastEventAt: string
  startedAt: string
  finishedAt: string
}

export interface OtaReleaseDetail {
  release: OtaRelease
  deployments: OtaDeployment[]
  deploymentsTotal: number
  audits: OtaReleaseAuditEntry[]
}

export interface OtaStatistics {
  publishedReleaseCount: number
  canaryReleaseCount: number
  inProgressDeploymentCount: number
  succeededDeploymentCount: number
  failedDeploymentCount: number
  rolledBackDeploymentCount: number
  rollbackRatePercent: number
  generatedAt: string
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
  rolloutPercentage: number
  targetGroup: string
  artifactUrl: string
  artifactFileName: string
  sha256: string
  signatureKeyId: string
  sizeBytes: number
  rollbackAllowed: boolean
  minimumSourceVersion: string
  releaseNotes: string
}

export interface UpdateOtaReleaseInput {
  firmwareVersion: string
  hardwareRevision: string
  channel: OtaReleaseChannel
  rolloutPercentage: number
  targetGroup: string
  artifactUrl: string
  artifactFileName: string
  sha256: string
  signatureKeyId: string
  sizeBytes: number
  rollbackAllowed: boolean
  minimumSourceVersion: string
  releaseNotes: string
}

export interface AdminOtaClient {
  loadReleases(filters: OtaReleaseFilters): Promise<OtaReleaseListResult>
  createRelease(input: CreateOtaReleaseInput): Promise<OtaRelease>
  loadRelease(releaseId: string): Promise<OtaReleaseDetail>
  updateRelease(releaseId: string, input: UpdateOtaReleaseInput): Promise<OtaRelease>
  publishRelease(releaseId: string): Promise<OtaRelease>
  pauseRelease(releaseId: string): Promise<OtaRelease>
  withdrawRelease(releaseId: string): Promise<OtaRelease>
  rollbackRelease(releaseId: string, reason: string): Promise<OtaRelease>
  deleteRelease(releaseId: string): Promise<void>
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
          firmware_version: filters.firmwareVersion.trim(),
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
      return expectRelease(response.data?.data, 'release')
    },

    async loadRelease(releaseId: string): Promise<OtaReleaseDetail> {
      const response = await httpClient.get(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}`,
      )
      const payload = recordValue(response.data?.data)
      const release = toNullableRelease(payload.release ?? payload)
      if (release === null) {
        throw new Error('ota release response is missing a release')
      }
      const deployments = toDeployments(payload.deployments)
      const audits = toAudits(payload.audits)
      return {
        release,
        deployments,
        deploymentsTotal: nonNegativeInteger(
          payload.deployments_total ?? payload.deploymentsTotal,
          deployments.length,
        ),
        audits,
      }
    },

    async updateRelease(releaseId: string, input: UpdateOtaReleaseInput): Promise<OtaRelease> {
      const response = await httpClient.put(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}`,
        toUpdatePayload(input),
      )
      return expectRelease(response.data?.data, 'release')
    },

    async publishRelease(releaseId: string): Promise<OtaRelease> {
      const response = await httpClient.post(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/publish`,
      )
      return expectRelease(response.data?.data, 'release')
    },

    async pauseRelease(releaseId: string): Promise<OtaRelease> {
      const response = await httpClient.post(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/pause`,
      )
      return expectRelease(response.data?.data, 'release')
    },

    async withdrawRelease(releaseId: string): Promise<OtaRelease> {
      const response = await httpClient.post(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/withdraw`,
      )
      return expectRelease(response.data?.data, 'release')
    },

    async rollbackRelease(releaseId: string, reason: string): Promise<OtaRelease> {
      const response = await httpClient.post(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/rollback`,
        { reason: reason.trim() },
      )
      return expectRelease(response.data?.data, 'release')
    },

    async deleteRelease(releaseId: string): Promise<void> {
      await httpClient.delete(`${basePath}/releases/${encodeURIComponent(releaseId.trim())}`)
    },

    async loadDeployments(
      releaseId: string,
      filters: OtaDeploymentListFilters,
    ): Promise<OtaDeploymentListResult> {
      const response = await httpClient.get(
        `${basePath}/releases/${encodeURIComponent(releaseId.trim())}/deployments`,
        {
          params: {
            page: String(Math.max(1, filters.page)),
            page_size: String(Math.max(1, filters.pageSize)),
          },
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
      const payload = recordValue(response.data?.data)
      const deployment = toNullableDeployment(payload.deployment ?? payload)
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
    rollout_percentage: boundedPercentage(input.rolloutPercentage),
    target_group: input.targetGroup.trim(),
    artifact_url: input.artifactUrl.trim(),
    artifact_file_name: input.artifactFileName.trim(),
    sha256: input.sha256.trim(),
    signature_key_id: input.signatureKeyId.trim(),
    size_bytes: nonNegativeInteger(input.sizeBytes, 0),
    rollback_allowed: input.rollbackAllowed,
    min_supported_version: input.minimumSourceVersion.trim(),
    release_notes: input.releaseNotes.trim(),
  }
}

function toUpdatePayload(input: UpdateOtaReleaseInput): Record<string, unknown> {
  return {
    ...toCreatePayload(input),
  }
}

function expectRelease(value: unknown, key: string): OtaRelease {
  const payload = recordValue(value)
  const release = toNullableRelease(payload[key] ?? payload)
  if (release === null) {
    throw new Error('ota release response is missing a release')
  }
  return release
}

function toNullableRelease(value: unknown): OtaRelease | null {
  const record = recordValue(value)
  const releaseId = stringValue(record.release_id ?? record.releaseId ?? record.id).trim()
  const firmwareVersion = stringValue(
    record.firmware_version ?? record.firmwareVersion ?? record.version,
  ).trim()
  if (!releaseId && !firmwareVersion) {
    return null
  }
  return {
    releaseId: releaseId || firmwareVersion,
    firmwareVersion,
    hardwareRevision: stringValue(record.hardware_revision ?? record.hardwareRevision).trim(),
    channel: releaseChannel(record.channel),
    status: releaseStatus(record.status),
    rolloutPercentage: boundedPercentage(record.rollout_percentage ?? record.rolloutPercentage),
    targetGroup: stringValue(record.target_group ?? record.targetGroup).trim(),
    targetDeviceCount: nonNegativeInteger(
      record.target_device_count ?? record.targetDeviceCount,
      0,
    ),
    artifactUrl: stringValue(record.artifact_url ?? record.artifactUrl ?? record.url).trim(),
    artifactFileName: stringValue(
      record.artifact_file_name ?? record.artifactFileName ?? record.file_name ?? record.fileName,
    ).trim(),
    sha256: stringValue(record.sha256).trim(),
    signatureKeyId: stringValue(
      record.signature_key_id ?? record.signatureKeyId ?? record.signing_key_id,
    ).trim(),
    signatureStatus: signatureStatus(record.signature_status ?? record.signatureStatus),
    sizeBytes: nonNegativeInteger(record.size_bytes ?? record.sizeBytes ?? record.size, 0),
    rollbackAllowed: record.rollback_allowed === true || record.rollbackAllowed === true,
    minimumSourceVersion: stringValue(
      record.min_supported_version ??
        record.minimum_supported_version ??
        record.minimumSourceVersion,
    ).trim(),
    releaseNotes: stringValue(record.release_notes ?? record.releaseNotes).trim(),
    manifest: stringValue(record.manifest ?? record.manifest_json ?? record.manifestJson),
    createdBy: stringValue(record.created_by ?? record.createdBy).trim(),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    publishedAt: stringValue(record.published_at ?? record.publishedAt),
    pausedAt: stringValue(record.paused_at ?? record.pausedAt),
    rolledBackAt: stringValue(record.rolled_back_at ?? record.rolledBackAt),
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
    currentVersion: stringValue(
      record.current_version ?? record.currentVersion ?? record.from_version,
    ).trim(),
    targetVersion: stringValue(
      record.target_version ?? record.targetVersion ?? record.to_version,
    ).trim(),
    status: deploymentStatus(record.status),
    progressPercent: boundedPercentage(
      record.progress_percent ?? record.progressPercent ?? record.progress,
    ),
    failureCode: stringValue(record.failure_code ?? record.failureCode).trim(),
    failureMessage: stringValue(
      record.failure_message ?? record.failureMessage ?? record.message,
    ).trim(),
    lastEventAt: stringValue(record.last_event_at ?? record.lastEventAt),
    startedAt: stringValue(record.started_at ?? record.startedAt),
    finishedAt: stringValue(record.finished_at ?? record.finishedAt),
  }
}

function toAudits(value: unknown): OtaReleaseAuditEntry[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value.flatMap((item: unknown) => {
    const record = recordValue(item)
    const id = stringValue(record.id ?? record.audit_id ?? record.auditId).trim()
    const action = stringValue(record.action).trim()
    if (!id && !action) {
      return []
    }
    return [
      {
        id: id || `${action}-${stringValue(record.created_at)}`,
        action,
        status: stringValue(record.status).trim(),
        message: stringValue(record.message ?? record.detail).trim(),
        actorAccountId: stringValue(
          record.actor_account_id ?? record.actorAccountId ?? record.actor,
        ).trim(),
        createdAt: stringValue(record.created_at ?? record.createdAt),
      },
    ]
  })
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
    inProgressDeploymentCount: nonNegativeInteger(
      record.in_progress_deployment_count ?? record.inProgressDeploymentCount,
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
    rolledBackDeploymentCount: nonNegativeInteger(
      record.rolled_back_deployment_count ?? record.rolledBackDeploymentCount,
      0,
    ),
    rollbackRatePercent: boundedPercentage(
      record.rollback_rate_percent ?? record.rollbackRatePercent,
    ),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
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
    case 'rolled_back':
    case 'draft':
      return value
    default:
      return 'draft'
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
      return 'pending'
  }
}

function deploymentStatus(value: unknown): OtaDeploymentStatus {
  switch (value) {
    case 'pending':
    case 'downloading':
    case 'installing':
    case 'succeeded':
    case 'failed':
    case 'rolled_back':
    case 'cancelled':
      return value
    default:
      return 'pending'
  }
}

function boundedPercentage(value: unknown): number {
  const number = Number(value)
  if (!Number.isFinite(number)) {
    return 0
  }
  return Math.min(100, Math.max(0, Math.round(number)))
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
