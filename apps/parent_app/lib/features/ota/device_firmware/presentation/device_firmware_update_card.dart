import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../shared/widgets/app_reveal.dart';
import '../../../../shared/widgets/app_state_switcher.dart';
import '../application/device_firmware_controller.dart';
import '../domain/device_firmware_update.dart';

/// Compact firmware status card for a bound device detail page.
///
/// The parent app update card and this card use different copy and sources on
/// purpose: this card never refers to installing the parent application.
class DeviceFirmwareUpdateCard extends ConsumerWidget {
  const DeviceFirmwareUpdateCard({
    required this.deviceId,
    required this.deviceName,
    this.isDeviceOnline = true,
    this.onOpenDetails,
    super.key,
  });

  final String deviceId;
  final String deviceName;
  final bool isDeviceOnline;
  final VoidCallback? onOpenDetails;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final firmware = ref.watch(deviceFirmwareControllerProvider(deviceId));
    return AppReveal(
      child: Card(
        child: InkWell(
          borderRadius: BorderRadius.circular(20),
          onTap: onOpenDetails,
          child: Padding(
            padding: const EdgeInsets.all(18),
            child: AppStateSwitcher(
              stateKey: firmware.when(
                data: (_) => 'firmware-card-ready',
                error: (_, _) => 'firmware-card-error',
                loading: () => 'firmware-card-loading',
              ),
              child: firmware.when(
                loading: () => const _FirmwareCardLoading(),
                error: (error, _) => _FirmwareCardError(
                  message: deviceFirmwareErrorMessage(error),
                  onRetry: () => ref
                      .read(deviceFirmwareControllerProvider(deviceId).notifier)
                      .refresh(),
                ),
                data: (state) => _FirmwareCardContent(
                  deviceId: deviceId,
                  deviceName: deviceName,
                  isDeviceOnline: isDeviceOnline,
                  state: state,
                  onOpenDetails: onOpenDetails,
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _FirmwareCardLoading extends StatelessWidget {
  const _FirmwareCardLoading();

  @override
  Widget build(BuildContext context) {
    return const Row(
      children: [
        SizedBox(
          width: 22,
          height: 22,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
        SizedBox(width: 12),
        Text('正在读取设备固件状态…'),
      ],
    );
  }
}

class _FirmwareCardError extends StatelessWidget {
  const _FirmwareCardError({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(
          Icons.system_update_alt_rounded,
          color: Theme.of(context).colorScheme.error,
        ),
        const SizedBox(width: 12),
        Expanded(child: Text(message)),
        TextButton(onPressed: onRetry, child: const Text('重新加载')),
      ],
    );
  }
}

class _FirmwarePill extends StatelessWidget {
  const _FirmwarePill({
    required this.icon,
    required this.label,
    this.emphasized = false,
  });

  final IconData icon;
  final String label;
  final bool emphasized;

  @override
  Widget build(BuildContext context) {
    final color = emphasized
        ? Theme.of(context).colorScheme.primary
        : Theme.of(context).colorScheme.onSurfaceVariant;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(99),
        border: Border.all(color: color.withValues(alpha: 0.35)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 16, color: color),
          const SizedBox(width: 6),
          Text(
            label,
            style: Theme.of(context).textTheme.labelMedium?.copyWith(
              color: color,
              fontWeight: FontWeight.w600,
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

Color _statusColor(
  BuildContext context,
  DeviceFirmwareUpdateStatus? status, {
  required bool isDeviceOnline,
}) {
  if (!isDeviceOnline) {
    return Theme.of(context).colorScheme.tertiary;
  }
  return switch (status) {
    DeviceFirmwareUpdateStatus.failed => Theme.of(context).colorScheme.error,
    DeviceFirmwareUpdateStatus.rollbackAvailable => Theme.of(
      context,
    ).colorScheme.tertiary,
    _ => Theme.of(context).colorScheme.primary,
  };
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

class _FirmwareCardContent extends StatelessWidget {
  const _FirmwareCardContent({
    required this.deviceId,
    required this.deviceName,
    required this.isDeviceOnline,
    required this.state,
    required this.onOpenDetails,
  });

  final String deviceId;
  final String deviceName;
  final bool isDeviceOnline;
  final DeviceFirmwareState state;
  final VoidCallback? onOpenDetails;

  @override
  Widget build(BuildContext context) {
    final update = state.update;
    final status = update?.status;
    final accent = _statusColor(
      context,
      status,
      isDeviceOnline: isDeviceOnline,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: accent.withValues(alpha: 0.13),
                borderRadius: BorderRadius.circular(16),
              ),
              child: Icon(Icons.system_update_alt_rounded, color: accent),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '设备固件更新',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 4),
                  Text(deviceFirmwareStatusLabel(status)),
                ],
              ),
            ),
            if (state.isRefreshing)
              const SizedBox(
                width: 20,
                height: 20,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            else
              Icon(Icons.chevron_right_rounded, color: accent),
          ],
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            _FirmwarePill(
              icon: Icons.memory_rounded,
              label: '当前 ${_versionLabel(update?.currentVersion)}',
            ),
            if (update?.targetVersion?.isNotEmpty == true)
              _FirmwarePill(
                icon: Icons.arrow_upward_rounded,
                label: '目标 ${update!.targetVersion}',
                emphasized: update.updateAvailable,
              ),
            if (update?.release != null)
              _FirmwarePill(
                icon: Icons.workspace_premium_outlined,
                label: deviceFirmwareChannelLabel(update!.release!.channel),
              ),
          ],
        ),
        if (!isDeviceOnline) ...[
          const SizedBox(height: 12),
          _InlineNotice(
            icon: Icons.cloud_off_rounded,
            message: '设备当前离线，重新联网后才能开始更新。',
            color: Theme.of(context).colorScheme.tertiary,
          ),
        ] else if (state.pollingError != null) ...[
          const SizedBox(height: 12),
          _InlineNotice(
            icon: Icons.wifi_off_rounded,
            message: state.pollingError!,
            color: Theme.of(context).colorScheme.tertiary,
          ),
        ] else if (status?.isInProgress == true) ...[
          const SizedBox(height: 14),
          LinearProgressIndicator(
            value: _progressValue(update),
            minHeight: 8,
            borderRadius: BorderRadius.circular(99),
          ),
          const SizedBox(height: 6),
          Text('${update?.progressPercent.round() ?? 0}%'),
        ],
        if (onOpenDetails != null) ...[
          const SizedBox(height: 14),
          OutlinedButton.icon(
            onPressed: onOpenDetails,
            icon: const Icon(Icons.visibility_outlined),
            label: const Text('查看设备固件详情'),
          ),
        ],
      ],
    );
  }
}
