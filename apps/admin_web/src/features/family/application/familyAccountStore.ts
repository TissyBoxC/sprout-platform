import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import {
  apiNoticeWarning,
  readApiNotice,
  type ApiNotice,
} from '@/api/apiNotice'
import {
  toGrowthReport,
  type AdminGrowthReport,
} from '@/features/family/application/growthReportRow'
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
  warning: string
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

export interface AdminFamilySpendingLimit {
  monthlyLimitCents: number
  currentMonthSpentCents: number
  updatedAt: string
}

export type { AdminGrowthReport }

export interface AdminFamilyPagination {
  page: number
  pageSize: number
  pendingPageSize: number | null
  total: number
  totalPages: number
  hasPrevious: boolean
  hasMore: boolean
}

const DEFAULT_PAGE_SIZE = 20
const MAX_PAGE_SIZE = 100

// 管理端以家长账号为主，AI 服务、资料与设备都归属于该账号。
export const useFamilyAccountStore = defineStore('admin-families', () => {
  const httpClient = createHttpClient()
  const operationsClient = createAdminOperationsClient(httpClient)
  const families = ref<AdminFamily[]>([])
  const modelOptions = ref<AIModelOption[]>([])
  const devicesByFamily = ref<Record<string, AdminFamilyDevice[]>>({})
  const deviceErrorsByFamily = ref<Record<string, ApiError | null>>({})
  const deviceLoadingCounts = ref<Record<string, number>>({})
  const aiUsageByFamily = ref<Record<string, AdminFamilyAiUsage>>({})
  const aiUsageErrorsByFamily = ref<Record<string, ApiError | null>>({})
  const aiUsageLoadingCounts = ref<Record<string, number>>({})
  const spendingLimitByFamily = ref<Record<string, AdminFamilySpendingLimit>>({})
  const spendingLimitErrorsByFamily = ref<Record<string, ApiError | null>>({})
  const spendingLimitLoadingCounts = ref<Record<string, number>>({})
  /// Growth reports are keyed by family and hold the newest page only, which is
  /// enough to answer whether the generator is producing for that household.
  const growthReportsByFamily = ref<Record<string, AdminGrowthReport[]>>({})
  const growthReportErrorsByFamily = ref<Record<string, ApiError | null>>({})
  const growthReportLoadingCounts = ref<Record<string, number>>({})
  const isLoading = ref(false)
  const isLoadingModels = ref(false)
  const isSubmitting = ref(false)
  const isUpdatingProfile = ref(false)
  const isUpdatingAiAccount = ref(false)
  const isResettingPassword = ref(false)
  const error = ref<ApiError | null>(null)
  const modelError = ref<ApiError | null>(null)
  const lastMessage = ref('')
  const lastWarning = ref('')
  const pagination = ref<AdminFamilyPagination>({
    page: 1,
    pageSize: DEFAULT_PAGE_SIZE,
    pendingPageSize: null,
    total: 0,
    totalPages: 0,
    hasPrevious: false,
    hasMore: false,
  })
  let familiesRequestSequence = 0
  let familiesLoadingSequence = 0
  let activeFamiliesLoadingRequests = 0
  let modelsRequestSequence = 0
  let modelsLoadingSequence = 0
  let activeModelsLoadingRequests = 0
  const deviceRequestSequences = new Map<string, number>()
  const deviceLoadingSequences = new Map<string, number>()
  const aiUsageRequestSequences = new Map<string, number>()
  const aiUsageLoadingSequences = new Map<string, number>()
  const spendingLimitRequestSequences = new Map<string, number>()
  const spendingLimitLoadingSequences = new Map<string, number>()
  const growthReportRequestSequences = new Map<string, number>()
  const growthReportLoadingSequences = new Map<string, number>()
  const pageStart = computed(() =>
    pagination.value.total === 0
      ? 0
      : (pagination.value.page - 1) * pagination.value.pageSize + 1,
  )
  const pageEnd = computed(() =>
    Math.min(
      pagination.value.page * pagination.value.pageSize,
      pagination.value.total,
    ),
  )

  async function load(page = pagination.value.page): Promise<void> {
    const familiesLoadSequence = beginFamiliesLoading()
    error.value = null
    modelError.value = null
    try {
      await Promise.all([loadFamilies(page, true), loadModels()])
    } finally {
      finishFamiliesLoading(familiesLoadSequence)
    }
  }

  async function create(input: CreateAdminFamilyInput): Promise<boolean> {
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const response = await httpClient.post('/api/v1/admin/families', {
        phone: input.phone.trim(),
        password: input.password,
        guardian_family_name: input.guardianFamilyName.trim(),
        child_nickname: input.childNickname.trim(),
        child_birthday: input.childBirthday,
      })
      const warning = auditWarning(readApiNotice(response.data))
      const refreshError = await loadFamilies(1, false)
      const aiAccount = toAiAccountFromResponse(response.data?.data)
      lastWarning.value = warning
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
      const warning = auditWarning(readApiNotice(response.data))
      const existing = familyById(parentAccountId)?.aiAccount ?? null
      const responseAccount = toAiAccountFromResponse(
        response.data?.data,
        existing,
      )
      if (responseAccount !== null) {
        replaceAiAccount(parentAccountId, responseAccount)
      }
      const refreshError = await loadFamilies(pagination.value.page, false)
      return { succeeded: true, error: null, refreshError, warning }
    } catch (caught: unknown) {
      return {
        succeeded: false,
        error: mapApiError(caught),
        refreshError: null,
        warning: '',
      }
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
      const warning = auditWarning(readApiNotice(response.data))
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
      const refreshError = await loadFamilies(pagination.value.page, false)
      return { succeeded: true, error: null, refreshError, warning }
    } catch (caught: unknown) {
      return {
        succeeded: false,
        error: mapApiError(caught),
        refreshError: null,
        warning: '',
      }
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
      const response = await httpClient.put(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/profile`,
        {
          display_name: input.displayName.trim(),
          guardian_family_name: input.guardianFamilyName.trim(),
          child_nickname: input.childNickname.trim(),
          child_birthday: input.childBirthday,
        },
      )
      const warning = auditWarning(readApiNotice(response.data))
      const refreshError = await loadFamilies(pagination.value.page, false)
      return { succeeded: true, error: null, refreshError, warning }
    } catch (caught: unknown) {
      return {
        succeeded: false,
        error: mapApiError(caught),
        refreshError: null,
        warning: '',
      }
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
      const response = await httpClient.post(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/password`,
        { password },
      )
      const warning = auditWarning(readApiNotice(response.data))
      const refreshError = await loadFamilies(pagination.value.page, false)
      return { succeeded: true, error: null, refreshError, warning }
    } catch (caught: unknown) {
      return {
        succeeded: false,
        error: mapApiError(caught),
        refreshError: null,
        warning: '',
      }
    } finally {
      isResettingPassword.value = false
    }
  }

  async function loadFamilyDevices(
    parentAccountId: string,
  ): Promise<ApiError | null> {
    const sequence = (deviceRequestSequences.get(parentAccountId) ?? 0) + 1
    deviceRequestSequences.set(parentAccountId, sequence)
    const loadingSequence = advanceLoadingSequence(
      deviceLoadingSequences,
      parentAccountId,
    )
    updateCount(deviceLoadingCounts, parentAccountId, 1)
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
      if (deviceRequestSequences.get(parentAccountId) !== sequence) {
        return null
      }
      devicesByFamily.value = {
        ...devicesByFamily.value,
        [parentAccountId]: devices,
      }
      setDeviceError(parentAccountId, null)
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (deviceRequestSequences.get(parentAccountId) === sequence) {
        setDeviceError(parentAccountId, mapped)
        return mapped
      }
      return null
    } finally {
      if (deviceLoadingSequences.get(parentAccountId) === loadingSequence) {
        resetCount(deviceLoadingCounts, parentAccountId)
      }
    }
  }

  async function loadFamilyAIUsage(
    parentAccountId: string,
  ): Promise<ApiError | null> {
    const sequence = (aiUsageRequestSequences.get(parentAccountId) ?? 0) + 1
    aiUsageRequestSequences.set(parentAccountId, sequence)
    const loadingSequence = advanceLoadingSequence(
      aiUsageLoadingSequences,
      parentAccountId,
    )
    updateCount(aiUsageLoadingCounts, parentAccountId, 1)
    setAIUsageError(parentAccountId, null)
    try {
      const response = await httpClient.get(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/ai-usage`,
      )
      const usage = toAiUsage(response.data?.data)
      if (aiUsageRequestSequences.get(parentAccountId) !== sequence) {
        return null
      }
      if (usage !== null) {
        aiUsageByFamily.value = {
          ...aiUsageByFamily.value,
          [parentAccountId]: usage,
        }
      }
      setAIUsageError(parentAccountId, null)
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (aiUsageRequestSequences.get(parentAccountId) === sequence) {
        setAIUsageError(parentAccountId, mapped)
        return mapped
      }
      return null
    } finally {
      if (aiUsageLoadingSequences.get(parentAccountId) === loadingSequence) {
        resetCount(aiUsageLoadingCounts, parentAccountId)
      }
    }
  }

  /// Loads the guardian-set monthly cap for one family. The console only reads
  /// it: the cap belongs to the guardian, so an operator cannot change it here.
  /// A family that never set a cap answers with zero, which the page renders as
  /// "未设置" rather than as a zero yuan budget.
  async function loadFamilySpendingLimit(
    parentAccountId: string,
  ): Promise<ApiError | null> {
    const sequence = (spendingLimitRequestSequences.get(parentAccountId) ?? 0) + 1
    spendingLimitRequestSequences.set(parentAccountId, sequence)
    const loadingSequence = advanceLoadingSequence(
      spendingLimitLoadingSequences,
      parentAccountId,
    )
    updateCount(spendingLimitLoadingCounts, parentAccountId, 1)
    setSpendingLimitError(parentAccountId, null)
    try {
      const response = await httpClient.get(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/spending-limit`,
      )
      if (spendingLimitRequestSequences.get(parentAccountId) !== sequence) {
        return null
      }
      spendingLimitByFamily.value = {
        ...spendingLimitByFamily.value,
        [parentAccountId]: toSpendingLimit(response.data?.data),
      }
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (spendingLimitRequestSequences.get(parentAccountId) === sequence) {
        setSpendingLimitError(parentAccountId, mapped)
        return mapped
      }
      return null
    } finally {
      if (spendingLimitLoadingSequences.get(parentAccountId) === loadingSequence) {
        resetCount(spendingLimitLoadingCounts, parentAccountId)
      }
    }
  }

  /// Loads the newest growth reports for one family so an operator can see
  /// whether the generator has produced anything for this household.
  async function loadFamilyGrowthReports(
    parentAccountId: string,
  ): Promise<ApiError | null> {
    if (!parentAccountId) {
      return null
    }
    const sequence = (growthReportRequestSequences.get(parentAccountId) ?? 0) + 1
    growthReportRequestSequences.set(parentAccountId, sequence)
    const loadingSequence = advanceLoadingSequence(
      growthReportLoadingSequences,
      parentAccountId,
    )
    updateCount(growthReportLoadingCounts, parentAccountId, 1)
    setGrowthReportError(parentAccountId, null)
    try {
      const response = await httpClient.get(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/growth-reports`,
        { params: { limit: 10 } },
      )
      if (growthReportRequestSequences.get(parentAccountId) !== sequence) {
        return null
      }
      const reports = response.data?.data?.reports
      growthReportsByFamily.value = {
        ...growthReportsByFamily.value,
        [parentAccountId]: Array.isArray(reports)
          ? reports.map(toGrowthReport)
          : [],
      }
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (growthReportRequestSequences.get(parentAccountId) === sequence) {
        setGrowthReportError(parentAccountId, mapped)
        return mapped
      }
      return null
    } finally {
      if (growthReportLoadingSequences.get(parentAccountId) === loadingSequence) {
        resetCount(growthReportLoadingCounts, parentAccountId)
      }
    }
  }

  function setGrowthReportError(
    parentAccountId: string,
    value: ApiError | null,
  ): void {
    growthReportErrorsByFamily.value = {
      ...growthReportErrorsByFamily.value,
      [parentAccountId]: value,
    }
  }

  function growthReportsFor(parentAccountId: string): AdminGrowthReport[] {
    return growthReportsByFamily.value[parentAccountId] ?? []
  }

  function growthReportErrorFor(parentAccountId: string): ApiError | null {
    return growthReportErrorsByFamily.value[parentAccountId] ?? null
  }

  function isGrowthReportLoading(parentAccountId: string): boolean {
    return (growthReportLoadingCounts.value[parentAccountId] ?? 0) > 0
  }

  async function loadFamilies(
    page: number,
    reportError: boolean,
  ): Promise<ApiError | null> {
    return loadFamiliesWithPageSize(page, reportError, pagination.value.pageSize)
  }

  async function loadFamiliesWithPageSize(
    page: number,
    reportError: boolean,
    pageSize: number,
  ): Promise<ApiError | null> {
    const requestedPage = Math.max(1, Math.trunc(page))
    const requestedPageSize = clampPageSize(pageSize)
    const sequence = ++familiesRequestSequence
    try {
      const response = await httpClient.get('/api/v1/admin/families', {
        params: {
          page: requestedPage,
          page_size: requestedPageSize,
        },
      })
      if (sequence !== familiesRequestSequence) {
        return null
      }
      const payload = response.data?.data ?? {}
      const records =
        payload.accounts ?? payload.families ?? payload.parent_accounts ?? []
      families.value = Array.isArray(records) ? records.map(toFamily) : []
      pagination.value = toPagination(
        payload.pagination,
        requestedPage,
        requestedPageSize,
        families.value.length,
      )
      if (
        families.value.length === 0 &&
        pagination.value.total > 0 &&
        requestedPage > pagination.value.totalPages
      ) {
        const lastPage = Math.max(1, pagination.value.totalPages)
        pagination.value = { ...pagination.value, page: lastPage }
        return loadFamiliesWithPageSize(lastPage, reportError, requestedPageSize)
      }
      error.value = null
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (sequence === familiesRequestSequence && reportError) {
        error.value = mapped
      }
      return sequence === familiesRequestSequence ? mapped : null
    }
  }

  async function goToPage(page: number): Promise<void> {
    const nextPage = Math.max(1, Math.trunc(page))
    if (
      nextPage === pagination.value.page ||
      (pagination.value.totalPages > 0 &&
        nextPage > pagination.value.totalPages)
    ) {
      return
    }
    if (isLoading.value) {
      return
    }
    const loadSequence = beginFamiliesLoading()
    error.value = null
    try {
      await loadFamilies(nextPage, true)
    } finally {
      finishFamiliesLoading(loadSequence)
    }
  }

  async function setPageSize(pageSize: number): Promise<void> {
    if (isLoading.value) {
      return
    }
    const nextPageSize = clampPageSize(pageSize)
    setPendingPageSize(nextPageSize)
    const loadSequence = beginFamiliesLoading()
    error.value = null
    try {
      const succeeded = await loadFamiliesWithPageSize(1, true, nextPageSize)
      if (succeeded !== null) {
        error.value = succeeded
      }
    } finally {
      clearPendingPageSize()
      finishFamiliesLoading(loadSequence)
    }
  }

  async function retryCurrentPage(): Promise<void> {
    if (isLoading.value) {
      return
    }
    const loadSequence = beginFamiliesLoading()
    error.value = null
    try {
      await loadFamilies(pagination.value.page, true)
    } finally {
      finishFamiliesLoading(loadSequence)
    }
  }

  async function loadModels(): Promise<void> {
    if (isLoadingModels.value) {
      return
    }
    const loadSequence = beginModelsLoading()
    const sequence = modelsRequestSequence
    try {
      const models = await operationsClient.loadAIModels()
      if (sequence === modelsRequestSequence) {
        modelOptions.value = models
        modelError.value = null
      }
    } catch (caught: unknown) {
      if (sequence === modelsRequestSequence) {
        modelError.value = mapApiError(caught)
      }
    } finally {
      finishModelsLoading(loadSequence)
    }
  }

  function beginFamiliesLoading(): number {
    const loadSequence = ++familiesLoadingSequence
    activeFamiliesLoadingRequests += 1
    isLoading.value = true
    return loadSequence
  }

  function finishFamiliesLoading(loadSequence: number): void {
    if (loadSequence !== familiesLoadingSequence) {
      return
    }
    activeFamiliesLoadingRequests = Math.max(
      0,
      activeFamiliesLoadingRequests - 1,
    )
    if (activeFamiliesLoadingRequests === 0) {
      isLoading.value = false
    }
  }

  function beginModelsLoading(): number {
    const loadSequence = ++modelsLoadingSequence
    modelsRequestSequence += 1
    activeModelsLoadingRequests += 1
    isLoadingModels.value = true
    return loadSequence
  }

  function finishModelsLoading(loadSequence: number): void {
    if (loadSequence !== modelsLoadingSequence) {
      return
    }
    activeModelsLoadingRequests = Math.max(0, activeModelsLoadingRequests - 1)
    if (activeModelsLoadingRequests === 0) {
      isLoadingModels.value = false
    }
  }

  function setPendingPageSize(pageSize: number): void {
    pagination.value = {
      ...pagination.value,
      pendingPageSize: pageSize,
    }
  }

  function clearPendingPageSize(): void {
    pagination.value = {
      ...pagination.value,
      pendingPageSize: null,
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
    return (deviceLoadingCounts.value[parentAccountId] ?? 0) > 0
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

  function setSpendingLimitError(
    parentAccountId: string,
    value: ApiError | null,
  ): void {
    spendingLimitErrorsByFamily.value = {
      ...spendingLimitErrorsByFamily.value,
      [parentAccountId]: value,
    }
  }

  function spendingLimitFor(
    parentAccountId: string,
  ): AdminFamilySpendingLimit | null {
    return spendingLimitByFamily.value[parentAccountId] ?? null
  }

  function spendingLimitErrorFor(parentAccountId: string): ApiError | null {
    return spendingLimitErrorsByFamily.value[parentAccountId] ?? null
  }

  function isSpendingLimitLoading(parentAccountId: string): boolean {
    return (spendingLimitLoadingCounts.value[parentAccountId] ?? 0) > 0
  }

  function isFamilyAIUsageLoading(parentAccountId: string): boolean {
    return (aiUsageLoadingCounts.value[parentAccountId] ?? 0) > 0
  }

  return {
    aiUsageErrorFor,
    aiUsageFor,
    loadFamilySpendingLimit,
    spendingLimitFor,
    spendingLimitErrorFor,
    isSpendingLimitLoading,
    loadFamilyGrowthReports,
    growthReportsFor,
    growthReportErrorFor,
    isGrowthReportLoading,
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
    lastWarning,
    load,
    loadFamilyAIUsage,
    loadFamilyDevices,
    loadModels,
    modelError,
    modelOptions,
    pageEnd,
    pageStart,
    pagination,
    resetParentPassword,
    retryCurrentPage,
    retryAiAccount,
    setPageSize,
    goToPage,
    updateAiAccount,
    updateParentProfile,
  }
})

function auditWarning(notice: ApiNotice | null): string {
  return apiNoticeWarning(notice)
}

function toPagination(
  value: unknown,
  fallbackPage: number,
  fallbackPageSize: number,
  recordCount: number,
): AdminFamilyPagination {
  const record = isRecord(value) ? value : {}
  const total = nonNegativeInteger(record.total, recordCount)
  const pageSize = clampPageSize(
    positiveInteger(record.page_size ?? record.pageSize, fallbackPageSize),
  )
  const totalPages = nonNegativeInteger(
    record.total_pages ?? record.totalPages,
    total === 0 ? 0 : Math.ceil(total / pageSize),
  )
  const page = positiveInteger(record.page, fallbackPage)
  return {
    page,
    pageSize,
    pendingPageSize: null,
    total,
    totalPages,
    hasPrevious: page > 1,
    hasMore: record.has_more === true || (totalPages > 0 && page < totalPages),
  }
}

function clampPageSize(value: number): number {
  return Math.min(MAX_PAGE_SIZE, Math.max(1, Math.trunc(value)))
}

function positiveInteger(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

function nonNegativeInteger(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= 0 ? parsed : fallback
}

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

function toSpendingLimit(value: unknown): AdminFamilySpendingLimit {
  const record = (value ?? {}) as Record<string, unknown>
  return {
    monthlyLimitCents: numberValue(record.monthly_limit_cents),
    currentMonthSpentCents: numberValue(record.current_month_spent_cents),
    updatedAt: stringValue(record.updated_at),
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

function updateCount(
  counts: { value: Record<string, number> },
  key: string,
  delta: number,
): void {
  const nextCount = Math.max(0, (counts.value[key] ?? 0) + delta)
  const next = { ...counts.value }
  if (nextCount === 0) {
    delete next[key]
  } else {
    next[key] = nextCount
  }
  counts.value = next
}

function resetCount(
  counts: { value: Record<string, number> },
  key: string,
): void {
  if (!(key in counts.value)) {
    return
  }
  const next = { ...counts.value }
  delete next[key]
  counts.value = next
}

function advanceLoadingSequence(
  sequences: Map<string, number>,
  key: string,
): number {
  const next = (sequences.get(key) ?? 0) + 1
  sequences.set(key, next)
  return next
}
