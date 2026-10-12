import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface AIModelOption {
  id: string
  latencyMs: number | null
}

export interface AIAccountDefaults {
  defaultBalanceUsd: number | null
  defaultConcurrencyLimit: number | null
  source: string
}

export interface AIGatewayChildPolicyBand {
  name: string
  allowedModels: string[]
  maxOutputTokens: number
  contentRating: string
}

export interface AIGatewayChildPolicyTier {
  ageTier: string
  ageBand: string
  bound: boolean
  ageBandMissing: boolean
  allowedModels: string[]
  maxOutputTokens: number
  contentRating: string
}

export interface AIGatewayChildPolicy {
  enabled: boolean
  requirePolicyVersion: boolean
  defaultAgeBand: string
  injectSafetyBoundary: boolean
  aligned: boolean
  bands: AIGatewayChildPolicyBand[]
  tiers: AIGatewayChildPolicyTier[]
}

export interface ReleaseArtifact {
  version: string
  kind: string
  platform: string
  downloadUrl: string
  sha256: string
}

export interface AdminOperationsClient {
  loadAIAccountDefaults(): Promise<AIAccountDefaults | null>
  loadAIModels(): Promise<AIModelOption[]>
  loadAIGatewayChildPolicy(): Promise<AIGatewayChildPolicy | null>
  loadReleaseArtifact(
    version: string,
    kind: string,
    platform: string,
  ): Promise<ReleaseArtifact | null>
}

/// Creates the client for backend-owned AI defaults, model telemetry, and
/// release artifact lookup. The UI never guesses provider credentials or files.
export function createAdminOperationsClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminOperationsClient {
  return {
    async loadAIAccountDefaults(): Promise<AIAccountDefaults | null> {
      const response = await httpClient.get('/api/v1/admin/ai-account-defaults')
      const value = recordValue(response.data?.data)
      if (Object.keys(value).length === 0) {
        return null
      }
      return {
        defaultBalanceUsd: nullableNumber(
          value.default_balance_usd ?? value.defaultBalanceUsd,
        ),
        defaultConcurrencyLimit: nullableNumber(
          value.default_concurrency ?? value.defaultConcurrencyLimit,
        ),
        source: stringValue(value.source, 'sub2api'),
      }
    },

    async loadAIModels(): Promise<AIModelOption[]> {
      const response = await httpClient.get('/api/v1/admin/ai-models')
      const records = response.data?.data?.models
      if (!Array.isArray(records)) {
        return []
      }
      return records
        .map(toAIModelOption)
        .filter((model): model is AIModelOption => model !== null)
    },

    async loadAIGatewayChildPolicy(): Promise<AIGatewayChildPolicy | null> {
      const response = await httpClient.get(
        '/api/v1/admin/ai-gateway/child-policy',
      )
      const value = recordValue(response.data?.data)
      if (Object.keys(value).length === 0) {
        return null
      }
      const bands = Array.isArray(value.bands)
        ? value.bands
            .map(toChildPolicyBand)
            .filter((band): band is AIGatewayChildPolicyBand => band !== null)
        : []
      const tiers = Array.isArray(value.tiers)
        ? value.tiers
            .map(toChildPolicyTier)
            .filter((tier): tier is AIGatewayChildPolicyTier => tier !== null)
        : []
      return {
        enabled: value.enabled === true,
        requirePolicyVersion: value.require_policy_version === true,
        defaultAgeBand: stringValue(
          value.default_age_band ?? value.defaultAgeBand,
        ),
        injectSafetyBoundary: value.inject_safety_boundary === true,
        aligned: value.aligned === true,
        bands,
        tiers,
      }
    },

    async loadReleaseArtifact(
      version: string,
      kind: string,
      platform: string,
    ): Promise<ReleaseArtifact | null> {
      const response = await httpClient.get(
        `/api/v1/admin/release-artifacts/${encodeURIComponent(version.trim())}`,
        { params: { kind, platform } },
      )
      const value = recordValue(response.data?.data?.artifact)
      if (Object.keys(value).length === 0) {
        return null
      }
      return {
        version: stringValue(value.version, version.trim()),
        kind: stringValue(value.kind, kind),
        platform: stringValue(value.platform, platform),
        downloadUrl: stringValue(value.download_url ?? value.downloadUrl),
        sha256: stringValue(value.sha256),
      }
    },
  }
}

function toAIModelOption(value: unknown): AIModelOption | null {
  const record = recordValue(value)
  const id = stringValue(record.id ?? record.model ?? record.name)
  if (!id) {
    return null
  }
  return {
    id,
    latencyMs: nullableNumber(record.latency_ms ?? record.latencyMs),
  }
}

function toChildPolicyBand(value: unknown): AIGatewayChildPolicyBand | null {
  const record = recordValue(value)
  const name = stringValue(record.name)
  if (!name) {
    return null
  }
  return {
    name,
    allowedModels: stringArray(record.allowed_models ?? record.allowedModels),
    maxOutputTokens: numberValue(
      record.max_output_tokens ?? record.maxOutputTokens,
    ),
    contentRating: stringValue(record.content_rating ?? record.contentRating),
  }
}

function toChildPolicyTier(value: unknown): AIGatewayChildPolicyTier | null {
  const record = recordValue(value)
  const ageTier = stringValue(record.age_tier ?? record.ageTier)
  if (!ageTier) {
    return null
  }
  return {
    ageTier,
    ageBand: stringValue(record.age_band ?? record.ageBand),
    bound: record.bound === true,
    ageBandMissing:
      (record.age_band_missing ?? record.ageBandMissing) === true,
    allowedModels: stringArray(record.allowed_models ?? record.allowedModels),
    maxOutputTokens: numberValue(
      record.max_output_tokens ?? record.maxOutputTokens,
    ),
    contentRating: stringValue(record.content_rating ?? record.contentRating),
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

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value
    .map((item) => (typeof item === 'string' ? item : ''))
    .filter((item) => item.length > 0)
}

function numberValue(value: unknown, fallback = 0): number {
  const parsed = nullableNumber(value)
  return parsed === null ? fallback : parsed
}

function nullableNumber(value: unknown): number | null {
  if (value === null || value === undefined || value === '') {
    return null
  }
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : null
}
