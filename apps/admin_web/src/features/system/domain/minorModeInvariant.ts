import type { AxiosInstance } from 'axios'

/**
 * Minor mode is enforced by the platform, not stored as an admin preference.
 * The client verifies the backend projection and rejects a stale service that
 * still advertises the protection as disabled.
 */
export const MINOR_MODE_ENFORCED_LABEL = '固定开启'

export function assertMinorModeNotDisabled(safety: Record<string, unknown>): true {
  if (safety.minor_mode_default === false) {
    throw new Error('minor mode invariant is disabled by the settings response')
  }
  return true
}

export async function verifyMinorModeEnforced(
  httpClient: AxiosInstance,
): Promise<void> {
  const response = await httpClient.get('/api/v1/admin/settings')
  const data =
    typeof response.data?.data === 'object' && response.data.data !== null
      ? (response.data.data as Record<string, unknown>)
      : {}
  const settings =
    typeof data.settings === 'object' && data.settings !== null
      ? (data.settings as Record<string, unknown>)
      : {}
  const safety =
    typeof settings.safety === 'object' && settings.safety !== null
      ? (settings.safety as Record<string, unknown>)
      : {}
  assertMinorModeNotDisabled(safety)
}
