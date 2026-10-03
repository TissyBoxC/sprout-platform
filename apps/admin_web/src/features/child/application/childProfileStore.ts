import { defineStore } from 'pinia'
import { ref } from 'vue'

import { mapApiError, type ApiError } from '@/api/apiError'
import { createHttpClient } from '@/api/httpClient'
import {
  parseChildProfiles,
  parseParentAccountOptions,
  parseParentPolicy,
  type ChildProfile,
  type ParentAccountOption,
  type ParentPolicy,
} from '@/features/child/domain/childProfile'

// This is a read-only support view, so the store intentionally keeps the
// administrator from editing or deleting a family's child data.
export const useChildProfileStore = defineStore('admin-child-profiles', () => {
  const httpClient = createHttpClient()
  const families = ref<ParentAccountOption[]>([])
  const childrenByFamily = ref<Record<string, ChildProfile[]>>({})
  const childrenErrorsByFamily = ref<Record<string, ApiError | null>>({})
  const policyByChild = ref<Record<string, ParentPolicy | null>>({})
  const policyErrorsByChild = ref<Record<string, ApiError | null>>({})
  const policyLoadingByChild = ref<Record<string, boolean>>({})
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

  async function loadPolicy(childId: string): Promise<ApiError | null> {
    policyLoadingByChild.value = {
      ...policyLoadingByChild.value,
      [childId]: true,
    }
    policyErrorsByChild.value = {
      ...policyErrorsByChild.value,
      [childId]: null,
    }
    try {
      const response = await httpClient.get(
        `/api/v1/admin/children/${encodeURIComponent(childId)}/policy`,
      )
      policyByChild.value = {
        ...policyByChild.value,
        [childId]: parseParentPolicy(response.data?.data?.policy),
      }
      return null
    } catch (caught: unknown) {
      const mapped = mapApiError(caught)
      if (mapped.kind === 'not_found') {
        policyByChild.value = {
          ...policyByChild.value,
          [childId]: null,
        }
        return null
      }
      policyErrorsByChild.value = {
        ...policyErrorsByChild.value,
        [childId]: mapped,
      }
      return mapped
    } finally {
      policyLoadingByChild.value = {
        ...policyLoadingByChild.value,
        [childId]: false,
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

  function policyFor(child: ChildProfile): ParentPolicy | null {
    if (Object.prototype.hasOwnProperty.call(policyByChild.value, child.childId)) {
      return policyByChild.value[child.childId] ?? null
    }
    return child.policy
  }

  function policyErrorFor(childId: string): ApiError | null {
    return policyErrorsByChild.value[childId] ?? null
  }

  function isPolicyLoading(childId: string): boolean {
    return policyLoadingByChild.value[childId] ?? false
  }

  return {
    childrenErrorFor,
    childrenFor,
    error,
    families,
    isChildrenLoading,
    isLoadingFamilies,
    isPolicyLoading,
    loadChildren,
    loadFamilies,
    loadPolicy,
    policyErrorFor,
    policyFor,
  }
})
