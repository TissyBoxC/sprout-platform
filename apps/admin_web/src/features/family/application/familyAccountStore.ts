import { defineStore } from 'pinia'
import { ref } from 'vue'

import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createAdminOperationsClient,
  type AIModelOption,
} from '@/api/adminOperations'
import { createHttpClient } from '@/api/httpClient'

export interface AdminFamilyAiAccount {
  providerAccountId: string
  status: string
  balanceUsd: number
  concurrencyLimit: number
  availableModels: string[]
  selectedModels: string[]
  allowedModels: string[]
  credentialReady: boolean
  updatedAt: string
}

export interface AdminFamily {
  parentAccountId: string
  phone: string
  parentDisplayName: string
  guardianFamilyName: string
  childNickname: string
  childBirthday: string
  status: string
  createdAt: string
  aiAccount: AdminFamilyAiAccount | null
}

export interface CreateAdminFamilyInput {
  phone: string
  password: string
  guardianFamilyName: string
  childNickname: string
  childBirthday: string
}

export interface UpdateAdminFamilyProfileInput {
  displayName: string
  guardianFamilyName: string
  childNickname: string
  childBirthday: string
}

export interface UpdateAdminFamilyAiAccountInput {
  providerAccountId: string
  status: string
  balanceUsd: number
  concurrencyLimit: number
  availableModels: string[]
}

export interface AdminFamilyDeviceRuntime {
  isOnline: boolean
  connectionState: string
  transport: string
  reportedAt: string
  receivedAt: string
}

export interface AdminFamilyDevice {
  deviceId: string
  deviceName: string
  hardwareModel: string
  firmwareVersion: string
  capabilities: string[]
  boundAt: string
  updatedAt: string
  runtime: AdminFamilyDeviceRuntime | null
}

export interface FamilyMutationResult {
  succeeded: boolean
  error: ApiError | null
  refreshError: ApiError | null
}

export interface AdminFamilyAiUsageModel {
  model: string
  requests: number
  inputTokens: number
  outputTokens: number
  cacheCreationTokens: number
  cacheReadTokens: number
  totalTokens: number
  costUsd: number
  actualCostUsd: number
}

export interface AdminFamilyAiUsage {
  providerAccountId: string
  startTime: string
  endTime: string
  requests: number
  inputTokens: number
  outputTokens: number
  totalTokens: number
  costUsd: number
  actualCostUsd: number
  averageDurationMs: number
  models: AdminFamilyAiUsageModel[]
}

// 管理端以家长账号为主，AI 服务、资料与设备都归属于该账号。
export const useFamilyAccountStore = defineStore('admin-families', () => {
  const httpClient = createHttpClient()
  const operationsClient = createAdminOperationsClient(httpClient)
  const families = ref<AdminFamily[]>([])
  const modelOptions = ref<AIModelOption[]>([])
  const devicesByFamily = ref<Record<string, AdminFamilyDevice[]>>({})
  const deviceErrorsByFamily = ref<Record<string, ApiError | null>>({})
  const deviceLoadingFamilyId = ref('')
  const aiUsageByFamily = ref<Record<string, AdminFamilyAiUsage>>({})
  const aiUsageErrorsByFamily = ref<Record<string, ApiError | null>>({})
  const aiUsageLoadingFamilyId = ref('')
  const isLoading = ref(false)
  const isLoadingModels = ref(false)
  const isSubmitting = ref(false)
  const isUpdatingProfile = ref(false)
  const isUpdatingAiAccount = ref(false)
  const isResettingPassword = ref(false)
  const error = ref<ApiError | null>(null)
  const modelError = ref<ApiError | null>(null)
  const lastMessage = ref('')

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    isLoadingModels.value = true
    modelError.value = null
    await Promise.all([loadFamilies(true), loadModels()])
    isLoading.value = false
  }

  async function create(input: CreateAdminFamilyInput): Promise<boolean> {
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    try {
      const response = await httpClient.post('/api/v1/admin/families', {
        phone: input.phone.trim(),
        password: input.password,
        guardian_family_name: input.guardianFamilyName.trim(),
        child_nickname: input.childNickname.trim(),
        child_birthday: input.childBirthday,
      })
      const refreshError = await loadFamilies(false)
      const aiAccount = toAiAccountFromResponse(response.data?.data)
      if (refreshError !== null) {
        lastMessage.value = '家长账号已创建，列表暂时没有更新，请重新加载。'
      } else if (aiAccount === null) {
        lastMessage.value =
          '家长账号已创建，AI 服务仍在准备中。可以在列表中重新开通。'
      } else {
        lastMessage.value = '家长账号已创建，家长 AI 服务已开通。'
      }
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isSubmitting.value = false
    }
  }

  async function retryAiAccount(
    parentAccountId: string,
  ): Promise<FamilyMutationResult> {
    isSubmitting.value = true
    try {
      const response = await httpClient.post(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/ai-account`,
      )
      const existing = familyById(parentAccountId)?.aiAccount ?? null
      const responseAccount = toAiAccountFromResponse(
        response.data?.data,
        existing,
      )
      if (responseAccount !== null) {
        replaceAiAccount(parentAccountId, responseAccount)
      }
      const refreshError = await loadFamilies(false)
      return { succeeded: true, error: null, refreshError }
    } catch (caught: unknown) {
      return { succeeded: false, error: mapApiError(caught), refreshError: null }
    } finally {
      isSubmitting.value = false
    }
  }

  async function updateAiAccount(
    input: UpdateAdminFamilyAiAccountInput,
  ): Promise<FamilyMutationResult> {
    isUpdatingAiAccount.value = true
    try {
      const response = await httpClient.put(
        `/api/v1/admin/ai-accounts/${encodeURIComponent(input.providerAccountId)}`,
        {
          status: input.status,
          balance_usd: input.balanceUsd,
          concurrency_limit: input.concurrencyLimit,
          available_models: input.availableModels,
          reason: '由管理端调整家长 AI 服务',
        },
      )
      const existing =
        familyByProviderAccountId(input.providerAccountId)?.aiAccount ?? null
      const responseAccount = toAiAccountFromResponse(
        response.data?.data,
        existing,
        input.providerAccountId,
      )
      if (responseAccount !== null) {
        replaceAiAccountByProviderId(input.providerAccountId, responseAccount)
      }
      const refreshError = await loadFamilies(false)
      return { succeeded: true, error: null, refreshError }
    } catch (caught: unknown) {
      return { succeeded: false, error: mapApiError(caught), refreshError: null }
    } finally {
      isUpdatingAiAccount.value = false
    }
  }

  async function updateParentProfile(
    parentAccountId: string,
    input: UpdateAdminFamilyProfileInput,
  ): Promise<FamilyMutationResult> {
    isUpdatingProfile.value = true
    try {
      await httpClient.put(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/profile`,
        {
          display_name: input.displayName.trim(),
          guardian_family_name: input.guardianFamilyName.trim(),
          child_nickname: input.childNickname.trim(),
          child_birthday: input.childBirthday,
        },
      )
      const refreshError = await loadFamilies(false)
      return { succeeded: true, error: null, refreshError }
    } catch (caught: unknown) {
      return { succeeded: false, error: mapApiError(caught), refreshError: null }
    } finally {
      isUpdatingProfile.value = false
    }
  }

  async function resetParentPassword(
    parentAccountId: string,
    password: string,
  ): Promise<FamilyMutationResult> {
    isResettingPassword.value = true
    try {
      await httpClient.post(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/password`,
        { password },
      )
      const refreshError = await loadFamilies(false)
      return { succeeded: true, error: null, refreshError }
    } catch (caught: unknown) {
      return { succeeded: false, error: mapApiError(caught), refreshError: null }
    } finally {
      isResettingPassword.value = false
    }
  }

  async function loadFamilyDevices(
    parentAccountId: string,
  ): Promise<ApiError | null> {
    deviceLoadingFamilyId.value = parentAccountId
    setDeviceError(parentAccountId, null)
    try {
      const response = await httpClient.get(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/devices`,
      )
      const records = response.data?.data?.devices
      const devices = Array.isArray(records)
        ? records
            .map(toFamilyDevice)
            .filter((device): device is AdminFamilyDevice => device !== null)
        : []
      devicesByFamily.value = {
        ...devicesByFamily.value,
        [parentAccountId]: devices,
      }
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      setDeviceError(parentAccountId, mapped)
      return mapped
    } finally {
      if (deviceLoadingFamilyId.value === parentAccountId) {
        deviceLoadingFamilyId.value = ''
      }
    }
  }

  async function loadFamilyAIUsage(
    parentAccountId: string,
  ): Promise<ApiError | null> {
    aiUsageLoadingFamilyId.value = parentAccountId
    setAIUsageError(parentAccountId, null)
    try {
      const response = await httpClient.get(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/ai-usage`,
      )
      const usage = toAiUsage(response.data?.data)
      if (usage !== null) {
        aiUsageByFamily.value = {
          ...aiUsageByFamily.value,
          [parentAccountId]: usage,
        }
      }
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      setAIUsageError(parentAccountId, mapped)
      return mapped
    } finally {
      if (aiUsageLoadingFamilyId.value === parentAccountId) {
        aiUsageLoadingFamilyId.value = ''
      }
    }
  }

  async function loadFamilies(reportError: boolean): Promise<ApiError | null> {
    if (reportError) {
      error.value = null
    }
    try {
      const response = await httpClient.get('/api/v1/admin/families')
      const payload = response.data?.data ?? {}
      const records =
        payload.accounts ?? payload.families ?? payload.parent_accounts ?? []
      families.value = Array.isArray(records) ? records.map(toFamily) : []
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (reportError) {
        error.value = mapped
      }
      return mapped
    }
  }

  async function loadModels(): Promise<void> {
    isLoadingModels.value = true
    try {
      modelOptions.value = await operationsClient.loadAIModels()
      modelError.value = null
    } catch (caught: unknown) {
      modelError.value = mapApiError(caught)
    } finally {
      isLoadingModels.value = false
    }
  }

  function familyById(parentAccountId: string): AdminFamily | null {
    return (
      families.value.find(
        (family) => family.parentAccountId === parentAccountId,
      ) ?? null
    )
  }

  function familyByProviderAccountId(
    providerAccountId: string,
  ): AdminFamily | null {
    return (
      families.value.find(
        (family) => family.aiAccount?.providerAccountId === providerAccountId,
      ) ?? null
    )
  }

  function replaceAiAccount(
    parentAccountId: string,
    aiAccount: AdminFamilyAiAccount,
  ): void {
    families.value = families.value.map((family) =>
      family.parentAccountId === parentAccountId
        ? { ...family, aiAccount }
        : family,
    )
  }

  function replaceAiAccountByProviderId(
    providerAccountId: string,
    aiAccount: AdminFamilyAiAccount,
  ): void {
    families.value = families.value.map((family) =>
      family.aiAccount?.providerAccountId === providerAccountId
        ? { ...family, aiAccount }
        : family,
    )
  }

  function setDeviceError(
    parentAccountId: string,
    value: ApiError | null,
  ): void {
    deviceErrorsByFamily.value = {
      ...deviceErrorsByFamily.value,
      [parentAccountId]: value,
    }
  }

  function devicesFor(parentAccountId: string): AdminFamilyDevice[] {
    return devicesByFamily.value[parentAccountId] ?? []
  }

  function deviceErrorFor(parentAccountId: string): ApiError | null {
    return deviceErrorsByFamily.value[parentAccountId] ?? null
  }

  function isFamilyDevicesLoading(parentAccountId: string): boolean {
    return deviceLoadingFamilyId.value === parentAccountId
  }

  function setAIUsageError(
    parentAccountId: string,
    value: ApiError | null,
  ): void {
    aiUsageErrorsByFamily.value = {
      ...aiUsageErrorsByFamily.value,
      [parentAccountId]: value,
    }
  }

  function aiUsageFor(parentAccountId: string): AdminFamilyAiUsage | null {
    return aiUsageByFamily.value[parentAccountId] ?? null
  }

  function aiUsageErrorFor(parentAccountId: string): ApiError | null {
    return aiUsageErrorsByFamily.value[parentAccountId] ?? null
  }

  function isFamilyAIUsageLoading(parentAccountId: string): boolean {
    return aiUsageLoadingFamilyId.value === parentAccountId
  }

  return {
    aiUsageErrorFor,
    aiUsageFor,
    create,
    deviceErrorFor,
    devicesFor,
    error,
    families,
    isLoading,
    isLoadingModels,
    isFamilyAIUsageLoading,
    isFamilyDevicesLoading,
    isResettingPassword,
    isSubmitting,
    isUpdatingAiAccount,
    isUpdatingProfile,
    lastMessage,
    load,
    loadFamilyAIUsage,
    loadFamilyDevices,
    loadModels,
    modelError,
    modelOptions,
    resetParentPassword,
    retryAiAccount,
    updateAiAccount,
    updateParentProfile,
  }
})

function toFamily(value: Record<string, unknown>): AdminFamily {
  const aiAccountValue =
    value.ai_account ?? value.parent_ai_account ?? value.aiAccount

  return {
    parentAccountId: stringValue(value.parent_account_id ?? value.id),
    phone: stringValue(value.phone ?? value.phone_number),
    parentDisplayName: stringValue(
      value.display_name ?? value.parent_display_name,
    ),
    guardianFamilyName: stringValue(
      value.guardian_family_name ?? value.parent_family_name,
    ),
    childNickname: stringValue(value.child_nickname),
    childBirthday: stringValue(value.child_birthday),
    status: stringValue(value.status, 'active'),
    createdAt: stringValue(value.created_at),
    aiAccount: toAiAccount(aiAccountValue),
  }
}

function toAiAccountFromResponse(
  value: unknown,
  fallback: AdminFamilyAiAccount | null = null,
  fallbackProviderAccountId = '',
): AdminFamilyAiAccount | null {
  const record = recordValue(value)
  const accountValue =
    record.ai_account ?? record.aiAccount ?? record.account ?? record
  return toAiAccount(accountValue, fallback, fallbackProviderAccountId)
}

function toAiAccount(
  value: unknown,
  fallback: AdminFamilyAiAccount | null = null,
  fallbackProviderAccountId = '',
): AdminFamilyAiAccount | null {
  if (!isRecord(value) && fallback === null) {
    return null
  }

  const record = isRecord(value) ? value : {}
  const providerAccountId = stringValue(
    record.provider_account_id ??
      record.providerAccountId ??
      record.account_id ??
      record.id,
    fallback?.providerAccountId ?? fallbackProviderAccountId,
  )
  if (!providerAccountId) {
    return null
  }

  return {
    providerAccountId,
    status: stringValue(record.status, fallback?.status ?? 'active'),
    balanceUsd: numberValue(
      record.balance_usd ?? record.balanceUsd,
      fallback?.balanceUsd ?? 0,
    ),
    concurrencyLimit: numberValue(
      record.concurrency_limit ?? record.concurrencyLimit,
      fallback?.concurrencyLimit ?? 1,
    ),
    availableModels: modelList(
      record.available_models ?? record.availableModels,
      fallback?.availableModels ?? [],
    ),
    selectedModels: modelList(
      record.selected_models ?? record.selectedModels,
      fallback?.selectedModels ?? [],
    ),
    allowedModels: modelList(
      record.allowed_models ?? record.allowedModels,
      fallback?.allowedModels ?? [],
    ),
    credentialReady:
      value !== undefined
        ? record.credential_ready === true || record.credentialReady === true
        : (fallback?.credentialReady ?? false),
    updatedAt: stringValue(
      record.updated_at ?? record.updatedAt,
      fallback?.updatedAt ?? '',
    ),
  }
}

function toFamilyDevice(value: unknown): AdminFamilyDevice | null {
  if (!isRecord(value)) {
    return null
  }
  const deviceId = stringValue(value.device_id ?? value.deviceId)
  if (!deviceId) {
    return null
  }
  return {
    deviceId,
    deviceName: stringValue(value.device_name ?? value.deviceName),
    hardwareModel: stringValue(value.hardware_model ?? value.hardwareModel),
    firmwareVersion: stringValue(
      value.firmware_version ?? value.firmwareVersion,
    ),
    capabilities: modelList(value.capabilities),
    boundAt: stringValue(value.bound_at ?? value.boundAt),
    updatedAt: stringValue(value.updated_at ?? value.updatedAt),
    runtime: isRecord(value.runtime)
      ? toFamilyDeviceRuntime(value.runtime)
      : null,
  }
}

function toAiUsage(value: unknown): AdminFamilyAiUsage | null {
  const record = recordValue(value)
  const totals = recordValue(record.totals)
  const providerAccountId = stringValue(
    record.provider_account_id ?? record.providerAccountId,
  )
  if (!providerAccountId) {
    return null
  }
  const models = Array.isArray(record.models)
    ? record.models
        .map(toAiUsageModel)
        .filter((model): model is AdminFamilyAiUsageModel => model !== null)
    : []
  return {
    providerAccountId,
    startTime: stringValue(record.start_time ?? record.startTime),
    endTime: stringValue(record.end_time ?? record.endTime),
    requests: numberValue(totals.requests),
    inputTokens: numberValue(totals.input_tokens ?? totals.inputTokens),
    outputTokens: numberValue(totals.output_tokens ?? totals.outputTokens),
    totalTokens: numberValue(totals.total_tokens ?? totals.totalTokens),
    costUsd: numberValue(totals.cost_usd ?? totals.costUsd),
    actualCostUsd: numberValue(
      totals.actual_cost_usd ?? totals.actualCostUsd,
    ),
    averageDurationMs: numberValue(
      totals.average_duration_ms ?? totals.averageDurationMs,
    ),
    models,
  }
}

function toAiUsageModel(value: unknown): AdminFamilyAiUsageModel | null {
  const record = recordValue(value)
  const model = stringValue(record.model)
  if (!model) {
    return null
  }
  return {
    model,
    requests: numberValue(record.requests),
    inputTokens: numberValue(record.input_tokens ?? record.inputTokens),
    outputTokens: numberValue(record.output_tokens ?? record.outputTokens),
    cacheCreationTokens: numberValue(
      record.cache_creation_tokens ?? record.cacheCreationTokens,
    ),
    cacheReadTokens: numberValue(
      record.cache_read_tokens ?? record.cacheReadTokens,
    ),
    totalTokens: numberValue(record.total_tokens ?? record.totalTokens),
    costUsd: numberValue(record.cost_usd ?? record.costUsd),
    actualCostUsd: numberValue(
      record.actual_cost_usd ?? record.actualCostUsd,
    ),
  }
}

function toFamilyDeviceRuntime(
  value: Record<string, unknown>,
): AdminFamilyDeviceRuntime {
  const connection = recordValue(value.connection)
  return {
    isOnline: value.is_online === true,
    connectionState: stringValue(connection.state),
    transport: stringValue(connection.transport),
    reportedAt: stringValue(value.reported_at ?? value.reportedAt),
    receivedAt: stringValue(value.received_at ?? value.receivedAt),
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function recordValue(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' && value.length > 0 ? value : fallback
}

function numberValue(value: unknown, fallback = 0): number {
  if (value === null || value === undefined || value === '') {
    return fallback
  }
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function modelList(value: unknown, fallback: string[] = []): string[] {
  return Array.isArray(value)
    ? value.map(String).filter((item) => item.length > 0)
    : fallback
}
