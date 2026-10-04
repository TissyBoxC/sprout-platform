import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export type DeviceHealthState = 'healthy' | 'degraded' | 'faulted' | 'unknown'

export interface AdminDiagnosticDevice {
  deviceId: string
  deviceName: string
  hardwareModel: string
  firmwareVersion: string
  isOnline: boolean
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

export interface DeviceDiagnosticsSnapshot {
  deviceId: string
  bootEvents: DeviceBootEvent[]
  failures: DeviceModuleFailure[]
  recoveryEvents: DeviceRecoveryEvent[]
  latestFailure: DeviceModuleFailure | null
  errorCount: number
  recoveryCount: number
  updatedAt: string
  healthState: DeviceHealthState
  retentionBootEvents: number
  retentionFailures: number
  retentionRecoveryEvents: number
}

export interface AdminDeviceDiagnosticsClient {
  loadDevices(): Promise<AdminDiagnosticDevice[]>
  loadDiagnostics(deviceId: string): Promise<DeviceDiagnosticsSnapshot>
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
    latestFailure,
    errorCount: numberValue(record.error_count ?? record.errorCount),
    recoveryCount: numberValue(record.recovery_count ?? record.recoveryCount),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
    healthState: healthState(record.health_state ?? record.healthState),
    retentionBootEvents: numberValue(record.retention_boot_events ?? record.retentionBootEvents),
    retentionFailures: numberValue(record.retention_failures ?? record.retentionFailures),
    retentionRecoveryEvents: numberValue(
      record.retention_recovery_events ?? record.retentionRecoveryEvents,
    ),
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
