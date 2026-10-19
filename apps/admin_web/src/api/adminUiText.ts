import type { AxiosInstance } from 'axios'

import {
  readApiNotice,
  type ApiMutationResult,
} from '@/api/apiNotice'
import { createHttpClient } from '@/api/httpClient'

export type UiTextPlatform = 'parent_app' | 'admin_web' | 'device_firmware'
export type UiTextStatus = 'draft' | 'in_review' | 'published' | 'withdrawn'

export interface UiTextEntry {
  uiTextKey: string
  value: string
  maxLength: number
  variables: string[]
}

/// One reviewable version of a UI text package. A logical package is addressed
/// by (package id, language, platform); versions are allocated by the backend.
export interface UiTextPackage {
  packageId: string
  packageVersion: number
  language: string
  platform: UiTextPlatform
  minClientVersion: string
  status: UiTextStatus
  signatureKeyId: string
  submittedAt: string
  approvedAt: string
  approvedBy: string
  publishedAt: string
  publishedBy: string
  withdrawnAt: string
  revision: number
  createdBy: string
  createdAt: string
  updatedAt: string
}

export interface UiTextVersionSummary {
  package: UiTextPackage
  entryCount: number
  isPublished: boolean
  isNewest: boolean
  publishedFor: boolean
}

export interface UiTextVersionPage {
  versions: UiTextVersionSummary[]
  page: number
  pageSize: number
  total: number
}

export interface UiTextDetail {
  package: UiTextPackage
  entries: UiTextEntry[]
}

export interface UiTextDraftInput {
  packageId: string
  language: string
  platform: UiTextPlatform
  minClientVersion: string
  entries: UiTextEntry[]
}

export interface UiTextVersionQuery {
  packageId?: string
  language?: string
  platform?: UiTextPlatform
  status?: UiTextStatus
  page?: number
  pageSize?: number
}

/// The identity that addresses one stored version. Edit, submit, review, and
/// publication routes all use it so a version can never be confused across a
/// platform or language.
export interface UiTextVersionRef {
  packageId: string
  language: string
  platform: UiTextPlatform
  packageVersion: number
}

export interface AdminUiTextClient {
  loadVersions(query?: UiTextVersionQuery): Promise<UiTextVersionPage>
  loadVersion(ref: UiTextVersionRef): Promise<UiTextDetail>
  createDraft(input: UiTextDraftInput): Promise<ApiMutationResult<UiTextDetail>>
  updateDraft(
    ref: UiTextVersionRef,
    input: UiTextDraftInput,
  ): Promise<ApiMutationResult<UiTextDetail>>
  submitVersion(ref: UiTextVersionRef): Promise<ApiMutationResult<UiTextPackage>>
  approveVersion(ref: UiTextVersionRef): Promise<ApiMutationResult<UiTextPackage>>
  rejectVersion(
    ref: UiTextVersionRef,
    reason: string,
  ): Promise<ApiMutationResult<UiTextPackage>>
  publishVersion(
    ref: UiTextVersionRef,
    signatureKeyId: string,
    signature: string,
  ): Promise<ApiMutationResult<UiTextPackage>>
  withdrawVersion(
    ref: UiTextVersionRef,
  ): Promise<ApiMutationResult<UiTextPackage>>
}

/// Creates the client for remote UI text management. Every mutation returns the
/// changed version so the page can refresh from the canonical detail endpoint.
export function createAdminUiTextClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminUiTextClient {
  return {
    async loadVersions(query: UiTextVersionQuery = {}): Promise<UiTextVersionPage> {
      const response = await httpClient.get('/api/v1/admin/ui-text/packages', {
        params: compactParams({
          package_id: query.packageId?.trim(),
          language: query.language?.trim(),
          platform: query.platform,
          status: query.status,
          page: query.page === undefined ? undefined : String(query.page),
          page_size: query.pageSize === undefined ? undefined : String(query.pageSize),
        }),
      })
      return toVersionPage(response.data?.data)
    },

    async loadVersion(ref: UiTextVersionRef): Promise<UiTextDetail> {
      const response = await httpClient.get(versionPath(ref))
      return requiredDetail(response.data?.data)
    },

    async createDraft(
      input: UiTextDraftInput,
    ): Promise<ApiMutationResult<UiTextDetail>> {
      const response = await httpClient.post('/api/v1/admin/ui-text/packages/drafts', {
        package_id: input.packageId.trim(),
        language: input.language.trim(),
        platform: input.platform,
        min_client_version: input.minClientVersion.trim(),
        entries: input.entries.map(entryPayload),
      })
      return {
        value: requiredDetail(response.data?.data),
        notice: readApiNotice(response.data),
      }
    },

    async updateDraft(
      ref: UiTextVersionRef,
      input: UiTextDraftInput,
    ): Promise<ApiMutationResult<UiTextDetail>> {
      const response = await httpClient.put(versionPath(ref), {
        min_client_version: input.minClientVersion.trim(),
        entries: input.entries.map(entryPayload),
      })
      return {
        value: requiredDetail(response.data?.data),
        notice: readApiNotice(response.data),
      }
    },

    async submitVersion(
      ref: UiTextVersionRef,
    ): Promise<ApiMutationResult<UiTextPackage>> {
      return runVersionAction(httpClient, ref, 'submit')
    },

    async approveVersion(
      ref: UiTextVersionRef,
    ): Promise<ApiMutationResult<UiTextPackage>> {
      return runVersionAction(httpClient, ref, 'approve')
    },

    async rejectVersion(
      ref: UiTextVersionRef,
      reason: string,
    ): Promise<ApiMutationResult<UiTextPackage>> {
      return runVersionAction(httpClient, ref, 'reject', { reason: reason.trim() })
    },

    async publishVersion(
      ref: UiTextVersionRef,
      signatureKeyId: string,
      signature: string,
    ): Promise<ApiMutationResult<UiTextPackage>> {
      return runVersionAction(httpClient, ref, 'publish', {
        signature_key_id: signatureKeyId.trim(),
        signature: signature.trim(),
      })
    },

    async withdrawVersion(
      ref: UiTextVersionRef,
    ): Promise<ApiMutationResult<UiTextPackage>> {
      return runVersionAction(httpClient, ref, 'withdraw')
    },
  }
}

async function runVersionAction(
  httpClient: AxiosInstance,
  ref: UiTextVersionRef,
  action: string,
  body?: Record<string, unknown>,
): Promise<ApiMutationResult<UiTextPackage>> {
  const response = await httpClient.post(`${versionPath(ref)}/${action}`, body ?? {})
  return {
    value: requiredPackage(response.data?.data),
    notice: readApiNotice(response.data),
  }
}

function versionPath(ref: UiTextVersionRef): string {
  const identity = [
    encodeURIComponent(ref.packageId),
    encodeURIComponent(ref.language),
    encodeURIComponent(ref.platform),
    String(ref.packageVersion),
  ].join('/')
  return `/api/v1/admin/ui-text/packages/${identity}`
}

function entryPayload(entry: UiTextEntry): Record<string, unknown> {
  return {
    ui_text_key: entry.uiTextKey.trim(),
    value: entry.value,
    max_length: entry.maxLength,
    variables: entry.variables,
  }
}

function toVersionPage(value: unknown): UiTextVersionPage {
  const record = recordValue(value)
  const versions = Array.isArray(record.versions) ? record.versions : []
  return {
    versions: versions.flatMap((item: unknown) => {
      const summary = toNullableVersionSummary(item)
      return summary === null ? [] : [summary]
    }),
    page: nonNegativeInteger(record.page, 1),
    pageSize: nonNegativeInteger(record.page_size ?? record.pageSize, 20),
    total: nonNegativeInteger(record.total, 0),
  }
}

function toNullableVersionSummary(value: unknown): UiTextVersionSummary | null {
  const record = recordValue(value)
  const pkg = toNullablePackage(record.package ?? value)
  if (pkg === null) {
    return null
  }
  return {
    package: pkg,
    entryCount: nonNegativeInteger(record.entry_count ?? record.entryCount, 0),
    isPublished: record.is_published === true || record.isPublished === true,
    isNewest: record.is_newest === true || record.isNewest === true,
    publishedFor: record.published_for === true || record.publishedFor === true,
  }
}

function requiredDetail(value: unknown): UiTextDetail {
  const record = recordValue(value)
  const pkg = toNullablePackage(record.package ?? value)
  if (pkg === null) {
    throw new Error('ui text response is missing a package version')
  }
  return {
    package: pkg,
    entries: toEntries(record.entries),
  }
}

function requiredPackage(value: unknown): UiTextPackage {
  const record = recordValue(value)
  const pkg = toNullablePackage(record.package ?? value)
  if (pkg === null) {
    throw new Error('ui text response is missing a package version')
  }
  return pkg
}

function toNullablePackage(value: unknown): UiTextPackage | null {
  const record = recordValue(value)
  const packageId = stringValue(record.package_id ?? record.packageId).trim()
  const packageVersion = nonNegativeInteger(record.package_version ?? record.packageVersion, 0)
  if (!packageId || packageVersion < 1) {
    return null
  }
  return {
    packageId,
    packageVersion,
    language: stringValue(record.language),
    platform: platformValue(record.platform),
    minClientVersion: stringValue(record.min_client_version ?? record.minClientVersion),
    status: statusValue(record.status),
    signatureKeyId: stringValue(record.signature_key_id ?? record.signatureKeyId),
    submittedAt: stringValue(record.submitted_at ?? record.submittedAt),
    approvedAt: stringValue(record.approved_at ?? record.approvedAt),
    approvedBy: stringValue(record.approved_by ?? record.approvedBy),
    publishedAt: stringValue(record.published_at ?? record.publishedAt),
    publishedBy: stringValue(record.published_by ?? record.publishedBy),
    withdrawnAt: stringValue(record.withdrawn_at ?? record.withdrawnAt),
    revision: nonNegativeInteger(record.revision, 0),
    createdBy: stringValue(record.created_by ?? record.createdBy),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
  }
}

function toEntries(value: unknown): UiTextEntry[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value.flatMap((item: unknown) => {
    const record = recordValue(item)
    const uiTextKey = stringValue(record.ui_text_key ?? record.uiTextKey).trim()
    if (!uiTextKey) {
      return []
    }
    return [
      {
        uiTextKey,
        value: stringValue(record.value),
        maxLength: positiveInteger(record.max_length ?? record.maxLength, 2000),
        variables: stringArray(record.variables),
      },
    ]
  })
}

function platformValue(value: unknown): UiTextPlatform {
  switch (value) {
    case 'admin_web':
    case 'device_firmware':
      return value
    default:
      return 'parent_app'
  }
}

function statusValue(value: unknown): UiTextStatus {
  switch (value) {
    case 'in_review':
    case 'published':
    case 'withdrawn':
      return value
    default:
      return 'draft'
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

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value.flatMap((item: unknown) =>
    typeof item === 'string' && item.trim() !== '' ? [item.trim()] : [],
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

function nonNegativeInteger(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed >= 0 ? Math.trunc(parsed) : fallback
}

function positiveInteger(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed >= 1 ? Math.trunc(parsed) : fallback
}
