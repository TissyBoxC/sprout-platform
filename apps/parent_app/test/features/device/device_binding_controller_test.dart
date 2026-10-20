import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/device/application/device_binding_controller.dart';
import 'package:parent_app/features/device/data/device_binding_api.dart';
import 'package:parent_app/providers.dart';

void main() {
  test('a failed delete restores only that device', () async {
    final api = _ConcurrentDeleteApi();
    final container = ProviderContainer(
      overrides: [deviceBindingApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(deviceBindingControllerProvider.future);
    final controller = container.read(deviceBindingControllerProvider.notifier);

    api.hold('device-a');
    final aStarted = api.waitForRemoval('device-a');
    final deletingA = controller.remove('device-a');
    await aStarted;

    api.hold('device-b');
    final bStarted = api.waitForRemoval('device-b');
    final deletingB = controller.remove('device-b');
    await bStarted;

    api.fail('device-a');
    api.complete('device-b');
    await Future.wait<Object?>([
      deletingA.catchError((Object error) => error),
      deletingB,
    ]);

    final state = container.read(deviceBindingControllerProvider).value!;
    expect(state.bindings.map((device) => device.deviceId), ['device-a']);
    expect(state.errorMessage, isNull);
  });

  test(
    'a second delete for the same device is ignored while in flight',
    () async {
      final api = _ConcurrentDeleteApi();
      final container = ProviderContainer(
        overrides: [deviceBindingApiProvider.overrideWithValue(api)],
      );
      addTearDown(container.dispose);

      await container.read(deviceBindingControllerProvider.future);
      final controller = container.read(
        deviceBindingControllerProvider.notifier,
      );

      api.hold('device-a');
      final started = api.waitForRemoval('device-a');
      final first = controller.remove('device-a');
      await started;
      await controller.remove('device-a');
      api.complete('device-a');
      await first;

      expect(api.removalCount('device-a'), 1);
    },
  );

  test('refresh failure keeps the last known devices visible', () async {
    final api = _RefreshFailureApi();
    final container = ProviderContainer(
      overrides: [deviceBindingApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(deviceBindingControllerProvider.future);
    api.failNextList = true;
    await container.read(deviceBindingControllerProvider.notifier).refresh();

    final state = container.read(deviceBindingControllerProvider).value;
    expect(state?.bindings.map((device) => device.deviceId), ['device-a']);
    expect(state?.errorMessage, '网络连接不稳定，请检查后重试');
  });

  test('a viewer cannot remove a device returned as read-only', () async {
    final api = _RefreshFailureApi(canManage: false);
    final container = ProviderContainer(
      overrides: [deviceBindingApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(deviceBindingControllerProvider.future);
    await container
        .read(deviceBindingControllerProvider.notifier)
        .remove('device-a');

    expect(api.removalCalls, 0);
    expect(
      container.read(deviceBindingControllerProvider).value?.errorMessage,
      '当前角色没有解除设备绑定的权限',
    );
  });
}

class _RefreshFailureApi implements DeviceBindingApi {
  _RefreshFailureApi({this.canManage = true});

  final bool canManage;
  bool failNextList = false;
  int removalCalls = 0;

  @override
  Future<List<BoundDevice>> list() async {
    if (failNextList) {
      failNextList = false;
      throw const AppException(
        kind: AppErrorKind.network,
        message: '网络连接不稳定，请检查后重试',
        retryable: true,
      );
    }
    return [
      BoundDevice(
        deviceId: 'device-a',
        deviceName: '初芽',
        hardwareModel: 'sprout_initial',
        firmwareVersion: '1.0.0',
        capabilities: const [],
        lifecycleStatus: 'active',
        boundAt: DateTime.utc(2026, 10, 1),
        runtime: null,
        canManage: canManage,
      ),
    ];
  }

  @override
  Future<void> remove(String deviceId) async {
    removalCalls += 1;
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

class _ConcurrentDeleteApi implements DeviceBindingApi {
  final Map<String, Completer<void>> _gates = {};
  final Map<String, int> _removalCounts = {};
  final Set<String> _started = {};
  final Map<String, Completer<void>> _waiters = {};

  Future<void> waitForRemoval(String deviceId) async {
    if (_started.contains(deviceId)) {
      _started.remove(deviceId);
      return;
    }
    final waiter = Completer<void>();
    _waiters[deviceId] = waiter;
    await waiter.future;
  }

  int removalCount(String deviceId) => _removalCounts[deviceId] ?? 0;

  void hold(String deviceId) {
    _gates[deviceId] = Completer<void>();
  }

  void complete(String deviceId) {
    final gate = _gates.remove(deviceId);
    if (gate != null && !gate.isCompleted) {
      gate.complete();
    }
  }

  void fail(String deviceId) {
    final gate = _gates.remove(deviceId);
    if (gate != null && !gate.isCompleted) {
      gate.completeError(StateError('network down'));
    }
  }

  @override
  Future<List<BoundDevice>> list() async {
    return [
      if (removalCount('device-a') == 0)
        _device('device-a', DateTime.utc(2026, 10, 1)),
      if (removalCount('device-b') == 0)
        _device('device-b', DateTime.utc(2026, 10, 2)),
    ];
  }

  @override
  Future<void> remove(String deviceId) async {
    _removalCounts.update(deviceId, (count) => count + 1, ifAbsent: () => 1);
    final waiter = _waiters.remove(deviceId);
    if (waiter != null) {
      waiter.complete();
    } else {
      _started.add(deviceId);
    }
    try {
      await _gates[deviceId]?.future;
    } on Object {
      _removalCounts.remove(deviceId);
      rethrow;
    }
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

BoundDevice _device(String id, DateTime boundAt) {
  return BoundDevice(
    deviceId: id,
    deviceName: id,
    hardwareModel: 'sprout_initial',
    firmwareVersion: '1.0.0',
    capabilities: const [],
    lifecycleStatus: 'active',
    boundAt: boundAt,
    runtime: null,
    canManage: true,
  );
}
