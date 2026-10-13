import type { AxiosInstance, AxiosRequestConfig } from 'axios'

import type { AuthSessionStore } from './authSession'

const sessionEstablishmentPaths = [
  '/api/v1/admin/auth/login',
  '/api/v1/admin/auth/mfa',
  '/api/v1/auth/login',
  '/api/v1/auth/register',
  '/api/v1/auth/refresh',
]

/// Refreshes an access token from the current refresh token.
export type RefreshSession = (refreshToken: string) => Promise<{
  accessToken: string
  refreshToken: string
}>

/// Serializes concurrent refreshes so one expiring session refreshes once.
export function createRefreshCoordinator(
  sessionStore: AuthSessionStore,
  refreshSession: RefreshSession,
) {
  let pendingRefresh: Promise<string> | null = null

  return async function refreshAccessToken(): Promise<string> {
    if (pendingRefresh !== null) {
      return pendingRefresh
    }

    const refreshToken = sessionStore.readRefreshToken()
    if (refreshToken === null) {
      sessionStore.clearSession()
      throw new Error('refresh token is unavailable')
    }

    pendingRefresh = refreshSession(refreshToken)
      .then((session) => {
        sessionStore.saveSession(session.accessToken, session.refreshToken)
        return session.accessToken
      })
      .catch((error: unknown) => {
        if (isDefinitiveRefreshFailure(error)) {
          sessionStore.clearSession()
        }
        throw error
      })
      .finally(() => {
        pendingRefresh = null
      })

    return pendingRefresh
  }
}

/// Adds bearer authentication and one retry after a successful refresh.
export function installAuthInterceptors(
  httpClient: AxiosInstance,
  sessionStore: AuthSessionStore,
  refreshSession: RefreshSession,
): void {
  const refreshAccessToken = createRefreshCoordinator(sessionStore, refreshSession)

  httpClient.interceptors.request.use((request) => {
    const accessToken = sessionStore.readAccessToken()
    if (accessToken !== null) {
      request.headers.set('Authorization', `Bearer ${accessToken}`)
    }
    return request
  })

  httpClient.interceptors.response.use(
    (response) => response,
    async (error: unknown) => {
      if (
        !isUnauthorizedResponse(error) ||
        isRetriedRequest(error.config) ||
        isSessionEstablishmentRequest(error.config) ||
        sessionStore.readRefreshToken() === null
      ) {
        return Promise.reject(error)
      }

      try {
        const accessToken = await refreshAccessToken()
        const retryConfig = error.config as RetriedRequestConfig
        retryConfig.isRetried = true
        retryConfig.headers = {
          ...retryConfig.headers,
          Authorization: `Bearer ${accessToken}`,
        }
        return await httpClient.request(retryConfig)
      } catch (refreshError: unknown) {
        return Promise.reject(refreshError)
      }
    },
  )
}

interface RetriedRequestConfig extends AxiosRequestConfig {
  isRetried?: boolean
}

interface UnauthorizedResponse {
  config?: RetriedRequestConfig
  response?: {
    status?: number
  }
}

function isUnauthorizedResponse(error: unknown): error is UnauthorizedResponse {
  return (
    typeof error === 'object' &&
    error !== null &&
    'response' in error &&
    (error as UnauthorizedResponse).response?.status === 401
  )
}

/// A refresh failure is definitive only when the server rejected the refresh
/// token. Network outages and 5xx responses leave the stored token intact so
/// the user can retry after the service recovers.
function isDefinitiveRefreshFailure(error: unknown): boolean {
  if (
    typeof error !== 'object' ||
    error === null ||
    !('response' in error)
  ) {
    return false
  }
  const response = (error as UnauthorizedResponse).response
  return response?.status === 401 || response?.status === 403
}

function isRetriedRequest(config: AxiosRequestConfig | undefined): boolean {
  return (config as RetriedRequestConfig | undefined)?.isRetried === true
}

function isSessionEstablishmentRequest(
  config: AxiosRequestConfig | undefined,
): boolean {
  const requestUrl = config?.url
  if (typeof requestUrl !== 'string') {
    return false
  }
  return sessionEstablishmentPaths.some((path) => requestUrl.includes(path))
}
