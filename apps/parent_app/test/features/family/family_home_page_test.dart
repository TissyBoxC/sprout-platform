import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/auth/application/auth_controller.dart';
import 'package:parent_app/features/auth/data/auth_api.dart';
import 'package:parent_app/features/device/data/device_binding_api.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/data/family_api.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';
import 'package:parent_app/features/family/presentation/family_home_page.dart';
import 'package:parent_app/providers.dart';

void main() {
  testWidgets('a failed pull refresh keeps a retryable message', (
    tester,
  ) async {
    final authApi = _HomeAuthApi();
    final deviceApi = _HomeDeviceApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          authControllerProvider.overrideWith(_SignedInAuthController.new),
          authApiProvider.overrideWithValue(authApi),
          familyApiProvider.overrideWithValue(_HomeFamilyApi()),
          deviceBindingApiProvider.overrideWithValue(deviceApi),
        ],
        child: const MaterialApp(home: FamilyHomePage()),
      ),
    );
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
  _HomeAuthApi() : super(_UnusedApiClient());

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
    return const ParentOverview(
      todayConversationCount: 3,
      todaySpentUsd: 1.2,
      remainingBalanceUsd: 20,
      balanceUsd: 30,
      deviceCount: 1,
      onlineDeviceCount: 1,
    );
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
    return const [];
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
