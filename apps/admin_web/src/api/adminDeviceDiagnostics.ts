import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export type DeviceHealthState = 'healthy' | 'degraded' | 'faulted' | 'unknown'

export interface AdminDiagnosticDevice {
  deviceId: string
  deviceName: string
  hardwareModel: string
  firmwareVersion: string
  isOnline: boolean
  temperature: DeviceTemperature
}

export type DeviceTemperatureState = 'unknown' | 'normal' | 'warm' | 'hot' | 'critical'

export interface DeviceTemperature {
  state: DeviceTemperatureState
  celsiusTenths: number | null
}

export interface DeviceBootEvent {
  eventId: string
  sequence: number
  uptimeMs: number
  bootCount: number
  resetReason: string
  firmwareVersion: string
  reportedAt: string
}

export interface DeviceModuleFailure {
  eventId: string
  sequence: number
  moduleName: string
  errorCode: string
  failureCount: number
  firmwareVersion: string
  reportedAt: string
}

export interface DeviceRecoveryEvent {
  eventId: string
  sequence: number
  moduleName: string
  firmwareVersion: string
  reportedAt: string
}

export interface DeviceInteractionEvent {
  eventId: string
  sequence: number
  eventType: string
  detailCode: string
  durationMs: number
  firmwareVersion: string
  reportedAt: string
}

export interface DeviceCrashEvent {
  eventId: string
  sequence: number
  cause: string
  faultKind: string
  taskName: string
  programCounter: number
  frameCount: number
  coreId: number
  firmwareVersion: string
  reportedAt: string
}

export interface DeviceDiagnosticsSnapshot {
  deviceId: string
  bootEvents: DeviceBootEvent[]
  failures: DeviceModuleFailure[]
  recoveryEvents: DeviceRecoveryEvent[]
  interactionEvents: DeviceInteractionEvent[]
  crashEvents: DeviceCrashEvent[]
  latestFailure: DeviceModuleFailure | null
  latestCrash: DeviceCrashEvent | null
  errorCount: number
  recoveryCount: number
  crashCount: number
  updatedAt: string
  healthState: DeviceHealthState
  retentionBootEvents: number
  retentionFailures: number
  retentionRecoveryEvents: number
  retentionInteractionEvents: number
  retentionCrashEvents: number
}

export interface DeviceDiagnosticsCrashCause {
  cause: string
  count: number
}

export interface DeviceDiagnosticsSummary {
  deviceId: string
  windowDays: number
  bootCount: number
  failureCount: number
  recoveryCount: number
  crashCount: number
  crashCauses: DeviceDiagnosticsCrashCause[]
  healthState: DeviceHealthState
  lastReportedAt: string | null
  generatedAt: string
}

export interface AdminDeviceDiagnosticsClient {
  loadDevices(): Promise<AdminDiagnosticDevice[]>
  loadDiagnostics(deviceId: string): Promise<DeviceDiagnosticsSnapshot>
  loadSummary(deviceId: string, windowDays?: number): Promise<DeviceDiagnosticsSummary>
}

/// Loads the administrator-visible device inventory and one device's bounded
/// diagnostic history. The client only reads projected fields so internal
/// storage details are never exposed to the page.
export function createAdminDeviceDiagnosticsClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminDeviceDiagnosticsClient {
  return {
    async loadDevices(): Promise<AdminDiagnosticDevice[]> {
      const response = await httpClient.get('/api/v1/admin/devices')
      const payload = recordValue(response.data?.data)
      const records = arrayValue(payload.devices)
      return records.flatMap((value) => {
        const device = toNullableDevice(value)
        return device === null ? [] : [device]
      })
    },

    async loadDiagnostics(deviceId: string): Promise<DeviceDiagnosticsSnapshot> {
      const normalizedDeviceId = deviceId.trim()
      const response = await httpClient.get(
        `/api/v1/admin/devices/${encodeURIComponent(normalizedDeviceId)}/diagnostics`,
      )
      return toDiagnostics(response.data?.data)
    },

    async loadSummary(deviceId: string, windowDays = 7): Promise<DeviceDiagnosticsSummary> {
      const normalizedDeviceId = deviceId.trim()
      const response = await httpClient.get(
        `/api/v1/admin/devices/${encodeURIComponent(normalizedDeviceId)}/diagnostics/summary`,
        { params: { window_days: windowDays } },
      )
      return toSummary(response.data?.data)
    },
  }
}

function toNullableDevice(value: unknown): AdminDiagnosticDevice | null {
  const record = recordValue(value)
  const deviceId = stringValue(record.device_id ?? record.deviceId).trim()
  if (!deviceId) {
    return null
  }

  const runtime = recordValue(record.runtime)
  return {
    deviceId,
    deviceName: stringValue(record.device_name ?? record.deviceName),
    hardwareModel: stringValue(record.hardware_model ?? record.hardwareModel),
    firmwareVersion: stringValue(record.firmware_version ?? record.firmwareVersion),
    isOnline: runtime.is_online === true || record.is_online === true,
    temperature: toTemperature(runtime.temperature),
  }
}

function toTemperature(value: unknown): DeviceTemperature {
  const record = recordValue(value)
  const rawTenths = record.celsius_tenths ?? record.celsiusTenths
  const parsed = Number(rawTenths)
  const celsiusTenths =
    rawTenths === null || rawTenths === undefined || !Number.isFinite(parsed)
      ? null
      : Math.trunc(parsed)
  return {
    state: temperatureState(record.state),
    celsiusTenths,
  }
}

function temperatureState(value: unknown): DeviceTemperatureState {
  switch (value) {
    case 'normal':
    case 'warm':
    case 'hot':
    case 'critical':
      return value
    default:
      return 'unknown'
  }
}

function toDiagnostics(value: unknown): DeviceDiagnosticsSnapshot {
  const record = recordValue(value)
  const deviceId = stringValue(record.device_id ?? record.deviceId).trim()
  if (!deviceId) {
    throw new Error('diagnostics response is missing a device identifier')
  }

  const failures = arrayValue(record.failures).flatMap((item) => {
    const failure = toNullableFailure(item)
    return failure === null ? [] : [failure]
  })
  const latestFailureValue = record.latest_failure ?? record.latestFailure
  const latestFailure =
    latestFailureValue === null || latestFailureValue === undefined
      ? null
      : toNullableFailure(latestFailureValue)
  const latestCrashValue = record.latest_crash ?? record.latestCrash
  const latestCrash =
    latestCrashValue === null || latestCrashValue === undefined
      ? null
      : toNullableCrashEvent(latestCrashValue)

  return {
    deviceId,
    bootEvents: arrayValue(record.boot_events).flatMap((item) => {
      const event = toNullableBootEvent(item)
      return event === null ? [] : [event]
    }),
    failures,
    recoveryEvents: arrayValue(record.recovery_events).flatMap((item) => {
      const event = toNullableRecoveryEvent(item)
      return event === null ? [] : [event]
    }),
    interactionEvents: arrayValue(record.interaction_events).flatMap((item) => {
      const event = toNullableInteractionEvent(item)
      return event === null ? [] : [event]
    }),
    crashEvents: arrayValue(record.crash_events).flatMap((item) => {
      const event = toNullableCrashEvent(item)
      return event === null ? [] : [event]
    }),
    latestFailure,
    latestCrash,
    errorCount: numberValue(record.error_count ?? record.errorCount),
    recoveryCount: numberValue(record.recovery_count ?? record.recoveryCount),
    crashCount: numberValue(record.crash_count ?? record.crashCount),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
    healthState: healthState(record.health_state ?? record.healthState),
    retentionBootEvents: numberValue(record.retention_boot_events ?? record.retentionBootEvents),
    retentionFailures: numberValue(record.retention_failures ?? record.retentionFailures),
    retentionRecoveryEvents: numberValue(
      record.retention_recovery_events ?? record.retentionRecoveryEvents,
    ),
    retentionInteractionEvents: numberValue(
      record.retention_interaction_events ?? record.retentionInteractionEvents,
    ),
    retentionCrashEvents: numberValue(record.retention_crash_events ?? record.retentionCrashEvents),
  }
}

function toNullableBootEvent(value: unknown): DeviceBootEvent | null {
  const record = recordValue(value)
  const sequence = numberValue(record.sequence)
  const eventId = stringValue(record.event_id ?? record.eventId).trim()
  if (!eventId && !sequence) {
    return null
  }

  return {
    eventId: eventId || `boot-${sequence}`,
    sequence,
    uptimeMs: numberValue(record.uptime_ms ?? record.uptimeMs),
    bootCount: numberValue(record.boot_count ?? record.bootCount),
    resetReason: stringValue(record.reset_reason ?? record.resetReason),
    firmwareVersion: stringValue(record.firmware_version ?? record.firmwareVersion),
    reportedAt: stringValue(record.reported_at ?? record.reportedAt),
  }
}

function toNullableFailure(value: unknown): DeviceModuleFailure | null {
  const record = recordValue(value)
  const sequence = numberValue(record.sequence)
  const eventId = stringValue(record.event_id ?? record.eventId).trim()
  const moduleName = stringValue(record.module_name ?? record.moduleName).trim()
  if (!eventId && !sequence && !moduleName) {
    return null
  }

  return {
    eventId: eventId || `failure-${sequence}`,
    sequence,
    moduleName,
    errorCode: stringValue(record.error_code ?? record.errorCode),
    failureCount: numberValue(record.failure_count ?? record.failureCount),
    firmwareVersion: stringValue(record.firmware_version ?? record.firmwareVersion),
    reportedAt: stringValue(record.reported_at ?? record.reportedAt),
  }
}

function toNullableRecoveryEvent(value: unknown): DeviceRecoveryEvent | null {
  const record = recordValue(value)
  const sequence = numberValue(record.sequence)
  const eventId = stringValue(record.event_id ?? record.eventId).trim()
  const moduleName = stringValue(record.module_name ?? record.moduleName).trim()
  if (!eventId && !sequence && !moduleName) {
    return null
  }

  return {
    eventId: eventId || `recovery-${sequence}`,
    sequence,
    moduleName,
    firmwareVersion: stringValue(record.firmware_version ?? record.firmwareVersion),
    reportedAt: stringValue(record.reported_at ?? record.reportedAt),
  }
}

function toNullableInteractionEvent(value: unknown): DeviceInteractionEvent | null {
  const record = recordValue(value)
  const sequence = numberValue(record.sequence)
  const eventId = stringValue(record.event_id ?? record.eventId).trim()
  const eventType = stringValue(record.event_type ?? record.eventType).trim()
  if (!eventId && !sequence && !eventType) {
    return null
  }

  return {
    eventId: eventId || `interaction-${sequence}`,
    sequence,
    eventType,
    detailCode: stringValue(record.detail_code ?? record.detailCode),
    durationMs: numberValue(record.duration_ms ?? record.durationMs),
    firmwareVersion: stringValue(record.firmware_version ?? record.firmwareVersion),
    reportedAt: stringValue(record.reported_at ?? record.reportedAt),
  }
}

function toNullableCrashEvent(value: unknown): DeviceCrashEvent | null {
  const record = recordValue(value)
  const sequence = numberValue(record.sequence)
  const eventId = stringValue(record.event_id ?? record.eventId).trim()
  const cause = stringValue(record.cause).trim()
  if (!eventId && !sequence && !cause) {
    return null
  }

  return {
    eventId: eventId || `crash-${sequence}`,
    sequence,
    cause: cause || 'unknown',
    faultKind: stringValue(record.fault_kind ?? record.faultKind) || 'unknown',
    taskName: stringValue(record.task_name ?? record.taskName),
    programCounter: numberValue(record.program_counter ?? record.programCounter),
    frameCount: numberValue(record.frame_count ?? record.frameCount),
    coreId: numberValue(record.core_id ?? record.coreId),
    firmwareVersion: stringValue(record.firmware_version ?? record.firmwareVersion),
    reportedAt: stringValue(record.reported_at ?? record.reportedAt),
  }
}

function toSummary(value: unknown): DeviceDiagnosticsSummary {
  const record = recordValue(value)
  const deviceId = stringValue(record.device_id ?? record.deviceId).trim()
  if (!deviceId) {
    throw new Error('diagnostics summary response is missing a device identifier')
  }
  const lastReportedValue = record.last_reported_at ?? record.lastReportedAt
  return {
    deviceId,
    windowDays: numberValue(record.window_days ?? record.windowDays),
    bootCount: numberValue(record.boot_count ?? record.bootCount),
    failureCount: numberValue(record.failure_count ?? record.failureCount),
    recoveryCount: numberValue(record.recovery_count ?? record.recoveryCount),
    crashCount: numberValue(record.crash_count ?? record.crashCount),
    crashCauses: arrayValue(record.crash_causes ?? record.crashCauses).flatMap((item) => {
      const cause = recordValue(item)
      const name = stringValue(cause.cause).trim()
      if (!name) {
        return []
      }
      return [{ cause: name, count: numberValue(cause.count) }]
    }),
    healthState: healthState(record.health_state ?? record.healthState),
    lastReportedAt:
      typeof lastReportedValue === 'string' && lastReportedValue ? lastReportedValue : null,
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function healthState(value: unknown): DeviceHealthState {
  switch (value) {
    case 'healthy':
    case 'degraded':
    case 'faulted':
    case 'unknown':
      return value
    default:
      return 'unknown'
  }
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function recordValue(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}

function numberValue(value: unknown): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? Math.max(0, parsed) : 0
}
