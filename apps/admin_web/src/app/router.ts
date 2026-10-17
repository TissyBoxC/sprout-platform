import { createRouter, createWebHistory } from 'vue-router'

import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'

const DashboardPage = () =>
  import('@/features/dashboard/presentation/DashboardPage.vue')
const DevicePage = () => import('@/features/device/presentation/DevicePage.vue')
const UITextPage = () => import('@/features/ui_text/presentation/UITextPage.vue')
const LoginPage = () => import('@/features/auth/presentation/LoginPage.vue')
const FamilyAccountsPage = () =>
  import('@/features/family/presentation/FamilyAccountsPage.vue')
const ChildProfilesPage = () =>
  import('@/features/child/presentation/ChildProfilesPage.vue')
const ReleasePage = () => import('@/features/ota/presentation/ReleasePage.vue')
const DeviceOtaPage = () =>
  import('@/features/ota/presentation/DeviceOtaPage.vue')
const SystemSettingsPage = () =>
  import('@/features/system/presentation/SystemSettingsPage.vue')
const AdminPermissionPage = () =>
  import('@/features/admin_permission/presentation/AdminPermissionPage.vue')
const VersionManagementPage = () =>
  import('@/features/version_management/presentation/VersionManagementPage.vue')
const FeatureCenterPage = () =>
  import('@/features/feature_center/presentation/FeatureCenterPage.vue')
const DownloadFilesPage = () =>
  import('@/features/downloads/presentation/DownloadFilesPage.vue')
const ContentLibraryPage = () =>
  import('@/features/content/presentation/ContentLibraryPage.vue')
const ThemePackPage = () =>
  import('@/features/content/presentation/ThemePackPage.vue')
const OfflinePolicyPage = () =>
  import('@/features/content/presentation/OfflinePolicyPage.vue')
const DeviceDiagnosticsPage = () =>
  import('@/features/audit/presentation/DeviceDiagnosticsPage.vue')
const OperationAuditPage = () =>
  import('@/features/audit/presentation/OperationAuditPage.vue')
const UsageReportPage = () =>
  import('@/features/usage_report/presentation/UsageReportPage.vue')
const NotificationCenterPage = () =>
  import('@/features/notifications/presentation/NotificationCenterPage.vue')
const VisionStatsPage = () =>
  import('@/features/vision/presentation/VisionStatsPage.vue')
const DisplayStatsPage = () =>
  import('@/features/display/presentation/DisplayStatsPage.vue')
const TouchStatsPage = () =>
  import('@/features/touch/presentation/TouchStatsPage.vue')
const CellularStatsPage = () =>
  import('@/features/cellular/presentation/CellularStatsPage.vue')
const PowerStatsPage = () =>
  import('@/features/power/presentation/PowerStatsPage.vue')
const MotionStatsPage = () =>
  import('@/features/motion/presentation/MotionStatsPage.vue')
const FormFactorStatsPage = () =>
  import('@/features/form_factor/presentation/FormFactorStatsPage.vue')
const VideoStatsPage = () =>
  import('@/features/video_companion/presentation/VideoStatsPage.vue')
const VideoSessionStatsPage = () =>
  import('@/features/video_session/presentation/VideoSessionStatsPage.vue')
const BluetoothAudioStatsPage = () =>
  import('@/features/bluetooth_audio/presentation/BluetoothAudioStatsPage.vue')
const LearningVisualizationStatsPage = () =>
  import(
    '@/features/learning_visualization/presentation/LearningVisualizationStatsPage.vue'
  )
const ImagePrivacyStatsPage = () =>
  import('@/features/image_privacy/presentation/ImagePrivacyStatsPage.vue')
const WearableStatsPage = () =>
  import('@/features/wearable/presentation/WearableStatsPage.vue')
const MultiDeviceStatsPage = () =>
  import('@/features/multi_device/presentation/MultiDeviceStatsPage.vue')
const ScreenStatsPage = () =>
  import('@/features/screen/presentation/ScreenStatsPage.vue')
const ForbiddenPage = () =>
  import('@/features/auth/presentation/ForbiddenPage.vue')

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
