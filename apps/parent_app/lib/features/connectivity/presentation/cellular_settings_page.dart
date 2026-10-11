import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/cellular_controller.dart';
import '../domain/cellular_policy.dart';

/// Guardian view of the family cellular policy.
///
/// The page edits bounded scalars only: enable flags, radio preference, roaming,
/// monthly quota, low-quota warning and keepalive. No subscriber identifiers or
/// call detail are captured.
class CellularSettingsPage extends ConsumerWidget {
  const CellularSettingsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final policy = ref.watch(cellularControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('蜂窝网络'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: policy.when(
          data: (_) => 'cellular-policy-data',
          error: (_, _) => 'cellular-policy-error',
          loading: () => 'cellular-policy-loading',
        ),
        child: policy.when(
          loading: () => const Center(
            key: ValueKey<String>('cellular-policy-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('cellular-policy-error'),
            message: CellularController.messageFor(error),
            onRetry: () =>
                ref.read(cellularControllerProvider.notifier).refresh(),
          ),
          data: (value) => _PolicyForm(
            key: const ValueKey<String>('cellular-policy-data'),
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
    CellularPolicy policy,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(cellularControllerProvider.notifier).save(policy);
      messenger.showSnackBar(const SnackBar(content: Text('蜂窝网络设置已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(CellularController.messageFor(error))),
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

  final CellularPolicy policy;
  final Future<void> Function(CellularPolicy policy) onSave;

  @override
  State<_PolicyForm> createState() => _PolicyFormState();
}

class _PolicyFormState extends State<_PolicyForm> {
  late CellularPolicy _draft;
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
              title: const Text('启用蜂窝网络'),
              subtitle: const Text('关闭后设备仅使用 Wi-Fi'),
              value: _draft.enabled,
              onChanged: (value) =>
                  setState(() => _draft = _draft.copyWith(enabled: value)),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('网络优先级', style: Theme.of(context).textTheme.titleMedium),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    children: [
                      _PreferenceChip(
                        label: '优先 Wi-Fi',
                        value: RadioPreference.wifiFirst,
                        selected: _draft.preference,
                        onSelected: _setPreference,
                      ),
                      _PreferenceChip(
                        label: '优先蜂窝',
                        value: RadioPreference.cellularFirst,
                        selected: _draft.preference,
                        onSelected: _setPreference,
                      ),
                      _PreferenceChip(
                        label: '仅蜂窝',
                        value: RadioPreference.cellularOnly,
                        selected: _draft.preference,
                        onSelected: _setPreference,
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
          delay: const Duration(milliseconds: 80),
          child: _NumberSlider(
            title: '月度流量上限',
            value: _draft.monthlyQuotaMb.toDouble(),
            min: 0,
            max: 20480,
            step: 256,
            unit: 'MB',
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(monthlyQuotaMb: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 120),
          child: _NumberSlider(
            title: '低流量提醒',
            value: _draft.lowQuotaWarnPercent.toDouble(),
            min: 5,
            max: 100,
            unit: '%',
            divisions: 19,
            onChanged: (value) => setState(
              () =>
                  _draft = _draft.copyWith(lowQuotaWarnPercent: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 160),
          child: _NumberSlider(
            title: '保活间隔',
            value: _draft.keepaliveSeconds.toDouble(),
            min: 30,
            max: 3600,
            step: 30,
            unit: '秒',
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(keepaliveSeconds: value.round()),
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
                  title: const Text('允许漫游'),
                  value: _draft.roamingAllowed,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(roamingAllowed: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('远程唤醒'),
                  subtitle: const Text('允许平台按策略唤醒设备'),
                  value: _draft.remoteWakeEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(remoteWakeEnabled: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('计量网络限制上传'),
                  subtitle: const Text('达到流量上限后停止非必要上传'),
                  value: _draft.meteredUploadLimit,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(meteredUploadLimit: value),
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
          label: const Text('保存蜂窝网络设置'),
        ),
      ],
    );
  }

  void _setPreference(RadioPreference preference) {
    setState(() => _draft = _draft.copyWith(preference: preference));
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

class _PreferenceChip extends StatelessWidget {
  const _PreferenceChip({
    required this.label,
    required this.value,
    required this.selected,
    required this.onSelected,
  });

  final String label;
  final RadioPreference value;
  final RadioPreference selected;
  final ValueChanged<RadioPreference> onSelected;

  @override
  Widget build(BuildContext context) {
    return ChoiceChip(
      label: Text(label),
      selected: selected == value,
      onSelected: (_) => onSelected(value),
    );
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
