export const DEFAULT_GUARDIAN_CONSENT_VERSION = '2026-01'

export function readGuardianConsentVersion(
  account: Record<string, unknown>,
): string {
  return typeof account.guardian_consent_version === 'string'
    ? account.guardian_consent_version
    : DEFAULT_GUARDIAN_CONSENT_VERSION
}

export function writeGuardianConsentVersion(
  settings: { guardianConsentVersion: string },
): string {
  return settings.guardianConsentVersion.trim()
}
