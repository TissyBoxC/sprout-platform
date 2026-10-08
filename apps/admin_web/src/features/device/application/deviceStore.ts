import { defineStore } from 'pinia'
import { ref } from 'vue'

import { mapApiError, type ApiError } from '@/api/apiError'
import { createHttpClient } from '@/api/httpClient'

export type DeviceCommandType =
  | 'refresh_configuration'
  | 'reconnect_network'
  | 'resync_time'
  | 'factory_reset'

export type ProvisioningState = 'unprovisioned' | 'provisioning' | 'provisioned'

export type ProvisioningSessionState = 'ready' | 'reauth_required' | 'revoked'

export type ProvisioningEventType =
  | 'provisioning_started'
  | 'wifi_configured'
  | 'wifi_failed'
  | 'binding_completed'
  | 'binding_removed'
  | 'network_reconnected'
  | 'network_lost'
  | 'time_synced'
  | 'auth_revoked'
  | 'auth_restored'
  | 'binding_confirmed'
  | 'binding_pending'

export interface DeviceProvisioning {
  state: ProvisioningState
  wifiConfigured: boolean
  sessionState: ProvisioningSessionState
  droppedEvents: number
  lastProvisionedAt: string | null
}

export interface ProvisioningEvent {
  eventId: string
  eventType: ProvisioningEventType
  sequence: number
  detailCode: string
  durationMs: number
  firmwareVersion: string
  reportedAt: string
}

export interface ProvisioningSnapshot extends DeviceProvisioning {
  deviceId: string
  newestSequence: number
  droppedEvents: number
  updatedAt: string
  events: ProvisioningEvent[]
}

/// Runtime projection used by the operations console.
export interface DeviceRuntime {
  isOnline: boolean
  connectionState: string
  transport: string
  networkQuality: string
  rssiDbm: number
  latencyMs: number
  packetLossPercent: number
  timeSyncState: string
  lastSyncedAt: string
  offlineState: string
  offlineReason: string
  fallbackActive: boolean
  pendingTelemetry: number
  reportedAt: string
  receivedAt: string
  provisioning?: DeviceProvisioning
}

/// Bound device and its latest runtime snapshot.
export interface AdminDevice {
  deviceId: string
  parentAccountId: string
  deviceName: string
  hardwareModel: string
  firmwareVersion: string
  capabilities: string[]
  lifecycleStatus: string
  boundAt: string
  updatedAt: string
  runtime: DeviceRuntime | null
}

export interface DeviceCommand {
  commandId: string
  deviceId: string
  commandType: DeviceCommandType
  status: string
  requestId: string
  createdAt: string
  deliveredAt: string
  acknowledgedAt: string
  resultCode: string
}

/// Loads device state and owns administrator maintenance commands.
export const useDeviceStore = defineStore('admin-devices', () => {
  const httpClient = createHttpClient()
  const devices = ref<AdminDevice[]>([])
  const commands = ref<Record<string, DeviceCommand[]>>({})
  const isLoading = ref(false)
  const isSubmitting = ref(false)
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')
  const provisioningSnapshots = ref<Record<string, ProvisioningSnapshot | undefined>>({})
  const isLoadingProvisioning = ref(false)
  const provisioningError = ref<ApiError | null>(null)
  const isRevokingSessions = ref(false)
  const revokeError = ref<ApiError | null>(null)

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      const response = await httpClient.get('/api/v1/admin/devices')
      devices.value = (response.data.data.devices ?? []).map(toDevice)
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoading.value = false
    }
  }

  /**
   * 按家长范围解绑设备，成功后刷新设备列表。
   *
   * 后端以 parentAccountId 和 deviceId 共同限定目标，避免管理员解绑到
   * 其他家庭的设备。列表接口当前不返回 parent_account_id 时，调用方不得
   * 猜测该值。
   */
  async function unbind(parentAccountId: string, deviceId: string): Promise<boolean> {
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    try {
      await httpClient.delete(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/devices/${encodeURIComponent(deviceId)}`,
      )
      updateRevokedSession(deviceId)
      await loadProvisioning(deviceId)
      await load()
      lastMessage.value = '设备已解除绑定。'
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isSubmitting.value = false
    }
  }

  async function loadCommands(deviceId: string): Promise<void> {
    error.value = null
    try {
      const response = await httpClient.get(`/api/v1/admin/devices/${deviceId}/commands`)
      commands.value = {
        ...commands.value,
        [deviceId]: (response.data.data.commands ?? []).map(toCommand),
      }
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    }
  }

  async function sendCommand(deviceId: string, commandType: DeviceCommandType): Promise<boolean> {
    isSubmitting.value = true
    error.value = null
    lastMessage.value = ''
    try {
      await httpClient.post(`/api/v1/admin/devices/${deviceId}/commands`, {
        command_type: commandType,
      })
      lastMessage.value = '操作已发送，设备将在下一次连接时执行。'
      await loadCommands(deviceId)
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    } finally {
      isSubmitting.value = false
    }
  }

  async function loadProvisioning(deviceId: string): Promise<ProvisioningSnapshot | null> {
    const normalizedDeviceId = deviceId.trim()
    if (!normalizedDeviceId) {
      provisioningError.value = {
        kind: 'validation',
        message: '没有找到这台设备，请重新加载后再试。',
        retryable: false,
      }
      return null
    }

    isLoadingProvisioning.value = true
    provisioningError.value = null
    try {
      const response = await httpClient.get(
        `/api/v1/admin/devices/${encodeURIComponent(normalizedDeviceId)}/provisioning`,
      )
      const snapshot = toProvisioningSnapshot(response.data?.data, normalizedDeviceId)
      provisioningSnapshots.value = {
        ...provisioningSnapshots.value,
        [normalizedDeviceId]: snapshot ?? undefined,
      }
      return snapshot
    } catch (caught: unknown) {
      provisioningError.value = withProvisioningLoadMessage(mapApiError(caught))
      return null
    } finally {
      isLoadingProvisioning.value = false
    }
  }

  async function revokeSessions(deviceId: string, disableDevice: boolean): Promise<boolean> {
    const normalizedDeviceId = deviceId.trim()
    if (!normalizedDeviceId) {
      revokeError.value = {
        kind: 'validation',
        message: '没有找到这台设备，请重新加载后再试。',
        retryable: false,
      }
      return false
    }

    isRevokingSessions.value = true
    revokeError.value = null
    lastMessage.value = ''
    try {
      const response = await httpClient.post(
        `/api/v1/admin/devices/${encodeURIComponent(normalizedDeviceId)}/sessions/revoke`,
        { disable_device: disableDevice },
      )
      const payload = record(response.data?.data)
      if (payload.sessions_revoked !== true) {
        revokeError.value = {
          kind: 'unexpected',
          message: '会话吊销没有完成，请重新加载设备状态后再试。',
          retryable: true,
        }
        return false
      }

      updateRevokedSession(normalizedDeviceId)
      lastMessage.value = disableDevice
        ? '设备会话已吊销，设备已停用。设备需要由维护人员重新启用后才能继续使用。'
        : '设备会话已吊销，设备将在下次连接时重新认证。'
      await loadProvisioning(normalizedDeviceId)
      return true
    } catch (caught: unknown) {
      revokeError.value = withRevokeMessage(mapApiError(caught))
      return false
    } finally {
      isRevokingSessions.value = false
    }
  }

  function updateRevokedSession(deviceId: string): void {
    const snapshot = provisioningSnapshots.value[deviceId]
    if (snapshot) {
      provisioningSnapshots.value = {
        ...provisioningSnapshots.value,
        [deviceId]: {
          ...snapshot,
          sessionState: 'revoked',
        },
      }
    }
    const devicesWithRevokedSession = devices.value.map((device) =>
      device.deviceId === deviceId && device.runtime
        ? {
            ...device,
            runtime: {
              ...device.runtime,
              provisioning: device.runtime.provisioning
                ? { ...device.runtime.provisioning, sessionState: 'revoked' as const }
                : {
                    state: 'unprovisioned' as const,
                    wifiConfigured: false,
                    sessionState: 'revoked' as const,
                    droppedEvents: 0,
                    lastProvisionedAt: null,
                  },
            },
          }
        : device,
    )
    devices.value = devicesWithRevokedSession
  }

  return {
    commands,
    devices,
    error,
    isLoading,
    isLoadingProvisioning,
    isSubmitting,
    isRevokingSessions,
    lastMessage,
    provisioningError,
    provisioningSnapshots,
    revokeError,
    load,
    loadCommands,
    loadProvisioning,
    revokeSessions,
    sendCommand,
    unbind,
  }
})

function toDevice(value: Record<string, unknown>): AdminDevice {
  return {
    deviceId: String(value.device_id ?? ''),
    parentAccountId: String(value.parent_account_id ?? ''),
    deviceName: String(value.device_name ?? ''),
    hardwareModel: String(value.hardware_model ?? ''),
    firmwareVersion: String(value.firmware_version ?? ''),
    capabilities: stringList(value.capabilities),
    lifecycleStatus: String(value.lifecycle_status ?? ''),
    boundAt: String(value.bound_at ?? ''),
    updatedAt: String(value.updated_at ?? ''),
    runtime: isRecord(value.runtime) ? toRuntime(value.runtime) : null,
  }
}

function toRuntime(value: Record<string, unknown>): DeviceRuntime {
  const connection = record(value.connection)
  const quality = record(value.network_quality)
  const timeSync = record(value.time_sync)
  const offline = record(value.offline)
  return {
    isOnline: value.is_online === true,
    connectionState: String(connection.state ?? ''),
    transport: String(connection.transport ?? ''),
    networkQuality: String(quality.level ?? ''),
    rssiDbm: number(quality.rssi_dbm),
    latencyMs: number(quality.latency_ms),
    packetLossPercent: number(quality.packet_loss_percent),
    timeSyncState: String(timeSync.state ?? ''),
    lastSyncedAt: String(timeSync.last_synced_at ?? ''),
    offlineState: String(offline.state ?? ''),
    offlineReason: String(offline.reason ?? ''),
    fallbackActive: offline.fallback_active === true,
    pendingTelemetry: number(offline.pending_telemetry),
    reportedAt: String(value.reported_at ?? ''),
    receivedAt: String(value.received_at ?? ''),
    provisioning: toProvisioning(value.provisioning),
  }
}

function toProvisioning(value: unknown): DeviceProvisioning | undefined {
  if (!isRecord(value)) {
    return undefined
  }

  const state = provisioningState(value.state)
  const sessionState = provisioningSessionState(value.session_state)
  if (state === null || sessionState === null) {
    return undefined
  }

  return {
    state,
    wifiConfigured: value.wifi_configured === true,
    sessionState,
    droppedEvents: number(value.dropped_events),
    lastProvisionedAt: nullableString(value.last_provisioned_at),
  }
}

function toProvisioningSnapshot(
  value: unknown,
  fallbackDeviceId: string,
): ProvisioningSnapshot | null {
  if (!isRecord(value)) {
    return null
  }

  const state = provisioningState(value.state)
  const sessionState = provisioningSessionState(value.session_state)
  if (state === null || sessionState === null) {
    return null
  }

  return {
    deviceId: String(value.device_id ?? '').trim() || fallbackDeviceId,
    state,
    wifiConfigured: value.wifi_configured === true,
    sessionState,
    droppedEvents: number(value.dropped_events),
    lastProvisionedAt: nullableString(value.last_provisioned_at),
    newestSequence: number(value.newest_sequence),
    updatedAt: String(value.updated_at ?? ''),
    events: Array.isArray(value.events)
      ? value.events.flatMap((event) => {
          const parsed = toProvisioningEvent(event)
          return parsed === null ? [] : [parsed]
        })
      : [],
  }
}

function toProvisioningEvent(value: unknown): ProvisioningEvent | null {
  if (!isRecord(value)) {
    return null
  }

  const eventType = provisioningEventType(value.event_type)
  if (eventType === null) {
    return null
  }

  const sequence = number(value.sequence)
  const eventId = String(value.event_id ?? '').trim()
  return {
    eventId: eventId || `provisioning-${sequence}`,
    eventType,
    sequence,
    detailCode: String(value.detail_code ?? ''),
    durationMs: number(value.duration_ms),
    firmwareVersion: String(value.firmware_version ?? ''),
    reportedAt: String(value.reported_at ?? ''),
  }
}

function provisioningState(value: unknown): ProvisioningState | null {
  switch (value) {
    case 'unprovisioned':
    case 'provisioning':
    case 'provisioned':
      return value
    default:
      return null
  }
}

function provisioningSessionState(value: unknown): ProvisioningSessionState | null {
  switch (value) {
    case 'ready':
    case 'reauth_required':
    case 'revoked':
      return value
    default:
      return null
  }
}

function provisioningEventType(value: unknown): ProvisioningEventType | null {
  switch (value) {
    case 'provisioning_started':
    case 'wifi_configured':
    case 'wifi_failed':
    case 'binding_completed':
    case 'binding_removed':
    case 'network_reconnected':
    case 'network_lost':
    case 'time_synced':
    case 'auth_revoked':
    case 'auth_restored':
    case 'binding_confirmed':
    case 'binding_pending':
      return value
    default:
      return null
  }
}

function toCommand(value: Record<string, unknown>): DeviceCommand {
  return {
    commandId: String(value.command_id ?? ''),
    deviceId: String(value.device_id ?? ''),
    commandType: String(value.command_type ?? '') as DeviceCommandType,
    status: String(value.status ?? ''),
    requestId: String(value.request_id ?? ''),
    createdAt: String(value.created_at ?? ''),
    deliveredAt: String(value.delivered_at ?? ''),
    acknowledgedAt: String(value.acknowledged_at ?? ''),
    resultCode: String(value.result_code ?? ''),
  }
}

function record(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : {}
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.map(String) : []
}

function number(value: unknown): number {
  return typeof value === 'number' ? value : Number(value ?? 0)
}

function nullableString(value: unknown): string | null {
  return typeof value === 'string' && value.length > 0 ? value : null
}

function withProvisioningLoadMessage(error: ApiError): ApiError {
  switch (error.kind) {
    case 'not_found':
      return {
        ...error,
        message: '暂时找不到这台设备的配网记录，请重新加载设备列表后再试。',
      }
    case 'network':
      return {
        ...error,
        message: '网络连接不稳定，配网记录暂时无法读取，请检查网络后重试。',
      }
    case 'service_unavailable':
      return {
        ...error,
        message: '配网记录暂时无法读取，请稍后重试。',
      }
    default:
      return error
  }
}

function withRevokeMessage(error: ApiError): ApiError {
  switch (error.kind) {
    case 'not_found':
      return {
        ...error,
        message: '没有找到这台设备，请重新加载设备列表后再试。',
      }
    case 'network':
      return {
        ...error,
        message: '网络连接不稳定，设备会话没有变更，请检查网络后重试。',
      }
    case 'service_unavailable':
      return {
        ...error,
        message: '服务暂时不可用，设备会话没有变更，请稍后重试。',
      }
    default:
      return error
  }
}
