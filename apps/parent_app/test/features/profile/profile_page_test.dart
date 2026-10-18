import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/auth/application/auth_controller.dart';
import 'package:parent_app/features/auth/data/auth_api.dart';
import 'package:parent_app/features/profile/presentation/profile_page.dart';

void main() {
  testWidgets('shows the signed-in guardian and child summary', (tester) async {
    await tester.pumpWidget(
      _wrap(
        _FakeAuthController(
          AuthState(
            isLoading: false,
            account: _account(),
            aiAccount: _aiAccount(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('我的'), findsOneWidget);
    expect(find.text('小萌'), findsWidgets);
    expect(find.text('朵朵'), findsWidgets);
    expect(find.text('家长资料'), findsOneWidget);
    expect(find.text('13800138000'), findsOneWidget);
    expect(find.text('AI 陪伴账号'), findsOneWidget);
    expect(find.text('12.50'), findsOneWidget);
    expect(find.text('2 台'), findsOneWidget);
  });

  testWidgets('falls back to placeholder copy when profile fields are empty', (
    tester,
  ) async {
    await tester.pumpWidget(
      _wrap(
        _FakeAuthController(
          AuthState(
            isLoading: false,
            account: _account(
              displayName: '',
              guardianFamilyName: '',
              childNickname: '',
              childBirthday: '',
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('家长'), findsWidgets);
    expect(find.text('未填写'), findsWidgets);
  });

  testWidgets('prompts to sign in when there is no account', (tester) async {
    await tester.pumpWidget(
      _wrap(_FakeAuthController(const AuthState.signedOut())),
    );
    await tester.pumpAndSettle();

    expect(find.text('请先登录家长账号'), findsOneWidget);
  });

  testWidgets('shows a retry state when the account fails to load', (
    tester,
  ) async {
    final controller = _FakeAuthController(const AuthState.signedOut())
      ..error = const AppException(
        kind: AppErrorKind.network,
        message: '网络连接不稳定，请检查后重试',
        retryable: true,
      );
    await tester.pumpWidget(_wrap(controller));
    await tester.pumpAndSettle();

    expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
    expect(find.text('重新加载'), findsOneWidget);
  });

  testWidgets('prepares the AI service when it is still pending', (
    tester,
  ) async {
    final controller = _FakeAuthController(
      AuthState(isLoading: false, account: _account()),
    );
    await tester.pumpWidget(_wrap(controller));
    await tester.pumpAndSettle();

    expect(find.text('AI 陪伴服务正在准备'), findsOneWidget);
    await tester.tap(find.text('重新准备'));
    await tester.pumpAndSettle();

    expect(controller.retryAiCalls, 1);
  });

  testWidgets('hides AI balance and models until the provider is ready', (
    tester,
  ) async {
    final controller = _FakeAuthController(
      AuthState(
        isLoading: false,
        account: _account(),
        aiAccount: _aiAccount(providerReady: false),
      ),
    );
    await tester.pumpWidget(_wrap(controller));
    await tester.pumpAndSettle();

    expect(find.text('AI 陪伴服务尚未就绪'), findsOneWidget);
    expect(find.text('12.50'), findsNothing);
    expect(find.text('选择可用模型'), findsNothing);

    await tester.tap(find.text('重新检查'));
    await tester.pumpAndSettle();

    expect(controller.retryAiCalls, 1);
  });

  testWidgets('shows retry failures inline instead of swallowing them', (
    tester,
  ) async {
    final controller =
        _FakeAuthController(
            AuthState(
              isLoading: false,
              account: _account(),
              aiAccount: _aiAccount(providerReady: false),
            ),
          )
          ..retryError = const AppException(
            kind: AppErrorKind.network,
            message: '网络连接不稳定，请检查后重试',
            retryable: true,
          );
    await tester.pumpWidget(_wrap(controller));
    await tester.pumpAndSettle();

    await tester.tap(find.text('重新检查'));
    await tester.pumpAndSettle();

    expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
    expect(find.text('重新检查'), findsOneWidget);
  });

  testWidgets('saving all models sends an explicit empty selection', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(800, 2400));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = _FakeAuthController(
      AuthState(isLoading: false, account: _account(), aiAccount: _aiAccount()),
    );
    await tester.pumpWidget(_wrap(controller));
    await tester.pumpAndSettle();

    await tester.tap(find.text('选择可用模型'));
    await tester.pumpAndSettle();

    expect(find.text('全部已开通模型'), findsOneWidget);
    await tester.tap(find.text('全部已开通模型'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存模型选择'));
    await tester.pumpAndSettle();

    expect(controller.savedModels, [<String>[]]);
  });

  testWidgets('shows model-save failures and prevents duplicate saves', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(800, 2400));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final saveCompleter = Completer<void>();
    final controller = _FakeAuthController(
      AuthState(isLoading: false, account: _account(), aiAccount: _aiAccount()),
    )..modelSaveCompleter = saveCompleter;
    await tester.pumpWidget(_wrap(controller));
    await tester.pumpAndSettle();

    await tester.tap(find.text('选择可用模型'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存模型选择'));
    await tester.pump();

    expect(find.text('正在保存…'), findsOneWidget);
    expect(controller.modelSaveCalls, 1);

    saveCompleter.completeError(
      const AppException(
        kind: AppErrorKind.network,
        message: '保存没有完成，请检查网络后重试',
        retryable: true,
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('保存没有完成，请检查网络后重试'), findsOneWidget);
    expect(controller.modelSaveCalls, 1);
  });

  testWidgets('confirms before signing out', (tester) async {
    await tester.binding.setSurfaceSize(const Size(800, 2400));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = _FakeAuthController(
      AuthState(isLoading: false, account: _account()),
    );
    await tester.pumpWidget(_wrap(controller));
    await tester.pumpAndSettle();

    final logoutLabel = find.text('退出登录');
    await tester.ensureVisible(logoutLabel);
    await tester.pumpAndSettle();
    await tester.tap(logoutLabel);
    await tester.pumpAndSettle();

    expect(find.text('退出后需要重新输入手机号或邮箱登录，设备绑定不会解除。'), findsOneWidget);
    await tester.tap(find.widgetWithText(FilledButton, '退出登录'));
    await tester.pumpAndSettle();

    expect(controller.logoutCalls, 1);
  });
}

Widget _wrap(AuthController controller) {
  return ProviderScope(
    overrides: [authControllerProvider.overrideWith(() => controller)],
    child: const MaterialApp(home: ProfilePage()),
  );
}

ParentAccount _account({
  String displayName = '小萌',
  String guardianFamilyName = '李',
  String childNickname = '朵朵',
  String childBirthday = '2021-06-01',
}) {
  return ParentAccount(
    id: 'parent-1',
    email: 'guardian@example.com',
    phone: '13800138000',
    displayName: displayName,
    guardianFamilyName: guardianFamilyName,
    childNickname: childNickname,
    childBirthday: childBirthday,
    role: 'owner',
    status: 'active',
  );
}

AiAccount _aiAccount({bool providerReady = true}) {
  return AiAccount(
    status: 'active',
    balanceUsd: 12.5,
    concurrencyLimit: 2,
    availableModels: const ['sprout-mini'],
    selectedModels: const ['sprout-mini'],
    allowedModels: const ['sprout-mini'],
    providerReady: providerReady,
  );
}

class _FakeAuthController extends AuthController {
  _FakeAuthController(this._initial);

  final AuthState _initial;
  Object? error;
  Object? retryError;
  Object? modelSaveError;
  Completer<void>? modelSaveCompleter;
  int retryAiCalls = 0;
  int logoutCalls = 0;
  int modelSaveCalls = 0;
  final List<List<String>> savedModels = [];

  @override
  Future<AuthState> build() async {
    if (error != null) {
      throw error!;
    }
    return _initial;
  }

  @override
  Future<void> retryAIService() async {
    retryAiCalls += 1;
    if (retryError != null) {
      throw retryError!;
    }
  }

  @override
  Future<void> updateSelectedModels(List<String> selectedModels) async {
    modelSaveCalls += 1;
    savedModels.add(List<String>.of(selectedModels));
    if (modelSaveCompleter != null) {
      await modelSaveCompleter!.future;
    }
    if (modelSaveError != null) {
      throw modelSaveError!;
    }
  }

  @override
  Future<void> logout() async {
    logoutCalls += 1;
  }
}
