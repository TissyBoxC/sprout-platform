import type { AxiosInstance } from 'axios'

import { createHttpClient } from '@/api/httpClient'

export interface BluetoothAudioBucket {
  label: string
  count: number
}

export interface AdminBluetoothAudioStats {
  windowDays: number
  deviceCount: number
  supportedCount: number
  speakerModeCount: number
  connectedCount: number
  pairedDeviceTotal: number
  outdatedCount: number
  stateBuckets: BluetoothAudioBucket[]
  codecBuckets: BluetoothAudioBucket[]
  generatedAt: string
}

export interface AdminBluetoothAudioClient {
  loadStats(windowDays: number): Promise<AdminBluetoothAudioStats>
}

/// Creates the administrator Bluetooth audio statistics client. Only aggregate
/// counts are fetched; no pairing keys or audio ever leave the platform here.
export function createAdminBluetoothAudioClient(
  httpClient: AxiosInstance = createHttpClient(),
): AdminBluetoothAudioClient {
  return {
    async loadStats(windowDays: number): Promise<AdminBluetoothAudioStats> {
      const response = await httpClient.get('/api/v1/admin/bluetooth-audio/stats', {
        params: { window_days: windowDays },
      })
      return normalizeStats(response.data?.data)
    },
  }
}

function normalizeStats(value: unknown): AdminBluetoothAudioStats {
  const record = recordValue(value)
  return {
    windowDays: numberValue(record.window_days ?? record.windowDays),
    deviceCount: numberValue(record.device_count ?? record.deviceCount),
    supportedCount: numberValue(record.supported_count ?? record.supportedCount),
    speakerModeCount: numberValue(record.speaker_mode_count ?? record.speakerModeCount),
    connectedCount: numberValue(record.connected_count ?? record.connectedCount),
    pairedDeviceTotal: numberValue(record.paired_device_total ?? record.pairedDeviceTotal),
    outdatedCount: numberValue(record.outdated_count ?? record.outdatedCount),
    stateBuckets: arrayValue(record.state_buckets ?? record.stateBuckets).map(normalizeBucket),
    codecBuckets: arrayValue(record.codec_buckets ?? record.codecBuckets).map(normalizeBucket),
    generatedAt: stringValue(record.generated_at ?? record.generatedAt),
  }
}

function normalizeBucket(item: unknown): BluetoothAudioBucket {
  const entry = recordValue(item)
  return {
    label: stringValue(entry.label, 'unknown'),
    count: numberValue(entry.count),
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
