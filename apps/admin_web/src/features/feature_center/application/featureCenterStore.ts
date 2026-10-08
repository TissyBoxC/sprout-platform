import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import {
  createAdminFeatureCenterClient,
  isFeatureCenterVersionConflict,
  type AdminFeatureCenterClient,
  type FeatureCenterFeature,
  type FeatureConfigField,
  type FeatureConfigValue,
  type UpdateFeatureConfigInput,
} from '@/api/adminFeatureCenter'
import { mapApiError, type ApiError } from '@/api/apiError'
import { createHttpClient } from '@/api/httpClient'

export interface FeatureCenterFilters {
  category: string
  keyword: string
}

export interface FeatureCategoryGroup {
  id: string
  label: string
  features: FeatureCenterFeature[]
}

export interface FeatureSaveResult {
  succeeded: boolean
  conflicted: boolean
  error: ApiError | null
}

export const ALL_FEATURE_CATEGORIES = 'all'

/// Owns the feature catalog, editor draft, and all feature-center mutations.
///
/// The list endpoint is authoritative. The store never fabricates defaults for
/// missing features or merges a partial update response into the list without
/// normalizing it through the API client first.
export const useFeatureCenterStore = defineStore('admin-feature-center', () => {
  const client: AdminFeatureCenterClient = createAdminFeatureCenterClient(
    createHttpClient(),
  )

  const features = ref<FeatureCenterFeature[]>([])
  const filters = ref<FeatureCenterFilters>({
    category: ALL_FEATURE_CATEGORIES,
    keyword: '',
  })
  const selectedFeatureId = ref('')
  const selectedFeature = ref<FeatureCenterFeature | null>(null)
  const draftConfig = ref<Record<string, FeatureConfigValue>>({})
  const draftVersion = ref(0)
  const secretDrafts = ref<Record<string, string>>({})

  const isLoading = ref(false)
  const isDetailLoading = ref(false)
  const isSaving = ref(false)
  const isCheckingHealth = ref(false)
  const isRefreshing = ref(false)
  const error = ref<ApiError | null>(null)
  const detailError = ref<ApiError | null>(null)
  const conflictMessage = ref('')
  const lastMessage = ref('')

  const categories = computed(() => {
    const byId = new Map<string, string>()
    for (const feature of features.value) {
      if (!byId.has(feature.category)) {
        byId.set(feature.category, feature.categoryLabel || feature.category)
      }
    }
    return [...byId.entries()]
      .map(([id, label]) => ({ id, label }))
      .sort((left, right) => left.label.localeCompare(right.label, 'zh-CN'))
  })

  const filteredFeatures = computed(() => {
    const keyword = filters.value.keyword.trim().toLocaleLowerCase('zh-CN')
    return features.value.filter((feature) => {
      if (
        filters.value.category !== ALL_FEATURE_CATEGORIES &&
        feature.category !== filters.value.category
      ) {
        return false
      }
      if (!keyword) {
        return true
      }
      return [
        feature.name,
        feature.description,
        feature.owner,
        feature.categoryLabel,
        feature.id,
      ].some((value) => value.toLocaleLowerCase('zh-CN').includes(keyword))
    })
  })

  const groupedFeatures = computed<FeatureCategoryGroup[]>(() => {
    const groups = new Map<string, FeatureCategoryGroup>()
    for (const feature of filteredFeatures.value) {
      const group = groups.get(feature.category) ?? {
        id: feature.category,
        label: feature.categoryLabel || feature.category,
        features: [],
      }
      group.features.push(feature)
      groups.set(feature.category, group)
    }
    return [...groups.values()]
  })

  const canEdit = computed(
    () =>
      selectedFeature.value !== null &&
      selectedFeature.value.status !== 'planned' &&
      selectedFeature.value.readOnlyReason === '' &&
      selectedFeature.value.configSchema.some((field) => field.editable),
  )

  const isDirty = computed(() => {
    const feature = selectedFeature.value
    if (feature === null) {
      return false
    }
    return (
      draftVersion.value !== feature.version ||
      !sameConfig(draftConfig.value, feature.config) ||
      Object.values(secretDrafts.value).some((value) => value.trim() !== '')
    )
  })

  const canSave = computed(
    () =>
      canEdit.value &&
      isDirty.value &&
      !isSaving.value,
  )

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      const nextFeatures = await client.listFeatures()
      features.value = nextFeatures

      if (selectedFeatureId.value) {
        const selected = nextFeatures.find(
          (feature) => feature.id === selectedFeatureId.value,
        )
        if (selected !== undefined) {
          applyFeatureToEditor(selected)
        } else if (nextFeatures.length > 0) {
          await selectFeature(nextFeatures[0]?.id ?? '')
        } else {
          clearSelection()
        }
      } else if (nextFeatures.length > 0) {
        await selectFeature(nextFeatures[0]?.id ?? '')
      }
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      features.value = []
      clearSelection()
    } finally {
      isLoading.value = false
    }
  }

  async function refresh(): Promise<void> {
    isRefreshing.value = true
    lastMessage.value = ''
    conflictMessage.value = ''
    try {
      await load()
      lastMessage.value = '功能配置已刷新。'
    } finally {
      isRefreshing.value = false
    }
  }

  async function selectFeature(featureId: string): Promise<void> {
    const normalizedId = featureId.trim()
    if (!normalizedId) {
      return
    }

    selectedFeatureId.value = normalizedId
    const listFeature =
      features.value.find((feature) => feature.id === normalizedId) ?? null
    if (listFeature !== null) {
      applyFeatureToEditor(listFeature)
    } else {
      selectedFeature.value = null
      draftConfig.value = {}
      draftVersion.value = 0
      secretDrafts.value = {}
    }

    detailError.value = null
    conflictMessage.value = ''
    lastMessage.value = ''
    isDetailLoading.value = true
    try {
      const detail = await client.getFeature(normalizedId)
      upsertFeature(detail)
      applyFeatureToEditor(detail)
    } catch (caught: unknown) {
      detailError.value = mapApiError(caught)
    } finally {
      isDetailLoading.value = false
    }
  }

  function setFilters(nextFilters: Partial<FeatureCenterFilters>): void {
    filters.value = { ...filters.value, ...nextFilters }
  }

  function setConfigValue(key: string, value: FeatureConfigValue): void {
    draftConfig.value = {
      ...draftConfig.value,
      [key]: value,
    }
    clearMessages()
  }

  function setSecretDraft(key: string, value: string): void {
    secretDrafts.value = {
      ...secretDrafts.value,
      [key]: value,
    }
    clearMessages()
  }

  function clearSecretDraft(key: string): void {
    const nextDrafts = { ...secretDrafts.value }
    delete nextDrafts[key]
    secretDrafts.value = nextDrafts
    clearMessages()
  }

  function discardDraft(): void {
    if (selectedFeature.value !== null) {
      applyFeatureToEditor(selectedFeature.value)
    }
  }

  async function save(): Promise<FeatureSaveResult> {
    const feature = selectedFeature.value
    if (feature === null) {
      return {
        succeeded: false,
        conflicted: false,
        error: null,
      }
    }

    const config = payloadConfig(
      feature,
      draftConfig.value,
      secretDrafts.value,
    )
    const input: UpdateFeatureConfigInput = {
      values: config,
      expectedVersion: draftVersion.value,
    }

    isSaving.value = true
    error.value = null
    detailError.value = null
    conflictMessage.value = ''
    lastMessage.value = ''
    try {
      const updated = await client.updateFeatureConfig(feature.id, input)
      upsertFeature(updated)
      applyFeatureToEditor(updated)
      lastMessage.value = `${updated.name || feature.name} 的配置已保存。`
      return { succeeded: true, conflicted: false, error: null }
    } catch (caught: unknown) {
      if (isFeatureCenterVersionConflict(caught)) {
        conflictMessage.value =
          '这项功能已被其他人更新。请重新加载后再保存，当前未提交的修改仍保留在页面上。'
        return { succeeded: false, conflicted: true, error: null }
      }
      const mapped = mapApiError(caught)
      error.value = mapped
      return { succeeded: false, conflicted: false, error: mapped }
    } finally {
      isSaving.value = false
    }
  }

  async function checkHealth(featureId = selectedFeatureId.value): Promise<boolean> {
    const normalizedId = featureId.trim()
    if (!normalizedId) {
      return false
    }

    isCheckingHealth.value = true
    error.value = null
    conflictMessage.value = ''
    lastMessage.value = ''
    try {
      const updated = await client.checkFeatureHealth(normalizedId)
      upsertFeature(updated)
      if (selectedFeature.value?.id === normalizedId) {
        selectedFeature.value = {
          ...selectedFeature.value,
          health: updated.health,
          updatedAt: updated.updatedAt,
        }
      }
      const feature = features.value.find((item) => item.id === normalizedId)
      lastMessage.value = `${feature?.name ?? '功能'} 的健康状态已更新。`
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isCheckingHealth.value = false
    }
  }

  function clearMessages(): void {
    error.value = null
    detailError.value = null
    conflictMessage.value = ''
    lastMessage.value = ''
  }

  function applyFeatureToEditor(feature: FeatureCenterFeature): void {
    selectedFeatureId.value = feature.id
    selectedFeature.value = cloneFeature(feature)
    draftConfig.value = cloneConfig(feature.config)
    draftVersion.value = feature.version
    secretDrafts.value = {}
  }

  function upsertFeature(feature: FeatureCenterFeature): void {
    const index = features.value.findIndex((item) => item.id === feature.id)
    if (index === -1) {
      features.value = [...features.value, feature]
      return
    }
    const nextFeatures = [...features.value]
    nextFeatures[index] = feature
    features.value = nextFeatures
  }

  function clearSelection(): void {
    selectedFeatureId.value = ''
    selectedFeature.value = null
    draftConfig.value = {}
    draftVersion.value = 0
    secretDrafts.value = {}
    detailError.value = null
    conflictMessage.value = ''
  }

  return {
    canSave,
    canEdit,
    categories,
    checkHealth,
    clearMessages,
    clearSecretDraft,
    conflictMessage,
    discardDraft,
    detailError,
    draftConfig,
    draftVersion,
    error,
    features,
    filteredFeatures,
    filters,
    groupedFeatures,
    isCheckingHealth,
    isDetailLoading,
    isDirty,
    isLoading,
    isRefreshing,
    isSaving,
    lastMessage,
    load,
    refresh,
    save,
    secretDrafts,
    selectedFeature,
    selectedFeatureId,
    selectFeature,
    setConfigValue,
    setFilters,
    setSecretDraft,
  }
})

function cloneFeature(feature: FeatureCenterFeature): FeatureCenterFeature {
  return {
    ...feature,
    config: cloneConfig(feature.config),
    configSchema: feature.configSchema.map((field) => ({
      ...field,
      options: [...field.options],
    })),
    secretConfigured: { ...feature.secretConfigured },
    metrics: feature.metrics.map((metric) => ({ ...metric })),
    health: { ...feature.health },
  }
}

function cloneConfig(
  config: Record<string, FeatureConfigValue>,
): Record<string, FeatureConfigValue> {
  return Object.fromEntries(
    Object.entries(config).map(([key, value]) => [
      key,
      Array.isArray(value) ? [...value] : value,
    ]),
  )
}

function sameConfig(
  left: Record<string, FeatureConfigValue>,
  right: Record<string, FeatureConfigValue>,
): boolean {
  const leftKeys = Object.keys(left).sort()
  const rightKeys = Object.keys(right).sort()
  if (leftKeys.length !== rightKeys.length) {
    return false
  }
  return leftKeys.every((key, index) => {
    if (key !== rightKeys[index]) {
      return false
    }
    const leftValue = left[key]
    const rightValue = right[key]
    if (Array.isArray(leftValue) && Array.isArray(rightValue)) {
      return (
        leftValue.length === rightValue.length &&
        leftValue.every(
          (value, valueIndex) => value === rightValue.at(valueIndex),
        )
      )
    }
    return leftValue === rightValue
  })
}

function payloadConfig(
  feature: FeatureCenterFeature,
  draftConfig: Record<string, FeatureConfigValue>,
  secretDrafts: Record<string, string>,
): Record<string, FeatureConfigValue> {
  const config: Record<string, FeatureConfigValue> = {}

  for (const field of feature.configSchema) {
    if (!field.editable) {
      continue
    }
    if (field.type === 'secret') {
      const replacement = secretDrafts[field.key]?.trim() ?? ''
      if (replacement !== '') {
        config[field.key] = replacement
      }
      continue
    }
    const value = draftConfig[field.key]
    if (value !== undefined) {
      config[field.key] = value
    }
  }

  for (const [key, value] of Object.entries(draftConfig)) {
    if (!(key in config) && !looksLikeSecretField(key, feature.configSchema)) {
      config[key] = value
    }
  }

  return config
}

function looksLikeSecretField(
  key: string,
  fields: FeatureConfigField[],
): boolean {
  return (
    fields.some((field) => field.key === key && field.type === 'secret') ||
    /(?:^|_)(?:secret|token|password|credential|api_key|private_key)(?:$|_)/i.test(
      key,
    )
  )
}
