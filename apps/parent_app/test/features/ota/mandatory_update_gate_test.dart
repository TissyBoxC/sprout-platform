import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/auth/data/auth_api.dart';
import 'package:parent_app/features/ota/application/app_update_service.dart';
import 'package:parent_app/features/ota/presentation/mandatory_update_gate.dart';

void main() {
  testWidgets('mandatory client update blocks the application', (tester) async {
    final service = _FakeUpdateCoordinator();
    await tester.pumpWidget(
      _gateApp(
        service: service,
        updateLoader: (_, _) async => _clientUpdate(isMandatory: true),
        platform: const _AndroidUpdatePlatform(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('需要更新'), findsOneWidget);
    expect(find.text('立即更新'), findsOneWidget);
    // There is no dismissal affordance: no "later" and no close control.
    expect(find.text('稍后更新'), findsNothing);
    expect(find.text('返回'), findsNothing);
  });

  testWidgets('optional client update does not block the application', (
    tester,
  ) async {
    final service = _FakeUpdateCoordinator();
    await tester.pumpWidget(
      _gateApp(
        service: service,
        updateLoader: (_, _) async => _clientUpdate(isMandatory: false),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('需要更新'), findsNothing);
    expect(find.text('应用首页'), findsOneWidget);
  });

  testWidgets('mandatory resource update never blocks a client update loop', (
    tester,
  ) async {
    final service = _FakeUpdateCoordinator();
    await tester.pumpWidget(
      _gateApp(
        service: service,
        updateLoader: (_, _) async => const AppUpdateInfo(
          kind: 'resource',
          version: '1.0.1',
          downloadUrl: 'https://example.com/resource.zip',
          sha256:
              'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
              'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
          releaseNotes: '内容更新',
          isMandatory: true,
          minSupportedVersion: '1.0.0',
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('需要更新'), findsNothing);
    expect(find.text('应用首页'), findsOneWidget);
  });

  testWidgets('gate receives both installed versions for update discovery', (
    tester,
  ) async {
    final service = _FakeUpdateCoordinator(
      installed: const AppUpdateVersions(
        clientVersion: '1.0.0',
        resourceVersion: '1.0.1',
      ),
    );
    AppUpdateVersions? observed;
    await tester.pumpWidget(
      _gateApp(
        service: service,
        updateLoader: (_, installed) async {
          observed = installed;
          return reconcileInstalledUpdate(
            candidate: const AppUpdateInfo(
              kind: 'resource',
              version: '1.0.1',
              downloadUrl: 'https://example.com/resource.zip',
              sha256:
                  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
                  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
              releaseNotes: '内容更新',
              isMandatory: true,
              minSupportedVersion: '1.0.0',
            ),
            installed: installed,
          );
        },
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(observed?.clientVersion, '1.0.0');
    expect(observed?.resourceVersion, '1.0.1');
    expect(find.text('需要更新'), findsNothing);
    expect(find.text('应用首页'), findsOneWidget);
  });

  testWidgets('install runs without any dismissal control appearing', (
    tester,
  ) async {
    final service = _FakeUpdateCoordinator();
    await tester.pumpWidget(
      _gateApp(
        service: service,
        updateLoader: (_, _) async => _clientUpdate(isMandatory: true),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    await tester.tap(find.text('立即更新'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(service.didInstallClientUpdate, isTrue);
    expect(find.text('等待安装完成'), findsOneWidget);
    expect(find.text('稍后更新'), findsNothing);
  });

  testWidgets('a failed first check blocks entry and can be retried', (
    tester,
  ) async {
    final service = _FakeUpdateCoordinator();
    var attempts = 0;
    await tester.pumpWidget(
      _gateApp(
        service: service,
        updateLoader: (_, _) async {
          attempts += 1;
          if (attempts == 1) {
            throw StateError('update service offline');
          }
          return null;
        },
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('安全检查'), findsOneWidget);
    expect(find.text('重新检查'), findsOneWidget);
    expect(find.byType(ModalBarrier), findsWidgets);

    await tester.tap(find.text('重新检查'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(attempts, 2);
    expect(find.text('安全检查'), findsNothing);
    expect(find.byType(LinearProgressIndicator), findsNothing);
  });

  testWidgets('disabling the startup check skips the blocking overlay', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [mandatoryUpdateCheckProvider.overrideWithValue(false)],
        child: MaterialApp(
          home: MandatoryUpdateGate(
            updateService: _FakeUpdateCoordinator(),
            child: const Scaffold(body: Center(child: Text('应用首页'))),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('应用首页'), findsOneWidget);
    expect(find.text('安全检查'), findsNothing);
    expect(find.text('暂时无法确认当前版本是否可以安全使用。'), findsNothing);
  });

  testWidgets('an iOS client update does not expose an APK install flow', (
    tester,
  ) async {
    final service = _FakeUpdateCoordinator();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mandatoryUpdateCheckProvider.overrideWithValue(true),
          mandatoryUpdatePlatformProvider.overrideWithValue(
            const _TestUpdatePlatform(),
          ),
        ],
        child: MaterialApp(
          home: MandatoryUpdateGate(
            updateService: service,
            updateLoader: (_, _) async => _clientUpdate(isMandatory: true),
            child: const Scaffold(body: Center(child: Text('应用首页'))),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    await tester.tap(find.text('立即更新'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(service.didInstallClientUpdate, isFalse);
    expect(find.text('稍后去 App Store 更新'), findsOneWidget);
    expect(find.text('请在 App Store 更新到新版本后继续使用'), findsOneWidget);
  });
}

class _TestUpdatePlatform implements MandatoryUpdatePlatform {
  const _TestUpdatePlatform();

  @override
  bool get canInstallInApp => false;

  @override
  String get storeLabel => 'App Store';
}

class _AndroidUpdatePlatform implements MandatoryUpdatePlatform {
  const _AndroidUpdatePlatform();

  @override
  bool get canInstallInApp => true;

  @override
  String get storeLabel => 'Google Play';
}

Widget _gateApp({
  required AppUpdateCoordinator service,
  required Future<AppUpdateInfo?> Function(
    AppUpdateCoordinator service,
    AppUpdateVersions installed,
  )
  updateLoader,
  MandatoryUpdatePlatform platform = const _AndroidUpdatePlatform(),
}) {
  return ProviderScope(
    overrides: [
      mandatoryUpdateCheckProvider.overrideWithValue(true),
      mandatoryUpdatePlatformProvider.overrideWithValue(platform),
    ],
    child: MaterialApp(
      home: MandatoryUpdateGate(
        updateService: service,
        updateLoader: updateLoader,
        child: const Scaffold(body: Center(child: Text('应用首页'))),
      ),
    ),
  );
}

AppUpdateInfo _clientUpdate({required bool isMandatory}) {
  return AppUpdateInfo(
    kind: 'client',
    version: '1.0.1',
    downloadUrl: 'https://example.com/app.apk',
    sha256: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    releaseNotes: '重要修复',
    isMandatory: isMandatory,
    minSupportedVersion: '1.0.0',
  );
}

class _FakeUpdateCoordinator implements AppUpdateCoordinator {
  _FakeUpdateCoordinator({
    this.installed = const AppUpdateVersions(
      clientVersion: '1.0.0',
      resourceVersion: '',
    ),
  });

  final AppUpdateVersions installed;
  bool didInstallClientUpdate = false;

  @override
  Future<AppUpdateVersions> installedUpdateVersions() async => installed;

  @override
  Future<String> installedVersion() async => installed.clientVersion;

  @override
  String? updateProblem(AppUpdateInfo update) => null;

  @override
  Future<void> installClientUpdate({
    required AppUpdateInfo update,
    required void Function(AppUpdateProgress progress) onProgress,
    CancelToken? cancelToken,
  }) async {
    didInstallClientUpdate = true;
    onProgress(
      const AppUpdateProgress(stage: AppUpdateStage.installing, fraction: 1),
    );
  }

  @override
  Future<void> installResourceUpdate({
    required AppUpdateInfo update,
    required void Function(AppUpdateProgress progress) onProgress,
    CancelToken? cancelToken,
  }) async {}
}
