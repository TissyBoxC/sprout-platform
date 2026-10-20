import assert from 'node:assert/strict'
import test from 'node:test'

import { canApplyMutationResult } from '../../src/features/feature_center/domain/mutationGuard.js'

test('accepts a response only for the current feature selection', () => {
  assert.equal(canApplyMutationResult(4, 4, 'voice_gateway', 'voice_gateway'), true)
})

test('rejects a response after the operator switches features', () => {
  assert.equal(canApplyMutationResult(4, 5, 'voice_gateway', 'vision'), false)
  assert.equal(canApplyMutationResult(4, 4, 'voice_gateway', 'vision'), false)
})

test('rejects an empty or stale generation result', () => {
  assert.equal(canApplyMutationResult(4, 5, 'voice_gateway', 'voice_gateway'), false)
  assert.equal(canApplyMutationResult(4, 4, '', ''), false)
})
