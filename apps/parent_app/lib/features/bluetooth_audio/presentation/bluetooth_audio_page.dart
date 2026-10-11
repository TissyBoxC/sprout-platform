import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/bluetooth_audio_controller.dart';
import '../domain/bluetooth_audio.dart';

/// Guardian view of the family Bluetooth audio policy and device status.
class BluetoothAudioPage extends ConsumerWidget {
  const BluetoothAudioPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(bluetoothAudioControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('蓝牙音箱'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (value) => value.isEmpty
              ? 'bluetooth-audio-empty'
              : 'bluetooth-audio-content',
          error: (_, _) => 'bluetooth-audio-error',
          loading: () => 'bluetooth-audio-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('bluetooth-audio-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _BluetoothAudioErrorState(
            key: const ValueKey<String>('bluetooth-audio-error'),
            message: BluetoothAudioController.messageFor(error),
            onRetry: () =>
                ref.read(bluetoothAudioControllerProvider.notifier).refresh(),
          ),
          data: (value) => _BluetoothAudioContent(
            key: ValueKey<String>(
              value.isEmpty
                  ? 'bluetooth-audio-empty'
                  : 'bluetooth-audio-content',
            ),
            state: value,
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

class _BluetoothAudioContent extends ConsumerWidget {
  const _BluetoothAudioContent({required this.state, super.key});

  final BluetoothAudioState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(bluetoothAudioControllerProvider.notifier);
    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(child: _PolicyCard(state: state)),
          const SizedBox(height: 20),
          AppReveal(child: _DeviceSection(state: state)),
        ],
      ),
    );
  }
}

class _PolicyCard extends ConsumerWidget {
  const _PolicyCard({required this.state});

  final BluetoothAudioState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final policy = state.policy;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.speaker_rounded),
                const SizedBox(width: 10),
                Text('音箱设置', style: Theme.of(context).textTheme.titleMedium),
                const Spacer(),
                Switch(
                  value: policy.enabled,
                  onChanged: (value) =>
                      _publish(context, ref, policy, enabled: value),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              policy.enabled
                  ? '已开启：设备可作为蓝牙音箱被搜索和连接。'
                  : '未开启：在监护人打开前，设备不会广播或被连接。',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            const SizedBox(height: 12),
            _InfoRow(
              icon: Icons.bluetooth_searching_rounded,
              label: '允许配对',
              value: policy.pairingAllowed ? '允许' : '不允许',
            ),
            _InfoRow(
              icon: Icons.graphic_eq_rounded,
              label: '优先编码',
              value: policy.preferredCodec.label,
            ),
            _InfoRow(
              icon: Icons.volume_up_rounded,
              label: '音量上限',
              value: '${policy.maxVolumePercent}%',
            ),
            _InfoRow(
              icon: Icons.autorenew_rounded,
              label: '自动重连',
              value: policy.autoReconnect ? '开启' : '关闭',
            ),
            if (policy.displayName.isNotEmpty)
              _InfoRow(
                icon: Icons.label_outline_rounded,
                label: '音箱名称',
                value: policy.displayName,
              ),
            const SizedBox(height: 12),
            Align(
              alignment: Alignment.centerRight,
              child: FilledButton.tonalIcon(
                onPressed: () => _openEditor(context, ref, policy),
                icon: const Icon(Icons.tune_rounded),
                label: const Text('编辑设置'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _DeviceSection extends ConsumerWidget {
  const _DeviceSection({required this.state});

  final BluetoothAudioState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Icon(Icons.devices_other_rounded),
            const SizedBox(width: 10),
            Text('设备状态', style: Theme.of(context).textTheme.titleMedium),
            const Spacer(),
            Text(
              '已连接 ${state.connectedCount}',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
        const SizedBox(height: 8),
        if (state.devices.isEmpty)
          const _EmptyCard(
            icon: Icons.bluetooth_disabled_rounded,
            message: '还没有设备上报蓝牙状态。',
          )
        else
          for (final device in state.devices) _DeviceTile(device: device),
      ],
    );
  }
}

class _DeviceTile extends StatelessWidget {
  const _DeviceTile({required this.device});

  final BluetoothAudioDevice device;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        leading: Icon(
          device.connected
              ? Icons.bluetooth_connected_rounded
              : Icons.bluetooth_rounded,
          color: device.connected
              ? Theme.of(context).colorScheme.primary
              : null,
        ),
        title: Text(_shortId(device.deviceId)),
        subtitle: Text(
          device.supported
              ? '${adapterStateLabel(device.adapterState)} · '
                    '已配对 ${device.pairedDeviceCount} · '
                    '${activeCodecLabel(device.activeCodec)} · '
                    '音量 ${device.volumePercent}%'
              : '此设备不支持蓝牙音箱',
        ),
        trailing: device.supported && !device.upToDate
            ? const Tooltip(
                message: '尚未应用最新设置',
                child: Icon(Icons.sync_problem_rounded),
              )
            : null,
      ),
    );
  }
}

class _InfoRow extends StatelessWidget {
  const _InfoRow({
    required this.icon,
    required this.label,
    required this.value,
  });

  final IconData icon;
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Icon(icon, size: 18, color: Theme.of(context).colorScheme.outline),
          const SizedBox(width: 10),
          Text(label),
          const Spacer(),
          Text(
            value,
            style: Theme.of(
              context,
            ).textTheme.bodyMedium?.copyWith(fontWeight: FontWeight.w600),
          ),
        ],
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

class _BluetoothAudioErrorState extends StatelessWidget {
  const _BluetoothAudioErrorState({
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

Future<void> _publish(
  BuildContext context,
  WidgetRef ref,
  BluetoothAudioPolicy policy, {
  bool? enabled,
}) async {
  final draft = BluetoothAudioDraft(
    enabled: enabled ?? policy.enabled,
    speakerModeAllowed: policy.speakerModeAllowed,
    pairingAllowed: policy.pairingAllowed,
    autoReconnect: policy.autoReconnect,
    preferredCodec: policy.preferredCodec,
    maxVolumePercent: policy.maxVolumePercent,
    displayName: policy.displayName,
  );
  try {
    await ref
        .read(bluetoothAudioControllerProvider.notifier)
        .publishPolicy(draft);
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(BluetoothAudioController.messageFor(error))),
      );
    }
  }
}

Future<void> _openEditor(
  BuildContext context,
  WidgetRef ref,
  BluetoothAudioPolicy policy,
) {
  return Navigator.of(context).push<void>(
    MaterialPageRoute<void>(
      builder: (context) => _BluetoothAudioEditPage(policy: policy),
    ),
  );
}

String _shortId(String deviceId) {
  if (deviceId.length <= 12) {
    return deviceId.isEmpty ? '未知设备' : deviceId;
  }
  return '${deviceId.substring(0, 6)}…${deviceId.substring(deviceId.length - 4)}';
}

class _BluetoothAudioEditPage extends ConsumerStatefulWidget {
  const _BluetoothAudioEditPage({required this.policy});

  final BluetoothAudioPolicy policy;

  @override
  ConsumerState<_BluetoothAudioEditPage> createState() =>
      _BluetoothAudioEditPageState();
}

class _BluetoothAudioEditPageState
    extends ConsumerState<_BluetoothAudioEditPage> {
  late bool _enabled;
  late bool _speakerModeAllowed;
  late bool _pairingAllowed;
  late bool _autoReconnect;
  late BluetoothCodec _preferredCodec;
  late double _maxVolumePercent;
  late final TextEditingController _displayNameController;
  bool _isSubmitting = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    final policy = widget.policy;
    _enabled = policy.enabled;
    _speakerModeAllowed = policy.speakerModeAllowed;
    _pairingAllowed = policy.pairingAllowed;
    _autoReconnect = policy.autoReconnect;
    _preferredCodec = policy.preferredCodec;
    _maxVolumePercent = policy.maxVolumePercent.toDouble();
    _displayNameController = TextEditingController(text: policy.displayName);
  }

  @override
  void dispose() {
    _displayNameController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    final draft = BluetoothAudioDraft(
      enabled: _enabled,
      speakerModeAllowed: _speakerModeAllowed,
      pairingAllowed: _pairingAllowed,
      autoReconnect: _autoReconnect,
      preferredCodec: _preferredCodec,
      maxVolumePercent: _maxVolumePercent.round(),
      displayName: _displayNameController.text.trim(),
    );
    try {
      await ref
          .read(bluetoothAudioControllerProvider.notifier)
          .publishPolicy(draft);
      if (mounted) {
        Navigator.of(context).pop();
      }
    } on Object catch (error) {
      if (mounted) {
        setState(
          () => _errorMessage = BluetoothAudioController.messageFor(error),
        );
      }
    } finally {
      if (mounted) {
        setState(() => _isSubmitting = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('蓝牙音箱设置')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('开启蓝牙音箱'),
              subtitle: const Text('关闭后设备不会广播，也不会接受新的配对'),
              value: _enabled,
              onChanged: (value) => setState(() => _enabled = value),
            ),
            const Divider(),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('允许作为音箱播放'),
              subtitle: const Text('允许已连接的手机或平板把音频投到设备'),
              value: _speakerModeAllowed && _enabled,
              onChanged: _enabled
                  ? (value) => setState(() => _speakerModeAllowed = value)
                  : null,
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('允许新的配对'),
              subtitle: const Text('关闭后只保留已配对设备'),
              value: _pairingAllowed && _enabled,
              onChanged: _enabled
                  ? (value) => setState(() => _pairingAllowed = value)
                  : null,
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('自动重连'),
              subtitle: const Text('已配对设备在范围内时自动连接'),
              value: _autoReconnect,
              onChanged: (value) => setState(() => _autoReconnect = value),
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<BluetoothCodec>(
              initialValue: _preferredCodec,
              decoration: const InputDecoration(labelText: '优先音频编码'),
              items: [
                for (final codec in BluetoothCodec.values)
                  DropdownMenuItem<BluetoothCodec>(
                    value: codec,
                    child: Text(codec.label),
                  ),
              ],
              onChanged: (value) =>
                  setState(() => _preferredCodec = value ?? _preferredCodec),
            ),
            const SizedBox(height: 16),
            Text('音量上限：${_maxVolumePercent.round()}%'),
            Slider(
              value: _maxVolumePercent,
              max: 100,
              divisions: 20,
              label: '${_maxVolumePercent.round()}%',
              onChanged: (value) => setState(() => _maxVolumePercent = value),
            ),
            const SizedBox(height: 8),
            TextField(
              controller: _displayNameController,
              maxLength: 48,
              decoration: const InputDecoration(
                labelText: '音箱名称（可选）',
                hintText: '例如：客厅音箱',
              ),
            ),
            if (_errorMessage != null) ...[
              const SizedBox(height: 8),
              Text(
                _errorMessage!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 20),
            FilledButton(
              onPressed: _isSubmitting ? null : _submit,
              child: Text(_isSubmitting ? '正在保存…' : '保存设置'),
            ),
          ],
        ),
      ),
    );
  }
}
