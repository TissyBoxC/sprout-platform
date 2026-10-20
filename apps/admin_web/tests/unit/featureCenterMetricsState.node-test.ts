import assert from 'node:assert/strict'
import test from 'node:test'

import { normalizeMetricsState } from '../../src/features/feature_center/domain/metricsState.js'

test('uses the explicit unavailable metrics state from the service', () => {
  assert.equal(normalizeMetricsState('unavailable', true), 'unavailable')
})

test('keeps an available state even when every metric value is zero', () => {
  assert.equal(normalizeMetricsState('available', true), 'available')
})

test('falls back to response shape for an older backend', () => {
  assert.equal(normalizeMetricsState(undefined, true), 'available')
  assert.equal(normalizeMetricsState(undefined, false), 'not_configured')
  assert.equal(normalizeMetricsState('unexpected', true), 'available')
})
