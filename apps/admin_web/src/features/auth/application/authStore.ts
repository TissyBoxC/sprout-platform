import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { resetAdminSessionStores } from '@/app/sessionBoundary'
import { createHttpClient } from '@/api/httpClient'
import { SessionStorageAuthSessionStore } from '@/api/authSession'
import { mapApiError, type ApiError } from '@/api/apiError'
import {
  ADMIN_PERMISSIONS,
  hasAdminPermission,
  type AdminPermission,
} from '@/features/auth/domain/adminPermission'

/// One administrator session returned by the device platform.
export interface AdminAccount {
  id: string
  email: string
  displayName: string
  role: string
  permissions: string[]
}

/// Owns administrator login, session restoration, and logout.
export const useAuthStore = defineStore('admin-auth', () => {
  const sessionStore = new SessionStorageAuthSessionStore()
  const httpClient = createHttpClient({ sessionStore })
  const account = ref<AdminAccount | null>(null)
  const isRestoring = ref(true)
  const error = ref<ApiError | null>(null)
  const mfaChallengeToken = ref<string | null>(null)

  const isSignedIn = computed(() => account.value !== null)
  const isMFARequired = computed(() => mfaChallengeToken.value !== null)
  const canReadFeatureCenter = computed(() =>
    can(ADMIN_PERMISSIONS.featureCenterRead),
  )
  const canManageFeatureCenter = computed(() =>
    can(ADMIN_PERMISSIONS.featureCenterManage),
  )

  function can(permission: AdminPermission): boolean {
    return hasAdminPermission(account.value?.permissions ?? [], permission)
  }

  async function restoreSession(): Promise<void> {
    isRestoring.value = true
    error.value = null
    try {
      if (sessionStore.readRefreshToken() === null) {
        account.value = null
        return
      }
      const response = await httpClient.get('/api/v1/auth/me')
      account.value = toAccount(response.data.data.account)
    } catch (caught: unknown) {
      const mappedError = mapApiError(caught)
      if (mappedError.kind === 'unauthenticated' || mappedError.kind === 'insufficient_permission') {
        sessionStore.clearSession()
        account.value = null
      }
      error.value = mappedError
    } finally {
      isRestoring.value = false
    }
  }

  async function login(email: string, password: string): Promise<boolean> {
    error.value = null
    mfaChallengeToken.value = null
    try {
      const response = await httpClient.post('/api/v1/admin/auth/login', {
        email,
        password,
      })
      const data = response.data.data
      if (data.status === 'mfa_required' && typeof data.challenge_token === 'string') {
        mfaChallengeToken.value = data.challenge_token
        return true
      }
      error.value = {
        kind: 'unexpected',
        message: '登录没有完成，请稍后重试',
        retryable: true,
      }
      return false
    } catch (caught: unknown) {
      const mappedError = mapApiError(caught)
      error.value = mappedError
      if (mappedError.kind === 'unauthenticated') {
        mfaChallengeToken.value = null
      }
      return false
    }
  }

  async function completeMFA(code: string): Promise<boolean> {
    error.value = null
    const challengeToken = mfaChallengeToken.value
    if (challengeToken === null) {
      return false
    }
    try {
      const response = await httpClient.post('/api/v1/admin/auth/mfa', {
        challenge_token: challengeToken,
        code,
      })
      const data = response.data.data
      sessionStore.saveSession(data.access_token, data.refresh_token)
      const signedInAccount = toAccount(data.account)
      if (signedInAccount.role !== 'admin') {
        sessionStore.clearSession()
        account.value = null
        mfaChallengeToken.value = null
        error.value = {
          kind: 'insufficient_permission',
          message: '你没有权限访问此页面',
          retryable: false,
        }
        return false
      }
      account.value = signedInAccount
      mfaChallengeToken.value = null
      return true
    } catch (caught: unknown) {
      error.value = mapApiError(caught)
      return false
    }
  }

  function cancelMFA(): void {
    mfaChallengeToken.value = null
    error.value = null
  }

  async function logout(): Promise<void> {
    const refreshToken = sessionStore.readRefreshToken()
    try {
      if (refreshToken !== null) {
        await httpClient.post('/api/v1/auth/logout', {
          refresh_token: refreshToken,
        })
      }
    } catch {
      // Local sign-out still succeeds when the network is unavailable.
    } finally {
      sessionStore.clearSession()
      resetAdminSessionStores()
      account.value = null
      mfaChallengeToken.value = null
    }
  }

  return {
    account,
    canReadFeatureCenter,
    canManageFeatureCenter,
    can,
    error,
    isRestoring,
    isSignedIn,
    isMFARequired,
    mfaChallengeToken,
    login,
    completeMFA,
    cancelMFA,
    logout,
    restoreSession,
  }
})

function toAccount(value: {
  id: string
  email: string
  display_name: string
  role: string
  admin_permissions?: unknown
}): AdminAccount {
  return {
    id: value.id,
    email: value.email,
    displayName: value.display_name,
    role: value.role,
    permissions: stringList(value.admin_permissions),
  }
}

function stringList(value: unknown): string[] {
  return Array.isArray(value)
    ? value
        .filter((item): item is string => typeof item === 'string')
        .map((item) => item.trim())
        .filter(Boolean)
    : []
}
