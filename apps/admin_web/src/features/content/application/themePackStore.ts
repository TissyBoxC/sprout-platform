import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import {
  createAdminContentClient,
  type AdminContentClient,
  type ContentAgeTier,
  type ThemePack,
  type ThemePackInput,
} from '@/api/adminContent'
import { mapApiError, type ApiError } from '@/api/apiError'

export interface ThemePackStatusResult {
  succeeded: boolean
  error: ApiError | null
}

/// Owns the admin theme pack list and its create/edit/publish/withdraw actions.
/// The page reads this store as the single source of truth so a status change
/// never leaves the list and the editor out of sync.
export const useThemePackStore = defineStore('admin-content-theme-packs', () => {
  const client: AdminContentClient = createAdminContentClient()

  const packs = ref<ThemePack[]>([])
  const isLoading = ref(false)
  const isSubmitting = ref(false)
  const error = ref<ApiError | null>(null)
  const lastMessage = ref('')

  const draftCount = computed(
    () => packs.value.filter((pack) => pack.status === 'draft').length,
  )
  const publishedCount = computed(
    () => packs.value.filter((pack) => pack.status === 'published').length,
  )

  async function load(): Promise<void> {
    isLoading.value = true
    error.value = null
    try {
      packs.value = await client.loadThemePacks()
    } catch (loadError) {
      error.value = mapApiError(loadError)
    } finally {
      isLoading.value = false
    }
  }

  async function save(input: ThemePackInput): Promise<ThemePackStatusResult> {
    isSubmitting.value = true
    lastMessage.value = ''
    try {
      const saved = await client.saveThemePack(input)
      merge(saved)
      lastMessage.value = `主题包「${saved.title}」已保存`
      return { succeeded: true, error: null }
    } catch (saveError) {
      const mapped = mapApiError(saveError)
      return { succeeded: false, error: mapped }
    } finally {
      isSubmitting.value = false
    }
  }

  async function publish(themeId: string): Promise<ThemePackStatusResult> {
    return runAction(themeId, () => client.publishThemePack(themeId), '已发布主题包')
  }

  async function withdraw(themeId: string): Promise<ThemePackStatusResult> {
    return runAction(themeId, () => client.withdrawThemePack(themeId), '已撤回主题包')
  }

  async function remove(themeId: string): Promise<ThemePackStatusResult> {
    isSubmitting.value = true
    lastMessage.value = ''
    try {
      await client.deleteThemePack(themeId)
      packs.value = packs.value.filter((pack) => pack.themeId !== themeId)
      lastMessage.value = '已删除主题包'
      return { succeeded: true, error: null }
    } catch (removeError) {
      return { succeeded: false, error: mapApiError(removeError) }
    } finally {
      isSubmitting.value = false
    }
  }

  async function runAction(
    themeId: string,
    action: () => Promise<ThemePack>,
    message: string,
  ): Promise<ThemePackStatusResult> {
    isSubmitting.value = true
    lastMessage.value = ''
    try {
      const updated = await action()
      merge(updated)
      lastMessage.value = message
      return { succeeded: true, error: null }
    } catch (actionError) {
      return { succeeded: false, error: mapApiError(actionError) }
    } finally {
      isSubmitting.value = false
    }
  }

  function merge(pack: ThemePack): void {
    const index = packs.value.findIndex((item) => item.themeId === pack.themeId)
    if (index === -1) {
      packs.value = [pack, ...packs.value]
      return
    }
    const next = [...packs.value]
    next[index] = pack
    packs.value = next
  }

  return {
    packs,
    isLoading,
    isSubmitting,
    error,
    lastMessage,
    draftCount,
    publishedCount,
    load,
    save,
    publish,
    withdraw,
    remove,
  }
})

export const THEME_PACK_AGE_TIERS: ContentAgeTier[] = [
  'age_3_4',
  'age_5_6',
  'age_7_8',
]

export function themePackStatusLabel(status: string): string {
  return (
    {
      draft: '草稿',
      published: '已发布',
      withdrawn: '已撤回',
    }[status] ?? status
  )
}
