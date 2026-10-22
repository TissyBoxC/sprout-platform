import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
import '../application/intercom_controller.dart';
import '../data/intercom_api.dart';

/// Lets a guardian call one device and shows what the call is doing.
///
/// The screen always states whether the device microphone is live. A guardian
/// must never have to guess whether they are being heard or are hearing the
/// child, so ringing and connected are two visually distinct states, not one
/// "calling" spinner.
class IntercomPanel extends ConsumerWidget {
  const IntercomPanel({super.key, required this.deviceId, this.deviceName = ''});

  final String deviceId;
  final String deviceName;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final callState = ref.watch(intercomCallControllerProvider(deviceId));
    return callState.when(
      loading: () => const SizedBox.shrink(),
      error: (error, _) => _Notice(_messageFor(error)),
      data: (call) => _Body(
        deviceId: deviceId,
        deviceName: deviceName,
        call: call,
      ),
    );
  }
}

class _Body extends ConsumerWidget {
  const _Body({
    required this.deviceId,
    required this.deviceName,
    required this.call,
  });

  final String deviceId;
  final String deviceName;
  final DeviceCall? call;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (call == null) {
      return _IdleCard(deviceId: deviceId, deviceName: deviceName);
    }
    if (call!.isRinging) {
      return _RingingCard(deviceId: deviceId, call: call!);
    }
    if (call!.isActive) {
      return _ActiveCard(deviceId: deviceId, call: call!);
    }
    return _FinishedCard(deviceId: deviceId, call: call!);
  }
}

class _IdleCard extends ConsumerWidget {
  const _IdleCard({required this.deviceId, required this.deviceName});

  final String deviceId;
  final String deviceName;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final label = deviceName.isEmpty ? '这台设备' : deviceName;
    return _Shell(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text('语音对讲', style: theme.textTheme.titleSmall),
          const SizedBox(height: 6),
          Text('呼叫$label后，设备会提示孩子，孩子按键接受才会接通。'),
          const SizedBox(height: 12),
          FilledButton(
            onPressed: () => _start(context, ref),
            child: const Text('呼叫设备'),
          ),
        ],
      ),
    );
  }

  Future<void> _start(BuildContext context, WidgetRef ref) async {
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      await ref.read(intercomCallControllerProvider(deviceId).notifier).start();
    } catch (error) {
      messenger?.showSnackBar(SnackBar(content: Text(_messageFor(error))));
    }
  }
}

class _RingingCard extends ConsumerWidget {
  const _RingingCard({required this.deviceId, required this.call});

  final String deviceId;
  final DeviceCall call;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return _Shell(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text('正在呼叫', style: theme.textTheme.titleSmall),
          const SizedBox(height: 6),
          const Text('设备正在提示孩子。孩子按键接受后才会接通，现在还没有传输声音。'),
          const SizedBox(height: 8),
          _Countdown(deadline: call.deadline, prefix: '等待接听'),
          const SizedBox(height: 12),
          TextButton(
            onPressed: () => _cancel(context, ref),
            child: const Text('取消呼叫'),
          ),
        ],
      ),
    );
  }

  Future<void> _cancel(BuildContext context, WidgetRef ref) async {
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      await ref.read(intercomCallControllerProvider(deviceId).notifier).cancel();
    } catch (error) {
      messenger?.showSnackBar(SnackBar(content: Text(_messageFor(error))));
    }
  }
}

class _ActiveCard extends ConsumerWidget {
  const _ActiveCard({required this.deviceId, required this.call});

  final String deviceId;
  final DeviceCall call;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return _Shell(
      accent: const Color(0xFF4CAF50),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              const Icon(Icons.mic, size: 18, color: Color(0xFF4CAF50)),
              const SizedBox(width: 6),
              Text(
                '已接通',
                style: theme.textTheme.titleSmall?.copyWith(
                  color: const Color(0xFF2E7D32),
                ),
              ),
            ],
          ),
          const SizedBox(height: 6),
          const Text('孩子已接受，现在可以与设备双向通话。设备和家长接听端的麦克风都在工作。'),
          const SizedBox(height: 8),
          Text('通话时长 ${_formatDuration(call)}'),
          const SizedBox(height: 12),
          FilledButton(
            onPressed: () => _hangUp(context, ref),
            child: const Text('挂断'),
          ),
        ],
      ),
    );
  }

  Future<void> _hangUp(BuildContext context, WidgetRef ref) async {
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      await ref.read(intercomCallControllerProvider(deviceId).notifier).hangUp();
    } catch (error) {
      messenger?.showSnackBar(SnackBar(content: Text(_messageFor(error))));
    }
  }
}

class _FinishedCard extends ConsumerWidget {
  const _FinishedCard({required this.deviceId, required this.call});

  final String deviceId;
  final DeviceCall call;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return _Shell(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(_outcomeTitle(call), style: theme.textTheme.titleSmall),
          const SizedBox(height: 6),
          Text(_outcomeDetail(call)),
          const SizedBox(height: 12),
          TextButton(
            onPressed: () => ref
                .read(intercomCallControllerProvider(deviceId).notifier)
                .reset(),
            child: const Text('返回'),
          ),
        ],
      ),
    );
  }
}

/// Counts down to the ring deadline so the guardian knows it will end itself.
class _Countdown extends StatefulWidget {
  const _Countdown({required this.deadline, required this.prefix});

  final DateTime deadline;
  final String prefix;

  @override
  State<_Countdown> createState() => _CountdownState();
}

class _CountdownState extends State<_Countdown> {
  Timer? _ticker;

  @override
  void initState() {
    super.initState();
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) {
        setState(() {});
      }
    });
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final remaining = widget.deadline.difference(DateTime.now().toUtc());
    final seconds = remaining.isNegative ? 0 : remaining.inSeconds;
    return Text('${widget.prefix} ${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')}');
  }
}

class _Shell extends StatelessWidget {
  const _Shell({required this.child, this.accent});

  final Widget child;
  final Color? accent;

  @override
  Widget build(BuildContext context) {
    final borderColor = accent ?? const Color(0xFFF7D8E2);
    return Container(
      margin: const EdgeInsets.only(top: 12),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: accent == null ? const Color(0xFFFFF9FA) : const Color(0xFFF4FBF4),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: borderColor),
      ),
      child: child,
    );
  }
}

class _Notice extends StatelessWidget {
  const _Notice(this.message);

  final String message;

  @override
  Widget build(BuildContext context) {
    return _Shell(child: Text(message));
  }
}

String _outcomeTitle(DeviceCall call) {
  return switch (call.state) {
    DeviceCallState.ended => '通话已结束',
    DeviceCallState.missed => '未接听',
    DeviceCallState.declined => '孩子拒绝了这次呼叫',
    _ => '呼叫已取消',
  };
}

String _outcomeDetail(DeviceCall call) {
  return switch (call.state) {
    DeviceCallState.ended => '本次通话时长 ${_formatDuration(call)}。',
    DeviceCallState.missed => '孩子在 ${call.deadline.difference(call.startedAt).inSeconds ~/ 60} 分钟内没有接听，呼叫已自动结束。',
    DeviceCallState.declined => '孩子选择不接听。可以稍后再呼叫一次。',
    _ => '这次呼叫没有接通。',
  };
}

String _formatDuration(DeviceCall call) {
  final seconds = call.durationSeconds;
  return '${seconds ~/ 60} 分 ${seconds % 60} 秒';
}

/// Maps a failure to text a guardian can act on.
String _messageFor(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '暂时无法呼叫设备，请稍后重试。';
}
