import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/error/app_exception.dart';
import '../../../core/theme/app_motion.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../ota/device_firmware/presentation/device_firmware_update_panel.dart';
import '../application/device_binding_controller.dart';
import '../data/device_binding_api.dart';
import '../domain/device_provisioning_status.dart';

/// Lists bound devices and starts QR or nearby-device provisioning.
class DeviceListPage extends ConsumerWidget {
  const DeviceListPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final binding = ref.watch(deviceBindingControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('设备'),
        actions: [
          IconButton(
            tooltip: '添加设备',
            onPressed: () => context.push('/devices/scan'),
            icon: const Icon(Icons.add_circle_outline_rounded),
          ),
          const SizedBox(width: 4),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => context.push('/devices/scan'),
        icon: const Icon(Icons.add_rounded),
        label: const Text('添加设备'),
      ),
      body: AppStateSwitcher(
        stateKey: binding.when(
          data: (_) => 'devices-content',
          error: (_, _) => 'devices-error',
          loading: () => 'devices-loading',
        ),
        child: binding.when(
          loading: () => const Center(
            key: ValueKey<String>('devices-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => Center(
            key: const ValueKey<String>('devices-error'),
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Icon(Icons.cloud_off_outlined, size: 42),
                  const SizedBox(height: 12),
                  const Text('暂时无法读取设备'),
                  const SizedBox(height: 6),
                  const Text(
                    '请检查网络连接后重试，已绑定的设备不会因此丢失。',
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 12),
                  FilledButton(
                    onPressed: () => ref
                        .read(deviceBindingControllerProvider.notifier)
                        .refresh(),
                    child: const Text('重新加载'),
                  ),
                ],
              ),
            ),
          ),
          data: (state) {
            return RefreshIndicator(
              key: const ValueKey<String>('devices-content'),
              onRefresh: () =>
                  ref.read(deviceBindingControllerProvider.notifier).refresh(),
              child: ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  AppReveal(
                    child: Card(
                      child: Padding(
                        padding: const EdgeInsets.all(18),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            Text(
                              '添加新设备',
                              style: Theme.of(context).textTheme.titleMedium,
                            ),
                            const SizedBox(height: 6),
                            const Text('带屏幕的初芽会显示二维码；没有屏幕时请在附近设备中选择。'),
                            const SizedBox(height: 16),
                            FilledButton.icon(
                              onPressed: () => context.push('/devices/scan'),
                              icon: const Icon(Icons.qr_code_scanner),
                              label: const Text('扫描设备绑定码'),
                            ),
                            const SizedBox(height: 10),
                            AnimatedSwitcher(
                              duration: AppMotion.fast,
                              child: OutlinedButton.icon(
                                key: ValueKey<bool>(state.isScanning),
                                onPressed: state.isScanning
                                    ? null
                                    : () => ref
                                          .read(
                                            deviceBindingControllerProvider
                                                .notifier,
                                          )
                                          .scanNearby(),
                                icon: state.isScanning
                                    ? const SizedBox(
                                        width: 18,
                                        height: 18,
                                        child: CircularProgressIndicator(
                                          strokeWidth: 2,
                                        ),
                                      )
                                    : const Icon(Icons.bluetooth_searching),
                                label: Text(
                                  state.isScanning ? '正在寻找设备…' : '在附近设备中添加',
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                  AnimatedSize(
                    duration: AppMotion.standard,
                    curve: AppMotion.enterCurve,
                    alignment: Alignment.topCenter,
                    child: state.errorMessage == null
                        ? const SizedBox(width: double.infinity)
                        : Padding(
                            padding: const EdgeInsets.only(top: 12),
                            child: Card(
                              child: ListTile(
                                leading: const Icon(Icons.info_outline),
                                title: Text(state.errorMessage!),
                              ),
                            ),
                          ),
                  ),
                  if (state.devices.isNotEmpty) ...[
                    const SizedBox(height: 20),
                    Text(
                      '发现的设备',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 8),
                    for (var index = 0; index < state.devices.length; index++)
                      AppReveal(
                        delay: Duration(milliseconds: 55 * index.clamp(0, 5)),
                        child: Card(
                          child: ListTile(
                            leading: const Icon(Icons.bluetooth),
                            title: Text(state.devices[index].name),
                            subtitle: Text(
                              '信号 ${state.devices[index].rssi} dBm',
                            ),
                            trailing: const Icon(Icons.chevron_right),
                            onTap: () => _openDiscoveredDevice(
                              context,
                              state.devices[index].name,
                            ),
                          ),
                        ),
                      ),
                  ],
                  const SizedBox(height: 24),
                  Text('已绑定设备', style: Theme.of(context).textTheme.titleMedium),
                  const SizedBox(height: 8),
                  if (state.bindings.isEmpty)
                    const Card(
                      child: ListTile(
                        leading: Icon(Icons.toys_outlined),
                        title: Text('还没有绑定设备'),
                        subtitle: Text('完成上面的步骤后，设备会显示在这里。'),
                      ),
                    )
                  else
                    for (var index = 0; index < state.bindings.length; index++)
                      AppReveal(
                        delay: Duration(milliseconds: 55 * index.clamp(0, 5)),
                        child: _BoundDeviceCard(
                          device: state.bindings[index],
                          onTap: () =>
                              _showDeviceDetails(
                                context,
                                state.bindings[index],
                              ).then((reconnect) {
                                if (reconnect == true && context.mounted) {
                                  _startReconnect(
                                    context,
                                    state.bindings[index],
                                  );
                                }
                              }),
                          onReconnect: () =>
                              _startReconnect(context, state.bindings[index]),
                          onRemove: () => _confirmRemove(
                            context,
                            ref,
                            state.bindings[index].deviceId,
                            state.bindings[index].deviceName,
                          ),
                        ),
                      ),
                ],
              ),
            );
          },
        ),
      ),
    );
  }

  Future<void> _startReconnect(BuildContext context, BoundDevice device) async {
    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (sheetContext) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(24, 8, 24, 32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('重新连接设备', style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 10),
              Text('请让“${device.deviceName}”保持开机，并让设备显示绑定二维码。'),
              const SizedBox(height: 16),
              FilledButton.icon(
                onPressed: () {
                  Navigator.of(sheetContext).pop();
                  context.push('/devices/scan');
                },
                icon: const Icon(Icons.qr_code_scanner_rounded),
                label: const Text('扫描设备绑定码'),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _openDiscoveredDevice(
    BuildContext context,
    String deviceName,
  ) async {
    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (context) => Padding(
        padding: const EdgeInsets.fromLTRB(24, 8, 24, 32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              '连接 $deviceName',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: 10),
            const Text('请扫描设备屏幕上显示的二维码继续连接。'),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () {
                Navigator.of(context).pop();
                context.push('/devices/scan');
              },
              child: const Text('扫描设备二维码'),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _confirmRemove(
    BuildContext context,
    WidgetRef ref,
    String deviceId,
    String deviceName,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('解除设备绑定'),
        content: Text('解除后，这台设备将不再属于当前家长账号。确定解除“$deviceName”吗？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('保留绑定'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('解除绑定'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      try {
        await ref
            .read(deviceBindingControllerProvider.notifier)
            .remove(deviceId);
        if (context.mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(const SnackBar(content: Text('设备已解除绑定')));
        }
      } on Object catch (error) {
        if (context.mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(_deviceErrorMessage(error))));
        }
      }
    }
  }

  Future<bool?> _showDeviceDetails(BuildContext context, BoundDevice device) {
    return showModalBottomSheet<bool>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (sheetContext) => SafeArea(
        child: ConstrainedBox(
          constraints: BoxConstraints(
            maxHeight: MediaQuery.sizeOf(sheetContext).height * 0.9,
          ),
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(24, 8, 24, 32),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  device.deviceName,
                  style: Theme.of(sheetContext).textTheme.titleLarge,
                ),
                const SizedBox(height: 8),
                Text(
                  _deviceSubtitle(device),
                  style: Theme.of(sheetContext).textTheme.bodyMedium,
                ),
                const SizedBox(height: 16),
                _ProvisioningStatusPanel(
                  status: device.runtime?.provisioning,
                  onReconnect: () => Navigator.of(sheetContext).pop(true),
                ),
                const SizedBox(height: 20),
                Text(
                  '设备能力',
                  style: Theme.of(sheetContext).textTheme.titleMedium,
                ),
                const SizedBox(height: 8),
                if (device.capabilities.isEmpty)
                  const Text('设备暂时没有上报可用能力。')
                else
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final capability in device.capabilities)
                        Chip(label: Text(_capabilityLabel(capability))),
                    ],
                  ),
                const SizedBox(height: 24),
                const Divider(),
                const SizedBox(height: 12),
                DeviceFirmwareUpdatePanel(
                  deviceId: device.deviceId,
                  deviceName: device.deviceName,
                  isDeviceOnline: device.runtime?.isOnline == true,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _BoundDeviceCard extends StatelessWidget {
  const _BoundDeviceCard({
    required this.device,
    required this.onTap,
    required this.onReconnect,
    required this.onRemove,
  });

  final BoundDevice device;
  final VoidCallback onTap;
  final VoidCallback onReconnect;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    final provisioning = device.runtime?.provisioning;
    final needsReconnect = _needsReconnect(provisioning);
    final tertiary = Theme.of(context).colorScheme.tertiary;
    return Card(
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(20),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 14, 8, 14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  Container(
                    width: 44,
                    height: 44,
                    decoration: BoxDecoration(
                      color: needsReconnect
                          ? Theme.of(context).colorScheme.errorContainer
                          : Theme.of(context).colorScheme.primaryContainer,
                      borderRadius: BorderRadius.circular(16),
                    ),
                    child: Icon(
                      needsReconnect
                          ? Icons.link_off_rounded
                          : Icons.toys_rounded,
                      color: needsReconnect
                          ? Theme.of(context).colorScheme.onErrorContainer
                          : Theme.of(context).colorScheme.primary,
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          device.deviceName,
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        const SizedBox(height: 4),
                        Text(_deviceSubtitle(device)),
                      ],
                    ),
                  ),
                  IconButton(
                    tooltip: '解除绑定',
                    onPressed: onRemove,
                    icon: const Icon(Icons.link_off_rounded),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  _DeviceStatusPill(
                    icon: Icons.wifi_tethering_rounded,
                    label: _provisioningStateLabel(provisioning),
                    emphasized: provisioning?.state == 'provisioning',
                  ),
                  _DeviceStatusPill(
                    icon: needsReconnect
                        ? Icons.warning_amber_rounded
                        : Icons.verified_user_outlined,
                    label: _sessionStateLabel(provisioning),
                    emphasized: needsReconnect,
                    color: needsReconnect ? tertiary : null,
                  ),
                ],
              ),
              if (needsReconnect) ...[
                const SizedBox(height: 12),
                Container(
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(
                    color: Theme.of(context).colorScheme.errorContainer,
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        _sessionWarning(provisioning),
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.onErrorContainer,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        '需要重新完成连接，孩子的设备才能继续使用。',
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.onErrorContainer,
                        ),
                      ),
                      const SizedBox(height: 10),
                      FilledButton.icon(
                        onPressed: onReconnect,
                        icon: const Icon(Icons.refresh_rounded),
                        label: const Text('重新连接设备'),
                        style: FilledButton.styleFrom(
                          backgroundColor: Theme.of(
                            context,
                          ).colorScheme.onErrorContainer,
                          foregroundColor: Theme.of(
                            context,
                          ).colorScheme.errorContainer,
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _DeviceStatusPill extends StatelessWidget {
  const _DeviceStatusPill({
    required this.icon,
    required this.label,
    this.emphasized = false,
    this.color,
  });

  final IconData icon;
  final String label;
  final bool emphasized;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final effectiveColor = color ?? Theme.of(context).colorScheme.primary;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: emphasized
            ? effectiveColor.withValues(alpha: 0.12)
            : Theme.of(context).colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(99),
        border: Border.all(
          color: emphasized
              ? effectiveColor.withValues(alpha: 0.44)
              : Theme.of(context).colorScheme.outlineVariant,
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 17, color: effectiveColor),
          const SizedBox(width: 6),
          Text(
            label,
            style: Theme.of(context).textTheme.labelMedium?.copyWith(
              color: effectiveColor,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }
}

class _ProvisioningStatusPanel extends StatelessWidget {
  const _ProvisioningStatusPanel({
    required this.status,
    required this.onReconnect,
  });

  final DeviceProvisioningStatus? status;
  final VoidCallback onReconnect;

  @override
  Widget build(BuildContext context) {
    final needsReconnect = _needsReconnect(status);
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: needsReconnect
            ? Theme.of(context).colorScheme.errorContainer
            : Theme.of(context).colorScheme.primaryContainer,
        borderRadius: BorderRadius.circular(18),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '配网与会话',
            style: Theme.of(
              context,
            ).textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 8),
          Text('配网状态：${_provisioningStateLabel(status)}'),
          Text('会话状态：${_sessionStateLabel(status)}'),
          if ((status?.droppedEvents ?? 0) > 0)
            Text('待补传事件：${status!.droppedEvents}'),
          if (status?.lastProvisionedAt != null) ...[
            const SizedBox(height: 4),
            Text('最近配网：${_relativeTime(status!.lastProvisionedAt!)}'),
          ],
          if (needsReconnect) ...[
            const SizedBox(height: 10),
            Text(
              '${_sessionWarning(status)}。请重新连接设备后继续使用。',
              style: TextStyle(
                color: Theme.of(context).colorScheme.onErrorContainer,
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(height: 12),
            FilledButton.icon(
              onPressed: onReconnect,
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('重新连接设备'),
            ),
          ],
        ],
      ),
    );
  }
}

String _deviceSubtitle(BoundDevice device) {
  final runtime = device.runtime;
  final details = <String>[
    _lifecycleStatusLabel(device.lifecycleStatus),
    if (runtime != null) runtime.isOnline ? '在线' : '离线',
    if (runtime?.networkQuality.isNotEmpty == true)
      '网络${_networkQualityLabel(runtime!.networkQuality)}',
    if (runtime?.receivedAt != null)
      '最近连接 ${_relativeTime(runtime!.receivedAt!)}',
    if (device.hardwareModel.isNotEmpty) device.hardwareModel,
    if (device.firmwareVersion.isNotEmpty) device.firmwareVersion,
  ];
  return details.join(' · ');
}

String _provisioningStateLabel(DeviceProvisioningStatus? status) {
  return switch (status?.state) {
    'unprovisioned' => '未配网',
    'provisioning' => '配网中',
    'provisioned' => '已配网',
    _ => '配网状态未知',
  };
}

String _sessionStateLabel(DeviceProvisioningStatus? status) {
  return switch (status?.sessionState) {
    'ready' => '连接正常',
    'reauth_required' => '需要重新连接',
    'revoked' => '设备连接已失效',
    _ => '连接状态未知',
  };
}

bool _needsReconnect(DeviceProvisioningStatus? status) {
  final sessionState = status?.sessionState;
  return sessionState == 'reauth_required' || sessionState == 'revoked';
}

String _sessionWarning(DeviceProvisioningStatus? status) {
  return status?.sessionState == 'revoked' ? '设备连接已失效' : '设备需要重新连接';
}

String _lifecycleStatusLabel(String status) {
  return switch (status) {
    'active' => '已绑定',
    'inactive' => '已解绑',
    'pending' => '待绑定',
    'revoked' => '已解除',
    _ => '状态未知',
  };
}

String _capabilityLabel(String capability) {
  return switch (capability) {
    'display' => '屏幕',
    'touch' => '触摸',
    'camera' => '摄像头',
    'audio_input' => '麦克风',
    'audio_output' => '扬声器',
    'bluetooth' => '蓝牙',
    'wifi' => '无线网络',
    'cellular_4g' => '移动网络',
    'battery' => '电池',
    'motion' => '动作感应',
    _ => capability,
  };
}

String _deviceErrorMessage(Object error) {
  if (error is AppException && error.message.trim().isNotEmpty) {
    return error.message;
  }
  return '暂时无法解除绑定，请检查网络后重试';
}

String _networkQualityLabel(String quality) {
  return switch (quality) {
    'excellent' => '很好',
    'good' => '良好',
    'fair' => '一般',
    'poor' => '较差',
    _ => '未知',
  };
}

String _relativeTime(DateTime value) {
  final difference = DateTime.now().difference(value.toLocal());
  if (difference.inMinutes < 1) {
    return '刚刚';
  }
  if (difference.inHours < 1) {
    return '${difference.inMinutes} 分钟前';
  }
  if (difference.inDays < 1) {
    return '${difference.inHours} 小时前';
  }
  return '${difference.inDays} 天前';
}
