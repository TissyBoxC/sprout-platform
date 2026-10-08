import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/settings/application/privacy_controller.dart';
import 'package:parent_app/features/settings/application/privacy_export_store.dart';
import 'package:parent_app/features/settings/domain/privacy_state.dart';
import 'package:parent_app/features/settings/presentation/privacy_page.dart';

void main() {
  testWidgets('shows consent, export, retention, and deletion state', (
    tester,
  ) async {
    await tester.pumpWidget(_privacyApp(const AsyncData(_activeStatus)));
    await tester.pumpAndSettle();

    expect(find.text('家长授权'), findsOneWidget);
    expect(find.text('授权有效'), findsOneWidget);
    expect(find.text('获取数据副本'), findsOneWidget);
    expect(find.text('数据保留说明'), findsOneWidget);
    expect(find.text('保留 30 天'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('申请注销账号'), 300);
    await tester.pumpAndSettle();
    expect(find.text('申请注销账号'), findsOneWidget);
  });

  testWidgets('exports a data copy and shows the saved file', (tester) async {
    final controller = _FakePrivacyController(
      const AsyncData(_activeStatus),
      exportFile: PrivacyExportFile(
        filePath: '/tmp/sprout-privacy.json',
        exportedAt: DateTime(2026, 10, 8, 21),
        sizeBytes: 2048,
      ),
    );
    await tester.pumpWidget(_privacyAppWithController(controller));
    await tester.pumpAndSettle();

    await tester.tap(find.text('生成数据副本'));
    await tester.pumpAndSettle();

    expect(controller.exportCount, 1);
    expect(find.text('最近导出的副本'), findsOneWidget);
    expect(find.text('sprout-privacy.json'), findsOneWidget);
    expect(find.textContaining('2.0 千字节'), findsOneWidget);
  });

  testWidgets('opens the generated data copy', (tester) async {
    final launcher = _FakeExportLauncher();
    await tester.pumpWidget(
      _privacyAppWithController(
        _FakePrivacyController(
          const AsyncData(_activeStatus),
          exportFile: _exportFile,
        ),
        exportLauncher: launcher,
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('生成数据副本'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('打开副本'));
    await tester.pumpAndSettle();

    expect(launcher.openCount, 1);
  });

  testWidgets('shares the generated data copy through the system sheet', (
    tester,
  ) async {
    final launcher = _FakeExportLauncher();
    await tester.pumpWidget(
      _privacyAppWithController(
        _FakePrivacyController(
          const AsyncData(_activeStatus),
          exportFile: _exportFile,
        ),
        exportLauncher: launcher,
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('生成数据副本'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存或分享'));
    await tester.pumpAndSettle();

    expect(launcher.shareCount, 1);
    expect(find.text('数据副本已交给所选应用'), findsOneWidget);
  });

  testWidgets('keeps a cancelled share silent and retryable', (tester) async {
    final launcher = _FakeExportLauncher(
      shareOutcome: const PrivacyExportShareOutcome(
        PrivacyExportShareResult.dismissed,
      ),
    );
    await tester.pumpWidget(
      _privacyAppWithController(
        _FakePrivacyController(
          const AsyncData(_activeStatus),
          exportFile: _exportFile,
        ),
        exportLauncher: launcher,
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('生成数据副本'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存或分享'));
    await tester.pumpAndSettle();

    expect(launcher.shareCount, 1);
    expect(find.text('数据副本已交给所选应用'), findsNothing);
    expect(find.text('保存或分享'), findsOneWidget);
  });

  testWidgets('reports an unavailable share and allows another try', (
    tester,
  ) async {
    final launcher = _FakeExportLauncher(
      shareOutcome: const PrivacyExportShareOutcome(
        PrivacyExportShareResult.unavailable,
      ),
    );
    await tester.pumpWidget(
      _privacyAppWithController(
        _FakePrivacyController(
          const AsyncData(_activeStatus),
          exportFile: _exportFile,
        ),
        exportLauncher: launcher,
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('生成数据副本'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存或分享'));
    await tester.pumpAndSettle();

    expect(find.text('暂时无法分享，文件已经保存在本机，可以稍后重试'), findsOneWidget);
    expect(find.text('保存或分享'), findsOneWidget);
  });

  testWidgets('requires typed confirmation before requesting deletion', (
    tester,
  ) async {
    final controller = _FakePrivacyController(const AsyncData(_activeStatus));
    await tester.pumpWidget(_privacyAppWithController(controller));
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(find.text('申请注销账号'), 300);
    await tester.pumpAndSettle();
    await tester.tap(find.text('申请注销账号'));
    await tester.pumpAndSettle();

    expect(find.text('申请注销账号'), findsWidgets);
    final requestButton = find.widgetWithText(FilledButton, '申请注销');
    expect(tester.widget<FilledButton>(requestButton).onPressed, isNull);

    await tester.enterText(find.byType(TextField).last, '注销');
    await tester.pump();
    await tester.tap(requestButton);
    await tester.pumpAndSettle();

    expect(controller.deletionRequestCount, 1);
    expect(find.textContaining('等待期内可以取消'), findsOneWidget);
  });

  testWidgets('cancels a pending deletion after confirmation', (tester) async {
    final controller = _FakePrivacyController(
      AsyncData(_pendingDeletionStatus),
    );
    await tester.pumpWidget(_privacyAppWithController(controller));
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(find.text('取消注销'), 300);
    await tester.pumpAndSettle();
    await tester.tap(find.text('取消注销'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, '取消注销'));
    await tester.pumpAndSettle();

    expect(controller.cancelDeletionCount, 1);
    expect(find.text('注销申请已取消'), findsOneWidget);
  });

  testWidgets('withdrawing consent clears the session and returns to login', (
    tester,
  ) async {
    final controller = _FakePrivacyController(const AsyncData(_activeStatus));
    var sessionClosed = false;
    await tester.pumpWidget(
      _privacyAppWithController(
        controller,
        closeSession: () async => sessionClosed = true,
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('撤回授权并结束会话'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).last, '撤回');
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, '撤回授权'));
    await tester.pumpAndSettle();

    expect(controller.withdrawCount, 1);
    expect(sessionClosed, isTrue);
    expect(find.text('登录占位'), findsOneWidget);
  });

  testWidgets('reopens the platform reconsent route and refreshes status', (
    tester,
  ) async {
    final controller = _FakePrivacyController(
      AsyncData(_withdrawnStatus),
      restoredStatus: _activeStatus,
    );
    await tester.pumpWidget(
      _privacyAppWithController(
        controller,
        routes: [
          GoRoute(
            path: '/authorization/restore',
            builder: (_, _) =>
                const Scaffold(body: Center(child: Text('重新授权占位'))),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('重新确认授权'));
    await tester.pumpAndSettle();
    expect(find.text('重新授权占位'), findsOneWidget);

    Navigator.of(tester.element(find.text('重新授权占位'))).pop();
    await tester.pumpAndSettle();

    expect(controller.refreshCount, 1);
    expect(find.text('授权有效'), findsOneWidget);
    expect(find.text('家长授权已恢复'), findsOneWidget);
  });

  testWidgets('does not invent a reconsent route when the platform omits it', (
    tester,
  ) async {
    final controller = _FakePrivacyController(
      const AsyncData(_withdrawnWithoutRouteStatus),
    );
    await tester.pumpWidget(_privacyAppWithController(controller));
    await tester.pumpAndSettle();

    final button = find.widgetWithText(FilledButton, '重新确认授权');
    expect(button, findsOneWidget);
    expect(tester.widget<FilledButton>(button).onPressed, isNull);
    expect(find.textContaining('请退出登录后重新进入授权流程'), findsOneWidget);
  });

  testWidgets('keeps the page usable after a retryable action error', (
    tester,
  ) async {
    final controller = _FakePrivacyController(
      const AsyncData(_activeStatus),
      exportError: const AppException(
        kind: AppErrorKind.network,
        message: '网络连接不稳定，请检查后重试',
        retryable: true,
      ),
    );
    await tester.pumpWidget(_privacyAppWithController(controller));
    await tester.pumpAndSettle();

    await tester.tap(find.text('生成数据副本'));
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(find.text('网络连接不稳定，请检查后重试'), 300);
    await tester.pumpAndSettle();
    expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('生成数据副本'), -300);
    await tester.pumpAndSettle();
    expect(find.text('生成数据副本'), findsOneWidget);
  });

  testWidgets('shows a safe message when the local file cannot be opened', (
    tester,
  ) async {
    final launcher = _FakeExportLauncher(
      openResult: PrivacyExportOpenResult.unavailable,
    );
    await tester.pumpWidget(
      _privacyAppWithController(
        _FakePrivacyController(
          const AsyncData(_activeStatus),
          exportFile: _exportFile,
        ),
        exportLauncher: launcher,
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('生成数据副本'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('打开副本'));
    await tester.pumpAndSettle();

    expect(find.textContaining('请使用“保存或分享”'), findsOneWidget);
  });

  testWidgets('shows a safe error state without raw exception details', (
    tester,
  ) async {
    await tester.pumpWidget(
      _privacyApp(
        const AsyncError(
          AppException(
            kind: AppErrorKind.serviceUnavailable,
            message: '服务暂时不可用，请稍后重试',
            retryable: true,
          ),
          StackTrace.empty,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('服务暂时不可用，请稍后重试'), findsOneWidget);
    expect(find.textContaining('Exception'), findsNothing);
  });
}

const _activeStatus = PrivacyStatus(
  consent: GuardianConsent(
    version: '2026-01',
    consentedAt: null,
    withdrawnAt: null,
    status: GuardianConsentStatus.active,
  ),
  deletion: PrivacyDeletion.none(),
  exportAvailable: true,
  policy: PrivacyRetentionPolicy(
    audioRetentionDays: 0,
    imageRetentionDays: 0,
    conversationRetentionDays: 30,
  ),
);

final _pendingDeletionStatus = PrivacyStatus(
  consent: _activeStatus.consent,
  deletion: PrivacyDeletion(
    status: PrivacyDeletionStatus.pending,
    requestedAt: DateTime(2026, 10, 8),
    scheduledFor: DateTime(2026, 10, 15),
    completedAt: null,
    cancellable: true,
  ),
  exportAvailable: true,
  policy: _activeStatus.policy,
);

final _withdrawnStatus = PrivacyStatus(
  consent: const GuardianConsent(
    version: '2026-01',
    consentedAt: null,
    withdrawnAt: null,
    status: GuardianConsentStatus.withdrawn,
    reconsentPath: '/authorization/restore',
  ),
  deletion: const PrivacyDeletion.none(),
  exportAvailable: true,
  policy: _activeStatus.policy,
);

const _withdrawnWithoutRouteStatus = PrivacyStatus(
  consent: GuardianConsent(
    version: '2026-01',
    consentedAt: null,
    withdrawnAt: null,
    status: GuardianConsentStatus.withdrawn,
  ),
  deletion: PrivacyDeletion.none(),
  exportAvailable: true,
  policy: PrivacyRetentionPolicy(
    audioRetentionDays: 0,
    imageRetentionDays: 0,
    conversationRetentionDays: 30,
  ),
);

final _exportFile = PrivacyExportFile(
  filePath: '/tmp/sprout-privacy.json',
  exportedAt: DateTime(2026, 10, 8, 21),
  sizeBytes: 2048,
);

Widget _privacyApp(AsyncValue<PrivacyStatus> state) {
  return _privacyAppWithController(_FakePrivacyController(state));
}

Widget _privacyAppWithController(
  PrivacyController controller, {
  Future<void> Function()? closeSession,
  PrivacyExportOpener? exportLauncher,
  List<RouteBase> routes = const [],
}) {
  final router = GoRouter(
    initialLocation: '/me/privacy',
    routes: [
      GoRoute(path: '/me/privacy', builder: (_, _) => const PrivacyPage()),
      GoRoute(
        path: '/login',
        builder: (_, _) => const Scaffold(body: Center(child: Text('登录占位'))),
      ),
      GoRoute(
        path: '/me',
        builder: (_, _) => const Scaffold(body: Center(child: Text('我的占位'))),
      ),
      ...routes,
    ],
  );
  return ProviderScope(
    overrides: [
      privacyControllerProvider.overrideWith(() => controller),
      privacySessionCloserProvider.overrideWithValue(
        closeSession ?? () async {},
      ),
      privacyExportOpenerProvider.overrideWithValue(
        exportLauncher ?? _FakeExportLauncher(),
      ),
    ],
    child: MaterialApp.router(routerConfig: router),
  );
}

class _FakePrivacyController extends PrivacyController {
  _FakePrivacyController(
    this.initialState, {
    this.exportFile,
    this.exportError,
    this.restoredStatus,
  });

  final AsyncValue<PrivacyStatus> initialState;
  final PrivacyExportFile? exportFile;
  final Object? exportError;
  final PrivacyStatus? restoredStatus;
  int exportCount = 0;
  int refreshCount = 0;
  int deletionRequestCount = 0;
  int cancelDeletionCount = 0;
  int withdrawCount = 0;

  @override
  Future<void> refresh() async {
    refreshCount += 1;
    final next = restoredStatus;
    if (next != null) {
      state = AsyncData(next);
    }
  }

  @override
  Future<PrivacyStatus> build() async {
    return initialState.when(
      data: (value) => value,
      error: (error, stackTrace) => throw error,
      loading: () => _activeStatus,
    );
  }

  @override
  Future<PrivacyExportFile> exportData() async {
    exportCount += 1;
    final error = exportError;
    if (error != null) {
      throw error;
    }
    return exportFile ??
        PrivacyExportFile(
          filePath: '/tmp/sprout-privacy.json',
          exportedAt: DateTime(2026, 10, 8, 21),
          sizeBytes: 128,
        );
  }

  @override
  Future<PrivacyDeletion> requestDeletion() async {
    deletionRequestCount += 1;
    state = AsyncData(_pendingDeletionStatus);
    return _pendingDeletionStatus.deletion;
  }

  @override
  Future<PrivacyDeletion> cancelDeletion() async {
    cancelDeletionCount += 1;
    state = const AsyncData(_activeStatus);
    return const PrivacyDeletion.none();
  }

  @override
  Future<GuardianConsent> withdrawConsent() async {
    withdrawCount += 1;
    return const GuardianConsent(
      version: '2026-01',
      consentedAt: null,
      withdrawnAt: null,
      status: GuardianConsentStatus.withdrawn,
    );
  }
}

class _FakeExportLauncher implements PrivacyExportOpener {
  _FakeExportLauncher({
    this.openResult = PrivacyExportOpenResult.opened,
    this.shareOutcome = const PrivacyExportShareOutcome(
      PrivacyExportShareResult.shared,
    ),
  });

  final PrivacyExportOpenResult openResult;
  final PrivacyExportShareOutcome shareOutcome;
  int openCount = 0;
  int shareCount = 0;

  @override
  Future<PrivacyExportOpenResult> open(PrivacyExportFile file) async {
    openCount += 1;
    return openResult;
  }

  @override
  Future<PrivacyExportShareOutcome> share(PrivacyExportFile file) async {
    shareCount += 1;
    return shareOutcome;
  }
}
