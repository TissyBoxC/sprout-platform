import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../content_management/application/content_library_controller.dart';
import '../../content_management/domain/content_package.dart';
import '../../device/application/device_binding_controller.dart';
import '../../device/data/device_binding_api.dart';
import '../application/playback_controller.dart';
import '../domain/playback_request.dart';

/// Guardian view of on-demand playback: queue a package to a bound device and
/// watch the request progress.
class PlaybackPage extends ConsumerWidget {
  const PlaybackPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(playbackControllerProvider);
    final devices = ref.watch(deviceBindingControllerProvider);
    final catalog = ref.watch(contentLibraryControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('点播'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _openRequestSheet(
          context,
          ref,
          devices.value?.bindings ?? const <BoundDevice>[],
          _catalogPackages(catalog.value),
        ),
        icon: const Icon(Icons.playlist_play_rounded),
        label: const Text('点播'),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (value) => value.isEmpty ? 'playback-empty' : 'playback-content',
          error: (_, _) => 'playback-error',
          loading: () => 'playback-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('playback-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _PlaybackErrorState(
            key: const ValueKey<String>('playback-error'),
            message: PlaybackController.messageFor(error),
            onRetry: () => ref.read(playbackControllerProvider.notifier).refresh(),
          ),
          data: (value) => _PlaybackContent(
            key: ValueKey<String>(
              value.isEmpty ? 'playback-empty' : 'playback-content',
            ),
            state: value,
            devices: devices.value?.bindings ?? const <BoundDevice>[],
          ),
        ),
      ),
    );
  }
}

void _leave(BuildContext context) {
  if (context.canPop()) {
    context.pop();
  } else {
    context.go('/home');
  }
}

List<ContentPackage> _catalogPackages(ContentLibraryState? state) {
  if (state == null) {
    return const [];
  }
  return [for (final entry in state.entries) entry.package];
}

class _PlaybackContent extends ConsumerWidget {
  const _PlaybackContent({
    required this.state,
    required this.devices,
    super.key,
  });

  final PlaybackState state;
  final List<BoundDevice> devices;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(playbackControllerProvider.notifier);
    if (state.isEmpty) {
      return RefreshIndicator(
        onRefresh: controller.refresh,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
          children: const [
            _EmptyCard(
              icon: Icons.playlist_play_rounded,
              message: '还没有点播记录，点右下角选择内容推送到设备吧。',
            ),
          ],
        ),
      );
    }
    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(child: _DeviceFilter(state: state, devices: devices)),
          const SizedBox(height: 12),
          for (final request in state.requests) ...[
            AppReveal(
              child: _PlaybackRequestTile(
                request: request,
                devices: devices,
              ),
            ),
            const SizedBox(height: 10),
          ],
        ],
      ),
    );
  }
}

class _DeviceFilter extends ConsumerWidget {
  const _DeviceFilter({required this.state, required this.devices});

  final PlaybackState state;
  final List<BoundDevice> devices;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (devices.length <= 1) {
      return const SizedBox.shrink();
    }
    final controller = ref.read(playbackControllerProvider.notifier);
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        FilterChip(
          label: const Text('全部设备'),
          selected: state.deviceFilter == null,
          onSelected: (_) => controller.filterByDevice(null),
        ),
        for (final device in devices)
          FilterChip(
            label: Text(_deviceName(device)),
            selected: state.deviceFilter == device.deviceId,
            onSelected: (_) => controller.filterByDevice(device.deviceId),
          ),
      ],
    );
  }
}

class _PlaybackRequestTile extends ConsumerWidget {
  const _PlaybackRequestTile({required this.request, required this.devices});

  final PlaybackRequest request;
  final List<BoundDevice> devices;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      child: ListTile(
        leading: Icon(_statusIcon(request.status)),
        title: Text(request.packageId),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${_deviceName(_deviceById(devices, request.deviceId))} · ${request.status.label}',
            ),
            Text(
              _formatDate(request.requestedAt),
              style: Theme.of(context).textTheme.bodySmall,
            ),
            if (request.note.isNotEmpty)
              Text(request.note, style: Theme.of(context).textTheme.bodySmall),
          ],
        ),
        trailing: request.canCancel
            ? IconButton(
                tooltip: '取消点播',
                icon: const Icon(Icons.close_rounded),
                onPressed: () => _confirmCancel(context, ref, request),
              )
            : null,
      ),
    );
  }
}

class _EmptyCard extends StatelessWidget {
  const _EmptyCard({required this.icon, required this.message});

  final IconData icon;
  final String message;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Row(
          children: [
            Icon(icon, color: Theme.of(context).colorScheme.primary),
            const SizedBox(width: 12),
            Expanded(child: Text(message)),
          ],
        ),
      ),
    );
  }
}

class _PlaybackErrorState extends StatelessWidget {
  const _PlaybackErrorState({
    required this.message,
    required this.onRetry,
    super.key,
  });

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 12),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

Future<void> _confirmCancel(
  BuildContext context,
  WidgetRef ref,
  PlaybackRequest request,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('取消点播'),
      content: const Text('确定要取消这条点播吗？'),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('再想想'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('取消点播'),
        ),
      ],
    ),
  );
  if (confirmed != true) {
    return;
  }
  try {
    await ref
        .read(playbackControllerProvider.notifier)
        .cancelRequest(request.id);
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(PlaybackController.messageFor(error))),
      );
    }
  }
}

Future<void> _openRequestSheet(
  BuildContext context,
  WidgetRef ref,
  List<BoundDevice> devices,
  List<ContentPackage> packages,
) async {
  if (devices.isEmpty) {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('请先绑定一台设备')));
    return;
  }
  if (packages.isEmpty) {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('内容库还没有可点播的内容')));
    return;
  }
  await showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    isScrollControlled: true,
    builder: (context) =>
        _RequestSheet(devices: devices, packages: packages),
  );
}

/// Lets the guardian choose a device and a package, optionally add a note, and
/// queue the request.
class _RequestSheet extends ConsumerStatefulWidget {
  const _RequestSheet({required this.devices, required this.packages});

  final List<BoundDevice> devices;
  final List<ContentPackage> packages;

  @override
  ConsumerState<_RequestSheet> createState() => _RequestSheetState();
}

class _RequestSheetState extends ConsumerState<_RequestSheet> {
  late String _deviceId;
  late String _packageId;
  final _noteController = TextEditingController();
  bool _isSubmitting = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _deviceId = widget.devices.first.deviceId;
    _packageId = widget.packages.first.packageId;
  }

  @override
  void dispose() {
    _noteController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    try {
      await ref
          .read(playbackControllerProvider.notifier)
          .requestPlayback(
            deviceId: _deviceId,
            packageId: _packageId,
            note: _noteController.text.trim(),
          );
      if (mounted) {
        Navigator.of(context).pop();
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = PlaybackController.messageFor(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isSubmitting = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.only(
          left: 20,
          right: 20,
          top: 8,
          bottom: 20 + MediaQuery.viewInsetsOf(context).bottom,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('点播内容', style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 16),
            DropdownButtonFormField<String>(
              initialValue: _deviceId,
              decoration: const InputDecoration(labelText: '播放设备'),
              items: [
                for (final device in widget.devices)
                  DropdownMenuItem<String>(
                    value: device.deviceId,
                    child: Text(_deviceName(device)),
                  ),
              ],
              onChanged: (value) =>
                  setState(() => _deviceId = value ?? _deviceId),
            ),
            const SizedBox(height: 16),
            DropdownButtonFormField<String>(
              initialValue: _packageId,
              decoration: const InputDecoration(labelText: '选择内容'),
              items: [
                for (final package in widget.packages)
                  DropdownMenuItem<String>(
                    value: package.packageId,
                    child: Text(package.title),
                  ),
              ],
              onChanged: (value) =>
                  setState(() => _packageId = value ?? _packageId),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _noteController,
              maxLength: 200,
              decoration: const InputDecoration(
                labelText: '备注（可选）',
                hintText: '例如：睡前播放',
              ),
            ),
            if (_errorMessage != null) ...[
              const SizedBox(height: 8),
              Text(
                _errorMessage!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 12),
            FilledButton.icon(
              onPressed: _isSubmitting ? null : _submit,
              icon: const Icon(Icons.playlist_play_rounded),
              label: Text(_isSubmitting ? '正在发送…' : '推送到设备'),
            ),
          ],
        ),
      ),
    );
  }
}

IconData _statusIcon(PlaybackStatus status) {
  return switch (status) {
    PlaybackStatus.pending => Icons.schedule_rounded,
    PlaybackStatus.acknowledged => Icons.downloading_rounded,
    PlaybackStatus.completed => Icons.check_circle_rounded,
    PlaybackStatus.failed => Icons.error_outline_rounded,
    PlaybackStatus.cancelled => Icons.cancel_outlined,
  };
}

BoundDevice? _deviceById(List<BoundDevice> devices, String deviceId) {
  for (final device in devices) {
    if (device.deviceId == deviceId) {
      return device;
    }
  }
  return null;
}

String _deviceName(BoundDevice? device) {
  if (device == null) {
    return '设备';
  }
  return device.deviceName.isEmpty ? device.deviceId : device.deviceName;
}

String _formatDate(DateTime value) {
  final local = value.toLocal();
  final month = local.month.toString().padLeft(2, '0');
  final day = local.day.toString().padLeft(2, '0');
  final hour = local.hour.toString().padLeft(2, '0');
  final minute = local.minute.toString().padLeft(2, '0');
  return '${local.year}-$month-$day $hour:$minute';
}
