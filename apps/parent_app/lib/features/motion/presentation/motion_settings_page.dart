import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/motion_controller.dart';
import '../domain/motion_policy.dart';

/// Guardian view of the family motion and motor policy.
///
/// The page edits bounded scalars only: locomotion permission, speed ceiling,
/// maximum run time and the stop-acknowledgement rule. No raw motor telemetry
/// is captured.
class MotionSettingsPage extends ConsumerWidget {
  const MotionSettingsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final policy = ref.watch(motionControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('动作与电机'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: policy.when(
          data: (_) => 'motion-policy-data',
          error: (_, _) => 'motion-policy-error',
          loading: () => 'motion-policy-loading',
        ),
        child: policy.when(
          loading: () => const Center(
            key: ValueKey<String>('motion-policy-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('motion-policy-error'),
            message: MotionController.messageFor(error),
            onRetry: () =>
                ref.read(motionControllerProvider.notifier).refresh(),
          ),
          data: (value) => _PolicyForm(
            key: const ValueKey<String>('motion-policy-data'),
            policy: value,
            onSave: (next) => _save(context, ref, next),
          ),
        ),
      ),
    );
  }

  Future<void> _save(
    BuildContext context,
    WidgetRef ref,
    MotionPolicy policy,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(motionControllerProvider.notifier).save(policy);
      messenger.showSnackBar(const SnackBar(content: Text('动作设置已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(MotionController.messageFor(error))),
      );
    }
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
      return;
    }
    context.go('/me');
  }
}

class _PolicyForm extends StatefulWidget {
  const _PolicyForm({required this.policy, required this.onSave, super.key});

  final MotionPolicy policy;
  final Future<void> Function(MotionPolicy policy) onSave;

  @override
  State<_PolicyForm> createState() => _PolicyFormState();
}

class _PolicyFormState extends State<_PolicyForm> {
  late MotionPolicy _draft;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _draft = widget.policy;
  }

  @override
  void didUpdateWidget(covariant _PolicyForm oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.policy.revision != widget.policy.revision) {
      _draft = widget.policy;
    }
  }

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: [
        AppReveal(
          child: Card(
            child: SwitchListTile(
              title: const Text('启用动作控制'),
              subtitle: const Text('关闭后设备不执行任何电机或舵机动作'),
              value: _draft.enabled,
              onChanged: (value) =>
                  setState(() => _draft = _draft.copyWith(enabled: value)),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: _NumberSlider(
            title: '最大速度',
            value: _draft.maxSpeedPercent.toDouble(),
            min: 1,
            max: 100,
            unit: '%',
            divisions: 99,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(maxSpeedPercent: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: _NumberSlider(
            title: '最长运行时间',
            value: _draft.maxRuntimeMs.toDouble(),
            min: 200,
            max: 60000,
            step: 100,
            unit: '毫秒',
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(maxRuntimeMs: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 120),
          child: _NumberSlider(
            title: '停止缓冲',
            value: _draft.stopGraceMs.toDouble(),
            min: 0,
            max: 60000,
            step: 50,
            unit: '毫秒',
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(stopGraceMs: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 160),
          child: Card(
            child: Column(
              children: [
                SwitchListTile(
                  title: const Text('允许行走与舞蹈'),
                  subtitle: const Text('关闭时设备只做尾巴和手势等原地动作'),
                  value: _draft.allowLocomotion,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(allowLocomotion: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('断开自动停止'),
                  subtitle: const Text('与平台失联时立即停止动作，回到安全状态'),
                  value: _draft.requireStopAck,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(requireStopAck: value),
                  ),
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 24),
        FilledButton.icon(
          onPressed: _saving ? null : _submit,
          icon: const Icon(Icons.check_rounded),
          label: const Text('保存动作设置'),
        ),
      ],
    );
  }

  Future<void> _submit() async {
    setState(() => _saving = true);
    try {
      await widget.onSave(_draft);
    } finally {
      if (mounted) {
        setState(() => _saving = false);
      }
    }
  }
}

class _NumberSlider extends StatelessWidget {
  const _NumberSlider({
    required this.title,
    required this.value,
    required this.min,
    required this.max,
    required this.unit,
    required this.onChanged,
    this.divisions,
    this.step,
  });

  final String title;
  final double value;
  final double min;
  final double max;
  final String unit;
  final int? divisions;
  final int? step;
  final ValueChanged<double> onChanged;

  @override
  Widget build(BuildContext context) {
    final effectiveDivisions =
        divisions ?? (step == null ? null : ((max - min) / step!).round());
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Text(title, style: Theme.of(context).textTheme.titleMedium),
                Text('${value.round()} $unit'),
              ],
            ),
            Slider(
              value: value.clamp(min, max),
              min: min,
              max: max,
              divisions: effectiveDivisions,
              label: '${value.round()} $unit',
              onChanged: onChanged,
            ),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry, super.key});

  final String message;
  final VoidCallback onRetry;

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
            FilledButton(onPressed: onRetry, child: const Text('重试')),
          ],
        ),
      ),
    );
  }
}
