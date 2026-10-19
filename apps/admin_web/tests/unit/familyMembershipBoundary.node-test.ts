import assert from 'node:assert/strict'
import test from 'node:test'

import {
  adminCanProxyFamilyWrites,
  familyRoleBoundaries,
} from '../../src/features/family/domain/familyMembershipBoundary.js'

test('describes the complete family role boundary', () => {
  assert.deepEqual(
    familyRoleBoundaries.map((boundary) => boundary.role),
    ['owner', 'guardian', 'viewer'],
  )
  assert.ok(
    familyRoleBoundaries.every(
      (boundary) =>
        boundary.label.length > 0 &&
        boundary.availability.length > 0 &&
        boundary.description.length > 0,
    ),
  )
})

test('does not proxy family writes through the signed-in administrator family', () => {
  assert.equal(adminCanProxyFamilyWrites, false)
})
