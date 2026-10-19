import assert from 'node:assert/strict'
import test from 'node:test'

import {
  DEFAULT_GUARDIAN_CONSENT_VERSION,
  readGuardianConsentVersion,
  writeGuardianConsentVersion,
} from '../../src/features/system/domain/guardianConsentVersion.js'

test('falls back to the conservative consent version for legacy settings', () => {
  const version = readGuardianConsentVersion({
    registration_enabled: true,
    phone_verification_required: true,
    email_login_enabled: true,
  })

  assert.equal(version, DEFAULT_GUARDIAN_CONSENT_VERSION)
})

test('preserves the published consent version in the save payload', () => {
  const version = readGuardianConsentVersion({
    guardian_consent_version: '2026-06',
  })

  assert.equal(version, '2026-06')
  assert.equal(writeGuardianConsentVersion({ guardianConsentVersion: ' 2026-06 ' }), '2026-06')
})
