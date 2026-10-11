import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/display_controller.dart';
import '../domain/display_policy.dart';

/// Guardian view of the family eye-care display policy.
///
/// The page edits bounded scalars only: brightness, colour temperature, viewing
/// distance, daily screen time and break reminders. Nothing visual is captured
/// or transmitted.
class DisplaySettingsPage extends ConsumerWidget {
  const DisplaySettingsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final policy = ref.watch(displayControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('护眼屏幕'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: policy.when(
          data: (_) => 'display-policy-data',
          error: (_, _) => 'display-policy-error',
          loading: () => 'display-policy-loading',
        ),
        child: policy.when(
          loading: () => const Center(
            key: ValueKey<String>('display-policy-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('display-policy-error'),
            message: DisplayController.messageFor(error),
            onRetry: () =>
                ref.read(displayControllerProvider.notifier).refresh(),
          ),
          data: (value) => _PolicyForm(
            key: const ValueKey<String>('display-policy-data'),
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
    DisplayPolicy policy,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(displayControllerProvider.notifier).save(policy);
      messenger.showSnackBar(const SnackBar(content: Text('护眼设置已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(DisplayController.messageFor(error))),
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

  final DisplayPolicy policy;
  final Future<void> Function(DisplayPolicy policy) onSave;

  @override
  State<_PolicyForm> createState() => _PolicyFormState();
}

class _PolicyFormState extends State<_PolicyForm> {
  late DisplayPolicy _draft;
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
          child: _NumberSlider(
            title: '屏幕亮度',
            value: _draft.brightnessPercent.toDouble(),
            min: 5,
            max: 100,
            unit: '%',
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(brightnessPercent: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: _NumberSlider(
            title: '色温',
            value: _draft.colorTemperatureKelvin.toDouble(),
            min: 2700,
            max: 6500,
            unit: 'K',
            divisions: 38,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(
                colorTemperatureKelvin: value.round(),
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: _NumberSlider(
            title: '观看距离提醒',
            value: _draft.minViewDistanceCm.toDouble(),
            min: 20,
            max: 120,
            unit: '厘米',
            divisions: 20,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(minViewDistanceCm: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 120),
          child: _NumberSlider(
            title: '每日使用上限',
            value: _draft.dailyLimitMinutes.toDouble(),
            min: 5,
            max: 600,
            unit: '分钟',
            divisions: 119,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(dailyLimitMinutes: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 160),
          child: _NumberSlider(
            title: '休息提醒间隔',
            value: _draft.breakIntervalMinutes.toDouble(),
            min: 5,
            max: 240,
            unit: '分钟',
            divisions: 47,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(
                breakIntervalMinutes: value.round(),
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 200),
          child: Card(
            child: Column(
              children: [
                SwitchListTile(
                  title: const Text('自动亮度'),
                  value: _draft.autoBrightness,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(autoBrightness: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('夜间护眼'),
                  value: _draft.nightLight,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(nightLight: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('距离提醒'),
                  value: _draft.distanceReminder,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(distanceReminder: value),
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
          label: const Text('保存护眼设置'),
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
  });

  final String title;
  final double value;
  final double min;
  final double max;
  final String unit;
  final int? divisions;
  final ValueChanged<double> onChanged;

  @override
  Widget build(BuildContext context) {
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
              divisions: divisions,
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
