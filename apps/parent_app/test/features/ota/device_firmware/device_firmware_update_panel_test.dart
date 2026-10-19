import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/ota/device_firmware/application/device_firmware_controller.dart';
import 'package:parent_app/features/ota/device_firmware/data/device_firmware_api.dart';
import 'package:parent_app/features/ota/device_firmware/domain/device_firmware_update.dart';
import 'package:parent_app/features/ota/device_firmware/presentation/device_firmware_update_panel.dart';

void main() {
  testWidgets('shows firmware details and requires confirmation to install', (
    tester,
  ) async {
    final api = _FakeDeviceFirmwareApi(_availableUpdate());
    await tester.pumpWidget(_app(api, isDeviceOnline: true));
    await tester.pumpAndSettle();

    expect(find.text('初芽 的固件'), findsOneWidget);
    expect(find.text('新版本 0.10.0'), findsOneWidget);
    expect(find.text('提升设备更新稳定性。'), findsOneWidget);
    expect(find.textContaining('当前任务是常规升级'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, '开始升级设备固件'), findsOneWidget);

    await tester.tap(find.widgetWithText(FilledButton, '开始升级设备固件'));
    await tester.pumpAndSettle();

    expect(find.text('暂不更新'), findsOneWidget);
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(api.installCalls, 0);

    await tester.tap(find.text('开始更新'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));

    expect(api.installCalls, 1);
  });

  testWidgets('disables immediate update while the device is offline', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(_FakeDeviceFirmwareApi(_availableUpdate()), isDeviceOnline: false),
    );
    await tester.pumpAndSettle();

    final button = tester.widget<FilledButton>(
      find.widgetWithText(FilledButton, '设备离线，暂时不能升级'),
    );
    expect(button.onPressed, isNull);
    expect(find.text('设备当前离线。请让设备保持开机并连接网络后再升级。'), findsOneWidget);
  });

  testWidgets('does not offer a second action while installation is running', (
    tester,
  ) async {
    final api = _FakeDeviceFirmwareApi(
      _availableUpdate().copyWith(
        status: DeviceFirmwareUpdateStatus.downloading,
        progressPercent: 42,
      ),
    );
    await tester.pumpWidget(_app(api, isDeviceOnline: true));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));

    expect(find.text('正在下载固件'), findsWidgets);
    expect(find.text('42%'), findsOneWidget);
    expect(find.text('开始升级设备固件'), findsNothing);
    expect(find.text('重试设备固件更新'), findsNothing);
  });

  testWidgets('shows retry for a failed firmware deployment', (tester) async {
    final api = _FakeDeviceFirmwareApi(
      _availableUpdate().copyWith(
        status: DeviceFirmwareUpdateStatus.failed,
        errorCode: 'download_failed',
      ),
    );
    await tester.pumpWidget(_app(api, isDeviceOnline: true));
    await tester.pumpAndSettle();

    expect(find.text('固件下载没有完成，请检查网络后重试'), findsOneWidget);
    expect(find.text('重试设备固件更新'), findsOneWidget);
  });

  testWidgets('shows an authorized rollback as a distinct deployment', (
    tester,
  ) async {
    final update = _availableUpdate().copyWith(
      status: DeviceFirmwareUpdateStatus.rollbackAvailable,
      deploymentId: 'ota_deployment_rollback',
      deploymentKind: DeviceFirmwareDeploymentKind.rollback,
      rollbackReleaseId: 'ota_release_001',
      rollbackOfDeploymentId: 'ota_deployment_previous',
    );
    await tester.pumpWidget(
      _app(_FakeDeviceFirmwareApi(update), isDeviceOnline: true),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('当前任务是授权回退'), findsOneWidget);
    expect(find.text('回退到上一个版本'), findsOneWidget);
    expect(find.text('开始升级设备固件'), findsNothing);
  });

  testWidgets('explains why rollback is unavailable', (tester) async {
    final update = _availableUpdate().copyWith(
      status: DeviceFirmwareUpdateStatus.upToDate,
    );
    await tester.pumpWidget(
      _app(_FakeDeviceFirmwareApi(update), isDeviceOnline: true),
    );
    await tester.pumpAndSettle();

    expect(find.text('当前没有可回退的已授权版本'), findsOneWidget);
  });
}

Widget _app(DeviceFirmwareApi api, {required bool isDeviceOnline}) {
  return ProviderScope(
    overrides: [deviceFirmwareApiProvider.overrideWithValue(api)],
    child: MaterialApp(
      home: Scaffold(
        body: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: DeviceFirmwareUpdatePanel(
            deviceId: 'device_demo_001',
            deviceName: '初芽',
            isDeviceOnline: isDeviceOnline,
          ),
        ),
      ),
    ),
  );
}

DeviceFirmwareUpdate _availableUpdate() {
  return DeviceFirmwareUpdate(
    deviceId: 'device_demo_001',
    deploymentId: 'ota_deployment_001',
    deploymentKind: DeviceFirmwareDeploymentKind.install,
    status: DeviceFirmwareUpdateStatus.available,
    currentVersion: '0.9.0',
    targetVersion: '0.10.0',
    updateAvailable: true,
    progressPercent: 0,
    checkedAt: DateTime.parse('2026-10-09T00:00:00Z'),
    release: DeviceFirmwareRelease(
      releaseId: 'ota_release_001',
      firmwareVersion: '0.10.0',
      hardwareRevision: 's3_n16r8',
      channel: 'stable',
      status: 'published',
      artifactKey: '0.10.0/stable/esp32_s3/firmware/sprout.bin',
      artifactUrl: 'https://api.clarkhub.cn/releases/0.10.0/sprout.bin',
      sha256:
          'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      sizeBytes: 4194304,
      signatureKeyId: 'ota_signing_001',
      signature: 'MEUCIQDExampleSignatureForContractValidationOnly',
      rollbackAllowed: true,
      minSourceVersion: '0.9.0',
      publishedAt: DateTime(2026, 10, 9),
      canaryPercent: 0,
      targetType: 'all',
      targetId: '',
      releaseNotes: '提升设备更新稳定性。',
    ),
  );
}

class _FakeDeviceFirmwareApi implements DeviceFirmwareApi {
  _FakeDeviceFirmwareApi(this.update);

  DeviceFirmwareUpdate update;
  int installCalls = 0;

  @override
  Future<DeviceFirmwareUpdate> fetch(String deviceId) async => update;

  @override
  Future<DeviceFirmwareUpdate> install(
    String deviceId, {
    String? deploymentId,
  }) async {
    installCalls++;
    update = update.copyWith(
      status: DeviceFirmwareUpdateStatus.downloading,
      progressPercent: 1,
    );
    return update;
  }

  @override
  Future<DeviceFirmwareUpdate> retry(String deviceId) async {
    update = update.copyWith(
      status: DeviceFirmwareUpdateStatus.queued,
      progressPercent: 0,
    );
    return update;
  }

  @override
  Future<DeviceFirmwareUpdate> rollback(String deviceId) async {
    update = update.copyWith(status: DeviceFirmwareUpdateStatus.rolledBack);
    return update;
  }

  @override
  Future<DeviceFirmwareUpdate> status(String deviceId) async => update;
}
