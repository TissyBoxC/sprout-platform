import { defineStore } from 'pinia'
import { ref } from 'vue'

import { mapApiError, type ApiError } from '@/api/apiError'
import { createHttpClient } from '@/api/httpClient'
import {
  parseChildProfiles,
  parseParentAccountOptions,
  parentAccountPageHasMore,
  type ChildProfile,
  type ParentAccountOption,
} from '@/features/child/domain/childProfile'

// This is a read-only support view, so the store intentionally keeps the
// administrator from editing or deleting a family's child data. Each child
// carries an explicit policy state so a read failure is never rendered as an
// empty policy.
export const useChildProfileStore = defineStore('admin-child-profiles', () => {
  const httpClient = createHttpClient()
  const families = ref<ParentAccountOption[]>([])
  const childrenByFamily = ref<Record<string, ChildProfile[]>>({})
  const childrenErrorsByFamily = ref<Record<string, ApiError | null>>({})
  const isLoadingFamilies = ref(false)
  const childrenLoadingCounts = ref<Record<string, number>>({})
  const error = ref<ApiError | null>(null)
  const childrenRequestSequences = new Map<string, number>()
  const childrenLoadingSequences = new Map<string, number>()
  let familiesRequestSequence = 0

  async function loadFamilies(): Promise<ApiError | null> {
    const requestSequence = ++familiesRequestSequence
    isLoadingFamilies.value = true
    error.value = null
    try {
      const options: ParentAccountOption[] = []
      for (let page = 1; ; page += 1) {
        const response = await httpClient.get('/api/v1/admin/families', {
          params: { page, page_size: 100 },
        })
        const payload = response.data?.data
        options.push(...parseParentAccountOptions(payload))
        if (!parentAccountPageHasMore(payload)) {
          break
        }
      }
      if (requestSequence !== familiesRequestSequence) {
        return null
      }
      families.value = options
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (requestSequence === familiesRequestSequence) {
        error.value = mapped
        families.value = []
      }
      return requestSequence === familiesRequestSequence ? mapped : null
    } finally {
      if (requestSequence === familiesRequestSequence) {
        isLoadingFamilies.value = false
      }
    }
  }

  async function loadChildren(parentAccountId: string): Promise<ApiError | null> {
    const requestSequence =
      (childrenRequestSequences.get(parentAccountId) ?? 0) + 1
    childrenRequestSequences.set(parentAccountId, requestSequence)
    const loadingSequence =
      (childrenLoadingSequences.get(parentAccountId) ?? 0) + 1
    childrenLoadingSequences.set(parentAccountId, loadingSequence)
    setChildrenLoading(parentAccountId, true)
    childrenErrorsByFamily.value = {
      ...childrenErrorsByFamily.value,
      [parentAccountId]: null,
    }
    try {
      const response = await httpClient.get(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/children`,
      )
      if (childrenRequestSequences.get(parentAccountId) !== requestSequence) {
        return null
      }
      childrenByFamily.value = {
        ...childrenByFamily.value,
        [parentAccountId]: parseChildProfiles(response.data?.data),
      }
      childrenErrorsByFamily.value = {
        ...childrenErrorsByFamily.value,
        [parentAccountId]: null,
      }
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (childrenRequestSequences.get(parentAccountId) === requestSequence) {
        childrenErrorsByFamily.value = {
          ...childrenErrorsByFamily.value,
          [parentAccountId]: mapped,
        }
      }
      return childrenRequestSequences.get(parentAccountId) === requestSequence
        ? mapped
        : null
    } finally {
      if (childrenLoadingSequences.get(parentAccountId) === loadingSequence) {
        setChildrenLoading(parentAccountId, false)
      }
    }
  }

  function setChildrenLoading(
    parentAccountId: string,
    isActive: boolean,
  ): void {
    const next = { ...childrenLoadingCounts.value }
    if (isActive) {
      next[parentAccountId] = (next[parentAccountId] ?? 0) + 1
    } else {
      const remaining = (next[parentAccountId] ?? 0) - 1
      if (remaining > 0) {
        next[parentAccountId] = remaining
      } else {
        delete next[parentAccountId]
      }
    }
    childrenLoadingCounts.value = next
  }

  function childrenFor(parentAccountId: string): ChildProfile[] {
    return childrenByFamily.value[parentAccountId] ?? []
  }

  function childrenErrorFor(parentAccountId: string): ApiError | null {
    return childrenErrorsByFamily.value[parentAccountId] ?? null
  }

  function isChildrenLoading(parentAccountId: string): boolean {
    return (childrenLoadingCounts.value[parentAccountId] ?? 0) > 0
  }

  return {
    childrenErrorFor,
    childrenFor,
    error,
    families,
    isChildrenLoading,
    isLoadingFamilies,
    loadChildren,
    loadFamilies,
  }
})
