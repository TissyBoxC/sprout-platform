const adminAuthStoreId = 'admin-auth'

import type { Pinia } from 'pinia'

import { disposeAdminFeatureStores } from '@/app/store'

type SessionCleanup = () => void

const cleanupHandlers = new Set<SessionCleanup>()
let featureStoreBoundary: (() => void) | null = null

/// Binds the current administrator session to the Pinia instance that owns
/// its feature state. The adapter keeps registry access out of feature code.
export function bindAdminSessionStore(pinia: Pinia): void {
  featureStoreBoundary = () => disposeAdminFeatureStores(pinia, 'admin-auth')
}

/// Registers an owner-supplied cleanup action for an administrator session.
///
/// Stores never rely on Pinia's private instance registry. Each feature owns
/// its reset logic and the registry only coordinates lifecycle ordering.
export function registerAdminSessionCleanup(
  cleanup: SessionCleanup,
): () => void {
  cleanupHandlers.add(cleanup)
  return () => cleanupHandlers.delete(cleanup)
}

/// Invalidates every registered feature state and in-flight request guard.
export function resetAdminSessionStores(): void {
  const failures: unknown[] = []
  try {
    featureStoreBoundary?.()
  } catch (error: unknown) {
    failures.push(error)
  }
  for (const cleanup of cleanupHandlers) {
    try {
      cleanup()
    } catch (error: unknown) {
      failures.push(error)
    }
  }
  if (failures.length > 0) {
    throw new AggregateError(failures, 'administrator session cleanup failed')
  }
}
