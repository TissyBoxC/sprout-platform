import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/error/app_exception.dart';
import '../../../../core/providers.dart';
import '../../../auth/application/auth_controller.dart';
import '../data/device_firmware_api.dart';
import '../domain/device_firmware_update.dart';

/// UI state for one device firmware update.
///
/// The controller keeps the last successful snapshot while polling so a
/// transient network error does not make the device look like it disappeared.
class DeviceFirmwareState {
  const DeviceFirmwareState({
    required this.update,
    required this.isRefreshing,
    required this.isActing,
    this.actionError,
    this.pollingError,
  });

  const DeviceFirmwareState.initial()
    : update = null,
      isRefreshing = false,
      isActing = false,
      actionError = null,
      pollingError = null;

  final DeviceFirmwareUpdate? update;
  final bool isRefreshing;
  final bool isActing;
  final String? actionError;
  final String? pollingError;

  DeviceFirmwareUpdateStatus? get status => update?.status;

  bool get canStartUpdate =>
      update != null &&
      update!.status == DeviceFirmwareUpdateStatus.available &&
      update!.updateAvailable &&
      update!.release?.isInstallable == true &&
      !update!.status.isInProgress;

  bool get canRetry => update?.status.allowsRetry == true;

  bool get canRollback =>
      update?.rollbackAvailable == true ||
      update?.status.allowsRollback == true;

  DeviceFirmwareState copyWith({
    DeviceFirmwareUpdate? update,
    bool? isRefreshing,
    bool? isActing,
    String? actionError,
    String? pollingError,
    bool clearActionError = false,
    bool clearPollingError = false,
  }) {
    return DeviceFirmwareState(
      update: update ?? this.update,
      isRefreshing: isRefreshing ?? this.isRefreshing,
      isActing: isActing ?? this.isActing,
      actionError: clearActionError ? null : (actionError ?? this.actionError),
      pollingError: clearPollingError
          ? null
          : (pollingError ?? this.pollingError),
    );
  }
}

/// Polling schedule for an in-progress deployment.
///
/// Fast at the start, then backing off for long installs where the device may
/// temporarily be offline while it reboots.
const List<Duration> _defaultPollBackoff = <Duration>[
  Duration(seconds: 2),
  Duration(seconds: 3),
  Duration(seconds: 5),
  Duration(seconds: 8),
  Duration(seconds: 13),
  Duration(seconds: 21),
  Duration(seconds: 34),
];

/// Loads, starts, polls, retries, and rolls back one device's firmware update.
class DeviceFirmwareController extends AsyncNotifier<DeviceFirmwareState> {
  DeviceFirmwareController(this.deviceId);

  final String deviceId;
  late final List<Duration> _pollBackoff;
  late final DeviceFirmwareApi _api;

  Timer? _pollTimer;
  int _pollIndex = 0;
  int _requestGeneration = 0;
  bool _disposed = false;

  /// Latest snapshot for callers that need to render a one-shot action result.
  DeviceFirmwareState? get currentState => state.value;

  @override
  Future<DeviceFirmwareState> build() async {
    _api = ref.read(deviceFirmwareApiProvider);
    _pollBackoff = ref.read(deviceFirmwarePollBackoffProvider);
    ref.onDispose(() {
      _disposed = true;
      _stopPolling();
    });
    try {
      final update = await _api.fetch(deviceId);
      _schedulePollingIfNeeded(update.status);
      return DeviceFirmwareState(
        update: update,
        isRefreshing: false,
        isActing: false,
      );
    } on Object catch (error) {
      throw _mappedError(error);
    }
  }

  Future<void> refresh() async {
    if (_disposed || state.isLoading) {
      return;
    }
    final current = state.value;
    state = AsyncData(
      current?.copyWith(isRefreshing: true, clearActionError: true) ??
          const DeviceFirmwareState(
            update: null,
            isRefreshing: true,
            isActing: false,
          ),
    );
    try {
      final update = await _api.fetch(deviceId);
      if (_disposed) {
        return;
      }
      state = AsyncData(
        DeviceFirmwareState(
          update: update,
          isRefreshing: false,
          isActing: false,
        ),
      );
      _schedulePollingIfNeeded(update.status);
    } on Object catch (error) {
      if (_disposed) {
        return;
      }
      final mapped = _mappedError(error);
      if (current == null) {
        state = AsyncError<DeviceFirmwareState>(mapped, StackTrace.current);
      } else {
        state = AsyncData(
          current.copyWith(isRefreshing: false, pollingError: mapped.message),
        );
      }
    }
  }

  /// Starts a firmware installation after the caller has obtained explicit
  /// guardian confirmation.
  Future<bool> startInstall() async {
    final current = state.value;
    if (_disposed || current == null || !current.canStartUpdate) {
      return false;
    }
    return _performAction(
      () => _api.install(deviceId, deploymentId: current.update?.deploymentId),
      failureFallback: '暂时无法开始设备固件更新，请稍后重试',
    );
  }

  Future<bool> retry() async {
    final current = state.value;
    if (_disposed || current == null || !current.canRetry) {
      return false;
    }
    return _performAction(
      () => _api.retry(deviceId),
      failureFallback: '暂时无法重试设备固件更新，请稍后重试',
    );
  }

  Future<bool> rollback() async {
    final current = state.value;
    if (_disposed || current == null || !current.canRollback) {
      return false;
    }
    return _performAction(
      () => _api.rollback(deviceId),
      failureFallback: '暂时无法回退设备固件，请稍后重试',
    );
  }

  Future<bool> _performAction(
    Future<DeviceFirmwareUpdate> Function() action, {
    required String failureFallback,
  }) async {
    final current = state.value;
    if (current == null || current.isActing) {
      return false;
    }
    state = AsyncData(current.copyWith(isActing: true, clearActionError: true));
    try {
      final update = await action();
      if (_disposed) {
        return false;
      }
      state = AsyncData(
        DeviceFirmwareState(
          update: update,
          isRefreshing: false,
          isActing: false,
        ),
      );
      _schedulePollingIfNeeded(update.status);
      return true;
    } on Object catch (error) {
      if (_disposed) {
        return false;
      }
      final mapped = _mappedError(error);
      state = AsyncData(
        current.copyWith(
          isActing: false,
          actionError: mapped.message.isEmpty
              ? failureFallback
              : mapped.message,
        ),
      );
      return false;
    }
  }

  void _schedulePollingIfNeeded(DeviceFirmwareUpdateStatus status) {
    _stopPolling();
    if (_disposed || !status.isInProgress) {
      return;
    }
    final delay = _pollBackoff[_pollIndex.clamp(0, _pollBackoff.length - 1)];
    _pollIndex = (_pollIndex + 1).clamp(0, _pollBackoff.length - 1);
    _pollTimer = Timer(delay, _poll);
  }

  Future<void> _poll() async {
    if (_disposed) {
      return;
    }
    final generation = ++_requestGeneration;
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncData(
      current.copyWith(isRefreshing: true, clearPollingError: true),
    );
    try {
      final update = await _api.status(deviceId);
      if (_disposed || generation != _requestGeneration) {
        return;
      }
      state = AsyncData(
        DeviceFirmwareState(
          update: update,
          isRefreshing: false,
          isActing: false,
        ),
      );
      if (update.status.isInProgress) {
        _schedulePollingIfNeeded(update.status);
      } else {
        _pollIndex = 0;
      }
    } on Object catch (error) {
      if (_disposed || generation != _requestGeneration) {
        return;
      }
      final mapped = _mappedError(error);
      state = AsyncData(
        current.copyWith(isRefreshing: false, pollingError: mapped.message),
      );
      // Keep polling through transient outages; the deployment may still be
      // running on the device while the guardian's network recovers.
      _schedulePollingIfNeeded(
        current.status ?? DeviceFirmwareUpdateStatus.queued,
      );
    }
  }

  void _stopPolling() {
    _pollTimer?.cancel();
    _pollTimer = null;
  }

  AppException _mappedError(Object error) {
    if (error is AppException) {
      return error;
    }
    if (error is FormatException) {
      return AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '设备固件信息暂时无法读取，请稍后重试',
        retryable: true,
        cause: error,
      );
    }
    return AppException(
      kind: AppErrorKind.unexpected,
      message: authErrorMessage(error),
      retryable: true,
      cause: error,
    );
  }
}

final deviceFirmwareApiProvider = Provider<DeviceFirmwareApi>((ref) {
  return DeviceFirmwareApi(ref.read(apiClientProvider));
});

/// Polling schedule used while a firmware deployment is running.
///
/// Overridable in tests so the state machine can be exercised without waiting
/// for production-length backoff intervals.
final deviceFirmwarePollBackoffProvider = Provider<List<Duration>>(
  (ref) => _defaultPollBackoff,
);

final deviceFirmwareControllerProvider = AsyncNotifierProvider.autoDispose
    .family<DeviceFirmwareController, DeviceFirmwareState, String>(
      DeviceFirmwareController.new,
    );

/// Converts an update state or failed action into concise Chinese copy.
String deviceFirmwareErrorMessage(Object? error) {
  if (error is AppException && error.message.trim().isNotEmpty) {
    return error.message;
  }
  return '设备固件状态暂时无法读取，请稍后重试';
}

/// Human-readable label for the current deployment stage.
String deviceFirmwareStatusLabel(DeviceFirmwareUpdateStatus? status) {
  return switch (status) {
    DeviceFirmwareUpdateStatus.upToDate => '已是最新版本',
    DeviceFirmwareUpdateStatus.available => '可以更新',
    DeviceFirmwareUpdateStatus.queued => '已排入更新队列',
    DeviceFirmwareUpdateStatus.downloading => '正在下载固件',
    DeviceFirmwareUpdateStatus.validating => '正在校验固件',
    DeviceFirmwareUpdateStatus.installing => '正在安装固件',
    DeviceFirmwareUpdateStatus.pendingVerify => '等待设备确认新版本',
    DeviceFirmwareUpdateStatus.succeeded => '更新完成',
    DeviceFirmwareUpdateStatus.failed => '更新没有完成',
    DeviceFirmwareUpdateStatus.rollbackAvailable => '可以回退到上一个版本',
    DeviceFirmwareUpdateStatus.rolledBack => '已回退到上一个版本',
    null => '正在读取设备固件状态',
  };
}

/// Guardian-facing explanation for an update error code.
String deviceFirmwareFailureMessage(String? code, String? serverMessage) {
  final message = serverMessage?.trim();
  if (message != null && message.isNotEmpty) {
    return message;
  }
  return switch (code) {
    'download_failed' => '固件下载没有完成，请检查网络后重试',
    'checksum_mismatch' => '固件校验没有通过，为保护设备已停止更新',
    'signature_invalid' => '固件签名没有通过，更新已停止',
    'insufficient_storage' => '设备空间不足，请先清理内容后重试',
    'device_offline' => '设备当前不在线，请让设备保持开机并连接网络',
    'battery_too_low' => '设备电量较低，请接通电源后重试',
    'install_failed' => '设备没有完成安装，请保持开机后重试',
    'verification_timeout' => '设备确认新版本超时，请稍后重试',
    'rollback_failed' => '设备回退没有完成，请让设备保持开机后重试',
    _ => '设备固件更新没有完成，请让设备保持开机后重试',
  };
}

String deviceFirmwareChannelLabel(String channel) {
  return switch (channel) {
    'stable' => '稳定版',
    'canary' => '尝鲜版',
    'internal' => '内部版',
    _ => '稳定版',
  };
}
