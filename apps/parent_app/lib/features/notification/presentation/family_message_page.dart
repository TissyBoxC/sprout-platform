import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/error/app_exception.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../device/application/device_binding_controller.dart';
import '../../device/data/device_binding_api.dart';
import '../application/notification_controller.dart';

/// Composes one guardian message and optionally shows it on a bound device.
class FamilyMessagePage extends ConsumerStatefulWidget {
  const FamilyMessagePage({super.key});

  @override
  ConsumerState<FamilyMessagePage> createState() => _FamilyMessagePageState();
}

class _FamilyMessagePageState extends ConsumerState<FamilyMessagePage> {
  final _formKey = GlobalKey<FormState>();
  final _messageController = TextEditingController();
  String? _deviceId;
  bool _sendToDevice = true;
  int _durationSeconds = 30;
  bool _isSubmitting = false;
  String? _errorMessage;

  @override
  void dispose() {
    _messageController.dispose();
    super.dispose();
  }

  Future<void> _submit(List<BoundDevice> devices) async {
    if (!_formKey.currentState!.validate()) {
      return;
    }
    final deviceId = _deviceId ?? (devices.isNotEmpty ? devices.first.deviceId : null);
    if (deviceId == null || deviceId.isEmpty) {
      setState(() => _errorMessage = '请先绑定一台设备，再给设备留言。');
      return;
    }
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    try {
      await ref.read(notificationApiProvider).sendFamilyMessage(
        deviceId: deviceId,
        body: _messageController.text.trim(),
        sendToDevice: _sendToDevice,
        displayDurationSeconds: _durationSeconds,
      );
      await ref.read(notificationControllerProvider.notifier).refresh();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              _sendToDevice ? '留言已发送，设备会显示这条消息。' : '留言已保存到家长端消息列表。',
            ),
          ),
        );
        context.pop();
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = _message(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isSubmitting = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final binding = ref.watch(deviceBindingControllerProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('给设备留言')),
      body: AppStateSwitcher(
        stateKey: binding.when(
          data: (value) => value.bindings.isEmpty ? 'message-empty' : 'message-form',
          error: (_, _) => 'message-error',
          loading: () => 'message-loading',
        ),
        child: binding.when(
          loading: () => const Center(
            key: ValueKey<String>('message-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _MessageErrorState(
            key: const ValueKey<String>('message-error'),
            message: _message(error),
            onRetry: () => ref
                .read(deviceBindingControllerProvider.notifier)
                .refresh(),
          ),
          data: (state) {
            final devices = state.bindings;
            if (devices.isEmpty) {
              return _NoDeviceState(
                key: const ValueKey<String>('message-empty'),
                onAdd: () => context.push('/devices/scan'),
              );
            }
            return Form(
              key: _formKey,
              child: ListView(
                padding: const EdgeInsets.all(20),
                children: [
                  Text(
                    '选择设备',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 10),
                  RadioGroup<String>(
                    groupValue: _deviceId ?? devices.first.deviceId,
                    onChanged: (value) => setState(() => _deviceId = value),
                    child: Column(
                      children: [
                        for (final device in devices)
                          Card(
                            child: RadioListTile<String>(
                              value: device.deviceId,
                              title: Text(device.deviceName),
                              subtitle: Text(
                                (device.runtime?.isOnline ?? false)
                                    ? '在线 · 可以立即显示'
                                    : '离线 · 设备联网后会自动收到',
                              ),
                            ),
                          ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 20),
                  Text(
                    '留言内容',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 10),
                  TextFormField(
                    controller: _messageController,
                    maxLength: 200,
                    maxLines: 4,
                    textInputAction: TextInputAction.newline,
                    decoration: const InputDecoration(
                      hintText: '例如：宝贝，记得多喝水，妈妈在等你哦～',
                      border: OutlineInputBorder(),
                    ),
                    validator: (value) {
                      final text = value?.trim() ?? '';
                      if (text.isEmpty) {
                        return '请输入要给宝贝的留言';
                      }
                      if (text.length > 200) {
                        return '留言不要超过 200 个字';
                      }
                      return null;
                    },
                  ),
                  SwitchListTile(
                    value: _sendToDevice,
                    contentPadding: EdgeInsets.zero,
                    title: const Text('在设备上显示'),
                    subtitle: const Text('关闭后只在家长端保存，不占用设备屏幕'),
                    onChanged: (value) =>
                        setState(() => _sendToDevice = value),
                  ),
                  if (_sendToDevice) ...[
                    const SizedBox(height: 4),
                    Text(
                      '设备显示时长',
                      style: Theme.of(context).textTheme.bodyLarge,
                    ),
                    const SizedBox(height: 6),
                    SegmentedButton<int>(
                      segments: const [
                        ButtonSegment(value: 15, label: Text('15 秒')),
                        ButtonSegment(value: 30, label: Text('30 秒')),
                        ButtonSegment(value: 60, label: Text('1 分钟')),
                      ],
                      selected: {_durationSeconds},
                      onSelectionChanged: (selection) =>
                          setState(() => _durationSeconds = selection.first),
                    ),
                  ],
                  if (_errorMessage != null) ...[
                    const SizedBox(height: 16),
                    Text(
                      _errorMessage!,
                      style: TextStyle(color: Theme.of(context).colorScheme.error),
                    ),
                  ],
                  const SizedBox(height: 24),
                  FilledButton.icon(
                    onPressed: _isSubmitting ? null : () => _submit(devices),
                    icon: const Icon(Icons.send_rounded),
                    label: Text(_isSubmitting ? '正在发送…' : '发送留言'),
                  ),
                ],
              ),
            );
          },
        ),
      ),
    );
  }
}

class _NoDeviceState extends StatelessWidget {
  const _NoDeviceState({required this.onAdd, super.key});

  final VoidCallback onAdd;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.toys_outlined,
              size: 44,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 14),
            const Text('还没有绑定设备'),
            const SizedBox(height: 6),
            const Text(
              '绑定初芽后，就可以给宝贝留言了。',
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: onAdd,
              icon: const Icon(Icons.add_rounded),
              label: const Text('添加设备'),
            ),
          ],
        ),
      ),
    );
  }
}

class _MessageErrorState extends StatelessWidget {
  const _MessageErrorState({
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
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

String _message(Object error) {
  if (error is AppException) {
    return error.message;
  }
  return '留言没有发送成功，请稍后重试';
}
