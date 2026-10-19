import type { AxiosInstance } from 'axios'

import {
  readApiNotice,
  type ApiMutationResult,
} from '@/api/apiNotice'
import { createHttpClient } from '@/api/httpClient'

export type ContentCategory =
  'story' | 'nursery_rhyme' | 'poetry' | 'english' | 'encyclopedia' | 'bedtime'

export type ContentAgeTier = 'age_3_4' | 'age_5_6' | 'age_7_8'

export type ContentStatus = 'draft' | 'in_review' | 'published' | 'withdrawn' | 'archived'

export interface ContentReviewEntry {
  actor: string
  action: string
  reason: string
  packageVersion: number
  createdAt: string
}

/// One reviewable version of a content package. Title, category, and age tiers
/// live on the version because a new version may change any of them.
export interface ContentVersion {
  packageId: string
  packageVersion: number
  title: string
  category: ContentCategory
  ageTiers: ContentAgeTier[]
  assetKey: string
  sha256: string
  sizeBytes: number
  status: ContentStatus
  publishedAt: string
  createdAt: string
  updatedAt: string
}

/// A content package groups every version of one piece of child content.
export interface ContentPackage {
  packageId: string
  versions: ContentVersion[]
  history: ContentReviewEntry[]
}

export interface ContentPackageQuery {
  category?: ContentCategory
  ageTier?: ContentAgeTier
  status?: ContentStatus
  keyword?: string
  page?: number
  pageSize?: number
}

export interface ContentPackagePage {
  packages: ContentPackage[]
  page: number
  pageSize: number
  total: number
}

export type ThemePackStatus = 'draft' | 'published' | 'withdrawn'

/// One curated, ordered collection of content packages. Members are package
/// ids rather than embedded versions so a theme follows each package across
/// new published versions.
export interface ThemePack {
  themeId: string
  title: string
  summary: string
  ageTiers: ContentAgeTier[]
  packageIds: string[]
  status: ThemePackStatus
  catalogRevision: number
  publishedAt: string
  createdAt: string
  updatedAt: string
}

export interface ThemePackInput {
  themeId: string
  title: string
  summary: string
  ageTiers: ContentAgeTier[]
  packageIds: string[]
}

/// Platform-wide offline download policy. One published revision applies to
/// every bound device; the revision lets the console tell whether a device is
/// already on the current policy.
export interface OfflinePolicy {
  offlineEnabled: boolean
  maxCachedPackages: number
  maxCacheBytes: number
  cacheTtlHours: number
  downloadOverWifiOnly: boolean
  revision: number
}

export interface OfflinePolicyInput {
  offlineEnabled: boolean
  maxCachedPackages: number
  maxCacheBytes: number
  cacheTtlHours: number
  downloadOverWifiOnly: boolean
}

/// One device's applied offline-policy revision for a family.
export interface OfflinePolicyAcknowledgement {
  deviceId: string
  revision: number
  upToDate: boolean
}

/// Fields shared by the create-draft and update-draft forms. A version is
/// allocated by the backend for an existing package, so the create payload
/// carries only package identity and one initial version's metadata.
export interface ContentDraftInput {
  title: string
  category: ContentCategory
  ageTiers: ContentAgeTier[]
  assetKey: string
  sha256: string
  sizeBytes: number
}

export interface ContentPackageCreateInput extends ContentDraftInput {
  packageId: string
}

export interface AdminContentClient {
  loadPackages(query?: ContentPackageQuery): Promise<ContentPackagePage>
  loadPackage(packageId: string): Promise<ContentPackage>
  createPackage(
    input: ContentPackageCreateInput,
  ): Promise<ApiMutationResult<ContentPackage | null>>
  updateVersion(
    packageId: string,
    packageVersion: number,
    input: ContentDraftInput,
  ): Promise<ApiMutationResult<ContentPackage | null>>
  submitVersion(
    packageId: string,
    packageVersion: number,
  ): Promise<ApiMutationResult<ContentPackage | null>>
  approveVersion(
    packageId: string,
    packageVersion: number,
  ): Promise<ApiMutationResult<ContentPackage | null>>
  rejectVersion(
    packageId: string,
    packageVersion: number,
    reason: string,
  ): Promise<ApiMutationResult<ContentPackage | null>>
  publishVersion(
    packageId: string,
    packageVersion: number,
  ): Promise<ApiMutationResult<ContentPackage | null>>
  withdrawVersion(
    packageId: string,
    packageVersion: number,
  ): Promise<ApiMutationResult<ContentPackage | null>>
  archiveVersion(
    packageId: string,
    packageVersion: number,
  ): Promise<ApiMutationResult<ContentPackage | null>>

  /// Theme packs: curated collections of published content packages.
  loadThemePacks(): Promise<ThemePack[]>
  saveThemePack(input: ThemePackInput): Promise<ApiMutationResult<ThemePack>>
  publishThemePack(themeId: string): Promise<ApiMutationResult<ThemePack>>
  withdrawThemePack(themeId: string): Promise<ApiMutationResult<ThemePack>>
  deleteThemePack(themeId: string): Promise<ApiMutationResult<null>>

  /// Offline download policy shared by the guardian app and every device.
  loadOfflinePolicy(): Promise<OfflinePolicy>
  saveOfflinePolicy(
    input: OfflinePolicyInput,
  ): Promise<ApiMutationResult<OfflinePolicy>>
  loadOfflinePolicyAcknowledgements(familyId: string): Promise<OfflinePolicyAcknowledgement[]>
}

/// Creates the client for the content library. Every mutation returns the
/// refreshed package when the backend echoes it, so pages can update the drawer
/// without a second round trip.
export function createAdminContentClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminContentClient {
  return {
    async loadPackages(query: ContentPackageQuery = {}): Promise<ContentPackagePage> {
      const response = await httpClient.get('/api/v1/admin/content/packages', {
        params: compactParams({
          category: query.category,
          age_tier: query.ageTier,
          status: query.status,
          keyword: query.keyword?.trim(),
          page: query.page === undefined ? undefined : String(query.page),
          page_size: query.pageSize === undefined ? undefined : String(query.pageSize),
        }),
      })
      return toPackagePage(response.data?.data)
    },

    async loadPackage(packageId: string): Promise<ContentPackage> {
      const response = await httpClient.get(
        `/api/v1/admin/content/packages/${encodeURIComponent(packageId)}`,
      )
      return requiredPackage(response.data?.data)
    },

    async createPackage(
      input: ContentPackageCreateInput,
    ): Promise<ApiMutationResult<ContentPackage | null>> {
      const response = await httpClient.post('/api/v1/admin/content/packages', {
        package_id: input.packageId.trim(),
        title: input.title.trim(),
        category: input.category,
        age_tiers: input.ageTiers,
        asset_key: input.assetKey.trim(),
        sha256: input.sha256.trim(),
        size_bytes: input.sizeBytes,
      })
      const created = optionalVersion(response.data?.data)
      return {
        value: created === null ? null : toPackageFromVersion(created),
        notice: readApiNotice(response.data),
      }
    },

    async updateVersion(
      packageId: string,
      packageVersion: number,
      input: ContentDraftInput,
    ): Promise<ApiMutationResult<ContentPackage | null>> {
      const response = await httpClient.put(versionPath(packageId, packageVersion), {
        title: input.title.trim(),
        category: input.category,
        age_tiers: input.ageTiers,
        asset_key: input.assetKey.trim(),
        sha256: input.sha256.trim(),
        size_bytes: input.sizeBytes,
      })
      const updated = optionalVersion(response.data?.data)
      return {
        value: updated === null ? null : toPackageFromVersion(updated),
        notice: readApiNotice(response.data),
      }
    },

    async submitVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ApiMutationResult<ContentPackage | null>> {
      return runVersionAction(httpClient, packageId, packageVersion, 'submit')
    },

    async approveVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ApiMutationResult<ContentPackage | null>> {
      return runVersionAction(httpClient, packageId, packageVersion, 'approve')
    },

    async rejectVersion(
      packageId: string,
      packageVersion: number,
      reason: string,
    ): Promise<ApiMutationResult<ContentPackage | null>> {
      return runVersionAction(httpClient, packageId, packageVersion, 'reject', {
        reason: reason.trim(),
      })
    },

    async publishVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ApiMutationResult<ContentPackage | null>> {
      return runVersionAction(httpClient, packageId, packageVersion, 'publish')
    },

    async withdrawVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ApiMutationResult<ContentPackage | null>> {
      return runVersionAction(httpClient, packageId, packageVersion, 'withdraw')
    },

    async archiveVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ApiMutationResult<ContentPackage | null>> {
      return runVersionAction(httpClient, packageId, packageVersion, 'archive')
    },

    async loadThemePacks(): Promise<ThemePack[]> {
      const response = await httpClient.get('/api/v1/admin/content/theme-packs')
      return toThemePacks(response.data?.data)
    },

    async saveThemePack(
      input: ThemePackInput,
    ): Promise<ApiMutationResult<ThemePack>> {
      const response = await httpClient.put(
        `/api/v1/admin/content/theme-packs/${encodeURIComponent(input.themeId.trim())}`,
        {
          theme_id: input.themeId.trim(),
          title: input.title.trim(),
          summary: input.summary.trim(),
          age_tiers: input.ageTiers,
          package_ids: input.packageIds,
        },
      )
      return {
        value: requiredThemePack(response.data?.data),
        notice: readApiNotice(response.data),
      }
    },

    async publishThemePack(themeId: string): Promise<ApiMutationResult<ThemePack>> {
      return runThemeAction(httpClient, themeId, 'publish')
    },

    async withdrawThemePack(themeId: string): Promise<ApiMutationResult<ThemePack>> {
      return runThemeAction(httpClient, themeId, 'withdraw')
    },

    async deleteThemePack(themeId: string): Promise<ApiMutationResult<null>> {
      const response = await httpClient.delete(
        `/api/v1/admin/content/theme-packs/${encodeURIComponent(themeId)}`,
      )
      return {
        value: null,
        notice: readApiNotice(response.data),
      }
    },

    async loadOfflinePolicy(): Promise<OfflinePolicy> {
      const response = await httpClient.get('/api/v1/admin/content/offline-policy')
      return toOfflinePolicy(response.data?.data)
    },

    async saveOfflinePolicy(
      input: OfflinePolicyInput,
    ): Promise<ApiMutationResult<OfflinePolicy>> {
      const response = await httpClient.put('/api/v1/admin/content/offline-policy', {
        offline_enabled: input.offlineEnabled,
        max_cached_packages: input.maxCachedPackages,
        max_cache_bytes: input.maxCacheBytes,
        cache_ttl_hours: input.cacheTtlHours,
        download_over_wifi_only: input.downloadOverWifiOnly,
      })
      return {
        value: toOfflinePolicy(response.data?.data),
        notice: readApiNotice(response.data),
      }
    },

    async loadOfflinePolicyAcknowledgements(
      familyId: string,
    ): Promise<OfflinePolicyAcknowledgement[]> {
      const response = await httpClient.get(
        '/api/v1/admin/content/offline-policy/acknowledgements',
        { params: { family_id: familyId.trim() } },
      )
      return toOfflinePolicyAcknowledgements(response.data?.data)
    },
  }
}

function toOfflinePolicy(value: unknown): OfflinePolicy {
  const record = recordValue(value)
  return {
    offlineEnabled: record.offline_enabled !== false && record.offlineEnabled !== false,
    maxCachedPackages: nonNegativeInteger(
      record.max_cached_packages ?? record.maxCachedPackages,
      20,
    ),
    maxCacheBytes: nonNegativeInteger(
      record.max_cache_bytes ?? record.maxCacheBytes,
      512 * 1024 * 1024,
    ),
    cacheTtlHours: nonNegativeInteger(record.cache_ttl_hours ?? record.cacheTtlHours, 720),
    downloadOverWifiOnly:
      record.download_over_wifi_only !== false && record.downloadOverWifiOnly !== false,
    revision: nonNegativeInteger(record.revision, 0),
  }
}

function toOfflinePolicyAcknowledgements(value: unknown): OfflinePolicyAcknowledgement[] {
  const record = recordValue(value)
  const source = Array.isArray(record.devices) ? record.devices : Array.isArray(value) ? value : []
  return source.flatMap((item: unknown) => {
    const entry = recordValue(item)
    const deviceId = stringValue(entry.device_id ?? entry.deviceId).trim()
    if (!deviceId) {
      return []
    }
    return [
      {
        deviceId,
        revision: nonNegativeInteger(entry.revision, 0),
        upToDate: entry.up_to_date === true || entry.upToDate === true,
      },
    ]
  })
}

async function runThemeAction(
  httpClient: AxiosInstance,
  themeId: string,
  action: string,
): Promise<ApiMutationResult<ThemePack>> {
  const response = await httpClient.post(
    `/api/v1/admin/content/theme-packs/${encodeURIComponent(themeId)}/${action}`,
    {},
  )
  return {
    value: requiredThemePack(response.data?.data),
    notice: readApiNotice(response.data),
  }
}

function toThemePacks(value: unknown): ThemePack[] {
  const record = recordValue(value)
  const source = Array.isArray(record.theme_packs)
    ? record.theme_packs
    : Array.isArray(value)
      ? value
      : []
  return source.flatMap((item: unknown) => {
    const pack = toNullableThemePack(item)
    return pack === null ? [] : [pack]
  })
}

function requiredThemePack(value: unknown): ThemePack {
  const record = recordValue(value)
  const pack = toNullableThemePack(record.theme_pack ?? value)
  if (pack === null) {
    throw new Error('theme pack response is missing a theme identifier')
  }
  return pack
}

function toNullableThemePack(value: unknown): ThemePack | null {
  const record = recordValue(value)
  const themeId = stringValue(record.theme_id ?? record.themeId).trim()
  if (!themeId) {
    return null
  }
  return {
    themeId,
    title: stringValue(record.title),
    summary: stringValue(record.summary),
    ageTiers: contentAgeTiers(record.age_tiers ?? record.ageTiers),
    packageIds: stringArray(record.package_ids ?? record.packageIds),
    status: themePackStatus(record.status),
    catalogRevision: nonNegativeInteger(record.catalog_revision ?? record.catalogRevision, 0),
    publishedAt: stringValue(record.published_at ?? record.publishedAt),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
  }
}

function themePackStatus(value: unknown): ThemePackStatus {
  switch (value) {
    case 'published':
    case 'withdrawn':
      return value
    default:
      return 'draft'
  }
}

async function runVersionAction(
  httpClient: AxiosInstance,
  packageId: string,
  packageVersion: number,
  action: string,
  body?: Record<string, unknown>,
): Promise<ApiMutationResult<ContentPackage | null>> {
  const response = await httpClient.post(
    `${versionPath(packageId, packageVersion)}/${action}`,
    body ?? {},
  )
  const version = optionalVersion(response.data?.data)
  return {
    value: version === null ? null : toPackageFromVersion(version),
    notice: readApiNotice(response.data),
  }
}

function versionPath(packageId: string, packageVersion: number): string {
  return `/api/v1/admin/content/packages/${encodeURIComponent(packageId)}/versions/${packageVersion}`
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

/// The list endpoint returns one row per package version, so versions sharing a
/// package identifier must be folded into a single package before the UI sees
/// them. Ordering is preserved so the newest version stays first.
function toPackagePage(value: unknown): ContentPackagePage {
  const record = recordValue(value)
  const source = Array.isArray(record.packages)
    ? record.packages
    : Array.isArray(value)
      ? value
      : []
  const packages = new Map<string, ContentPackage>()
  for (const item of source) {
    const version = toNullableVersion(item)
    if (version === null) {
      continue
    }
    const existing = packages.get(version.packageId)
    if (existing === undefined) {
      packages.set(version.packageId, toPackageFromVersion(version))
      continue
    }
    existing.versions.push(version)
  }
  return {
    packages: [...packages.values()],
    page: positiveInteger(record.page, 1),
    pageSize: positiveInteger(record.page_size ?? record.pageSize, 20),
    total: nonNegativeInteger(record.total, packages.size),
  }
}

function requiredPackage(value: unknown): ContentPackage {
  const record = recordValue(value)
  const packageId = stringValue(record.package_id ?? record.packageId).trim()
  const versions = Array.isArray(record.versions)
    ? record.versions.flatMap((item: unknown) => {
        const version = toNullableVersion(item)
        return version === null ? [] : [version]
      })
    : []
  if (!packageId || versions.length === 0) {
    throw new Error('content package response is missing a package identifier or version')
  }
  return {
    packageId,
    versions,
    history: toReviewHistory(record.history),
  }
}

function optionalVersion(value: unknown): ContentVersion | null {
  if (value === undefined || value === null) {
    return null
  }
  const record = recordValue(value)
  return toNullableVersion(record.package ?? value)
}

function toPackageFromVersion(version: ContentVersion): ContentPackage {
  return {
    packageId: version.packageId,
    versions: [version],
    history: [],
  }
}

function toNullableVersion(value: unknown): ContentVersion | null {
  const record = recordValue(value)
  const packageId = stringValue(record.package_id ?? record.packageId).trim()
  const packageVersion = Number(record.package_version ?? record.packageVersion)
  if (!packageId || !Number.isFinite(packageVersion) || packageVersion < 1) {
    return null
  }
  return {
    packageId,
    packageVersion: Math.trunc(packageVersion),
    title: stringValue(record.title),
    category: contentCategory(record.category),
    ageTiers: contentAgeTiers(record.age_tiers ?? record.ageTiers),
    assetKey: stringValue(record.asset_key ?? record.assetKey),
    sha256: stringValue(record.sha256),
    sizeBytes: nonNegativeInteger(record.size_bytes ?? record.sizeBytes, 0),
    status: contentStatus(record.status),
    publishedAt: stringValue(record.published_at ?? record.publishedAt),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
  }
}

function toReviewHistory(value: unknown): ContentReviewEntry[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value.flatMap((item: unknown) => {
    const record = recordValue(item)
    const action = stringValue(record.action)
    if (!action) {
      return []
    }
    return [
      {
        actor: stringValue(record.actor_account_id ?? record.actor),
        action,
        reason: stringValue(record.reason),
        packageVersion: nonNegativeInteger(record.package_version ?? record.packageVersion, 0),
        createdAt: stringValue(record.created_at ?? record.createdAt),
      },
    ]
  })
}

function contentCategory(value: unknown): ContentCategory {
  switch (value) {
    case 'story':
    case 'nursery_rhyme':
    case 'poetry':
    case 'english':
    case 'encyclopedia':
    case 'bedtime':
      return value
    default:
      return 'story'
  }
}

function contentStatus(value: unknown): ContentStatus {
  switch (value) {
    case 'in_review':
    case 'published':
    case 'withdrawn':
    case 'archived':
      return value
    default:
      return 'draft'
  }
}

function contentAgeTiers(value: unknown): ContentAgeTier[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value.flatMap((item: unknown) => (isAgeTier(item) ? [item] : []))
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value.flatMap((item: unknown) =>
    typeof item === 'string' && item.trim() !== '' ? [item.trim()] : [],
  )
}

function isAgeTier(value: unknown): value is ContentAgeTier {
  return value === 'age_3_4' || value === 'age_5_6' || value === 'age_7_8'
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
