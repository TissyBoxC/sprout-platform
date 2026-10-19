import type { AxiosInstance } from 'axios'

import { readApiNotice, type ApiMutationResult } from './apiNotice.js'

export type AdminFamilyMemberRole = 'owner' | 'guardian' | 'viewer'

export type AdminFamilyInvitationRole = 'guardian' | 'viewer'

export type AdminFamilyInvitationStatus = 'pending' | 'accepted' | 'revoked'

/// One family membership as the administrator console may display it. The
/// administrator endpoint exposes only the safe account summary: display name,
/// account role, and account status.
export interface AdminFamilyMember {
  id: string
  familyId: string
  userId: string
  role: AdminFamilyMemberRole
  displayName: string
  accountRole: string
  accountStatus: string
  createdAt: string
  updatedAt: string
}

/// One invitation addressed to a parent email. Accepted invitations remain
/// visible as history; the management actions are available only while
/// pending.
export interface AdminFamilyInvitation {
  id: string
  familyId: string
  email: string
  role: AdminFamilyInvitationRole
  status: AdminFamilyInvitationStatus
  invitedByUserId: string
  createdAt: string
  expiresAt: string
  acceptedAt: string
}

export interface AdminFamilyMembershipSnapshot {
  members: AdminFamilyMember[]
  invitations: AdminFamilyInvitation[]
  membershipRevision: number
}

export interface AdminFamilyMembershipClient {
  load(parentAccountId: string): Promise<AdminFamilyMembershipSnapshot>
  changeMemberRole(
    parentAccountId: string,
    memberUserId: string,
    role: AdminFamilyMemberRole,
    expectedRevision: number,
  ): Promise<ApiMutationResult<AdminFamilyMember | null>>
  removeMember(
    parentAccountId: string,
    memberUserId: string,
    expectedRevision: number,
  ): Promise<ApiMutationResult<null>>
  createInvitation(
    parentAccountId: string,
    email: string,
    role: AdminFamilyInvitationRole,
    expectedRevision: number,
  ): Promise<ApiMutationResult<AdminFamilyInvitation | null>>
  revokeInvitation(
    parentAccountId: string,
    invitationId: string,
    expectedRevision: number,
  ): Promise<ApiMutationResult<null>>
}

export const ADMIN_FAMILY_MEMBER_ROLE_OPTIONS: readonly {
  value: AdminFamilyMemberRole
  label: string
}[] = [
  { value: 'owner', label: '所有者' },
  { value: 'guardian', label: '守护者' },
  { value: 'viewer', label: '查看者' },
]

export const ADMIN_FAMILY_INVITATION_ROLE_OPTIONS: readonly {
  value: AdminFamilyInvitationRole
  label: string
}[] = [
  { value: 'guardian', label: '守护者' },
  { value: 'viewer', label: '查看者' },
]

/// Creates the administrator-only family membership client. The parent id is
/// always explicit so an operator never falls back to their own signed-in
/// family scope.
export function createAdminFamilyMembershipClient(
  httpClient: AxiosInstance,
): AdminFamilyMembershipClient {
  return {
    async load(parentAccountId: string): Promise<AdminFamilyMembershipSnapshot> {
      const familyPath = familyBasePath(parentAccountId)
      const [membersResponse, invitationsResponse] = await Promise.all([
        httpClient.get(`${familyPath}/members`),
        httpClient.get(`${familyPath}/invitations`),
      ])
      const membersPayload = recordValue(membersResponse.data?.data)
      const invitationsPayload = recordValue(invitationsResponse.data?.data)
      const membershipRevision = sharedMembershipRevision(membersPayload, invitationsPayload)
      return {
        members: arrayValue(membersPayload.members).flatMap((value) => {
          const member = toMember(flattenMemberRecord(value))
          return member === null ? [] : [member]
        }),
        invitations: arrayValue(invitationsPayload.invitations).flatMap((value) => {
          const invitation = toInvitation(value)
          return invitation === null ? [] : [invitation]
        }),
        membershipRevision,
      }
    },

    async changeMemberRole(
      parentAccountId: string,
      memberUserId: string,
      role: AdminFamilyMemberRole,
      expectedRevision: number,
    ): Promise<ApiMutationResult<AdminFamilyMember | null>> {
      const response = await httpClient.put(
        `${familyBasePath(parentAccountId)}/members/${encodeURIComponent(memberUserId)}`,
        {
          role,
        },
        {
          headers: { 'If-Match': revisionHeader(expectedRevision) },
        },
      )
      return {
        value: toMember(recordValue(response.data?.data).member),
        notice: readApiNotice(response.data),
      }
    },

    async removeMember(
      parentAccountId: string,
      memberUserId: string,
      expectedRevision: number,
    ): Promise<ApiMutationResult<null>> {
      const response = await httpClient.delete(
        `${familyBasePath(parentAccountId)}/members/${encodeURIComponent(memberUserId)}`,
        {
          headers: { 'If-Match': revisionHeader(expectedRevision) },
        },
      )
      return {
        value: null,
        notice: readApiNotice(response.data),
      }
    },

    async createInvitation(
      parentAccountId: string,
      email: string,
      role: AdminFamilyInvitationRole,
      expectedRevision: number,
    ): Promise<ApiMutationResult<AdminFamilyInvitation | null>> {
      const response = await httpClient.post(
        `${familyBasePath(parentAccountId)}/invitations`,
        {
          email: email.trim(),
          role,
        },
        {
          headers: { 'If-Match': revisionHeader(expectedRevision) },
        },
      )
      return {
        value: toInvitation(recordValue(response.data?.data).invitation),
        notice: readApiNotice(response.data),
      }
    },

    async revokeInvitation(
      parentAccountId: string,
      invitationId: string,
      expectedRevision: number,
    ): Promise<ApiMutationResult<null>> {
      const response = await httpClient.delete(
        `${familyBasePath(parentAccountId)}/invitations/${encodeURIComponent(invitationId)}`,
        {
          headers: { 'If-Match': revisionHeader(expectedRevision) },
        },
      )
      return {
        value: null,
        notice: readApiNotice(response.data),
      }
    },
  }
}

/// Converts the administrator member response. Any contact field a future
/// response might accidentally include is deliberately ignored.
export function toMember(value: unknown): AdminFamilyMember | null {
  const record = recordValue(value)
  const id = stringValue(record.id).trim()
  const familyId = stringValue(record.family_id ?? record.familyId).trim()
  const userId = stringValue(record.user_id ?? record.userId).trim()
  const role = memberRole(record.role)
  if (!id || !familyId || !userId || role === null) {
    return null
  }
  return {
    id,
    familyId,
    userId,
    role,
    displayName: stringValue(
      record.display_name ??
        record.displayName ??
        record.parent_display_name ??
        record.guardian_family_name,
    ).trim(),
    accountRole: stringValue(record.account_role ?? record.accountRole, 'parent').trim(),
    accountStatus: stringValue(record.account_status ?? record.accountStatus, 'active').trim(),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    updatedAt: stringValue(record.updated_at ?? record.updatedAt),
  }
}

/// Some administrator membership payloads embed the safe account projection
/// rather than flattening it. Merge both shapes before validation without
/// carrying any unapproved contact fields into the member model.
export function flattenMemberRecord(value: unknown): unknown {
  const record = recordValue(value)
  const account = recordValue(record.account ?? record.parent_account ?? record.user)
  if (Object.keys(account).length === 0) {
    return value
  }
  const safeAccount: Record<string, unknown> = {
    ...(typeof account.display_name === 'string' ? { display_name: account.display_name } : {}),
    ...(typeof account.account_role === 'string'
      ? { account_role: account.account_role }
      : typeof account.role === 'string'
        ? { account_role: account.role }
        : {}),
    ...(typeof account.account_status === 'string'
      ? { account_status: account.account_status }
      : typeof account.status === 'string'
        ? { account_status: account.status }
        : {}),
  }
  const membership = { ...record }
  delete membership.account
  delete membership.parent_account
  delete membership.user
  return {
    ...safeAccount,
    ...membership,
  }
}

/// Converts an invitation without guessing a status. Unknown states are
/// ignored so the operator never sees an action that the server cannot accept.
export function toInvitation(value: unknown): AdminFamilyInvitation | null {
  const record = recordValue(value)
  const id = stringValue(record.id).trim()
  const familyId = stringValue(record.family_id ?? record.familyId).trim()
  const email = stringValue(record.email).trim()
  const role = invitationRole(record.role)
  const status = invitationStatus(record.status)
  if (!id || !familyId || !email || role === null || status === null) {
    return null
  }
  return {
    id,
    familyId,
    email,
    role,
    status,
    invitedByUserId: stringValue(record.invited_by_user_id ?? record.invitedByUserId).trim(),
    createdAt: stringValue(record.created_at ?? record.createdAt),
    expiresAt: stringValue(record.expires_at ?? record.expiresAt),
    acceptedAt: stringValue(record.accepted_at ?? record.acceptedAt),
  }
}

/// Stable conflict detection for membership races. Only 409 responses are
/// conflict states because those are the responses that require a reload.
export function isAdminFamilyMembershipConflict(error: unknown): boolean {
  if (!isRecord(error)) {
    return false
  }
  const response = recordValue(error.response)
  const envelope = recordValue(response.data)
  const errorEnvelope = recordValue(envelope.error)
  const code = stringValue(errorEnvelope.code)
  return (
    Number(response.status) === 409 &&
    (code === 'membership_revision_conflict' ||
      code === 'member_exists' ||
      code === 'last_owner' ||
      code === 'invitation_state')
  )
}

export function sharedMembershipRevision(
  membersPayload: Record<string, unknown>,
  invitationsPayload: Record<string, unknown>,
): number {
  const membersRevision = positiveRevisionValue(membersPayload.membership_revision)
  const invitationsRevision = positiveRevisionValue(invitationsPayload.membership_revision)
  if (
    membersRevision === null ||
    invitationsRevision === null ||
    membersRevision !== invitationsRevision
  ) {
    throw new Error('family membership responses returned inconsistent revisions')
  }
  return membersRevision
}

function positiveRevisionValue(value: unknown): number | null {
  const revision = Number(value)
  return Number.isSafeInteger(revision) && revision >= 1 ? revision : null
}

function revisionHeader(revision: number): string {
  if (!Number.isSafeInteger(revision) || revision < 1) {
    throw new Error('family membership revision is invalid')
  }
  return String(revision)
}

function familyBasePath(parentAccountId: string): string {
  return `/api/v1/admin/families/${encodeURIComponent(parentAccountId.trim())}`
}

function memberRole(value: unknown): AdminFamilyMemberRole | null {
  return value === 'owner' || value === 'guardian' || value === 'viewer' ? value : null
}

function invitationRole(value: unknown): AdminFamilyInvitationRole | null {
  return value === 'guardian' || value === 'viewer' ? value : null
}

function invitationStatus(value: unknown): AdminFamilyInvitationStatus | null {
  return value === 'pending' || value === 'accepted' || value === 'revoked' ? value : null
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function recordValue(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : {}
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' && value.length > 0 ? value : fallback
}
