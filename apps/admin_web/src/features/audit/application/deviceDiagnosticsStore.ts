import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import {
  createAdminDeviceDiagnosticsClient,
  type AdminDiagnosticDevice,
  type DeviceDiagnosticsSnapshot,
} from '@/api/adminDeviceDiagnostics'
import { mapApiError, type ApiError } from '@/api/apiError'

/// Owns the administrator diagnostic view: device selection, one bounded
/// history snapshot per device, and the loading state used by the page.
export const useDeviceDiagnosticsStore = defineStore('admin-device-diagnostics', () => {
  const client = createAdminDeviceDiagnosticsClient()
  const devices = ref<AdminDiagnosticDevice[]>([])
  const snapshots = ref<Record<string, DeviceDiagnosticsSnapshot>>({})
  const selectedDeviceId = ref('')
  const isLoadingDevices = ref(false)
  const isLoadingDiagnostics = ref(false)
  const error = ref<ApiError | null>(null)
  const diagnosticsError = ref<ApiError | null>(null)
  const lastLoadedAt = ref('')

  const selectedDevice = computed(
    () => devices.value.find((device) => device.deviceId === selectedDeviceId.value) ?? null,
  )
  const snapshot = computed(() => snapshots.value[selectedDeviceId.value] ?? null)
  const onlineDeviceCount = computed(() => devices.value.filter((device) => device.isOnline).length)

  async function loadDevices(): Promise<void> {
    isLoadingDevices.value = true
    error.value = null
    try {
      devices.value = await client.loadDevices()
      if (devices.value.length === 0) {
        selectedDeviceId.value = ''
        return
      }
      if (!devices.value.some((device) => device.deviceId === selectedDeviceId.value)) {
        await selectDevice(devices.value[0]?.deviceId ?? '')
      }
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
    } finally {
      isLoadingDevices.value = false
    }
  }

  async function selectDevice(deviceId: string): Promise<void> {
    selectedDeviceId.value = deviceId
    if (!deviceId) {
      return
    }
    await loadDiagnostics(deviceId)
  }

  async function loadDiagnostics(deviceId = selectedDeviceId.value): Promise<void> {
    const normalizedDeviceId = deviceId.trim()
    if (!normalizedDeviceId) {
      return
    }
    isLoadingDiagnostics.value = true
    diagnosticsError.value = null
    try {
      const loaded = await client.loadDiagnostics(normalizedDeviceId)
      snapshots.value = { ...snapshots.value, [normalizedDeviceId]: loaded }
      lastLoadedAt.value = new Date().toISOString()
    } catch (caught: unknown) {
      diagnosticsError.value = mapApiError(caught)
    } finally {
      isLoadingDiagnostics.value = false
    }
  }

  async function refresh(): Promise<void> {
    await loadDevices()
    if (selectedDeviceId.value) {
      await loadDiagnostics(selectedDeviceId.value)
    }
  }

  return {
    devices,
    diagnosticsError,
    error,
    isLoadingDevices,
    isLoadingDiagnostics,
    lastLoadedAt,
    loadDevices,
    loadDiagnostics,
    onlineDeviceCount,
    refresh,
    selectDevice,
    selectedDevice,
    selectedDeviceId,
    snapshot,
    snapshots,
  }
})
