import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../shared/widgets/app_state_switcher.dart';
import '../application/device_firmware_controller.dart';
import '../domain/device_firmware_update.dart';

/// Full-width firmware panel for a device detail page or bottom sheet.
///
/// This is intentionally the only place that offers install, retry, and
/// rollback actions, so the parent app's own update flow stays separate.
class DeviceFirmwareUpdatePanel extends ConsumerWidget {
  const DeviceFirmwareUpdatePanel({
    required this.deviceId,
    required this.deviceName,
    this.isDeviceOnline = true,
    super.key,
  });

  final String deviceId;
  final String deviceName;
  final bool isDeviceOnline;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final firmware = ref.watch(deviceFirmwareControllerProvider(deviceId));
    return AppStateSwitcher(
      stateKey: firmware.when(
        data: (_) => 'firmware-panel-ready',
        error: (_, _) => 'firmware-panel-error',
        loading: () => 'firmware-panel-loading',
      ),
      child: firmware.when(
        loading: () => const Padding(
          padding: EdgeInsets.symmetric(vertical: 36),
          child: Center(child: CircularProgressIndicator()),
        ),
        error: (error, _) => _FirmwarePanelError(
          message: deviceFirmwareErrorMessage(error),
          onRetry: () => ref
              .read(deviceFirmwareControllerProvider(deviceId).notifier)
              .refresh(),
        ),
        data: (state) => _FirmwarePanelContent(
          deviceId: deviceId,
          deviceName: deviceName,
          isDeviceOnline: isDeviceOnline,
          state: state,
        ),
      ),
    );
  }
}

class _FirmwarePanelError extends StatelessWidget {
  const _FirmwarePanelError({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 28),
      child: Column(
        children: [
          Icon(
            Icons.system_update_alt_rounded,
            size: 42,
            color: Theme.of(context).colorScheme.error,
          ),
          const SizedBox(height: 12),
          Text(message, textAlign: TextAlign.center),
          const SizedBox(height: 16),
          FilledButton(onPressed: onRetry, child: const Text('重新加载')),
        ],
      ),
    );
  }
}

class _FirmwarePanelContent extends ConsumerWidget {
  const _FirmwarePanelContent({
    required this.deviceId,
    required this.deviceName,
    required this.isDeviceOnline,
    required this.state,
  });

  final String deviceId;
  final String deviceName;
  final bool isDeviceOnline;
  final DeviceFirmwareState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final update = state.update;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '$deviceName 的固件',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                  const SizedBox(height: 4),
                  Text(deviceFirmwareStatusLabel(update?.status)),
                ],
              ),
            ),
            IconButton(
              tooltip: '重新检查固件',
              onPressed: state.isRefreshing || state.isActing
                  ? null
                  : () => ref
                        .read(
                          deviceFirmwareControllerProvider(deviceId).notifier,
                        )
                        .refresh(),
              icon: state.isRefreshing
                  ? const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.refresh_rounded),
            ),
          ],
        ),
        if (update != null) ...[
          const SizedBox(height: 6),
          Text(
            '当前版本 ${_versionLabel(update.currentVersion)}'
            '${update.targetVersion == null ? '' : ' · 可更新至 ${update.targetVersion}'}',
          ),
          const SizedBox(height: 16),
          _FirmwareTimeline(update: update),
          if (update.status.isInProgress) ...[
            const SizedBox(height: 16),
            LinearProgressIndicator(
              value: _progressValue(update),
              minHeight: 10,
              borderRadius: BorderRadius.circular(99),
            ),
            const SizedBox(height: 8),
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Text(deviceFirmwareStatusLabel(update.status)),
                Text('${update.progressPercent.round()}%'),
              ],
            ),
          ],
          if (update.status == DeviceFirmwareUpdateStatus.failed) ...[
            const SizedBox(height: 16),
            _InlineNotice(
              icon: Icons.error_outline_rounded,
              message: deviceFirmwareFailureMessage(
                update.errorCode,
                update.errorMessage,
              ),
              color: Theme.of(context).colorScheme.error,
            ),
          ],
          if (update.release != null) ...[
            const SizedBox(height: 18),
            _ReleaseSummary(release: update.release!),
          ],
          if (!isDeviceOnline) ...[
            const SizedBox(height: 16),
            _InlineNotice(
              icon: Icons.cloud_off_rounded,
              message: '设备当前离线。请让设备保持开机并连接网络后再升级。',
              color: Theme.of(context).colorScheme.tertiary,
            ),
          ],
          if (state.actionError != null) ...[
            const SizedBox(height: 12),
            _InlineNotice(
              icon: Icons.info_outline_rounded,
              message: state.actionError!,
              color: Theme.of(context).colorScheme.error,
            ),
          ],
          const SizedBox(height: 20),
          _FirmwareActions(
            deviceId: deviceId,
            deviceName: deviceName,
            isDeviceOnline: isDeviceOnline,
            state: state,
          ),
        ],
      ],
    );
  }
}

class _FirmwareActions extends ConsumerWidget {
  const _FirmwareActions({
    required this.deviceId,
    required this.deviceName,
    required this.isDeviceOnline,
    required this.state,
  });

  final String deviceId;
  final String deviceName;
  final bool isDeviceOnline;
  final DeviceFirmwareState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final update = state.update!;
    final notifier = ref.read(
      deviceFirmwareControllerProvider(deviceId).notifier,
    );
    final isBusy = state.isActing || update.status.isInProgress;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (state.canStartUpdate)
          FilledButton.icon(
            onPressed: isDeviceOnline && !isBusy
                ? () => _confirmInstall(context, notifier, deviceName, update)
                : null,
            icon: const Icon(Icons.system_update_alt_rounded),
            label: Text(isDeviceOnline ? '开始更新设备固件' : '设备离线，暂时不能更新'),
          )
        else if (state.canRetry)
          FilledButton.icon(
            onPressed: isDeviceOnline && !isBusy
                ? () => _confirmRetry(context, notifier, deviceName)
                : null,
            icon: const Icon(Icons.refresh_rounded),
            label: const Text('重试设备固件更新'),
          )
        else if (state.canRollback)
          OutlinedButton.icon(
            onPressed: isDeviceOnline && !isBusy
                ? () => _confirmRollback(context, notifier, deviceName)
                : null,
            icon: const Icon(Icons.undo_rounded),
            label: const Text('回退到上一个版本'),
          )
        else if (update.status.isInProgress)
          FilledButton.icon(
            onPressed: null,
            icon: const SizedBox(
              width: 18,
              height: 18,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
            label: Text(deviceFirmwareStatusLabel(update.status)),
          )
        else if (update.status == DeviceFirmwareUpdateStatus.upToDate)
          const _UpToDateNotice(),
      ],
    );
  }
}

class _UpToDateNotice extends StatelessWidget {
  const _UpToDateNotice();

  @override
  Widget build(BuildContext context) {
    return _InlineNotice(
      icon: Icons.verified_rounded,
      message: '设备固件已经是最新版本。',
      color: Theme.of(context).colorScheme.primary,
    );
  }
}

class _ReleaseSummary extends StatelessWidget {
  const _ReleaseSummary({required this.release});

  final DeviceFirmwareRelease release;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: Theme.of(context).colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '新版本 ${release.firmwareVersion}',
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 6),
          Text(
            '${deviceFirmwareChannelLabel(release.channel)} · '
            '${_formatBytes(release.sizeBytes)} · '
            '${_formatDateTime(release.publishedAt)}',
          ),
          if (release.releaseNotes.trim().isNotEmpty) ...[
            const SizedBox(height: 12),
            Text(
              release.releaseNotes,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
          ],
          if (!release.isInstallable) ...[
            const SizedBox(height: 12),
            Text(
              '这个版本的更新信息还不完整，暂时不能安装。',
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
          ],
        ],
      ),
    );
  }
}

class _FirmwareTimeline extends StatelessWidget {
  const _FirmwareTimeline({required this.update});

  final DeviceFirmwareUpdate update;

  static const _stages = <_TimelineStage>[
    _TimelineStage('检查新版本', DeviceFirmwareUpdateStatus.available),
    _TimelineStage('下载固件', DeviceFirmwareUpdateStatus.downloading),
    _TimelineStage('校验固件', DeviceFirmwareUpdateStatus.validating),
    _TimelineStage('安装到设备', DeviceFirmwareUpdateStatus.installing),
    _TimelineStage('等待设备确认', DeviceFirmwareUpdateStatus.pendingVerify),
  ];

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        for (var index = 0; index < _stages.length; index++)
          _TimelineRow(
            label: _stages[index].label,
            isActive: _hasPassed(_stages[index].status),
            isLast: index == _stages.length - 1,
          ),
      ],
    );
  }

  bool _hasPassed(DeviceFirmwareUpdateStatus threshold) {
    if (update.status == DeviceFirmwareUpdateStatus.failed) {
      return false;
    }
    return update.status.index >= threshold.index;
  }
}

class _TimelineStage {
  const _TimelineStage(this.label, this.status);

  final String label;
  final DeviceFirmwareUpdateStatus status;
}

class _TimelineRow extends StatelessWidget {
  const _TimelineRow({
    required this.label,
    required this.isActive,
    required this.isLast,
  });

  final String label;
  final bool isActive;
  final bool isLast;

  @override
  Widget build(BuildContext context) {
    final color = isActive
        ? Theme.of(context).colorScheme.primary
        : Theme.of(context).colorScheme.outlineVariant;
    return IntrinsicHeight(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Column(
            children: [
              Container(
                width: 18,
                height: 18,
                decoration: BoxDecoration(
                  color: isActive ? color : Colors.transparent,
                  border: Border.all(color: color, width: 2),
                  shape: BoxShape.circle,
                ),
                child: isActive
                    ? const Icon(
                        Icons.check_rounded,
                        size: 12,
                        color: Colors.white,
                      )
                    : null,
              ),
              if (!isLast) Expanded(child: Container(width: 2, color: color)),
            ],
          ),
          const SizedBox(width: 10),
          Padding(
            padding: const EdgeInsets.only(bottom: 14),
            child: Text(
              label,
              style: TextStyle(
                color: isActive
                    ? Theme.of(context).colorScheme.onSurface
                    : Theme.of(context).colorScheme.outline,
                fontWeight: isActive ? FontWeight.w600 : FontWeight.w400,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _InlineNotice extends StatelessWidget {
  const _InlineNotice({
    required this.icon,
    required this.message,
    required this.color,
  });

  final IconData icon;
  final String message;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: color.withValues(alpha: 0.35)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 20, color: color),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              message,
              style: TextStyle(color: color, fontWeight: FontWeight.w600),
            ),
          ),
        ],
      ),
    );
  }
}

Future<void> _confirmInstall(
  BuildContext context,
  DeviceFirmwareController controller,
  String deviceName,
  DeviceFirmwareUpdate update,
) async {
  final release = update.release;
  final version = release?.firmwareVersion ?? update.targetVersion ?? '新版本';
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('开始更新设备固件'),
      content: Text(
        '将把“$deviceName”更新到 $version。\n\n'
        '更新期间请保持设备开机并连接电源，不要强制关机。'
        '完成后设备可能重启一次。',
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('暂不更新'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('开始更新'),
        ),
      ],
    ),
  );
  if (confirmed != true || !context.mounted) {
    return;
  }
  final didStart = await controller.startInstall();
  if (!context.mounted) {
    return;
  }
  if (!didStart) {
    final state = controller.currentState;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(state?.actionError ?? '固件更新没有开始，请稍后重试')),
    );
  }
}

Future<void> _confirmRetry(
  BuildContext context,
  DeviceFirmwareController controller,
  String deviceName,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('重试设备固件更新'),
      content: Text('将重新为“$deviceName”下载并安装设备固件。确定继续吗？'),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('暂不重试'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('重试更新'),
        ),
      ],
    ),
  );
  if (confirmed == true && context.mounted) {
    await controller.retry();
  }
}

Future<void> _confirmRollback(
  BuildContext context,
  DeviceFirmwareController controller,
  String deviceName,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('回退设备固件'),
      content: Text(
        '将把“$deviceName”恢复到上一个可用版本。'
        '回退期间请保持设备开机并连接电源。',
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('暂不回退'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('确认回退'),
        ),
      ],
    ),
  );
  if (confirmed == true && context.mounted) {
    await controller.rollback();
  }
}

double? _progressValue(DeviceFirmwareUpdate? update) {
  if (update == null) {
    return null;
  }
  return (update.progressPercent / 100).clamp(0.0, 1.0);
}

String _versionLabel(String? version) {
  if (version == null || version.trim().isEmpty) {
    return '版本未知';
  }
  return version;
}

String _formatBytes(int bytes) {
  if (bytes >= 1024 * 1024) {
    final megabytes = bytes / (1024 * 1024);
    return '${megabytes.toStringAsFixed(megabytes >= 10 ? 0 : 1)} MB';
  }
  if (bytes >= 1024) {
    return '${(bytes / 1024).toStringAsFixed(0)} KB';
  }
  return '$bytes 字节';
}

String _formatDateTime(DateTime value) {
  final local = value.toLocal();
  final year = local.year.toString();
  final month = local.month.toString().padLeft(2, '0');
  final day = local.day.toString().padLeft(2, '0');
  return '$year-$month-$day';
}
