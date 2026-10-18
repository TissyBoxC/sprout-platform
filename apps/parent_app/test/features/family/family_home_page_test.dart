import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/auth/application/auth_controller.dart';
import 'package:parent_app/features/auth/data/auth_api.dart';
import 'package:parent_app/features/device/application/device_binding_controller.dart';
import 'package:parent_app/features/device/data/device_binding_api.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/data/family_api.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';
import 'package:parent_app/features/family/presentation/family_home_page.dart';
import 'package:parent_app/providers.dart';

void main() {
  testWidgets('a family without devices says nothing is bound yet', (
    tester,
  ) async {
    await tester.pumpWidget(
      _homeApp(overview: _overview(deviceCount: 0), devices: const []),
    );
    await tester.pumpAndSettle();

    expect(find.text('尚未绑定'), findsOneWidget);
    expect(find.text('还没有绑定设备'), findsOneWidget);
    expect(find.text('设备离线'), findsNothing);
  });

  testWidgets('reports syncing when device runtime has not arrived', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(800, 1800));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.pumpWidget(
      _homeApp(
        overview: _overview(deviceCount: 1),
        devices: [_device(runtime: null)],
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('状态待同步'), findsOneWidget);
    expect(find.text('状态尚未同步'), findsOneWidget);
    expect(find.textContaining('离线'), findsNothing);
  });

  testWidgets('shows offline only for a real runtime snapshot', (tester) async {
    await tester.binding.setSurfaceSize(const Size(800, 1800));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.pumpWidget(
      _homeApp(
        overview: _overview(deviceCount: 1),
        devices: [
          _device(
            runtime: const DeviceRuntimeStatus(
              isOnline: false,
              connectionState: 'disconnected',
              transport: 'wifi',
              networkQuality: 'unknown',
              rssiDbm: 0,
              latencyMs: 0,
              packetLossPercent: 0,
              temperatureState: 'unknown',
              temperatureCelsiusTenths: null,
              recentCrashCount: 0,
              timeSyncState: 'unknown',
              lastSyncedAt: null,
              offlineState: 'offline',
              offlineReason: 'network_loss',
              fallbackActive: false,
              pendingTelemetry: 0,
              reportedAt: null,
              receivedAt: null,
              provisioning: null,
            ),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('状态待同步'), findsOneWidget);
    expect(find.text('离线 · 设备联网后会自动同步'), findsOneWidget);
  });

  testWidgets('a failed pull refresh keeps a retryable message', (
    tester,
  ) async {
    final authApi = _HomeAuthApi();
    final deviceApi = _HomeDeviceApi();
    await tester.pumpWidget(_homeApp(authApi: authApi, deviceApi: deviceApi));
    await tester.pumpAndSettle();

    deviceApi.fail = true;
    tester
        .state<RefreshIndicatorState>(find.byType(RefreshIndicator))
        .show()
        .ignore();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump(const Duration(milliseconds: 500));

    expect(find.text('网络连接不稳定，请稍后重试'), findsOneWidget);
    expect(find.text('重试'), findsOneWidget);
    expect(authApi.overviewCalls, 2);
    expect(deviceApi.listCalls, 2);
  });
}

Widget _homeApp({
  _HomeAuthApi? authApi,
  _HomeDeviceApi? deviceApi,
  ParentOverview? overview,
  List<BoundDevice> devices = const [],
}) {
  return ProviderScope(
    overrides: [
      authControllerProvider.overrideWith(_SignedInAuthController.new),
      deviceBindingControllerProvider.overrideWith(
        _HomeDeviceBindingController.new,
      ),
      authApiProvider.overrideWithValue(
        authApi ?? _HomeAuthApi(overview: overview, devices: devices),
      ),
      familyApiProvider.overrideWithValue(_HomeFamilyApi()),
      deviceBindingApiProvider.overrideWithValue(
        deviceApi ?? _HomeDeviceApi(devices: devices),
      ),
    ],
    child: const MaterialApp(home: FamilyHomePage()),
  );
}

ParentOverview _overview({required int deviceCount}) {
  return ParentOverview(
    todayConversationCount: 3,
    todaySpentUsd: 1.2,
    remainingBalanceUsd: 20,
    balanceUsd: 30,
    deviceCount: deviceCount,
    onlineDeviceCount: 0,
  );
}

BoundDevice _device({required DeviceRuntimeStatus? runtime}) {
  return BoundDevice(
    deviceId: 'device-001',
    deviceName: '初芽',
    hardwareModel: 'sprout_initial',
    firmwareVersion: '0.3.0',
    capabilities: const ['display'],
    lifecycleStatus: 'active',
    boundAt: DateTime.parse('2026-10-01T08:00:00Z'),
    runtime: runtime,
  );
}

class _SignedInAuthController extends AuthController {
  @override
  Future<AuthState> build() async {
    return const AuthState(
      isLoading: false,
      account: _account,
      aiAccount: null,
    );
  }
}

class _HomeAuthApi extends AuthApi {
  _HomeAuthApi({ParentOverview? overview, List<BoundDevice> devices = const []})
    : _overview = overview ?? _defaultOverview,
      super(_UnusedApiClient());

  final ParentOverview _overview;
  int overviewCalls = 0;

  @override
  Future<AuthResult> me() async {
    return const AuthResult(
      accessToken: '',
      refreshToken: '',
      account: _account,
    );
  }

  @override
  Future<ParentOverview> overview() async {
    overviewCalls += 1;
    return _overview;
  }
}

class _UnusedApiClient implements ApiClient {
  @override
  Future<Map<String, Object?>> delete(String path) =>
      throw UnimplementedError();

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) => throw UnimplementedError();

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) =>
      throw UnimplementedError();

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) => throw UnimplementedError();

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) =>
      throw UnimplementedError();
}

class _HomeDeviceApi implements DeviceBindingApi {
  _HomeDeviceApi({this.devices = const []});

  final List<BoundDevice> devices;
  bool fail = false;
  int listCalls = 0;

  @override
  Future<List<BoundDevice>> list() async {
    listCalls += 1;
    if (fail) {
      throw const AppException(
        kind: AppErrorKind.network,
        message: '网络连接不稳定，请检查后重试',
        retryable: true,
      );
    }
    return devices;
  }

  @override
  Future<BoundDevice> bind({
    required String token,
    required String deviceName,
  }) => throw UnimplementedError();

  @override
  Future<void> remove(String deviceId) => throw UnimplementedError();

  @override
  Future<DeviceProvisioningTicket> requestProvisioningTicket(
    String deviceId,
    String deviceSessionToken,
  ) => throw UnimplementedError();
}

class _HomeDeviceBindingController extends DeviceBindingController {
  @override
  Future<DeviceBindingState> build() async {
    final api = ref.read(deviceBindingApiProvider);
    try {
      final devices = await api.list();
      return DeviceBindingState(
        isScanning: false,
        devices: const [],
        bindings: devices,
      );
    } on Object catch (error) {
      return DeviceBindingState(
        isScanning: false,
        devices: const [],
        bindings: const [],
        errorMessage: authErrorMessage(error),
      );
    }
  }

  @override
  Future<void> refresh() async {
    final api = ref.read(deviceBindingApiProvider);
    try {
      final devices = await api.list();
      state = AsyncData(
        DeviceBindingState(
          isScanning: false,
          devices: const [],
          bindings: devices,
        ),
      );
    } on Object catch (error) {
      state = AsyncData(
        DeviceBindingState(
          isScanning: false,
          devices: const [],
          bindings: state.value?.bindings ?? const [],
          errorMessage: authErrorMessage(error),
        ),
      );
    }
  }
}

class _HomeFamilyApi implements FamilyApi {
  @override
  Future<FamilyCapability> fetchCapability() async => const FamilyCapability(
    familyId: 'family-1',
    ownerUserId: 'account-001',
    memberCount: 1,
    pendingInviteCount: 0,
    schemaVersion: '1.0.0',
  );

  @override
  Future<List<FamilyMember>> fetchMembers() async => const [];

  @override
  Future<List<FamilyInvitation>> fetchInvitations() async => const [];

  @override
  Future<FamilyInvitation> createInvitation({
    required String email,
    required String role,
  }) => throw UnimplementedError();

  @override
  Future<FamilyMember> updateMemberRole(String userId, String role) =>
      throw UnimplementedError();

  @override
  Future<void> removeMember(String userId) => throw UnimplementedError();

  @override
  Future<void> revokeInvitation(String invitationId) =>
      throw UnimplementedError();

  @override
  Future<List<FamilyInvitation>> fetchMyInvitations() async => const [];

  @override
  Future<FamilyMember> acceptInvitation({
    required String familyId,
    required String invitationId,
  }) => throw UnimplementedError();
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

const _defaultOverview = ParentOverview(
  todayConversationCount: 3,
  todaySpentUsd: 1.2,
  remainingBalanceUsd: 20,
  balanceUsd: 30,
  deviceCount: 1,
  onlineDeviceCount: 1,
);
