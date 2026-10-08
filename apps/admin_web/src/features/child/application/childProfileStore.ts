import { defineStore } from 'pinia'
import { ref } from 'vue'

import { mapApiError, type ApiError } from '@/api/apiError'
import { createHttpClient } from '@/api/httpClient'
import {
  parseChildProfiles,
  parseParentAccountOptions,
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
  const childrenLoadingFamilyId = ref('')
  const error = ref<ApiError | null>(null)

  async function loadFamilies(): Promise<ApiError | null> {
    isLoadingFamilies.value = true
    error.value = null
    try {
      const response = await httpClient.get('/api/v1/admin/families')
      families.value = parseParentAccountOptions(response.data?.data)
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      error.value = mapped
      families.value = []
      return mapped
    } finally {
      isLoadingFamilies.value = false
    }
  }

  async function loadChildren(parentAccountId: string): Promise<ApiError | null> {
    childrenLoadingFamilyId.value = parentAccountId
    childrenErrorsByFamily.value = {
      ...childrenErrorsByFamily.value,
      [parentAccountId]: null,
    }
    try {
      const response = await httpClient.get(
        `/api/v1/admin/families/${encodeURIComponent(parentAccountId)}/children`,
      )
      childrenByFamily.value = {
        ...childrenByFamily.value,
        [parentAccountId]: parseChildProfiles(response.data?.data),
      }
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      childrenErrorsByFamily.value = {
        ...childrenErrorsByFamily.value,
        [parentAccountId]: mapped,
      }
      return mapped
    } finally {
      if (childrenLoadingFamilyId.value === parentAccountId) {
        childrenLoadingFamilyId.value = ''
      }
    }
  }

  function childrenFor(parentAccountId: string): ChildProfile[] {
    return childrenByFamily.value[parentAccountId] ?? []
  }

  function childrenErrorFor(parentAccountId: string): ApiError | null {
    return childrenErrorsByFamily.value[parentAccountId] ?? null
  }

  function isChildrenLoading(parentAccountId: string): boolean {
    return childrenLoadingFamilyId.value === parentAccountId
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
