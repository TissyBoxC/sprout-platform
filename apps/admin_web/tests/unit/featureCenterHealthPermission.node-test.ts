import assert from 'node:assert/strict'
import test from 'node:test'

import { canRunFeatureHealthCheck } from '../../src/features/feature_center/domain/healthPermission.js'

test('allows a read-only administrator to probe the selected feature', () => {
  assert.equal(canRunFeatureHealthCheck(true, true, true), true)
})

test('requires read permission, loaded detail, and a selected feature', () => {
  assert.equal(canRunFeatureHealthCheck(false, true, true), false)
  assert.equal(canRunFeatureHealthCheck(true, false, true), false)
  assert.equal(canRunFeatureHealthCheck(true, true, false), false)
})
