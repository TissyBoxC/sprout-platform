import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/power_controller.dart';
import '../domain/power_policy.dart';

/// Guardian view of the family battery and low-power policy.
///
/// The page edits bounded scalars only: low-battery warning, low-power entry,
/// deep-sleep idle window and the mode toggles. No raw gauge telemetry is
/// captured.
class BatterySettingsPage extends ConsumerWidget {
  const BatterySettingsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final policy = ref.watch(powerControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('电池与省电'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: policy.when(
          data: (_) => 'power-policy-data',
          error: (_, _) => 'power-policy-error',
          loading: () => 'power-policy-loading',
        ),
        child: policy.when(
          loading: () => const Center(
            key: ValueKey<String>('power-policy-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('power-policy-error'),
            message: PowerController.messageFor(error),
            onRetry: () => ref.read(powerControllerProvider.notifier).refresh(),
          ),
          data: (value) => _PolicyForm(
            key: const ValueKey<String>('power-policy-data'),
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
    PowerPolicy policy,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(powerControllerProvider.notifier).save(policy);
      messenger.showSnackBar(const SnackBar(content: Text('电源设置已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(PowerController.messageFor(error))),
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

  final PowerPolicy policy;
  final Future<void> Function(PowerPolicy policy) onSave;

  @override
  State<_PolicyForm> createState() => _PolicyFormState();
}

class _PolicyFormState extends State<_PolicyForm> {
  late PowerPolicy _draft;
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
              title: const Text('启用省电策略'),
              subtitle: const Text('关闭后设备不自动进入低功耗或休眠'),
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
            title: '低电量提醒',
            value: _draft.lowBatteryWarnPercent.toDouble(),
            min: 5,
            max: 50,
            unit: '%',
            divisions: 9,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(
                lowBatteryWarnPercent: value.round(),
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: _NumberSlider(
            title: '进入低功耗',
            value: _draft.lowPowerEnterPercent.toDouble(),
            min: 5,
            max: 50,
            unit: '%',
            divisions: 9,
            onChanged: (value) => setState(
              () =>
                  _draft = _draft.copyWith(lowPowerEnterPercent: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 120),
          child: _NumberSlider(
            title: '空闲后休眠',
            value: _draft.sleepAfterSeconds.toDouble(),
            min: 60,
            max: 86400,
            step: 60,
            unit: '秒',
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(sleepAfterSeconds: value.round()),
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
                  title: const Text('自动低功耗'),
                  value: _draft.lowPowerEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(lowPowerEnabled: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('深度休眠'),
                  subtitle: const Text('保留唤醒源，空闲到达阈值后休眠'),
                  value: _draft.deepSleepEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(deepSleepEnabled: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('充电优先'),
                  subtitle: const Text('充电期间不进入低功耗'),
                  value: _draft.chargePriority,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(chargePriority: value),
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
          label: const Text('保存电源设置'),
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
