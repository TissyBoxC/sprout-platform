import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/app/router/app_router.dart';
import 'package:parent_app/features/auth/application/auth_controller.dart';
import 'package:parent_app/features/auth/data/auth_api.dart';
import 'package:parent_app/features/device/application/device_binding_controller.dart';
import 'package:parent_app/features/device/data/device_binding_api.dart';
import 'package:parent_app/providers.dart';

void main() {
  testWidgets(
    'invitation deep link survives sign-in without losing parameters',
    (tester) async {
      final auth = _TestAuthController();
      final container = ProviderContainer(
        overrides: [authControllerProvider.overrideWith(() => auth)],
      );
      final router = createAppRouter(container);
      addTearDown(() {
        router.dispose();
        container.dispose();
      });

      await tester.pumpWidget(MaterialApp.router(routerConfig: router));
      await tester.pumpAndSettle();
      expect(find.text('欢迎回家'), findsOneWidget);

      router.go('/family-invite?family_id=family-1&invitation_id=invite-1');
      await tester.pumpAndSettle();
      expect(find.text('欢迎回家'), findsOneWidget);

      auth.signIn();
      await tester.pumpAndSettle();

      expect(find.text('接受家庭邀请'), findsOneWidget);
      expect(find.text('加入这个家庭'), findsOneWidget);
    },
  );

  testWidgets(
    'deep link survives registration navigation without becoming a cycle',
    (tester) async {
      final auth = _TestAuthController();
      final container = ProviderContainer(
        overrides: [authControllerProvider.overrideWith(() => auth)],
      );
      final router = createAppRouter(container);
      addTearDown(() {
        router.dispose();
        container.dispose();
      });

      await tester.pumpWidget(MaterialApp.router(routerConfig: router));
      await tester.pumpAndSettle();

      router.go('/family-invite?family_id=family-2&invitation_id=invite-2');
      await tester.pumpAndSettle();
      await tester.tap(find.text('还没有账号？创建家长账号'));
      await tester.pumpAndSettle();
      expect(find.text('创建家长账号'), findsOneWidget);

      auth.signIn();
      await tester.pumpAndSettle();

      expect(find.text('接受家庭邀请'), findsOneWidget);
      expect(find.text('加入这个家庭'), findsOneWidget);
    },
  );

  test('login destination only accepts an in-app path and query', () {
    final destination = LoginDestination()
      ..capture(
        Uri.parse('/family-invite?family_id=family-1&invitation_id=invite-1'),
      );

    expect(
      destination.take(),
      '/family-invite?family_id=family-1&invitation_id=invite-1',
    );

    final unsafeLocations = <String>[
      'https://example.com/phishing',
      '//example.com/phishing',
      '/../private',
      '/%2E%2E/private',
      '/%2e%2e/private',
      '/home%2f..%2fprivate',
      '/home%5c..%5cprivate',
      '/home#fragment',
      '/home%0d%0aLocation:%20https://example.com',
    ];
    for (final unsafe in unsafeLocations) {
      destination.capture(Uri.parse(unsafe), rawLocation: unsafe);
      expect(destination.take(), isNull, reason: '$unsafe must be rejected');
    }
  });

  testWidgets('viewer cannot enter the device scan route directly', (
    tester,
  ) async {
    final auth = _TestAuthController();
    final container = ProviderContainer(
      overrides: [
        authControllerProvider.overrideWith(() => auth),
        deviceBindingApiProvider.overrideWithValue(_ViewerDeviceApi()),
      ],
    );
    addTearDown(container.dispose);
    final router = createAppRouter(container);
    addTearDown(router.dispose);
    await container.read(deviceBindingControllerProvider.future);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));
    auth.signIn();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));
    router.go('/devices/scan');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(router.state.uri.path, '/devices');
  });
}

class _ViewerDeviceApi implements DeviceBindingApi {
  @override
  Future<List<BoundDevice>> list() async {
    return [
      BoundDevice(
        deviceId: 'device-viewer',
        deviceName: '初芽',
        hardwareModel: 'sprout_initial',
        firmwareVersion: '1.0.0',
        capabilities: const [],
        lifecycleStatus: 'active',
        boundAt: DateTime.utc(2026, 10, 1),
        runtime: null,
        canManage: false,
      ),
    ];
  }

  @override
  Future<void> remove(String deviceId) {
    throw UnimplementedError();
  }

  @override
  Future<BoundDevice> bind({
    required String token,
    required String deviceName,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<DeviceProvisioningTicket> requestProvisioningTicket(
    String deviceId,
    String deviceSessionToken,
  ) {
    throw UnimplementedError();
  }
}

class _TestAuthController extends AuthController {
  @override
  Future<AuthState> build() async => const AuthState.signedOut();

  void signIn() {
    state = const AsyncData(
      AuthState(isLoading: false, account: _account, aiAccount: null),
    );
  }
}

const _account = ParentAccount(
  id: 'account-001',
  email: 'guardian@example.com',
  phone: '13800138000',
  displayName: '林家长',
  guardianFamilyName: '林',
  childNickname: '小芽',
  childBirthday: '2021-06-01',
  role: 'parent',
  status: 'active',
);
