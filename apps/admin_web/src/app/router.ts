import { createRouter, createWebHistory } from 'vue-router'

import DashboardPage from '@/features/dashboard/presentation/DashboardPage.vue'
import DevicePage from '@/features/device/presentation/DevicePage.vue'
import UITextPage from '@/features/ui_text/presentation/UITextPage.vue'
import LoginPage from '@/features/auth/presentation/LoginPage.vue'
import FamilyAccountsPage from '@/features/family/presentation/FamilyAccountsPage.vue'
import ChildProfilesPage from '@/features/child/presentation/ChildProfilesPage.vue'
import ReleasePage from '@/features/ota/presentation/ReleasePage.vue'
import SystemSettingsPage from '@/features/system/presentation/SystemSettingsPage.vue'
import VersionManagementPage from '@/features/version_management/presentation/VersionManagementPage.vue'
import DownloadFilesPage from '@/features/downloads/presentation/DownloadFilesPage.vue'
import { useAuthStore } from '@/features/auth/application/authStore'

/// Feature routes are registered here so removing a feature only changes its
/// page import and route entry.
export function createAdminRouter() {
  const router = createRouter({
    history: createWebHistory(import.meta.env.BASE_URL),
    routes: [
      {
        path: '/login',
        name: 'login',
        component: LoginPage,
        meta: { public: true },
      },
      {
        path: '/',
        name: 'dashboard',
        component: DashboardPage,
      },
      {
        path: '/families',
        name: 'families',
        component: FamilyAccountsPage,
      },
      {
        path: '/children',
        name: 'children',
        component: ChildProfilesPage,
      },
      {
        path: '/ai-accounts',
        redirect: '/families',
      },
      {
        path: '/releases',
        name: 'releases',
        component: ReleasePage,
      },
      {
        path: '/downloads',
        name: 'downloads',
        component: DownloadFilesPage,
      },
      {
        path: '/services',
        name: 'services',
        component: VersionManagementPage,
      },
      {
        path: '/settings',
        name: 'settings',
        component: SystemSettingsPage,
      },
      {
        path: '/devices',
        name: 'devices',
        component: DevicePage,
      },
      {
        path: '/ui-text',
        name: 'ui-text',
        component: UITextPage,
      },
    ],
  })

  router.beforeEach(async (to) => {
    const authStore = useAuthStore()
    if (authStore.isRestoring) {
      await authStore.restoreSession()
    }
    if (to.meta.public === true) {
      return authStore.isSignedIn ? '/' : true
    }
    return authStore.isSignedIn ? true : '/login'
  })

  return router
}
