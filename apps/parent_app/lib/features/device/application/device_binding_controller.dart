import 'package:flutter_blue_plus/flutter_blue_plus.dart';
import 'package:esp_provisioning_wifi/esp_provisioning_wifi.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
import '../../../providers.dart';
import '../../auth/application/auth_controller.dart';
import '../data/device_binding_api.dart';
import '../domain/device_payload.dart';

/// State for the nearby-device and scanned-token binding flow.
class DeviceBindingState {
  const DeviceBindingState({
    required this.isScanning,
    required this.devices,
    required this.bindings,
    this.errorMessage,
  });

  const DeviceBindingState.initial()
    : isScanning = false,
      devices = const [],
      bindings = const [],
      errorMessage = null;

  final bool isScanning;
  final List<DiscoveredDevice> devices;
  final List<BoundDevice> bindings;
  final String? errorMessage;

  DeviceBindingState copyWith({
    bool? isScanning,
    List<DiscoveredDevice>? devices,
    List<BoundDevice>? bindings,
    String? errorMessage,
    bool clearErrorMessage = false,
  }) {
    return DeviceBindingState(
      isScanning: isScanning ?? this.isScanning,
      devices: devices ?? this.devices,
      bindings: bindings ?? this.bindings,
      errorMessage: clearErrorMessage
          ? null
          : (errorMessage ?? this.errorMessage),
    );
  }
}

/// Device announced over BLE with the minimum metadata needed for selection.
class DiscoveredDevice {
  const DiscoveredDevice({
    required this.id,
    required this.name,
    required this.rssi,
    required this.serviceData,
  });

  final String id;
  final String name;
  final int rssi;
  final Map<String, List<int>> serviceData;
}

/// Owns discovery, scanned-token binding, and bound-device refresh.
class DeviceBindingController extends AsyncNotifier<DeviceBindingState> {
  late DeviceBindingApi _api;
  final _provisioningService = EspProvisioningService();
  final Set<String> _removingDeviceIds = <String>{};

  @override
  Future<DeviceBindingState> build() async {
    _api = ref.read(deviceBindingApiProvider);
    _removingDeviceIds.clear();
    try {
      final bindings = await _api.list();
      _removingDeviceIds.clear();
      return DeviceBindingState(
        isScanning: false,
        devices: const [],
        bindings: bindings,
      );
    } on Object catch (error) {
      _removingDeviceIds.clear();
      // A device API outage must not invalidate an otherwise valid parent
      // session. The family page renders this as an inline retry state.
      return DeviceBindingState(
        isScanning: false,
        devices: const [],
        bindings: const [],
        errorMessage: authErrorMessage(error),
      );
    }
  }

  Future<void> refresh() async {
    try {
      await refreshWithError();
    } on Object {
      // The state already exposes the stable retry message. RefreshIndicator
      // callers receive a completed future instead of an unhandled error.
    }
  }

  Future<void> refreshWithError({bool preserveStateOnFailure = false}) async {
    final current = state.value;
    try {
      final bindings = await _api.list();
      state = AsyncData(
        current?.copyWith(bindings: bindings, clearErrorMessage: true) ??
            DeviceBindingState(
              isScanning: false,
              devices: const [],
              bindings: bindings,
            ),
      );
    } on Object catch (error) {
      if (preserveStateOnFailure) {
        rethrow;
      }
      if (current == null) {
        state = AsyncError(error, StackTrace.current);
      } else {
        final latest = state.value;
        state = AsyncData(
          latest == null
              ? current.copyWith(errorMessage: authErrorMessage(error))
              : latest.copyWith(
                  bindings: latest.bindings.isEmpty
                      ? current.bindings
                      : latest.bindings,
                  errorMessage: authErrorMessage(error),
                ),
        );
      }
      rethrow;
    }
  }

  Future<void> scanNearby() async {
    final current = state.value ?? const DeviceBindingState.initial();
    state = AsyncData(
      current.copyWith(isScanning: true, clearErrorMessage: true),
    );
    try {
      await FlutterBluePlus.startScan(timeout: const Duration(seconds: 8));
      final results = await FlutterBluePlus.scanResults.firstWhere(
        (items) => items.isNotEmpty,
        orElse: () => const <ScanResult>[],
      );
      final devices = results
          .where((result) => result.device.platformName.trim().isNotEmpty)
          .map(
            (result) => DiscoveredDevice(
              id: result.device.remoteId.str,
              name: result.device.platformName.trim(),
              rssi: result.rssi,
              serviceData: <String, List<int>>{
                for (final entry
                    in result.advertisementData.serviceData.entries)
                  entry.key.toString(): entry.value,
              },
            ),
          )
          .toList(growable: false);
      state = AsyncData(current.copyWith(isScanning: false, devices: devices));
    } on Object catch (error) {
      state = AsyncData(
        current.copyWith(isScanning: false, errorMessage: _messageFor(error)),
      );
    } finally {
      await FlutterBluePlus.stopScan();
    }
  }

  /// Binds a token extracted from a QR code or BLE provisioning payload.
  Future<BoundDevice> bindToken({
    required String token,
    required String deviceName,
  }) async {
    final binding = await _api.bind(token: token, deviceName: deviceName);
    await refresh();
    return binding;
  }

  Future<DeviceProvisioningTicket> requestProvisioningTicket({
    required String deviceId,
    required String deviceSessionToken,
  }) {
    return _api.requestProvisioningTicket(deviceId, deviceSessionToken);
  }

  /// Confirms a freshly provisioned device through the platform binding API.
  Future<BoundDevice> bindProvisionedDevice({
    required DeviceSetupPayload setup,
  }) async {
    final bindingPayload = await readBindingPayload(setup);
    return bindToken(
      token: bindingPayload.bindingToken,
      deviceName: bindingPayload.deviceName,
    );
  }

  /// Scans for first-run devices advertising the configured local prefix.
  Future<List<String>> scanProvisioningDevices() async {
    try {
      await _provisioningService.cancelOperations();
      return await _provisioningService.scanBleDevices('SPROUT-');
    } on Object catch (error) {
      throw AppException(
        kind: AppErrorKind.unexpected,
        message: _provisioningMessage(error),
        retryable: true,
      );
    }
  }

  /// Scans Wi-Fi networks visible to the selected nearby device.
  Future<List<EspWifiNetwork>> scanWifiNetworks(
    DeviceSetupPayload setup,
  ) async {
    try {
      return await _provisioningService.scanWifiNetworks(
        setup.serviceName,
        setup.proofOfPossession,
        security: EspSecurityScheme.security2,
        username: setup.username,
      );
    } on Object catch (error) {
      throw AppException(
        kind: AppErrorKind.unexpected,
        message: _provisioningMessage(error),
        retryable: true,
      );
    }
  }

  /// Sends the selected Wi-Fi credentials to the nearby device.
  Future<void> provisionWifi({
    required DeviceSetupPayload setup,
    required String ssid,
    required String password,
  }) async {
    try {
      final isProvisioned = await _provisioningService.provisionWifi(
        setup.serviceName,
        setup.proofOfPossession,
        ssid,
        password,
        security: EspSecurityScheme.security2,
        username: setup.username,
      );
      if (!isProvisioned) {
        throw const AppException(
          kind: AppErrorKind.unexpected,
          message: '网络设置没有完成，请确认密码后重试',
          retryable: true,
        );
      }
    } on AppException {
      rethrow;
    } on Object catch (error) {
      throw AppException(
        kind: AppErrorKind.unexpected,
        message: _provisioningMessage(error),
        retryable: true,
      );
    }
  }

  /// Reads the final one-time binding payload after Wi-Fi setup succeeds.
  Future<DeviceBindingPayload> readBindingPayload(
    DeviceSetupPayload setup, {
    int maxAttempts = 8,
    Duration retryInterval = const Duration(seconds: 2),
  }) async {
    Object? lastError;
    for (var attempt = 1; attempt <= maxAttempts; attempt++) {
      try {
        final rawPayload = await _provisioningService.fetchCustomData(
          setup.serviceName,
          setup.proofOfPossession,
          security: EspSecurityScheme.security2,
          username: setup.username,
        );
        final payload = rawPayload == null
            ? null
            : DevicePayload.tryParse(rawPayload);
        if (payload is DeviceBindingPayload) {
          return payload;
        }
        lastError = const AppException(
          kind: AppErrorKind.unexpected,
          message: '设备还没有准备好，请让初芽保持开机后重试',
          retryable: true,
        );
      } on Object catch (error) {
        lastError = error;
      }
      if (attempt < maxAttempts) {
        await Future<void>.delayed(retryInterval);
      }
    }
    throw AppException(
      kind: AppErrorKind.unexpected,
      message: _provisioningMessage(lastError ?? '设备还没有准备好'),
      retryable: true,
    );
  }

  Future<void> cancelProvisioning() async {
    await _provisioningService.cancelOperations();
  }

  Future<void> remove(String deviceId) async {
    if (_removingDeviceIds.contains(deviceId)) {
      return;
    }
    final previous = state.value;
    if (previous == null) {
      return;
    }
    final removedDevice = previous.bindings
        .where((device) => device.deviceId == deviceId)
        .firstOrNull;
    if (removedDevice == null) {
      return;
    }
    if (!removedDevice.canManage) {
      state = AsyncData(previous.copyWith(errorMessage: '当前角色没有解除设备绑定的权限'));
      return;
    }
    _removingDeviceIds.add(deviceId);
    // Remove the device immediately so the confirmation cannot be followed by
    // a stale row if the refresh request is slow or briefly unavailable.
    state = AsyncData(
      previous.copyWith(
        bindings: previous.bindings
            .where((device) => device.deviceId != deviceId)
            .toList(growable: false),
        clearErrorMessage: true,
      ),
    );
    try {
      await _api.remove(deviceId);
      // Refresh failures keep the optimistic result rather than resurrecting
      // a successfully deleted row. The next page refresh reconciles the list.
      try {
        await refreshWithError(preserveStateOnFailure: true);
      } on Object {
        // The delete succeeded; a failed reconciliation must not restore the
        // row or replace the successful result with a mutation error.
      }
    } on Object {
      final current = state.value;
      if (current != null) {
        // Restore only this device. Another concurrent successful deletion
        // must remain deleted instead of being overwritten by the snapshot
        // that existed before either request started.
        final bindings = [...current.bindings];
        if (!bindings.any((device) => device.deviceId == deviceId)) {
          bindings.add(removedDevice);
        }
        bindings.sort((left, right) => right.boundAt.compareTo(left.boundAt));
        state = AsyncData(
          current.copyWith(
            bindings: List<BoundDevice>.unmodifiable(bindings),
            errorMessage: '设备没有移除，请稍后重试',
          ),
        );
      }
      rethrow;
    } finally {
      _removingDeviceIds.remove(deviceId);
    }
  }
}

String _messageFor(Object error) {
  if (error is AppException) {
    return error.message;
  }
  return '没有找到附近设备，请确认初芽已开机并处于配网状态';
}

String _provisioningMessage(Object error) {
  if (error is AppException) {
    return error.message;
  }
  return '没有完成连接，请让初芽保持开机并靠近手机后重试';
}

final deviceBindingControllerProvider =
    AsyncNotifierProvider<DeviceBindingController, DeviceBindingState>(
      DeviceBindingController.new,
    );
