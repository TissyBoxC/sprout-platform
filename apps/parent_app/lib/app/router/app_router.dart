import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../features/auth/application/auth_controller.dart';
import '../../features/auth/presentation/bind_email_page.dart';
import '../../features/auth/presentation/login_page.dart';
import '../../features/auth/presentation/register_page.dart';
import '../../features/child_profile/presentation/child_profile_page.dart';
import '../../features/content_management/presentation/content_library_page.dart';
import '../../features/device/presentation/device_list_page.dart';
import '../../features/device/domain/device_payload.dart';
import '../../features/device/presentation/device_provisioning_page.dart';
import '../../features/device/presentation/device_qr_scan_page.dart';
import '../../features/family/presentation/family_home_page.dart';
import '../../features/ota/presentation/app_update_page.dart';
import '../../features/parent_policy/presentation/parent_policy_page.dart';
import '../../features/profile/presentation/profile_page.dart';
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
        path: '/me/content',
        builder: (context, state) => const ContentLibraryPage(),
      ),
      GoRoute(
        path: '/me/children',
        builder: (context, state) => const ChildProfilePage(),
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
