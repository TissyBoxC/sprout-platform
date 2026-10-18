import { createPinia, type Pinia, type StoreGeneric } from 'pinia'

type PiniaWithStoreInstances = Pinia & {
  _s: Map<string, StoreGeneric>
}

/// Creates the global Pinia instance for the admin application.
export function createAdminStore() {
  return createPinia()
}

/// Disposes all non-auth stores after a session ends.
///
/// Pinia does not provide a public API for enumerating every instantiated
/// setup store. The application acquires the registry once here, behind a
/// narrow adapter, instead of coupling feature code to it.
export function disposeAdminFeatureStores(
  pinia: Pinia,
  authStoreId: string,
): void {
  const storeInstances = (pinia as PiniaWithStoreInstances)._s
  for (const [storeId, store] of [...storeInstances.entries()]) {
    if (storeId === authStoreId) {
      continue
    }
    try {
      store.$dispose()
    } finally {
      storeInstances.delete(storeId)
    }
  }
}
