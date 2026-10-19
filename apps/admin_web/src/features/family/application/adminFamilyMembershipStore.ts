import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import {
  ADMIN_FAMILY_INVITATION_ROLE_OPTIONS,
  ADMIN_FAMILY_MEMBER_ROLE_OPTIONS,
  isAdminFamilyMembershipConflict,
  type AdminFamilyInvitation,
  type AdminFamilyInvitationRole,
  type AdminFamilyMember,
  type AdminFamilyMemberRole,
  type AdminFamilyMembershipClient,
} from '@/api/adminFamilyMembership'
import { mapApiError, type ApiError } from '@/api/apiError'
import { apiNoticeWarning } from '@/api/apiNotice'
import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'

export interface AdminFamilyMembershipMutationResult {
  succeeded: boolean
  conflicted: boolean
  error: ApiError | null
  warning: string
}

/// Owns one administrator-visible family's membership screen. Every mutation
/// is followed by an authoritative reload so role, removal, and invitation
/// state cannot drift after a partial response.
export const useAdminFamilyMembershipStore = defineStore('admin-family-membership', () => {
  const authStore = useAuthStore()
  // The page installs the authenticated transport before the first read.
  let client: AdminFamilyMembershipClient | null = null

  const members = ref<AdminFamilyMember[]>([])
  const invitations = ref<AdminFamilyInvitation[]>([])
  const membershipRevision = ref(0)
  const activeParentAccountId = ref('')
  const isLoading = ref(false)
  const isSubmitting = ref(false)
  const error = ref<ApiError | null>(null)
  const conflictMessage = ref('')
  const lastMessage = ref('')
  const lastWarning = ref('')
  let requestGeneration = 0

  const canManage = computed(() => authStore.can(ADMIN_PERMISSIONS.familyManage))
  const canRead = computed(() => authStore.can(ADMIN_PERMISSIONS.familyRead))
  const isReadOnly = computed(() => canRead.value && !canManage.value)
  const pendingInvitations = computed(() =>
    invitations.value.filter((invitation) => invitation.status === 'pending'),
  )
  const memberRoleOptions = ADMIN_FAMILY_MEMBER_ROLE_OPTIONS
  const invitationRoleOptions = ADMIN_FAMILY_INVITATION_ROLE_OPTIONS

  async function load(parentAccountId: string): Promise<void> {
    const normalizedParentId = parentAccountId.trim()
    if (!normalizedParentId || !canRead.value || client === null) {
      clear()
      return
    }
    const generation = ++requestGeneration
    activeParentAccountId.value = normalizedParentId
    isLoading.value = true
    error.value = null
    conflictMessage.value = ''
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const snapshot = await client.load(normalizedParentId)
      if (generation !== requestGeneration) {
        return
      }
      members.value = snapshot.members
      invitations.value = snapshot.invitations
      membershipRevision.value = snapshot.membershipRevision
    } catch (caught: unknown) {
      if (generation === requestGeneration) {
        members.value = []
        invitations.value = []
        membershipRevision.value = 0
        error.value = mapApiError(caught)
      }
    } finally {
      if (generation === requestGeneration) {
        isLoading.value = false
      }
    }
  }

  async function changeMemberRole(
    memberUserId: string,
    role: AdminFamilyMemberRole,
  ): Promise<AdminFamilyMembershipMutationResult> {
    return runMutation('成员角色已更新。', () =>
      requireClient().changeMemberRole(
        activeParentAccountId.value,
        memberUserId,
        role,
        membershipRevision.value,
      ),
    )
  }

  async function removeMember(memberUserId: string): Promise<AdminFamilyMembershipMutationResult> {
    return runMutation('家庭成员已移除。', () =>
      requireClient().removeMember(
        activeParentAccountId.value,
        memberUserId,
        membershipRevision.value,
      ),
    )
  }

  async function createInvitation(
    email: string,
    role: AdminFamilyInvitationRole,
  ): Promise<AdminFamilyMembershipMutationResult> {
    return runMutation('邀请已发出。', () =>
      requireClient().createInvitation(
        activeParentAccountId.value,
        email,
        role,
        membershipRevision.value,
      ),
    )
  }

  async function revokeInvitation(
    invitationId: string,
  ): Promise<AdminFamilyMembershipMutationResult> {
    return runMutation('邀请已撤回。', () =>
      requireClient().revokeInvitation(
        activeParentAccountId.value,
        invitationId,
        membershipRevision.value,
      ),
    )
  }

  async function reloadAfterConflict(): Promise<void> {
    const parentAccountId = activeParentAccountId.value
    if (!parentAccountId) {
      return
    }
    await load(parentAccountId)
    if (error.value === null) {
      conflictMessage.value = ''
      lastMessage.value = '已读取最新成员和邀请，请重新操作。'
    }
  }

  function setClient(nextClient: AdminFamilyMembershipClient): void {
    client = nextClient
  }

  function requireClient(): AdminFamilyMembershipClient {
    if (client === null) {
      throw new Error('family membership client is not configured')
    }
    return client
  }

  async function runMutation(
    successMessage: string,
    mutation: () => Promise<{
      notice: {
        code: string
        message: string
      } | null
    }>,
  ): Promise<AdminFamilyMembershipMutationResult> {
    if (!canManage.value || !activeParentAccountId.value) {
      return {
        succeeded: false,
        conflicted: false,
        error: {
          kind: 'insufficient_permission',
          message: '你只能查看家庭成员，不能修改。',
          retryable: false,
        },
        warning: '',
      }
    }
    if (isSubmitting.value) {
      return {
        succeeded: false,
        conflicted: false,
        error: null,
        warning: '',
      }
    }
    isSubmitting.value = true
    error.value = null
    conflictMessage.value = ''
    lastMessage.value = ''
    lastWarning.value = ''
    try {
      const result = await mutation()
      const warning = apiNoticeWarning(result.notice)
      await load(activeParentAccountId.value)
      if (error.value !== null) {
        const refreshError = error.value
        lastMessage.value = '操作已保存，但成员列表暂时没有更新。请重新加载后再继续操作。'
        return {
          succeeded: true,
          conflicted: false,
          error: refreshError,
          warning,
        }
      }
      lastMessage.value = successMessage
      lastWarning.value = warning
      return {
        succeeded: true,
        conflicted: false,
        error: null,
        warning,
      }
    } catch (caught: unknown) {
      if (isAdminFamilyMembershipConflict(caught)) {
        conflictMessage.value =
          '家庭成员或邀请刚刚发生变化。请重新加载后再操作，当前页面不会覆盖他人的修改。'
        return {
          succeeded: false,
          conflicted: true,
          error: null,
          warning: '',
        }
      }
      error.value = mapApiError(caught)
      return {
        succeeded: false,
        conflicted: false,
        error: error.value,
        warning: '',
      }
    } finally {
      isSubmitting.value = false
    }
  }

  function clear(): void {
    requestGeneration += 1
    activeParentAccountId.value = ''
    members.value = []
    invitations.value = []
    membershipRevision.value = 0
    isLoading.value = false
    isSubmitting.value = false
    error.value = null
    conflictMessage.value = ''
    lastMessage.value = ''
    lastWarning.value = ''
  }

  return {
    activeParentAccountId,
    canManage,
    canRead,
    changeMemberRole,
    clear,
    conflictMessage,
    createInvitation,
    error,
    invitations,
    invitationRoleOptions,
    isReadOnly,
    isLoading,
    isSubmitting,
    lastMessage,
    lastWarning,
    load,
    memberRoleOptions,
    members,
    membershipRevision,
    pendingInvitations,
    reloadAfterConflict,
    removeMember,
    revokeInvitation,
    setClient,
  }
})
