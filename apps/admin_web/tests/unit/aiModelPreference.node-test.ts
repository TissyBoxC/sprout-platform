import assert from 'node:assert/strict'
import test from 'node:test'

import { preferredDefaultModel } from '../../src/features/system/domain/aiModelPreference.js'

test('prefers the gateway recommendation over a lower raw latency', () => {
  const model = preferredDefaultModel([
    { id: 'raw-fastest', latencyMs: 20 },
    { id: 'gateway-recommended', latencyMs: 180, isRecommended: true },
  ])

  assert.equal(model?.id, 'gateway-recommended')
})

test('falls back to the lowest measured latency when no recommendation exists', () => {
  const model = preferredDefaultModel([
    { id: 'unmeasured', latencyMs: null },
    { id: 'slow', latencyMs: 500 },
    { id: 'fast', latencyMs: 80 },
  ])

  assert.equal(model?.id, 'fast')
})

test('does not invent a latency when every model is unmeasured', () => {
  const model = preferredDefaultModel([
    { id: 'one', latencyMs: null },
    { id: 'two', latencyMs: null },
  ])

  assert.equal(model?.id, 'one')
})
