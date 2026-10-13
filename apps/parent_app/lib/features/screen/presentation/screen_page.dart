import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/screen_controller.dart';
import '../domain/screen.dart';

/// Guardian view of the family screen surface.
///
/// The page edits the default expression, animation and point-to-read settings
/// and the point-to-read dictionary. It only ever sends bounded names and text;
/// no pixels or frames leave the device.
class ScreenPage extends ConsumerWidget {
  const ScreenPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(screenControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('屏幕'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (_) => 'screen-data',
          error: (_, _) => 'screen-error',
          loading: () => 'screen-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('screen-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('screen-error'),
            message: ScreenController.messageFor(error),
            onRetry: () =>
                ref.read(screenControllerProvider.notifier).refresh(),
          ),
          data: (value) => _ScreenBody(
            key: const ValueKey<String>('screen-data'),
            state: value,
            onSavePolicy: (policy) => _savePolicy(context, ref, policy),
            onAddEntry: () => _addEntry(context, ref, value.catalog),
            onDeleteEntry: (id) => _deleteEntry(context, ref, id),
            onFetchPlayback: () => _fetchPlayback(context, ref),
            onResolvePlayback: () => _resolvePlayback(context, ref, value),
          ),
        ),
      ),
    );
  }

  Future<void> _savePolicy(
    BuildContext context,
    WidgetRef ref,
    ScreenPolicy policy,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(screenControllerProvider.notifier).savePolicy(policy);
      messenger.showSnackBar(const SnackBar(content: Text('屏幕设置已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(ScreenController.messageFor(error))),
      );
    }
  }

  Future<void> _addEntry(
    BuildContext context,
    WidgetRef ref,
    ScreenCatalog catalog,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    final draft = await showDialog<_EntryDraft>(
      context: context,
      builder: (dialogContext) => const _EntryDialog(),
    );
    if (draft == null) {
      return;
    }
    try {
      await ref
          .read(screenControllerProvider.notifier)
          .putEntry(term: draft.term, text: draft.text);
      messenger.showSnackBar(const SnackBar(content: Text('点读词条已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(ScreenController.messageFor(error))),
      );
    }
  }

  Future<void> _deleteEntry(
    BuildContext context,
    WidgetRef ref,
    String entryId,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(screenControllerProvider.notifier).deleteEntry(entryId);
      messenger.showSnackBar(const SnackBar(content: Text('点读词条已删除')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(ScreenController.messageFor(error))),
      );
    }
  }

  Future<void> _resolvePlayback(
    BuildContext context,
    WidgetRef ref,
    ScreenState current,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    final deviceId = await showDialog<String>(
      context: context,
      builder: (dialogContext) => const _DeviceDialog(),
    );
    if (deviceId == null || deviceId.isEmpty) {
      return;
    }
    try {
      final plan = await ref
          .read(screenControllerProvider.notifier)
          .resolvePlayback(
            deviceId: deviceId,
            bitrateKbps: current.policy.videoMaxBitrateKbps,
            fps: current.policy.videoMaxFps,
          );
      messenger.showSnackBar(
        SnackBar(
          content: Text('已下发 ${plan.bitrateKbps} kbps · ${plan.fps} fps'),
        ),
      );
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(ScreenController.messageFor(error))),
      );
    }
  }

  Future<void> _fetchPlayback(BuildContext context, WidgetRef ref) async {
    final messenger = ScaffoldMessenger.of(context);
    final deviceId = await showDialog<String>(
      context: context,
      builder: (dialogContext) => const _DeviceDialog(),
    );
    if (deviceId == null || deviceId.isEmpty) {
      return;
    }
    try {
      final plan = await ref
          .read(screenControllerProvider.notifier)
          .fetchPlayback(deviceId);
      messenger.showSnackBar(
        SnackBar(
          content: Text('当前参数 ${plan.bitrateKbps} kbps · ${plan.fps} fps'),
        ),
      );
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(ScreenController.messageFor(error))),
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

class _ScreenBody extends StatefulWidget {
  const _ScreenBody({
    required this.state,
    required this.onSavePolicy,
    required this.onAddEntry,
    required this.onDeleteEntry,
    required this.onFetchPlayback,
    required this.onResolvePlayback,
    super.key,
  });

  final ScreenState state;
  final Future<void> Function(ScreenPolicy policy) onSavePolicy;
  final VoidCallback onAddEntry;
  final ValueChanged<String> onDeleteEntry;
  final VoidCallback onFetchPlayback;
  final VoidCallback onResolvePlayback;

  @override
  State<_ScreenBody> createState() => _ScreenBodyState();
}

class _ScreenBodyState extends State<_ScreenBody> {
  late ScreenPolicy _draft;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _draft = widget.state.policy;
  }

  @override
  void didUpdateWidget(covariant _ScreenBody oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.state.policy.revision != widget.state.policy.revision) {
      _draft = widget.state.policy;
    }
  }

  @override
  Widget build(BuildContext context) {
    final catalog = widget.state.catalog;
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: [
        AppReveal(
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('默认表情', style: Theme.of(context).textTheme.titleMedium),
                  const SizedBox(height: 8),
                  DropdownButtonFormField<String>(
                    initialValue:
                        catalog.expressions.contains(_draft.defaultExpression)
                        ? _draft.defaultExpression
                        : 'happy',
                    decoration: const InputDecoration(labelText: '表情'),
                    items: [
                      for (final expression in catalog.expressions)
                        DropdownMenuItem(
                          value: expression,
                          child: Text(_expressionLabel(expression)),
                        ),
                    ],
                    onChanged: (value) => setState(
                      () => _draft = _draft.copyWith(
                        defaultExpression: value ?? _draft.defaultExpression,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: Card(
            child: Column(
              children: [
                SwitchListTile(
                  title: const Text('表情动画'),
                  subtitle: const Text('眨眼、挥手、点头和舞蹈动画'),
                  value: _draft.animationEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(animationEnabled: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('点读'),
                  subtitle: const Text('孩子点读时显示并朗读词条'),
                  value: _draft.pointReadEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(pointReadEnabled: value),
                  ),
                ),
                SwitchListTile(
                  title: const Text('视频播放'),
                  subtitle: const Text('允许在屏幕上播放视频'),
                  value: _draft.videoEnabled,
                  onChanged: (value) => setState(
                    () => _draft = _draft.copyWith(videoEnabled: value),
                  ),
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: _NumberSlider(
            title: '视频最高帧率',
            value: _draft.videoMaxFps.toDouble(),
            min: 5,
            max: 60,
            unit: 'fps',
            divisions: 55,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(videoMaxFps: value.round()),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 120),
          child: _NumberSlider(
            title: '视频最高码率',
            value: _draft.videoMaxBitrateKbps.toDouble(),
            min: 100,
            max: 8000,
            unit: 'kbps',
            divisions: 79,
            step: 100,
            onChanged: (value) => setState(
              () => _draft = _draft.copyWith(
                videoMaxBitrateKbps: (value / 100).round() * 100,
              ),
            ),
          ),
        ),
        const SizedBox(height: 24),
        FilledButton.icon(
          onPressed: _saving ? null : _submit,
          icon: const Icon(Icons.check_rounded),
          label: const Text('保存屏幕设置'),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 160),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      Text(
                        '点读词条',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      IconButton(
                        tooltip: '添加词条',
                        onPressed: widget.onAddEntry,
                        icon: const Icon(Icons.add_rounded),
                      ),
                    ],
                  ),
                  if (widget.state.dictionary.isEmpty)
                    const Text('还没有点读词条。')
                  else
                    for (final entry in widget.state.dictionary)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: const Icon(Icons.touch_app_outlined),
                        title: Text(entry.term),
                        subtitle: Text(entry.text),
                        trailing: IconButton(
                          tooltip: '删除词条',
                          onPressed: () => widget.onDeleteEntry(entry.id),
                          icon: const Icon(Icons.delete_outline_rounded),
                        ),
                      ),
                ],
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
                ListTile(
                  leading: const Icon(Icons.movie_outlined),
                  title: const Text('读取当前视频参数'),
                  subtitle: const Text('查看设备当前采用的码率和帧率'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: widget.onFetchPlayback,
                ),
                const Divider(height: 1),
                ListTile(
                  leading: const Icon(Icons.tune_rounded),
                  title: const Text('下发视频参数'),
                  subtitle: Text(
                    '按 ${_draft.videoMaxBitrateKbps} kbps · ${_draft.videoMaxFps} fps 下发到设备',
                  ),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: widget.onResolvePlayback,
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Future<void> _submit() async {
    setState(() => _saving = true);
    try {
      await widget.onSavePolicy(_draft);
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
    final displayValue = step == null
        ? value.round().toString()
        : (((value / step!).round()) * step!).toString();
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
                Text('$displayValue $unit'),
              ],
            ),
            Slider(
              value: value.clamp(min, max),
              min: min,
              max: max,
              divisions: divisions,
              label: '$displayValue $unit',
              onChanged: onChanged,
            ),
          ],
        ),
      ),
    );
  }
}

class _EntryDraft {
  const _EntryDraft({required this.term, required this.text});

  final String term;
  final String text;
}

class _EntryDialog extends StatefulWidget {
  const _EntryDialog();

  @override
  State<_EntryDialog> createState() => _EntryDialogState();
}

class _EntryDialogState extends State<_EntryDialog> {
  final TextEditingController _term = TextEditingController();
  final TextEditingController _text = TextEditingController();

  @override
  void dispose() {
    _term.dispose();
    _text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('添加点读词条'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: _term,
            decoration: const InputDecoration(labelText: '词条（如 apple）'),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _text,
            decoration: const InputDecoration(labelText: '展示或朗读内容'),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(
            context,
          ).pop(_EntryDraft(term: _term.text.trim(), text: _text.text.trim())),
          child: const Text('保存'),
        ),
      ],
    );
  }
}

class _DeviceDialog extends StatefulWidget {
  const _DeviceDialog();

  @override
  State<_DeviceDialog> createState() => _DeviceDialogState();
}

class _DeviceDialogState extends State<_DeviceDialog> {
  final TextEditingController _deviceId = TextEditingController();

  @override
  void dispose() {
    _deviceId.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('选择设备'),
      content: TextField(
        controller: _deviceId,
        decoration: const InputDecoration(labelText: '设备 ID'),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(_deviceId.text.trim()),
          child: const Text('下发'),
        ),
      ],
    );
  }
}

String _expressionLabel(String expression) {
  return ({
        'neutral': '平静',
        'happy': '开心',
        'sad': '难过',
        'curious': '好奇',
        'sleepy': '困了',
        'surprised': '惊讶',
        'excited': '兴奋',
      }[expression] ??
      expression);
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
