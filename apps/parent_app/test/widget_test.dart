import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/app/app.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/core/providers.dart';
import 'package:parent_app/core/storage/secure_store.dart';
import 'package:parent_app/features/auth/application/auth_controller.dart';
import 'package:parent_app/features/auth/data/auth_api.dart';
import 'package:parent_app/features/ota/presentation/mandatory_update_gate.dart';

void main() {
  testWidgets('signed-out users see the parent sign-in screen', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          secureStoreProvider.overrideWithValue(_EmptySecureStore()),
          mandatoryUpdateCheckProvider.overrideWithValue(false),
        ],
        child: const ParentApp(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.text('欢迎回家'), findsOneWidget);
    expect(find.text('登录'), findsOneWidget);
    expect(find.text('还没有账号？创建家长账号'), findsOneWidget);
  });

  testWidgets('email sign-in follows the public disabled policy', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          secureStoreProvider.overrideWithValue(_EmptySecureStore()),
          authApiProvider.overrideWithValue(_EmailDisabledAuthApi()),
          mandatoryUpdateCheckProvider.overrideWithValue(false),
        ],
        child: const ParentApp(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.text('手机号'), findsWidgets);
    expect(find.text('邮箱登录暂时关闭，请使用手机号登录'), findsOneWidget);

    await tester.enterText(
      find.byType(TextFormField).first,
      'guardian@example.com',
    );
    await tester.enterText(find.byType(TextFormField).last, 'sprout123');
    await tester.tap(find.widgetWithText(FilledButton, '登录'));
    await tester.pumpAndSettle();

    expect(find.text('请输入有效的手机号'), findsOneWidget);
  });

  testWidgets('login failure stays on the sign-in screen with a reason', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          secureStoreProvider.overrideWithValue(_EmptySecureStore()),
          authApiProvider.overrideWithValue(_FailingAuthApi()),
          mandatoryUpdateCheckProvider.overrideWithValue(false),
        ],
        child: const ParentApp(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    await tester.enterText(
      find.byType(TextFormField).first,
      'guardian@example.com',
    );
    await tester.enterText(find.byType(TextFormField).last, 'sprout123');
    await tester.tap(find.widgetWithText(FilledButton, '登录'));
    await tester.pumpAndSettle();

    expect(find.text('欢迎回家'), findsOneWidget);
    expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
  });

  testWidgets('invalid credentials keep the sign-in form and explain why', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          secureStoreProvider.overrideWithValue(_EmptySecureStore()),
          authApiProvider.overrideWithValue(_InvalidCredentialsAuthApi()),
          mandatoryUpdateCheckProvider.overrideWithValue(false),
        ],
        child: const ParentApp(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    await tester.enterText(find.byType(TextFormField).first, '13800138000');
    await tester.enterText(find.byType(TextFormField).last, 'sprout123');
    await tester.tap(find.widgetWithText(FilledButton, '登录'));
    await tester.pumpAndSettle();

    expect(find.text('欢迎回家'), findsOneWidget);
    expect(find.text('手机号、邮箱或密码不正确'), findsOneWidget);
  });

  testWidgets('registration failure stays on the form with a reason', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          secureStoreProvider.overrideWithValue(_EmptySecureStore()),
          authApiProvider.overrideWithValue(_FailingRegisterAuthApi()),
          mandatoryUpdateCheckProvider.overrideWithValue(false),
        ],
        child: const ParentApp(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    await tester.tap(find.text('还没有账号？创建家长账号'));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextFormField).at(0), '13800138000');
    await tester.enterText(find.byType(TextFormField).at(1), '000000');
    await tester.enterText(find.byType(TextFormField).at(2), 'sprout123');
    await tester.tap(find.byType(CheckboxListTile));
    final createAccountButton = find.widgetWithText(FilledButton, '创建账号');
    await tester.ensureVisible(createAccountButton);
    await tester.tap(createAccountButton);
    await tester.pumpAndSettle();

    expect(find.text('创建家长账号'), findsOneWidget);
    expect(find.text('这个手机号已经注册过'), findsOneWidget);
  });

  testWidgets('registration hides the code field when verification is off', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          secureStoreProvider.overrideWithValue(_EmptySecureStore()),
          authApiProvider.overrideWithValue(_VerificationOptionalAuthApi()),
          mandatoryUpdateCheckProvider.overrideWithValue(false),
        ],
        child: const ParentApp(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    await tester.tap(find.text('还没有账号？创建家长账号'));
    await tester.pumpAndSettle();

    expect(find.text('短信验证码'), findsNothing);
    expect(find.text('当前注册不需要短信验证码。'), findsOneWidget);
  });
}

class _EmptySecureStore implements SecureStore {
  @override
  Future<void> delete(String key) async {}

  @override
  Future<String?> read(String key) async => null;

  @override
  Future<void> write(String key, String value) async {}
}

class _FailingAuthApi implements AuthApi {
  @override
  Future<RegistrationPolicy> registrationPolicy() async {
    return const RegistrationPolicy(
      registrationEnabled: true,
      phoneVerificationRequired: true,
      emailLoginEnabled: true,
      guardianConsentVersion: '2026-03',
    );
  }

  @override
  Future<AuthResult> login({
    required String identifier,
    required String password,
  }) {
    throw const AppException(
      kind: AppErrorKind.network,
      message: '网络连接不稳定，请检查后重试',
      retryable: true,
    );
  }

  @override
  Future<AuthResult> register({
    required String phone,
    required String phoneVerificationCode,
    required String password,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
    required String guardianConsentVersion,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<void> sendPhoneVerification({
    required String phone,
    required String purpose,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AuthResult> refresh(String refreshToken) {
    throw UnimplementedError();
  }

  @override
  Future<void> logout(String refreshToken) {
    throw UnimplementedError();
  }

  @override
  Future<AuthResult> me() {
    throw UnimplementedError();
  }

  @override
  Future<ParentAccount> bindEmail({
    required String email,
    required String password,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AiAccount> updateSelectedModels(List<String> selectedModels) {
    throw UnimplementedError();
  }

  @override
  Future<ParentOverview> overview() {
    return Future<ParentOverview>.value(
      const ParentOverview(
        todayConversationCount: 0,
        todaySpentUsd: 0,
        remainingBalanceUsd: 0,
        balanceUsd: 0,
        grantedBalanceUsd: 0,
        deviceCount: 0,
        onlineDeviceCount: 0,
      ),
    );
  }

  @override
  Future<ParentAccount> updateProfile({
    required String displayName,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AppUpdateInfo?> appUpdate({
    required String platform,
    required String currentVersion,
    String? currentResourceVersion,
  }) {
    throw UnimplementedError();
  }
}

class _InvalidCredentialsAuthApi implements AuthApi {
  @override
  Future<RegistrationPolicy> registrationPolicy() async {
    return const RegistrationPolicy(
      registrationEnabled: true,
      phoneVerificationRequired: true,
      emailLoginEnabled: true,
      guardianConsentVersion: '2026-03',
    );
  }

  @override
  Future<AuthResult> login({
    required String identifier,
    required String password,
  }) {
    throw const AppException(
      kind: AppErrorKind.unauthenticated,
      message: '手机号、邮箱或密码不正确',
      retryable: false,
    );
  }

  @override
  Future<AuthResult> register({
    required String phone,
    required String phoneVerificationCode,
    required String password,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
    required String guardianConsentVersion,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<void> sendPhoneVerification({
    required String phone,
    required String purpose,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AuthResult> refresh(String refreshToken) {
    throw UnimplementedError();
  }

  @override
  Future<void> logout(String refreshToken) {
    throw UnimplementedError();
  }

  @override
  Future<AuthResult> me() {
    throw UnimplementedError();
  }

  @override
  Future<ParentAccount> bindEmail({
    required String email,
    required String password,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AiAccount> updateSelectedModels(List<String> selectedModels) {
    throw UnimplementedError();
  }

  @override
  Future<ParentOverview> overview() {
    return Future<ParentOverview>.value(
      const ParentOverview(
        todayConversationCount: 0,
        todaySpentUsd: 0,
        remainingBalanceUsd: 0,
        balanceUsd: 0,
        grantedBalanceUsd: 0,
        deviceCount: 0,
        onlineDeviceCount: 0,
      ),
    );
  }

  @override
  Future<ParentAccount> updateProfile({
    required String displayName,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AppUpdateInfo?> appUpdate({
    required String platform,
    required String currentVersion,
    String? currentResourceVersion,
  }) {
    throw UnimplementedError();
  }
}

class _EmailDisabledAuthApi extends _FailingAuthApi {
  @override
  Future<RegistrationPolicy> registrationPolicy() async {
    return const RegistrationPolicy(
      registrationEnabled: true,
      phoneVerificationRequired: false,
      emailLoginEnabled: false,
      guardianConsentVersion: '2026-03',
    );
  }
}

class _VerificationOptionalAuthApi extends _FailingAuthApi {
  @override
  Future<RegistrationPolicy> registrationPolicy() async {
    return const RegistrationPolicy(
      registrationEnabled: true,
      phoneVerificationRequired: false,
      emailLoginEnabled: true,
      guardianConsentVersion: '2026-03',
    );
  }
}

class _FailingRegisterAuthApi implements AuthApi {
  @override
  Future<RegistrationPolicy> registrationPolicy() async {
    return const RegistrationPolicy(
      registrationEnabled: true,
      phoneVerificationRequired: true,
      emailLoginEnabled: true,
      guardianConsentVersion: '2026-03',
    );
  }

  @override
  Future<AuthResult> register({
    required String phone,
    required String phoneVerificationCode,
    required String password,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
    required String guardianConsentVersion,
  }) {
    throw const AppException(
      kind: AppErrorKind.validation,
      message: '这个手机号已经注册过',
      retryable: false,
    );
  }

  @override
  Future<AuthResult> login({
    required String identifier,
    required String password,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<void> sendPhoneVerification({
    required String phone,
    required String purpose,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AuthResult> refresh(String refreshToken) {
    throw UnimplementedError();
  }

  @override
  Future<void> logout(String refreshToken) {
    throw UnimplementedError();
  }

  @override
  Future<AuthResult> me() {
    throw UnimplementedError();
  }

  @override
  Future<ParentAccount> bindEmail({
    required String email,
    required String password,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AiAccount> updateSelectedModels(List<String> selectedModels) {
    throw UnimplementedError();
  }

  @override
  Future<ParentOverview> overview() {
    return Future<ParentOverview>.value(
      const ParentOverview(
        todayConversationCount: 0,
        todaySpentUsd: 0,
        remainingBalanceUsd: 0,
        balanceUsd: 0,
        grantedBalanceUsd: 0,
        deviceCount: 0,
        onlineDeviceCount: 0,
      ),
    );
  }

  @override
  Future<ParentAccount> updateProfile({
    required String displayName,
    required String guardianFamilyName,
    required String childNickname,
    required String childBirthday,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AppUpdateInfo?> appUpdate({
    required String platform,
    required String currentVersion,
    String? currentResourceVersion,
  }) {
    throw UnimplementedError();
  }
}
