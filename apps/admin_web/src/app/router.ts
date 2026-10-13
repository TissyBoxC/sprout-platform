import { createRouter, createWebHistory } from 'vue-router'

import DashboardPage from '@/features/dashboard/presentation/DashboardPage.vue'
import DevicePage from '@/features/device/presentation/DevicePage.vue'
import UITextPage from '@/features/ui_text/presentation/UITextPage.vue'
import LoginPage from '@/features/auth/presentation/LoginPage.vue'
import FamilyAccountsPage from '@/features/family/presentation/FamilyAccountsPage.vue'
import ChildProfilesPage from '@/features/child/presentation/ChildProfilesPage.vue'
import ReleasePage from '@/features/ota/presentation/ReleasePage.vue'
import DeviceOtaPage from '@/features/ota/presentation/DeviceOtaPage.vue'
import SystemSettingsPage from '@/features/system/presentation/SystemSettingsPage.vue'
import AdminPermissionPage from '@/features/admin_permission/presentation/AdminPermissionPage.vue'
import VersionManagementPage from '@/features/version_management/presentation/VersionManagementPage.vue'
import FeatureCenterPage from '@/features/feature_center/presentation/FeatureCenterPage.vue'
import DownloadFilesPage from '@/features/downloads/presentation/DownloadFilesPage.vue'
import ContentLibraryPage from '@/features/content/presentation/ContentLibraryPage.vue'
import ThemePackPage from '@/features/content/presentation/ThemePackPage.vue'
import OfflinePolicyPage from '@/features/content/presentation/OfflinePolicyPage.vue'
import DeviceDiagnosticsPage from '@/features/audit/presentation/DeviceDiagnosticsPage.vue'
import OperationAuditPage from '@/features/audit/presentation/OperationAuditPage.vue'
import UsageReportPage from '@/features/usage_report/presentation/UsageReportPage.vue'
import NotificationCenterPage from '@/features/notifications/presentation/NotificationCenterPage.vue'
import VisionStatsPage from '@/features/vision/presentation/VisionStatsPage.vue'
import DisplayStatsPage from '@/features/display/presentation/DisplayStatsPage.vue'
import TouchStatsPage from '@/features/touch/presentation/TouchStatsPage.vue'
import CellularStatsPage from '@/features/cellular/presentation/CellularStatsPage.vue'
import PowerStatsPage from '@/features/power/presentation/PowerStatsPage.vue'
import MotionStatsPage from '@/features/motion/presentation/MotionStatsPage.vue'
import FormFactorStatsPage from '@/features/form_factor/presentation/FormFactorStatsPage.vue'
import VideoStatsPage from '@/features/video_companion/presentation/VideoStatsPage.vue'
import VideoSessionStatsPage from '@/features/video_session/presentation/VideoSessionStatsPage.vue'
import BluetoothAudioStatsPage from '@/features/bluetooth_audio/presentation/BluetoothAudioStatsPage.vue'
import LearningVisualizationStatsPage from '@/features/learning_visualization/presentation/LearningVisualizationStatsPage.vue'
import ImagePrivacyStatsPage from '@/features/image_privacy/presentation/ImagePrivacyStatsPage.vue'
import WearableStatsPage from '@/features/wearable/presentation/WearableStatsPage.vue'
import MultiDeviceStatsPage from '@/features/multi_device/presentation/MultiDeviceStatsPage.vue'
import ScreenStatsPage from '@/features/screen/presentation/ScreenStatsPage.vue'
import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'
import ForbiddenPage from '@/features/auth/presentation/ForbiddenPage.vue'

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
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/families',
        name: 'families',
        component: FamilyAccountsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.familyRead },
      },
      {
        path: '/children',
        name: 'children',
        component: ChildProfilesPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.familyRead },
      },
      {
        path: '/usage-reports',
        name: 'usage-reports',
        component: UsageReportPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.familyRead },
      },
      {
        path: '/notifications',
        name: 'notifications',
        component: NotificationCenterPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.notificationsRead },
      },
      {
        path: '/ai-accounts',
        redirect: '/families',
      },
      {
        path: '/releases',
        name: 'releases',
        component: ReleasePage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.storageRead },
      },
      {
        path: '/device-ota',
        name: 'device-ota',
        component: DeviceOtaPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.otaRead },
      },
      {
        path: '/downloads',
        name: 'downloads',
        component: DownloadFilesPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.storageRead },
      },
      {
        path: '/content',
        name: 'content',
        component: ContentLibraryPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.contentRead },
      },
      {
        path: '/content/theme-packs',
        name: 'content-theme-packs',
        component: ThemePackPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.contentRead },
      },
      {
        path: '/content/offline-policy',
        name: 'content-offline-policy',
        component: OfflinePolicyPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.contentRead },
      },
      {
        path: '/services',
        name: 'services',
        component: VersionManagementPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.serviceRead },
      },
      {
        path: '/settings',
        name: 'settings',
        component: SystemSettingsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.settingsRead },
      },
      {
        path: '/admin-permissions',
        name: 'admin-permissions',
        component: AdminPermissionPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.adminsRead },
      },
      {
        path: '/feature-center',
        name: 'feature-center',
        component: FeatureCenterPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.featureCenterRead },
      },
      {
        path: '/devices',
        name: 'devices',
        component: DevicePage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.devicesRead },
      },
      {
        path: '/device-diagnostics',
        name: 'device-diagnostics',
        component: DeviceDiagnosticsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.devicesRead },
      },
      {
        path: '/vision',
        name: 'vision',
        component: VisionStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/display',
        name: 'display',
        component: DisplayStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/touch',
        name: 'touch',
        component: TouchStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/cellular',
        name: 'cellular',
        component: CellularStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/power',
        name: 'power',
        component: PowerStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/motion',
        name: 'motion',
        component: MotionStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/form-factor',
        name: 'form-factor',
        component: FormFactorStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/video-companion',
        name: 'video-companion',
        component: VideoStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/video-session',
        name: 'video-session',
        component: VideoSessionStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/bluetooth-audio',
        name: 'bluetooth-audio',
        component: BluetoothAudioStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/learning-visualization',
        name: 'learning-visualization',
        component: LearningVisualizationStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/image-privacy',
        name: 'image-privacy',
        component: ImagePrivacyStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/wearable',
        name: 'wearable',
        component: WearableStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/multi-device',
        name: 'multi-device',
        component: MultiDeviceStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/screen',
        name: 'screen',
        component: ScreenStatsPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.statsRead },
      },
      {
        path: '/audit',
        name: 'audit',
        component: OperationAuditPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.auditRead },
      },
      {
        path: '/ui-text',
        name: 'ui-text',
        component: UITextPage,
        meta: { requiredPermission: ADMIN_PERMISSIONS.contentRead },
      },
      {
        path: '/forbidden',
        name: 'forbidden',
        component: ForbiddenPage,
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
    if (!authStore.isSignedIn) {
      return '/login'
    }
    const requiredPermission = to.meta.requiredPermission
    if (
      typeof requiredPermission === 'string' &&
      !authStore.can(requiredPermission as Parameters<typeof authStore.can>[0])
    ) {
      return {
        name: 'forbidden',
        query: { from: to.fullPath },
      }
    }
    return true
  })

  return router
}
