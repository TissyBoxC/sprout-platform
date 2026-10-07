import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export type ContentCategory =
  | 'story'
  | 'nursery_rhyme'
  | 'poetry'
  | 'english'
  | 'encyclopedia'
  | 'bedtime'

export type ContentAgeTier = 'age_3_4' | 'age_5_6' | 'age_7_8'

export type ContentStatus =
  | 'draft'
  | 'in_review'
  | 'published'
  | 'withdrawn'
  | 'archived'

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
  createPackage(input: ContentPackageCreateInput): Promise<ContentPackage | null>
  updateVersion(
    packageId: string,
    packageVersion: number,
    input: ContentDraftInput,
  ): Promise<ContentPackage | null>
  submitVersion(packageId: string, packageVersion: number): Promise<ContentPackage | null>
  approveVersion(packageId: string, packageVersion: number): Promise<ContentPackage | null>
  rejectVersion(
    packageId: string,
    packageVersion: number,
    reason: string,
  ): Promise<ContentPackage | null>
  publishVersion(packageId: string, packageVersion: number): Promise<ContentPackage | null>
  withdrawVersion(packageId: string, packageVersion: number): Promise<ContentPackage | null>
  archiveVersion(packageId: string, packageVersion: number): Promise<ContentPackage | null>
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

    async createPackage(input: ContentPackageCreateInput): Promise<ContentPackage | null> {
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
      return created === null ? null : toPackageFromVersion(created)
    },

    async updateVersion(
      packageId: string,
      packageVersion: number,
      input: ContentDraftInput,
    ): Promise<ContentPackage | null> {
      const response = await httpClient.put(
        versionPath(packageId, packageVersion),
        {
          title: input.title.trim(),
          category: input.category,
          age_tiers: input.ageTiers,
          asset_key: input.assetKey.trim(),
          sha256: input.sha256.trim(),
          size_bytes: input.sizeBytes,
        },
      )
      const updated = optionalVersion(response.data?.data)
      return updated === null ? null : toPackageFromVersion(updated)
    },

    async submitVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ContentPackage | null> {
      return runVersionAction(httpClient, packageId, packageVersion, 'submit')
    },

    async approveVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ContentPackage | null> {
      return runVersionAction(httpClient, packageId, packageVersion, 'approve')
    },

    async rejectVersion(
      packageId: string,
      packageVersion: number,
      reason: string,
    ): Promise<ContentPackage | null> {
      return runVersionAction(httpClient, packageId, packageVersion, 'reject', {
        reason: reason.trim(),
      })
    },

    async publishVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ContentPackage | null> {
      return runVersionAction(httpClient, packageId, packageVersion, 'publish')
    },

    async withdrawVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ContentPackage | null> {
      return runVersionAction(httpClient, packageId, packageVersion, 'withdraw')
    },

    async archiveVersion(
      packageId: string,
      packageVersion: number,
    ): Promise<ContentPackage | null> {
      return runVersionAction(httpClient, packageId, packageVersion, 'archive')
    },
  }
}

async function runVersionAction(
  httpClient: AxiosInstance,
  packageId: string,
  packageVersion: number,
  action: string,
  body?: Record<string, unknown>,
): Promise<ContentPackage | null> {
  const response = await httpClient.post(
    `${versionPath(packageId, packageVersion)}/${action}`,
    body ?? {},
  )
  const version = optionalVersion(response.data?.data)
  return version === null ? null : toPackageFromVersion(version)
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
        packageVersion: nonNegativeInteger(
          record.package_version ?? record.packageVersion,
          0,
        ),
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
