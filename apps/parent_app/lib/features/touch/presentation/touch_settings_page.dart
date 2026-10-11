import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/touch_controller.dart';
import '../domain/touch_config.dart';

/// Guardian view of the family touch configuration.
///
/// The page edits bounded scalars only: enable flags, sensitivity, rotation
/// and gesture thresholds. No raw touch samples or coordinates are captured.
class TouchSettingsPage extends ConsumerWidget {
  const TouchSettingsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final config = ref.watch(touchControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('触摸交互'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: config.when(
          data: (_) => 'touch-config-data',
          error: (_, _) => 'touch-config-error',
          loading: () => 'touch-config-loading',
        ),
        child: config.when(
          loading: () => const Center(
            key: ValueKey<String>('touch-config-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('touch-config-error'),
            message: TouchController.messageFor(error),
            onRetry: () => ref.read(touchControllerProvider.notifier).refresh(),
          ),
          data: (value) => _ConfigForm(
            key: const ValueKey<String>('touch-config-data'),
            config: value,
            onSave: (next) => _save(context, ref, next),
          ),
        ),
      ),
    );
  }

  Future<void> _save(
    BuildContext context,
    WidgetRef ref,
    TouchConfig config,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(touchControllerProvider.notifier).save(config);
      messenger.showSnackBar(const SnackBar(content: Text('触摸设置已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(TouchController.messageFor(error))),
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

class _ConfigForm extends StatefulWidget {
  const _ConfigForm({required this.config, required this.onSave, super.key});

  final TouchConfig config;
  final Future<void> Function(TouchConfig config) onSave;

  @override
  State<_ConfigForm> createState() => _ConfigFormState();
}

class _ConfigFormState extends State<_ConfigForm> {
  late TouchConfig _draft;
  bool _saving = false;

  static const List<int> _rotations = <int>[0, 90, 180, 270];

  @override
  void initState() {
    super.initState();
    _draft = widget.config;
  }

  @override
  void didUpdateWidget(covariant _ConfigForm oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.config.revision != widget.config.revision) {
      _draft = widget.config;
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
              title: const Text('启用触摸'),
              subtitle: const Text('关闭后仍可用按键、语音和灯光交互'),
              value: _draft.enabled,
              onChanged: (value) => setState(
                () => _draft = _draft.copyWith(enabled: value),
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: _NumberSlider(
            title: '触摸灵敏度',
            value: _draft.sensitivity.toDouble(),
            min: 10,
            max: 100,
            unit: '%',
            divisions: 18,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(sensitivity: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: _NumberSlider(
            title: '长按判定',
            value: _draft.longPressMs.toDouble(),
            min: 250,
            max: 3000,
            unit: '毫秒',
            divisions: 55,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(longPressMs: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 120),
          child: _NumberSlider(
            title: '双击判定',
            value: _draft.doubleTapMs.toDouble(),
            min: 120,
            max: 600,
            unit: '毫秒',
            divisions: 48,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(doubleTapMs: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 160),
          child: _NumberSlider(
            title: '滑动距离',
            value: _draft.swipeDistancePx.toDouble(),
            min: 16,
            max: 480,
            unit: '像素',
            divisions: 29,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(swipeDistancePx: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 200),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '屏幕方向',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    children: [
                      for (final rotation in _rotations)
                        ChoiceChip(
                          label: Text('$rotation°'),
                          selected: _draft.rotationDegrees == rotation,
                          onSelected: (_) => setState(
                            () => _draft = _draft.copyWith(
                              rotationDegrees: rotation,
                            ),
                          ),
                        ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 240),
          child: Card(
            child: Column(
              children: [
                SwitchListTile(
                  title: const Text('点击'),
                  value: _draft.tapEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(tapEnabled: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('滑动'),
                  value: _draft.swipeEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(swipeEnabled: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('长按'),
                  value: _draft.longPressEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(longPressEnabled: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('触觉反馈'),
                  value: _draft.hapticEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(hapticEnabled: value),
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
          label: const Text('保存触摸设置'),
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
