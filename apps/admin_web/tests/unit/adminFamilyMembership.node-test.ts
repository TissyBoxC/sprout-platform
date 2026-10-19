import assert from 'node:assert/strict'
import test from 'node:test'

import {
  flattenMemberRecord,
  isAdminFamilyMembershipConflict,
  sharedMembershipRevision,
  toInvitation,
  toMember,
} from '../../src/api/adminFamilyMembership.js'

import { readApiNotice } from '../../src/api/apiNotice.js'

test('the membership client test bundle resolves the shared notice contract', () => {
  assert.equal(
    readApiNotice({
      notice: {
        code: 'audit_write_failed',
        message: 'ignored',
      },
    })?.code,
    'audit_write_failed',
  )
})

test('normalizes a complete member and invitation snapshot', () => {
  const member = toMember({
    id: 'member-1',
    family_id: 'family-1',
    user_id: 'user-1',
    role: 'guardian',
    display_name: '李家长',
    email: 'guardian@example.com',
    phone: '13800000000',
    account_role: 'parent',
    account_status: 'active',
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-02T10:00:00Z',
  })
  assert.deepEqual(member, {
    id: 'member-1',
    familyId: 'family-1',
    userId: 'user-1',
    role: 'guardian',
    displayName: '李家长',
    accountRole: 'parent',
    accountStatus: 'active',
    createdAt: '2026-10-01T10:00:00Z',
    updatedAt: '2026-10-02T10:00:00Z',
  })

  const invitation = toInvitation({
    id: 'invitation-1',
    family_id: 'family-1',
    email: 'viewer@example.com',
    role: 'viewer',
    status: 'pending',
    invited_by_user_id: 'user-owner',
    created_at: '2026-10-01T10:00:00Z',
    expires_at: '2026-10-08T10:00:00Z',
    accepted_at: null,
  })
  assert.equal(invitation?.role, 'viewer')
  assert.equal(invitation?.status, 'pending')
  assert.equal(invitation?.acceptedAt, '')
})

test('accepts only one shared positive membership revision', () => {
  assert.equal(sharedMembershipRevision({ membership_revision: 7 }, { membership_revision: 7 }), 7)
  assert.throws(() =>
    sharedMembershipRevision({ membership_revision: 7 }, { membership_revision: 6 }),
  )
  assert.throws(() =>
    sharedMembershipRevision({ membership_revision: 0 }, { membership_revision: 0 }),
  )
})

test('rejects records that lack stable identity or known role state', () => {
  assert.equal(
    toMember({
      id: 'member-1',
      family_id: 'family-1',
      user_id: 'user-1',
      role: 'admin',
    }),
    null,
  )
  assert.equal(
    toInvitation({
      id: 'invitation-1',
      family_id: 'family-1',
      email: 'guardian@example.com',
      role: 'guardian',
      status: 'expired',
    }),
    null,
  )
})

test('keeps a nested safe account projection while preserving membership identity', () => {
  assert.deepEqual(
    flattenMemberRecord({
      id: 'member-1',
      user_id: 'user-1',
      role: 'guardian',
      account: {
        display_name: '李家长',
        role: 'parent',
        status: 'active',
      },
    }),
    {
      display_name: '李家长',
      account_role: 'parent',
      account_status: 'active',
      id: 'member-1',
      user_id: 'user-1',
      role: 'guardian',
    },
  )
})

test('ignores contact fields that the safe administrator projection does not expose', () => {
  const member = toMember({
    id: 'member-1',
    family_id: 'family-1',
    user_id: 'user-1',
    role: 'guardian',
    display_name: '李家长',
    email: 'must-not-render@example.com',
    phone: '13800000000',
    account_role: 'parent',
    account_status: 'active',
  })
  assert.ok(member)
  assert.equal('email' in member, false)
  assert.equal('phone' in member, false)
})

test('detects membership conflicts without treating other errors as conflicts', () => {
  assert.equal(
    isAdminFamilyMembershipConflict({
      response: {
        status: 409,
        data: { error: { code: 'membership_revision_conflict' } },
      },
    }),
    true,
  )
  assert.equal(
    isAdminFamilyMembershipConflict({
      response: { status: 403, data: { error: { code: 'forbidden' } } },
    }),
    false,
  )
  assert.equal(
    isAdminFamilyMembershipConflict({
      response: { status: 500, data: { error: { code: 'invitation_state' } } },
    }),
    false,
  )
})
