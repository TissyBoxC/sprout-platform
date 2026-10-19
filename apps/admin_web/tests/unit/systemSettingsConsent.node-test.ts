import assert from 'node:assert/strict'
import test from 'node:test'

import {
  DEFAULT_GUARDIAN_CONSENT_VERSION,
  readGuardianConsentVersion,
  writeGuardianConsentVersion,
} from '../../src/features/system/domain/guardianConsentVersion.js'
import {
  MINOR_MODE_ENFORCED_LABEL,
  assertMinorModeNotDisabled,
} from '../../src/features/system/domain/minorModeInvariant.js'

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

test('treats minor mode as a read-only backend invariant', () => {
  assert.equal(assertMinorModeNotDisabled({ minor_mode_default: true }), true)
  assert.equal(assertMinorModeNotDisabled({}), true)
  assert.throws(
    () => assertMinorModeNotDisabled({ minor_mode_default: false }),
    /minor mode invariant is disabled/,
  )
  assert.equal(MINOR_MODE_ENFORCED_LABEL, '固定开启')
})
