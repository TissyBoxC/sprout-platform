import axios, { type AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export type FeatureCenterStatus =
  | 'enabled'
  | 'planned'
  | 'disabled'
  | 'maintenance'
  | 'unknown'

export type FeatureConfigFieldType =
  | 'boolean'
  | 'integer'
  | 'number'
  | 'string'
  | 'select'
  | 'multiselect'
  | 'duration'
  | 'url'
  | 'secret'

export type FeatureConfigValue = boolean | number | string | string[] | null

export interface FeatureConfigOption {
  value: string
  label: string
}

export interface FeatureConfigField {
  key: string
  label: string
  description: string
  type: FeatureConfigFieldType
  required: boolean
  editable: boolean
  allowCustom: boolean
  defaultValue: FeatureConfigValue
  options: FeatureConfigOption[]
  minimum: number | null
  maximum: number | null
  step: number | null
  placeholder: string
  unit: string
}

export type FeatureHealthStatus =
  | 'healthy'
  | 'degraded'
  | 'unhealthy'
  | 'checking'
  | 'unknown'

export interface FeatureHealth {
  status: FeatureHealthStatus
  lastCheckedAt: string
  message: string
  latencyMs: number | null
}

export interface FeatureMetric {
  key: string
  label: string
  value: string
  unit: string
}

export interface FeatureCenterFeature {
  id: string
  name: string
  description: string
  owner: string
  category: string
  categoryLabel: string
  status: FeatureCenterStatus
  enabled: boolean
  version: number
  config: Record<string, FeatureConfigValue>
  configSchema: FeatureConfigField[]
  secretConfigured: Record<string, boolean>
  metrics: FeatureMetric[]
  health: FeatureHealth
  healthSource: string
  readOnlyReason: string
  updatedAt: string
}

interface ParsedConfigField {
  field: FeatureConfigField
  effectiveValue: FeatureConfigValue
  secretConfigured: boolean | null
}

export interface UpdateFeatureConfigInput {
  values: Record<string, FeatureConfigValue>
  expectedVersion: number
}

export interface AdminFeatureCenterClient {
  listFeatures(): Promise<FeatureCenterFeature[]>
  getFeature(featureId: string): Promise<FeatureCenterFeature>
  updateFeatureConfig(
    featureId: string,
    input: UpdateFeatureConfigInput,
  ): Promise<FeatureCenterFeature>
  checkFeatureHealth(featureId: string): Promise<FeatureCenterFeature>
}

export const FEATURE_CENTER_VERSION_CONFLICT_CODES = [
  'config_version_conflict',
  'feature_config_version_conflict',
  'feature_version_conflict',
  'config_version_conflict',
  'version_conflict',
  'stale_feature_config',
] as const

const versionConflictCodes = new Set<string>(
  FEATURE_CENTER_VERSION_CONFLICT_CODES,
)

/// Creates the client for the administrator feature catalog.
///
/// Secret values are intentionally not part of the normalized feature model.
/// The parser records only whether each secret is configured, and an update
/// sends a secret only when the administrator entered a replacement value.
export function createAdminFeatureCenterClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminFeatureCenterClient {
  const basePath = '/api/v1/admin/feature-center'

  return {
    async listFeatures(): Promise<FeatureCenterFeature[]> {
      const response = await httpClient.get(basePath)
      return parseFeatureList(response.data?.data)
    },

    async getFeature(featureId: string): Promise<FeatureCenterFeature> {
      const response = await httpClient.get(
        `${basePath}/${encodeURIComponent(featureId.trim())}`,
      )
      const feature = toNullableFeature(
        recordValue(response.data?.data).feature ?? response.data?.data,
      )
      if (feature === null) {
        throw new Error('missing feature')
      }
      return feature
    },

    async updateFeatureConfig(
      featureId: string,
      input: UpdateFeatureConfigInput,
    ): Promise<FeatureCenterFeature> {
      const response = await httpClient.put(
        `${basePath}/${encodeURIComponent(featureId.trim())}/config`,
        {
          values: input.values,
          expected_version: input.expectedVersion,
        },
      )
      const payload = recordValue(response.data?.data)
      const feature = toNullableFeature(payload.feature ?? payload)
      if (feature === null) {
        throw new Error('missing feature')
      }
      return feature
    },

    async checkFeatureHealth(featureId: string): Promise<FeatureCenterFeature> {
      const response = await httpClient.post(
        `${basePath}/${encodeURIComponent(featureId.trim())}/health-check`,
      )
      const payload = recordValue(response.data?.data)
      const feature = toNullableFeature(payload.feature ?? payload)
      if (feature === null) {
        throw new Error('missing feature')
      }
      return feature
    },
  }
}

/// Returns the backend error code across the supported response envelopes.
export function featureCenterErrorCode(error: unknown): string {
  if (!axios.isAxiosError(error)) {
    return ''
  }
  const payload = recordValue(error.response?.data)
  const backendError = recordValue(payload.error)
  return stringValue(backendError.code || payload.code).trim()
}

/// Detects optimistic-concurrency conflicts from current and legacy codes.
export function isFeatureCenterVersionConflict(error: unknown): boolean {
  const code = featureCenterErrorCode(error)
  if (versionConflictCodes.has(code)) {
    return true
  }

  if (!axios.isAxiosError(error) || error.response?.status !== 409) {
    return false
  }
  const payload = recordValue(error.response.data)
  const backendError = recordValue(payload.error)
  const message = stringValue(
    backendError.message || payload.message,
  ).toLowerCase()
  return message.includes('version') || message.includes('版本')
}

function parseFeatureList(value: unknown): FeatureCenterFeature[] {
  const payload = recordValue(value)
  const source = Array.isArray(value)
    ? value
    : Array.isArray(payload.features)
      ? payload.features
      : Array.isArray(payload.items)
        ? payload.items
        : []

  return source.flatMap((item: unknown) => {
    const feature = toNullableFeature(item)
    return feature === null ? [] : [feature]
  })
}

function toNullableFeature(value: unknown): FeatureCenterFeature | null {
  const record = recordValue(value)
  const id = stringValue(
    record.id ?? record.key ?? record.feature_id ?? record.featureId,
  ).trim()
  if (!id) {
    return null
  }

  const configSource = recordValue(
    record.values ??
      record.config ??
      record.config_values ??
      record.configValues,
  )
  const parsedSchema = parseConfigSchema(
    record.config_schema ??
      record.configSchema ??
      record.schema ??
      record.fields,
    configSource,
  )
  const configSchema = parsedSchema.map((item) => item.field)
  const secretKeys = new Set(
    configSchema
      .filter((field) => field.type === 'secret')
      .map((field) => field.key),
  )

  for (const key of Object.keys(configSource)) {
    if (looksLikeSecretKey(key)) {
      secretKeys.add(key)
    }
  }

  const config: Record<string, FeatureConfigValue> = {}
  const secretConfigured: Record<string, boolean> = {}
  for (const parsedField of parsedSchema) {
    const field = parsedField.field
    if (field.type === 'secret') {
      secretConfigured[field.key] =
        parsedField.secretConfigured ??
        secretIsConfigured(field.key, configSource, record)
      continue
    }
    const sourceValue =
      configSource[field.key] !== undefined
        ? configSource[field.key]
        : parsedField.effectiveValue
    config[field.key] = normalizeConfigValue(sourceValue, field)
  }

  for (const [key, rawValue] of Object.entries(configSource)) {
    if (secretKeys.has(key) || isSecretMetadataKey(key)) {
      continue
    }
    if (!(key in config)) {
      config[key] = normalizeUnknownConfigValue(rawValue)
    }
  }

  for (const key of secretKeys) {
    if (!(key in secretConfigured)) {
      secretConfigured[key] = secretIsConfigured(key, configSource, record)
    }
    if (!configSchema.some((field) => field.key === key)) {
      configSchema.push({
        key,
        label: humanizeKey(key),
        description: '',
        type: 'secret',
        required: false,
        editable: true,
        allowCustom: false,
        defaultValue: '',
        options: [],
        minimum: null,
        maximum: null,
        step: null,
        placeholder: '',
        unit: '',
      })
    }
  }

  const category = recordValue(record.category)
  const categoryId = stringValue(
    typeof record.category === 'string'
      ? record.category
      : category.id ?? category.key ?? category.value,
  ).trim()
  const status = featureStatus(record.status, record.enabled)
  const rawHealth = recordValue(record.health)
  const healthDetail = recordValue(
    record.health_detail ?? record.healthDetail,
  )
  const rawHealthStatus =
    typeof record.health === 'string' || typeof record.health === 'number'
      ? record.health
      : rawHealth.status ??
        record.health_status ??
        record.healthStatus ??
        healthDetail.status

  return {
    id,
    name: stringValue(
      record.name ?? record.display_name ?? record.displayName ?? record.title,
      id,
    ),
    description: stringValue(
      record.description ?? record.summary ?? record.detail,
    ),
    owner: stringValue(
      record.owner ?? record.owner_name ?? record.ownerName ?? record.team,
    ),
    category: categoryId || 'other',
    categoryLabel: stringValue(
      typeof record.category === 'string'
        ? category.label ?? category.name
        : category.label ?? category.name,
      categoryId || '其他',
    ),
    status,
    enabled:
      typeof record.enabled === 'boolean'
        ? record.enabled
        : status === 'enabled',
    version: nonNegativeInteger(
      record.config_version ??
        record.configVersion ??
        record.version ??
        record.revision,
    ),
    config,
    configSchema,
    secretConfigured,
    metrics: parseMetrics(
      record.read_only_metrics ??
        record.readOnlyMetrics ??
        record.metrics ??
        record.statistics,
    ),
    health: healthValue({
      status: rawHealthStatus,
      checked_at:
        healthDetail.checked_at ??
        healthDetail.checkedAt ??
        rawHealth.checked_at ??
        rawHealth.checkedAt ??
        record.last_health_check_at ??
        record.lastHealthCheckAt ??
        record.checked_at ??
        record.checkedAt,
      message:
        healthDetail.message ??
        healthDetail.detail ??
        rawHealth.message ??
        rawHealth.detail ??
        record.health_message ??
        record.healthMessage,
      latency_ms:
        healthDetail.latency_ms ??
        healthDetail.latencyMs ??
        rawHealth.latency_ms ??
        rawHealth.latencyMs ??
        record.health_latency_ms ??
        record.healthLatencyMs,
    }),
    healthSource: stringValue(
      record.health_source ??
        record.healthSource ??
        healthDetail.health_source ??
        healthDetail.healthSource ??
        rawHealth.health_source ??
        rawHealth.healthSource,
    ),
    readOnlyReason: stringValue(
      record.read_only_reason ?? record.readOnlyReason,
    ),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
  }
}

function parseConfigSchema(
  value: unknown,
  config: Record<string, unknown>,
): ParsedConfigField[] {
  const schema = recordValue(value)
  const rawFields = Array.isArray(value)
    ? value
    : Array.isArray(schema.fields)
      ? schema.fields
      : recordValue(schema.properties)
  const required = new Set(
    Array.isArray(schema.required)
      ? schema.required.map((item) => String(item))
      : [],
  )

  if (Array.isArray(rawFields)) {
    return rawFields.flatMap((field: unknown) => {
      const parsed = toNullableConfigField(field, config, required)
      return parsed === null ? [] : [parsed]
    })
  }

  return Object.entries(rawFields).flatMap(([key, field]) => {
    const parsed = toNullableConfigField(
      { key, ...recordValue(field) },
      config,
      required,
    )
    return parsed === null ? [] : [parsed]
  })
}

function toNullableConfigField(
  value: unknown,
  config: Record<string, unknown>,
  required: Set<string>,
): ParsedConfigField | null {
  const record = recordValue(value)
  const key = stringValue(
    record.key ?? record.name ?? record.id ?? record.property,
  ).trim()
  if (!key) {
    return null
  }

  const type = configFieldType(record, config[key])
  const options = configOptions(record)
  const rawDefault =
    record.default_value ??
    record.defaultValue ??
    record.default ??
    config[key]
  const rawValue = record.value ?? config[key] ?? rawDefault
  const placeholderField: FeatureConfigField = {
    key,
    label: '',
    description: '',
    type,
    required: false,
    editable: true,
    allowCustom: false,
    defaultValue: null,
    options,
    minimum: null,
    maximum: null,
    step: null,
    placeholder: '',
    unit: '',
  }
  const defaultValue = normalizeConfigValue(rawDefault, placeholderField)
  const field: FeatureConfigField = {
    key,
    label: stringValue(
      record.title ?? record.label ?? record.display_name ?? record.name,
      humanizeKey(key),
    ),
    description: stringValue(
      record.description ?? record.help ?? record.hint,
    ),
    type,
    required:
      required.has(key) ||
      record.required === true ||
      record.optional === false,
    editable:
      record.editable !== false &&
      record.read_only !== true &&
      record.readOnly !== true,
    allowCustom:
      record.allow_custom === true || record.allowCustom === true,
    defaultValue,
    options,
    minimum: nullableNumber(
      record.minimum ?? record.min ?? record.min_value ?? record.minValue,
    ),
    maximum: nullableNumber(
      record.maximum ?? record.max ?? record.max_value ?? record.maxValue,
    ),
    step: nullableNumber(record.step ?? record.multiple_of ?? record.multipleOf),
    placeholder: stringValue(record.placeholder ?? record.example),
    unit: stringValue(record.unit),
  }

  return {
    field,
    effectiveValue:
      type === 'secret'
        ? null
        : normalizeConfigValue(rawValue, {
            ...field,
            defaultValue,
          }),
    secretConfigured:
      type === 'secret' ? secretMetadataConfigured(rawValue) : null,
  }
}

function configFieldType(
  field: Record<string, unknown>,
  currentValue: unknown,
): FeatureConfigFieldType {
  const explicit = stringValue(field.type).toLowerCase()
  const format = stringValue(field.format).toLowerCase()
  const widget = stringValue(field.widget ?? field.control).toLowerCase()
  const combined = `${explicit} ${format} ${widget}`

  if (
    combined.includes('secret') ||
    combined.includes('password') ||
    combined.includes('credential') ||
    field.secret === true ||
    field.sensitive === true
  ) {
    return 'secret'
  }
  if (
    explicit === 'boolean' ||
    explicit === 'bool' ||
    typeof currentValue === 'boolean'
  ) {
    return 'boolean'
  }
  if (explicit === 'integer' || explicit === 'int' || explicit === 'int64') {
    return 'integer'
  }
  if (
    explicit === 'number' ||
    explicit === 'float' ||
    explicit === 'double' ||
    explicit === 'decimal'
  ) {
    return 'number'
  }
  if (
    explicit === 'multiselect' ||
    explicit === 'multi_select' ||
    explicit === 'multi-select' ||
    (explicit === 'array' && Array.isArray(currentValue))
  ) {
    return 'multiselect'
  }
  if (
    explicit === 'select' ||
    explicit === 'enum' ||
    explicit === 'choice' ||
    (Array.isArray(field.enum) && explicit !== 'array')
  ) {
    return 'select'
  }
  if (format === 'duration' || explicit === 'duration') {
    return 'duration'
  }
  if (format === 'uri' || format === 'url' || explicit === 'url') {
    return 'url'
  }
  return 'string'
}

function configOptions(field: Record<string, unknown>): FeatureConfigOption[] {
  const items = recordValue(field.items)
  const rawOptions =
    field.options ??
    field.enum ??
    items.options ??
    items.enum
  const source = Array.isArray(rawOptions)
    ? rawOptions
    : recordValue(rawOptions)

  if (Array.isArray(source)) {
    return source.flatMap((option: unknown) => {
      if (typeof option === 'string' || typeof option === 'number') {
        const value = String(option)
        return [{ value, label: value }]
      }
      const record = recordValue(option)
      const value = stringValue(record.value ?? record.key ?? record.id)
      if (!value) {
        return []
      }
      return [
        {
          value,
          label: stringValue(
            record.label ?? record.name ?? record.title,
            value,
          ),
        },
      ]
    })
  }

  return Object.entries(source).map(([value, label]) => ({
    value,
    label: stringValue(label, value),
  }))
}

function normalizeConfigValue(
  value: unknown,
  field: FeatureConfigField,
): FeatureConfigValue {
  switch (field.type) {
    case 'boolean':
      return typeof value === 'boolean' ? value : booleanValue(value)
    case 'integer': {
      const number = Math.trunc(nullableNumber(value) ?? 0)
      return clampNumber(number, field)
    }
    case 'number': {
      const number = nullableNumber(value) ?? 0
      return clampNumber(number, field)
    }
    case 'multiselect':
      return Array.isArray(value)
        ? value.map((item) => String(item)).filter(Boolean)
        : []
    case 'secret':
      return ''
    case 'select':
    case 'string':
    case 'duration':
    case 'url':
      return typeof value === 'string' || typeof value === 'number'
        ? String(value)
        : field.defaultValue === null
          ? ''
          : String(field.defaultValue)
  }
}

function normalizeUnknownConfigValue(value: unknown): FeatureConfigValue {
  if (
    typeof value === 'string' ||
    typeof value === 'number' ||
    typeof value === 'boolean'
  ) {
    return value
  }
  if (Array.isArray(value)) {
    return value.map((item) => String(item))
  }
  return null
}

function clampNumber(value: number, field: FeatureConfigField): number {
  let next = value
  if (field.minimum !== null) {
    next = Math.max(field.minimum, next)
  }
  if (field.maximum !== null) {
    next = Math.min(field.maximum, next)
  }
  return next
}

function secretIsConfigured(
  key: string,
  config: Record<string, unknown>,
  feature: Record<string, unknown>,
): boolean {
  const configured = recordValue(feature.secret_configured ?? feature.secretConfigured)
  if (configured[key] === true) {
    return true
  }
  if (
    booleanValue(
      config[`${key}_configured`] ??
        config[`${key}_is_configured`] ??
        config[`has_${key}`],
    )
  ) {
    return true
  }
  const configuredKeys = feature.configured_secrets ?? feature.configuredSecrets
  if (
    Array.isArray(configuredKeys) &&
    configuredKeys.map((item) => String(item)).includes(key)
  ) {
    return true
  }

  const value = config[key]
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase()
    return normalized !== '' && normalized !== 'null'
  }
  return typeof value === 'boolean' ? value : value !== null && value !== undefined
}

function secretMetadataConfigured(value: unknown): boolean {
  if (typeof value === 'boolean') {
    return value
  }
  if (typeof value === 'string') {
    return value.trim() !== ''
  }
  return recordValue(value).configured === true
}

function isSecretMetadataKey(key: string): boolean {
  return (
    key.endsWith('_configured') ||
    key.endsWith('_is_configured') ||
    key.startsWith('has_') ||
    key === 'configured_secrets' ||
    key === 'secret_configured'
  )
}

function looksLikeSecretKey(key: string): boolean {
  return /(?:^|_)(?:secret|token|password|credential|api_key|private_key)(?:$|_)/i.test(
    key,
  )
}

function parseMetrics(value: unknown): FeatureMetric[] {
  if (Array.isArray(value)) {
    return value.flatMap((item: unknown) => {
      const record = recordValue(item)
      const key = stringValue(record.key ?? record.name ?? record.id).trim()
      if (!key) {
        return []
      }
      return [
        {
          key,
          label: stringValue(record.label ?? record.title, humanizeKey(key)),
          value: metricDisplayValue(
            record.value ??
              record.display_value ??
              record.displayValue ??
              record.count,
          ),
          unit: stringValue(record.unit),
        },
      ]
    })
  }

  return flattenMetrics(recordValue(value))
}

function flattenMetrics(
  value: Record<string, unknown>,
  prefix = '',
): FeatureMetric[] {
  return Object.entries(value).flatMap(([key, item]) => {
    const metricKey = prefix ? `${prefix}.${key}` : key
    if (typeof item === 'object' && item !== null && !Array.isArray(item)) {
      return flattenMetrics(recordValue(item), metricKey)
    }
    return [
      {
        key: metricKey,
        label: humanizeKey(metricKey),
        value: metricDisplayValue(item),
        unit: '',
      },
    ]
  })
}

function metricDisplayValue(value: unknown): string {
  if (value === null || value === undefined || value === '') {
    return '暂未接入'
  }
  if (typeof value === 'boolean') {
    return value ? '是' : '否'
  }
  if (typeof value === 'number') {
    return Number.isFinite(value) ? String(value) : '暂未接入'
  }
  if (typeof value === 'string') {
    return value.trim() === '' || value.trim() === '暂无'
      ? '暂未接入'
      : value
  }
  return '暂未接入'
}

function healthValue(value: unknown): FeatureHealth {
  const record = recordValue(value)
  if (typeof value === 'string') {
    return {
      status: healthStatus(value),
      lastCheckedAt: '',
      message: '',
      latencyMs: null,
    }
  }
  const status = healthStatus(
    record.status ??
      record.state ??
      record.health ??
      record.health_status ??
      record.healthStatus,
  )

  return {
    status,
    lastCheckedAt: stringValue(
      record.checked_at ??
        record.checkedAt ??
        record.last_health_check_at ??
        record.lastHealthCheckAt ??
        record.last_checked_at ??
        record.lastCheckedAt ??
        record.updated_at ??
        record.updatedAt,
    ),
    message: stringValue(
      record.message ??
        record.detail ??
        record.reason ??
        record.summary ??
        record.health_message ??
        record.healthMessage,
    ),
    latencyMs: nullableNumber(
      record.latency_ms ??
        record.latencyMs ??
        record.duration_ms ??
        record.durationMs ??
        record.health_latency_ms ??
        record.healthLatencyMs,
    ),
  }
}

function featureStatus(
  value: unknown,
  enabled: unknown,
): FeatureCenterStatus {
  const normalized = stringValue(value).trim().toLowerCase()
  switch (normalized) {
    case 'enabled':
    case 'active':
    case 'on':
    case 'available':
      return 'enabled'
    case 'planned':
      return 'planned'
    case 'disabled':
    case 'inactive':
    case 'off':
    case 'paused':
      return 'disabled'
    case 'maintenance':
    case 'updating':
    case 'degraded':
      return 'maintenance'
    default:
      return typeof enabled === 'boolean'
        ? enabled
          ? 'enabled'
          : 'disabled'
        : 'unknown'
  }
}

function healthStatus(value: unknown): FeatureHealthStatus {
  switch (stringValue(value).trim().toLowerCase()) {
    case 'healthy':
    case 'ok':
    case 'up':
    case 'normal':
    case 'pass':
      return 'healthy'
    case 'degraded':
    case 'warning':
    case 'partial':
      return 'degraded'
    case 'unhealthy':
    case 'unavailable':
    case 'down':
    case 'error':
    case 'failed':
    case 'fail':
      return 'unhealthy'
    case 'checking':
    case 'pending':
      return 'checking'
    default:
      return 'unknown'
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

function nullableNumber(value: unknown): number | null {
  if (value === null || value === undefined || value === '') {
    return null
  }
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : null
}

function nonNegativeInteger(value: unknown): number {
  const parsed = nullableNumber(value)
  return parsed === null ? 0 : Math.max(0, Math.trunc(parsed))
}

function booleanValue(value: unknown): boolean {
  if (typeof value === 'boolean') {
    return value
  }
  if (typeof value === 'string') {
    return ['true', '1', 'yes', 'on', 'configured'].includes(
      value.trim().toLowerCase(),
    )
  }
  return value === 1
}

function humanizeKey(value: string): string {
  const normalized = value
    .replace(/[._-]+/g, ' ')
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .trim()
  return normalized || value
}
