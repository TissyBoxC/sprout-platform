import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../features/alarm_reminder/presentation/alarm_reminder_page.dart';
import '../../features/auth/application/auth_controller.dart';
import '../../features/auth/presentation/bind_email_page.dart';
import '../../features/auth/presentation/login_page.dart';
import '../../features/auth/presentation/register_page.dart';
import '../../features/bluetooth_audio/presentation/bluetooth_audio_page.dart';
import '../../features/child_profile/presentation/child_profile_page.dart';
import '../../features/companionship/presentation/companionship_page.dart';
import '../../features/content_management/presentation/content_library_page.dart';
import '../../features/device/presentation/device_list_page.dart';
import '../../features/device/domain/device_payload.dart';
import '../../features/device/presentation/device_provisioning_page.dart';
import '../../features/device/presentation/device_qr_scan_page.dart';
import '../../features/english/presentation/english_page.dart';
import '../../features/family/presentation/family_home_page.dart';
import '../../features/image_privacy/presentation/image_privacy_page.dart';
import '../../features/learning_visualization/presentation/learning_visualization_page.dart';
import '../../features/local_command/presentation/local_command_page.dart';
import '../../features/notification/presentation/family_message_page.dart';
import '../../features/notification/presentation/notification_inbox_page.dart';
import '../../features/offline_policy/presentation/offline_policy_page.dart';
import '../../features/ota/presentation/app_update_page.dart';
import '../../features/parent_policy/presentation/parent_policy_page.dart';
import '../../features/playback/presentation/playback_page.dart';
import '../../features/profile/presentation/profile_page.dart';
import '../../features/settings/presentation/privacy_page.dart';
import '../../features/screen/presentation/screen_page.dart';
import '../../features/usage_report/presentation/usage_report_page.dart';
import '../../features/vision/presentation/vision_page.dart';
import '../../features/form_factor/presentation/form_factor_page.dart';
import '../../features/video_companion/presentation/video_companion_page.dart';
import '../../features/wearable/presentation/wearable_page.dart';
import '../../features/display/presentation/display_settings_page.dart';
import '../../features/touch/presentation/touch_settings_page.dart';
import '../../features/connectivity/presentation/cellular_settings_page.dart';
import '../../features/power/presentation/battery_settings_page.dart';
import '../../features/motion/presentation/motion_settings_page.dart';
import '../../features/multi_device/presentation/multi_device_page.dart';
import '../../shared/widgets/parent_shell.dart';

/// Creates the application router with authentication-aware redirects.
GoRouter createAppRouter(ProviderContainer container) {
  return GoRouter(
    refreshListenable: _AuthRefreshListenable(container),
    initialLocation: '/home',
    redirect: (context, state) {
      final authState = container.read(authControllerProvider);
      if (authState.isLoading) {
        return null;
      }
      final authValue = authState.value;
      final canRetryRestore = authValue?.canRetryRestore ?? false;
      final location = state.matchedLocation;
      final isPublicRoute = location == '/login' || location == '/register';
      if (canRetryRestore && !isPublicRoute) {
        return null;
      }
      final isSignedIn = authValue?.isSignedIn ?? false;
      if (!isSignedIn && !isPublicRoute) {
        return '/login';
      }
      if (isSignedIn && isPublicRoute) {
        return '/home';
      }
      return null;
    },
    routes: [
      GoRoute(path: '/login', builder: (context, state) => const LoginPage()),
      GoRoute(
        path: '/register',
        builder: (context, state) => const RegisterPage(),
      ),
      GoRoute(path: '/family', redirect: (context, state) => '/home'),
      GoRoute(
        path: '/account/email',
        builder: (context, state) => const BindEmailPage(),
      ),
      GoRoute(
        path: '/me/update',
        builder: (context, state) => const AppUpdatePage(),
      ),
      GoRoute(
        path: '/me/privacy',
        builder: (context, state) => const PrivacyPage(),
      ),
      GoRoute(
        path: '/me/content',
        builder: (context, state) => const ContentLibraryPage(),
      ),
      GoRoute(
        path: '/me/children',
        builder: (context, state) => const ChildProfilePage(),
      ),
      GoRoute(
        path: '/me/usage-reports',
        builder: (context, state) => const UsageReportPage(),
      ),
      GoRoute(
        path: '/me/companionship',
        builder: (context, state) => const CompanionshipPage(),
      ),
      GoRoute(
        path: '/me/alarm-reminders',
        builder: (context, state) => const AlarmReminderPage(),
      ),
      GoRoute(
        path: '/me/bluetooth-audio',
        builder: (context, state) => const BluetoothAudioPage(),
      ),
      GoRoute(
        path: '/me/learning-visualization',
        builder: (context, state) => const LearningVisualizationPage(),
      ),
      GoRoute(
        path: '/me/image-privacy',
        builder: (context, state) => const ImagePrivacyPage(),
      ),
      GoRoute(
        path: '/me/english',
        builder: (context, state) => const EnglishPage(),
      ),
      GoRoute(
        path: '/me/playback',
        builder: (context, state) => const PlaybackPage(),
      ),
      GoRoute(
        path: '/me/local-commands',
        builder: (context, state) => const LocalCommandPage(),
      ),
      GoRoute(
        path: '/me/offline-policy',
        builder: (context, state) => const OfflinePolicyPage(),
      ),
      GoRoute(
        path: '/me/display',
        builder: (context, state) => const DisplaySettingsPage(),
      ),
      GoRoute(
        path: '/me/screen',
        builder: (context, state) => const ScreenPage(),
      ),
      GoRoute(
        path: '/me/touch',
        builder: (context, state) => const TouchSettingsPage(),
      ),
      GoRoute(
        path: '/me/cellular',
        builder: (context, state) => const CellularSettingsPage(),
      ),
      GoRoute(
        path: '/me/battery',
        builder: (context, state) => const BatterySettingsPage(),
      ),
      GoRoute(
        path: '/me/motion',
        builder: (context, state) => const MotionSettingsPage(),
      ),
      GoRoute(
        path: '/me/video-companion',
        builder: (context, state) => const VideoCompanionPage(),
      ),
      GoRoute(
        path: '/me/wearable',
        builder: (context, state) => const WearablePage(),
      ),
      GoRoute(
        path: '/me/multi-device',
        builder: (context, state) => const MultiDevicePage(),
      ),
      GoRoute(
        path: '/me/notifications',
        builder: (context, state) => const NotificationInboxPage(),
      ),
      GoRoute(
        path: '/me/notifications/message',
        builder: (context, state) => const FamilyMessagePage(),
      ),
      GoRoute(
        path: '/me/children/:childId/policy',
        builder: (context, state) {
          final childId = state.pathParameters['childId'];
          if (childId == null || childId.isEmpty) {
            return const _RouteNotFoundPage();
          }
          return ParentPolicyPage(childId: childId);
        },
      ),
      StatefulShellRoute.indexedStack(
        builder: (context, state, navigationShell) =>
            ParentShell(navigationShell: navigationShell),
        branches: [
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: '/home',
                builder: (context, state) => const FamilyHomePage(),
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: '/devices',
                builder: (context, state) => const DeviceListPage(),
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: '/me',
                builder: (context, state) => const ProfilePage(),
              ),
            ],
          ),
        ],
      ),
      GoRoute(
        path: '/devices/scan',
        builder: (context, state) => const DeviceQrScanPage(),
      ),
      GoRoute(
        path: '/devices/:deviceId/vision',
        builder: (context, state) {
          final deviceId = state.pathParameters['deviceId'];
          if (deviceId == null || deviceId.isEmpty) {
            return const _RouteNotFoundPage();
          }
          return VisionPage(deviceId: deviceId);
        },
      ),
      GoRoute(
        path: '/devices/:deviceId/form-factor',
        builder: (context, state) {
          final deviceId = state.pathParameters['deviceId'];
          if (deviceId == null || deviceId.isEmpty) {
            return const _RouteNotFoundPage();
          }
          return FormFactorPage(deviceId: deviceId);
        },
      ),
      GoRoute(
        path: '/devices/provision',
        builder: (context, state) {
          final setup = state.extra;
          if (setup is! DeviceSetupPayload) {
            return const _RouteNotFoundPage();
          }
          return DeviceProvisioningPage(setup: setup);
        },
      ),
    ],
    errorBuilder: (context, state) => const _RouteNotFoundPage(),
  );
}

class _AuthRefreshListenable extends ChangeNotifier {
  _AuthRefreshListenable(ProviderContainer container) {
    _subscription = container.listen(
      authControllerProvider,
      (_, _) => notifyListeners(),
      fireImmediately: true,
    );
  }

  late final ProviderSubscription<AsyncValue<AuthState>> _subscription;

  @override
  void dispose() {
    _subscription.close();
    super.dispose();
  }
}

class _RouteNotFoundPage extends StatelessWidget {
  const _RouteNotFoundPage();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('页面走丢了')),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text('这个页面已经移动或不再存在。'),
              const SizedBox(height: 16),
              FilledButton(
                onPressed: () => context.go('/home'),
                child: const Text('返回首页'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
