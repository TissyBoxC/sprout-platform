import assert from 'node:assert/strict'
import test from 'node:test'

import { canManageContent } from '../../src/features/content/domain/contentPermission.js'

test('allows content mutations only with the review capability', () => {
  assert.equal(canManageContent(true), true)
  assert.equal(canManageContent(false), false)
})
