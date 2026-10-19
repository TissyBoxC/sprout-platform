import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/ota/device_firmware/application/device_firmware_controller.dart';
import 'package:parent_app/features/ota/device_firmware/data/device_firmware_api.dart';
import 'package:parent_app/features/ota/device_firmware/domain/device_firmware_update.dart';

void main() {
  test('loads the latest state and does not poll while up to date', () async {
    final api = _FakeDeviceFirmwareApi(fetched: _update(status: 'up_to_date'));
    final container = _container(api);
    addTearDown(container.dispose);

    final state = await container.read(
      deviceFirmwareControllerProvider('device_demo_001').future,
    );

    expect(state.update?.status, DeviceFirmwareUpdateStatus.upToDate);
    expect(api.statusCalls, 0);
  });

  test('polls an in-progress deployment with backoff', () async {
    fakeAsync((async) {
      final api = _FakeDeviceFirmwareApi(
        fetched: _update(status: 'downloading', progress: 20),
        statuses: [
          _update(status: 'validating', progress: 50),
          _update(status: 'succeeded', progress: 100),
        ],
      );
      final container = ProviderContainer(
        overrides: [
          deviceFirmwareApiProvider.overrideWithValue(api),
          deviceFirmwarePollBackoffProvider.overrideWithValue(const [
            Duration(milliseconds: 10),
          ]),
        ],
      );
      final provider = deviceFirmwareControllerProvider('device_demo_001');
      final subscription = container.listen(
        provider,
        (_, _) {},
        fireImmediately: true,
      );
      container.read(provider.future).then((_) {});
      async.flushMicrotasks();

      async.elapse(const Duration(milliseconds: 10));
      async.flushMicrotasks();
      expect(api.statusCalls, 1);

      async.elapse(const Duration(milliseconds: 10));
      async.flushMicrotasks();
      expect(
        container.read(provider).value?.update?.status,
        DeviceFirmwareUpdateStatus.succeeded,
      );
      expect(api.statusCalls, 2);

      subscription.close();
      container.dispose();
    });
  });

  test('startInstall refuses duplicate actions while one is running', () async {
    final api = _FakeDeviceFirmwareApi(
      fetched: _update(status: 'available'),
      installCompleter: Completer<DeviceFirmwareUpdate>(),
    );
    final container = _container(api);
    addTearDown(container.dispose);
    await container.read(
      deviceFirmwareControllerProvider('device_demo_001').future,
    );
    final notifier = container.read(
      deviceFirmwareControllerProvider('device_demo_001').notifier,
    );

    final first = notifier.startInstall();
    final second = notifier.startInstall();

    expect(await second, isFalse);
    api.installCompleter!.complete(_update(status: 'downloading', progress: 1));
    expect(await first, isTrue);
    expect(api.installCalls, 1);
  });

  test('maps backend failures to an actionable message', () async {
    final api = _FakeDeviceFirmwareApi(
      fetched: _update(status: 'available'),
      installError: const AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '设备当前不在线，请让设备保持开机并连接网络',
        retryable: true,
      ),
    );
    final container = _container(api);
    addTearDown(container.dispose);
    await container.read(
      deviceFirmwareControllerProvider('device_demo_001').future,
    );
    final notifier = container.read(
      deviceFirmwareControllerProvider('device_demo_001').notifier,
    );

    expect(await notifier.startInstall(), isFalse);
    final state = notifier.currentState;
    expect(state?.actionError, '设备当前不在线，请让设备保持开机并连接网络');
  });

  test('refresh keeps the previous snapshot when the network fails', () async {
    final api = _FakeDeviceFirmwareApi(fetched: _update(status: 'available'));
    final container = _container(api);
    addTearDown(container.dispose);
    await container.read(
      deviceFirmwareControllerProvider('device_demo_001').future,
    );
    api.fetchError = const AppException(
      kind: AppErrorKind.network,
      message: '网络连接不稳定，请检查后重试',
      retryable: true,
    );

    final notifier = container.read(
      deviceFirmwareControllerProvider('device_demo_001').notifier,
    );
    await notifier.refresh();

    final state = notifier.currentState;
    expect(state?.update?.status, DeviceFirmwareUpdateStatus.available);
    expect(state?.pollingError, '网络连接不稳定，请检查后重试');
  });

  test('disposing the family provider cancels pending polling', () async {
    fakeAsync((async) {
      final api = _FakeDeviceFirmwareApi(
        fetched: _update(status: 'downloading', progress: 10),
      );
      final container = ProviderContainer(
        overrides: [
          deviceFirmwareApiProvider.overrideWithValue(api),
          deviceFirmwarePollBackoffProvider.overrideWithValue(const [
            Duration(milliseconds: 10),
          ]),
        ],
      );
      final provider = deviceFirmwareControllerProvider('device_demo_001');
      final subscription = container.listen(
        provider,
        (_, _) {},
        fireImmediately: true,
      );
      container.read(provider.future).then((_) {});
      async.flushMicrotasks();

      subscription.close();
      async.elapse(const Duration(milliseconds: 40));
      async.flushMicrotasks();

      expect(api.statusCalls, 0);
      container.dispose();
    });
  });
}

ProviderContainer _container(DeviceFirmwareApi api) {
  return ProviderContainer(
    overrides: [deviceFirmwareApiProvider.overrideWithValue(api)],
  );
}

DeviceFirmwareUpdate _update({required String status, double progress = 0}) {
  return DeviceFirmwareUpdate(
    deviceId: 'device_demo_001',
    deploymentId: 'ota_deployment_001',
    deploymentKind:
        DeviceFirmwareUpdateStatus.fromWireValue(status) ==
            DeviceFirmwareUpdateStatus.rollbackAvailable
        ? DeviceFirmwareDeploymentKind.rollback
        : DeviceFirmwareDeploymentKind.install,
    status: DeviceFirmwareUpdateStatus.fromWireValue(status),
    currentVersion: '0.9.0',
    targetVersion: '0.10.0',
    updateAvailable:
        DeviceFirmwareUpdateStatus.fromWireValue(status) !=
            DeviceFirmwareUpdateStatus.upToDate &&
        DeviceFirmwareUpdateStatus.fromWireValue(status) !=
            DeviceFirmwareUpdateStatus.rolledBack,
    rollbackReleaseId:
        DeviceFirmwareUpdateStatus.fromWireValue(status) ==
            DeviceFirmwareUpdateStatus.rollbackAvailable
        ? 'ota_release_001'
        : null,
    rollbackOfDeploymentId:
        DeviceFirmwareUpdateStatus.fromWireValue(status) ==
            DeviceFirmwareUpdateStatus.rollbackAvailable
        ? 'ota_deployment_previous'
        : null,
    progressPercent: progress,
    checkedAt: DateTime.parse('2026-10-09T00:00:00Z'),
    release:
        DeviceFirmwareUpdateStatus.fromWireValue(status) !=
            DeviceFirmwareUpdateStatus.upToDate
        ? DeviceFirmwareRelease(
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
          )
        : null,
  );
}

class _FakeDeviceFirmwareApi implements DeviceFirmwareApi {
  _FakeDeviceFirmwareApi({
    required this.fetched,
    this.statuses = const [],
    this.installCompleter,
    this.installError,
  });

  final DeviceFirmwareUpdate fetched;
  final List<DeviceFirmwareUpdate> statuses;
  final Completer<DeviceFirmwareUpdate>? installCompleter;
  final Object? installError;

  Object? fetchError;
  int statusCalls = 0;
  int installCalls = 0;

  @override
  Future<DeviceFirmwareUpdate> fetch(String deviceId) async {
    if (fetchError != null) {
      throw fetchError!;
    }
    return fetched;
  }

  @override
  Future<DeviceFirmwareUpdate> install(
    String deviceId, {
    String? deploymentId,
  }) async {
    installCalls++;
    if (installError != null) {
      throw installError!;
    }
    return installCompleter?.future ?? _update(status: 'queued');
  }

  @override
  Future<DeviceFirmwareUpdate> retry(String deviceId) async {
    return _update(status: 'queued');
  }

  @override
  Future<DeviceFirmwareUpdate> rollback(String deviceId) async {
    return _update(status: 'rolled_back');
  }

  @override
  Future<DeviceFirmwareUpdate> status(String deviceId) async {
    final index = statusCalls;
    statusCalls++;
    if (index >= statuses.length) {
      return statuses.isEmpty ? fetched : statuses.last;
    }
    return statuses[index];
  }
}
