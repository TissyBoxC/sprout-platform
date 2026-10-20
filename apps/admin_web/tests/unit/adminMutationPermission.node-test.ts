import assert from 'node:assert/strict'
import test from 'node:test'

import {
  canCreateFamilyAccount,
  canManageFamilyAIAccount,
  canManageFamilyAccount,
  canManageNotifications,
  canManageOTA,
  canManageStorage,
} from '../../src/features/support/domain/adminMutationPermission.js'

test('keeps read access separate from every management capability', () => {
  assert.equal(canManageFamilyAccount(false), false)
  assert.equal(canManageFamilyAIAccount(false), false)
  assert.equal(canManageStorage(false), false)
  assert.equal(canManageOTA(false), false)
  assert.equal(canManageNotifications(false), false)
})

test('requires both parent and AI management to create a parent account', () => {
  assert.equal(canCreateFamilyAccount(true, false), false)
  assert.equal(canCreateFamilyAccount(false, true), false)
  assert.equal(canCreateFamilyAccount(true, true), true)
})
