import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../features/alarm_reminder/presentation/alarm_reminder_page.dart';
import '../../features/assessment/presentation/assessment_page.dart';
import '../../features/auth/application/auth_controller.dart';
import '../../features/auth/presentation/bind_email_page.dart';
import '../../features/auth/presentation/login_page.dart';
import '../../features/auth/presentation/register_page.dart';
import '../../features/billing/domain/billing_errors.dart';
import '../../features/billing/presentation/billing_checkout_page.dart';
import '../../features/billing/presentation/billing_consent_page.dart';
import '../../features/billing/presentation/billing_orders_page.dart';
import '../../features/billing/presentation/billing_plans_page.dart';
import '../../features/billing/presentation/spending_limit_page.dart';
import '../../features/care_view/presentation/care_view_page.dart';
import '../../features/memory/presentation/memory_page.dart';
import '../../features/growth_report/presentation/growth_report_page.dart';
import '../../features/bluetooth_audio/presentation/bluetooth_audio_page.dart';
import '../../features/child_profile/presentation/child_profile_page.dart';
import '../../features/companionship/presentation/companionship_page.dart';
import '../../features/content_management/presentation/content_library_page.dart';
import '../../features/device/application/device_binding_controller.dart';
import '../../features/device/presentation/device_list_page.dart';
import '../../features/device/domain/device_payload.dart';
import '../../features/device/presentation/device_provisioning_page.dart';
import '../../features/device/presentation/device_qr_scan_page.dart';
import '../../features/english/presentation/english_page.dart';
import '../../features/family/presentation/family_home_page.dart';
import '../../features/family/presentation/family_invite_accept_page.dart';
import '../../features/family/presentation/family_invitations_page.dart';
import '../../features/family/presentation/family_members_page.dart';
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
  final loginDestination = LoginDestination();
  return GoRouter(
    refreshListenable: _AuthRefreshListenable(container),
    initialLocation: '/home',
    redirect: (context, routeState) {
      final authState = container.read(authControllerProvider);
      if (authState.isLoading) {
        return null;
      }
      final authValue = authState.value;
      final canRetryRestore = authValue?.canRetryRestore ?? false;
      final location = routeState.matchedLocation;
      final isLoginRoute = location == '/login';
      final isRegisterRoute = location == '/register';
      final isPublicRoute = isLoginRoute || isRegisterRoute;
      if (canRetryRestore && !isPublicRoute) {
        return null;
      }
      final isSignedIn = authValue?.isSignedIn ?? false;
      if (!isSignedIn && !isPublicRoute) {
        loginDestination.capture(
          routeState.uri,
          rawLocation: routeState.uri.toString(),
        );
        return '/login';
      }
      if (isSignedIn) {
        if (isLoginRoute || isRegisterRoute) {
          return loginDestination.take() ?? '/home';
        }
        loginDestination.clear();
      }
      return null;
    },
    routes: [
      GoRoute(
        path: '/login',
        builder: (context, state) => LoginPage(destination: loginDestination),
      ),
      GoRoute(
        path: '/register',
        builder: (context, state) =>
            RegisterPage(destination: loginDestination),
      ),
      GoRoute(path: '/family', redirect: (context, state) => '/home'),
      GoRoute(
        path: '/me/family',
        builder: (context, state) => const FamilyMembersPage(),
      ),
      GoRoute(
        path: '/me/family/invitations',
        builder: (context, state) => const FamilyInvitationsPage(),
      ),
      GoRoute(
        path: '/family-invite',
        builder: (context, state) {
          final familyId = state.uri.queryParameters['family_id']?.trim() ?? '';
          final invitationId =
              state.uri.queryParameters['invitation_id']?.trim() ?? '';
          if (familyId.isEmpty || invitationId.isEmpty) {
            return const _RouteNotFoundPage();
          }
          return FamilyInviteAcceptPage(
            familyId: familyId,
            invitationId: invitationId,
          );
        },
      ),
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
        path: '/me/billing',
        builder: (context, state) => const BillingPlansPage(),
      ),
      GoRoute(
        path: '/me/billing/orders',
        builder: (context, state) => const BillingOrdersPage(),
      ),
      GoRoute(
        path: '/me/billing/consent',
        builder: (context, state) => const BillingConsentPage(),
      ),
      GoRoute(
        path: '/me/billing/spending-limit',
        builder: (context, state) => const SpendingLimitPage(),
      ),
      // 记忆页面展示儿童的对话记忆，属于监护人自己的数据访问入口。
      GoRoute(
        path: '/me/memories',
        builder: (context, state) => const MemoryPage(),
      ),
      // 成长报告由平台按周/按月自动生成，家长只读回看。
      GoRoute(
        path: '/me/growth-reports',
        builder: (context, state) => const GrowthReportPage(),
      ),
      // 结算页必须带着选中的套餐进入；直接打开链接时没有可结算的内容。
      GoRoute(
        path: '/me/billing/checkout',
        builder: (context, state) {
          final plan = state.extra;
          if (plan is! BillingPlan) {
            return const _RouteNotFoundPage();
          }
          return BillingCheckoutPage(plan: plan);
        },
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
      // 测评从英语学习页进入；直接打开链接时只展示课程列表，不会开始任何测评。
      GoRoute(
        path: '/me/english/assessment',
        builder: (context, state) => const AssessmentGate(),
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
        redirect: (context, routeState) {
          final deviceState = container
              .read(deviceBindingControllerProvider)
              .value;
          final canManage =
              deviceState?.bindings.any((device) => device.canManage) ?? false;
          if (!canManage) {
            return '/devices';
          }
          return null;
        },
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
        path: '/devices/:deviceId/care-view',
        builder: (context, state) {
          final deviceId = state.pathParameters['deviceId'];
          if (deviceId == null || deviceId.isEmpty) {
            return const _RouteNotFoundPage();
          }
          final deviceName = state.uri.queryParameters['name']?.trim() ?? '';
          return CareViewPage(deviceId: deviceId, deviceName: deviceName);
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

/// Holds one authenticated in-app destination across a sign-in redirect.
///
/// Only a path and query from this app are accepted. Scheme, host, fragment,
/// control characters, and path traversal are rejected to prevent open
/// redirects or malformed routes from being replayed.
class LoginDestination {
  String? _pending;

  String? get pending => _pending;

  void capture(Uri uri, {String? rawLocation}) {
    final raw = rawLocation ?? uri.toString();
    final path = uri.path;
    final decodedRaw = _decodeForSafety(raw);
    final decodedPath = _decodeForSafety(path);
    final decodedQuery = _decodeForSafety(uri.query);
    if (decodedRaw == null ||
        !_hasSafeRawPath(raw) ||
        !path.startsWith('/') ||
        path.startsWith('//') ||
        uri.hasScheme ||
        uri.hasAuthority ||
        uri.hasFragment ||
        raw.contains('..') ||
        raw.contains('\\') ||
        decodedPath == null ||
        decodedQuery == null ||
        decodedPath.contains('..') ||
        decodedPath.contains('\\') ||
        _containsControlCharacter(decodedPath) ||
        _containsControlCharacter(decodedQuery)) {
      _pending = null;
      return;
    }
    if (path == '/login' || path == '/register') {
      _pending = null;
      return;
    }
    _pending = uri.toString();
  }

  String? take() {
    final destination = _pending;
    _pending = null;
    return destination;
  }

  void clear() {
    _pending = null;
  }
}

bool _hasSafeRawPath(String raw) {
  if (!raw.startsWith('/') || raw.startsWith('//')) {
    return false;
  }
  // Uri.parse normalizes percent-encoded dot segments before routing. Check
  // the original location as well so crafted deep links cannot become an
  // in-app path with traversal segments.
  final pathEnd = raw.indexOf(RegExp(r'[?#]'));
  final rawPath = pathEnd == -1 ? raw : raw.substring(0, pathEnd);
  final lowerPath = rawPath.toLowerCase();
  return !lowerPath.contains('%2e') &&
      !lowerPath.contains('%2f') &&
      !lowerPath.contains('%5c') &&
      !RegExp(r'%(?:0[0-9a-f]|1[0-9a-f]|7f)').hasMatch(lowerPath);
}

bool _containsControlCharacter(String value) {
  return value.codeUnits.any((codeUnit) => codeUnit < 0x20 || codeUnit == 0x7f);
}

String? _decodeForSafety(String value) {
  try {
    return Uri.decodeComponent(value);
  } on FormatException {
    return null;
  }
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
