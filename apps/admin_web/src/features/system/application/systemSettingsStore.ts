import { defineStore } from 'pinia'
import { ref } from 'vue'

import { mapApiError, type ApiError } from '@/api/apiError'
import {
  createAdminOperationsClient,
  type AIModelOption,
} from '@/api/adminOperations'
import { createHttpClient } from '@/api/httpClient'

export interface SystemSettings {
  defaultBalanceUsd: number
  defaultConcurrencyLimit: number
  defaultModels: string[]
  registrationEnabled: boolean
  smsVerificationEnabled: boolean
  emailLoginEnabled: boolean
  minorModeDefaultEnabled: boolean
  outputModerationEnabled: boolean
  crisisInterventionEnabled: boolean
  releaseChannel: string
  minimumClientVersion: string
  mandatoryUpdateThreshold: string
  audioRetentionDays: number
  imageRetentionDays: number
  conversationRetentionDays: number
}

export const DEFAULT_SYSTEM_SETTINGS: SystemSettings = {
  defaultBalanceUsd: 0,
  defaultConcurrencyLimit: 1,
  defaultModels: [],
  registrationEnabled: true,
  smsVerificationEnabled: true,
  emailLoginEnabled: true,
  minorModeDefaultEnabled: true,
  outputModerationEnabled: true,
  crisisInterventionEnabled: true,
  releaseChannel: 'stable',
  minimumClientVersion: '',
  mandatoryUpdateThreshold: '',
  audioRetentionDays: 0,
  imageRetentionDays: 0,
  conversationRetentionDays: 30,
}

// 设置按后端文档整体读取与整体保存，避免局部写入造成运行策略不一致。
export const useSystemSettingsStore = defineStore('admin-system-settings', () => {
  const httpClient = createHttpClient()
  const operationsClient = createAdminOperationsClient(httpClient)
  const settings = ref<SystemSettings>({ ...DEFAULT_SYSTEM_SETTINGS })
  const version = ref(0)
  const modelOptions = ref<AIModelOption[]>([])
  const defaultBalanceSource = ref('')
  const integrationNotice = ref('')
  const isLoading = ref(false)
  const isSaving = ref(false)
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    integrationNotice.value = ''
    try {
      const response = await httpClient.get('/api/v1/admin/settings')
      settings.value = toSettings(response.data.data?.settings ?? {})
      version.value = numberValue(response.data.data?.version, 0)
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoading.value = false
    }

    await synchronizeAIProviderDefaults()
  }

  async function save(): Promise<boolean> {
    isSaving.value = true
    error.value = null
    lastMessage.value = ''
    try {
      const response = await httpClient.put(
        '/api/v1/admin/settings',
        {
          settings: toPayload(settings.value),
          expected_version: version.value,
        },
      )
      settings.value = toSettings(response.data.data?.settings ?? {})
      version.value = numberValue(response.data.data?.version, version.value)
      lastMessage.value = '系统设置已保存。'
      return true
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (isSettingsVersionConflict(caught)) {
        lastMessage.value = ''
        error.value = {
          kind: 'validation',
          message: '系统设置已被其他管理员更新，请刷新后重试',
          retryable: true,
        }
      } else {
        error.value = mapped
      }
      return false
    } finally {
      isSaving.value = false
    }
  }

  return {
    defaultBalanceSource,
    error,
    integrationNotice,
    isLoading,
    isSaving,
    lastMessage,
    load,
    modelOptions,
    save,
    settings,
  }

  async function synchronizeAIProviderDefaults(): Promise<void> {
    const [defaultsResult, modelsResult] = await Promise.allSettled([
      operationsClient.loadAIAccountDefaults(),
      operationsClient.loadAIModels(),
    ])

    if (defaultsResult.status === 'fulfilled' && defaultsResult.value !== null) {
      const defaults = defaultsResult.value
      if (defaults.defaultBalanceUsd !== null) {
        settings.value.defaultBalanceUsd = defaults.defaultBalanceUsd
      }
      if (defaults.defaultConcurrencyLimit !== null) {
        settings.value.defaultConcurrencyLimit =
          defaults.defaultConcurrencyLimit
      }
      defaultBalanceSource.value =
        defaults.source === 'sub2api' ? 'AI 服务统一设置' : defaults.source
    } else {
      // 统一默认值接口尚未上线时，至少以当前已保存的运营设置为准，避免页面
      // 继续展示上一次部署遗留的余额。
      defaultBalanceSource.value = '当前系统设置'
    }

    if (modelsResult.status === 'fulfilled') {
      modelOptions.value = modelsResult.value
      const fastestModel = fastestAvailableModel(modelsResult.value)
      if (fastestModel !== null) {
        settings.value.defaultModels = [fastestModel.id]
      }
    }

    if (defaultsResult.status === 'rejected' || modelsResult.status === 'rejected') {
      integrationNotice.value =
        '暂时无法读取 AI 服务的最新配置，当前显示上次保存的内容。'
    }
  }
})

function isSettingsVersionConflict(error: unknown): boolean {
  const response = (
    error as { response?: { data?: { error?: { code?: unknown } } } }
  )?.response
  return response?.data?.error?.code === 'settings_version_conflict'
}

function fastestAvailableModel(models: AIModelOption[]): AIModelOption | null {
  const availableModels = models.filter(
    (model) => model.id && model.latencyMs !== null && model.latencyMs >= 0,
  )
  if (availableModels.length === 0) {
    return null
  }
  return availableModels.reduce((fastest, model) =>
    (model.latencyMs ?? Number.POSITIVE_INFINITY) <
    (fastest.latencyMs ?? Number.POSITIVE_INFINITY)
      ? model
      : fastest,
  )
}

function toSettings(value: Record<string, unknown>): SystemSettings {
  const defaults = DEFAULT_SYSTEM_SETTINGS
  const ai = recordValue(value.ai)
  const account = recordValue(value.account)
  const safety = recordValue(value.safety)
  const update = recordValue(value.update)
  const retention = recordValue(value.retention)

  return {
    defaultBalanceUsd: numberValue(ai.default_balance_usd, defaults.defaultBalanceUsd),
    defaultConcurrencyLimit: numberValue(
      ai.default_concurrency,
      defaults.defaultConcurrencyLimit,
    ),
    defaultModels: stringListValue(ai.default_models),
    registrationEnabled: booleanValue(
      account.registration_enabled,
      defaults.registrationEnabled,
    ),
    smsVerificationEnabled: booleanValue(
      account.phone_verification_required,
      defaults.smsVerificationEnabled,
    ),
    emailLoginEnabled: booleanValue(
      account.email_login_enabled,
      defaults.emailLoginEnabled,
    ),
    minorModeDefaultEnabled: booleanValue(
      safety.minor_mode_default,
      defaults.minorModeDefaultEnabled,
    ),
    outputModerationEnabled: booleanValue(
      safety.output_moderation_enabled,
      defaults.outputModerationEnabled,
    ),
    crisisInterventionEnabled: booleanValue(
      safety.crisis_intervention_enabled,
      defaults.crisisInterventionEnabled,
    ),
    releaseChannel: stringValue(update.channel, defaults.releaseChannel),
    minimumClientVersion: stringValue(
      update.min_client_version,
      defaults.minimumClientVersion,
    ),
    mandatoryUpdateThreshold: stringValue(
      update.force_upgrade_below,
      defaults.mandatoryUpdateThreshold,
    ),
    audioRetentionDays: numberValue(
      retention.audio_days,
      defaults.audioRetentionDays,
    ),
    imageRetentionDays: numberValue(
      retention.image_days,
      defaults.imageRetentionDays,
    ),
    conversationRetentionDays: numberValue(
      retention.conversation_days,
      defaults.conversationRetentionDays,
    ),
  }
}

function toPayload(settings: SystemSettings): Record<string, unknown> {
  return {
    ai: {
      default_balance_usd: settings.defaultBalanceUsd,
      default_concurrency: settings.defaultConcurrencyLimit,
      default_models: settings.defaultModels
        .map((model) => model.trim())
        .filter(Boolean),
    },
    account: {
      registration_enabled: settings.registrationEnabled,
      phone_verification_required: settings.smsVerificationEnabled,
      email_login_enabled: settings.emailLoginEnabled,
    },
    safety: {
      minor_mode_default: settings.minorModeDefaultEnabled,
      output_moderation_enabled: settings.outputModerationEnabled,
      crisis_intervention_enabled: settings.crisisInterventionEnabled,
      // P0 约束要求儿童音视频默认不上传。开启前必须实现逐项监护人授权流程。
      allow_audio_upload: false,
      allow_image_upload: false,
    },
    update: {
      channel: settings.releaseChannel,
      min_client_version: settings.minimumClientVersion,
      force_upgrade_below: settings.mandatoryUpdateThreshold,
    },
    retention: {
      audio_days: settings.audioRetentionDays,
      image_days: settings.imageRetentionDays,
      conversation_days: settings.conversationRetentionDays,
    },
  }
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function stringValue(value: unknown, fallback: string): string {
  return typeof value === 'string' ? value : fallback
}

function numberValue(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function booleanValue(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback
}

function stringListValue(value: unknown): string[] {
  return Array.isArray(value) ? value.map(String).filter(Boolean) : []
}
