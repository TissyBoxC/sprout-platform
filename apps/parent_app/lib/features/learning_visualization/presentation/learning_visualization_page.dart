import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/learning_visualization_controller.dart';
import '../domain/learning_visualization.dart';

/// Guardian view of the family learning-visualization policy and device status.
class LearningVisualizationPage extends ConsumerWidget {
  const LearningVisualizationPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(learningVisualizationControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('学习可视化'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (value) => value.isEmpty
              ? 'learning-visualization-empty'
              : 'learning-visualization-content',
          error: (_, _) => 'learning-visualization-error',
          loading: () => 'learning-visualization-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('learning-visualization-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _LearningVisualizationErrorState(
            key: const ValueKey<String>('learning-visualization-error'),
            message: LearningVisualizationController.messageFor(error),
            onRetry: () => ref
                .read(learningVisualizationControllerProvider.notifier)
                .refresh(),
          ),
          data: (value) => _LearningVisualizationContent(
            key: ValueKey<String>(
              value.isEmpty
                  ? 'learning-visualization-empty'
                  : 'learning-visualization-content',
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

class _LearningVisualizationContent extends ConsumerWidget {
  const _LearningVisualizationContent({required this.state, super.key});

  final LearningVisualizationState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(
      learningVisualizationControllerProvider.notifier,
    );
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

  final LearningVisualizationState state;

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
                const Icon(Icons.auto_awesome_rounded),
                const SizedBox(width: 10),
                Text('可视化设置', style: Theme.of(context).textTheme.titleMedium),
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
              policy.enabled ? '已开启：学习时会按设置显示动画和反馈。' : '未开启：设备不会显示学习动画和反馈效果。',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            const SizedBox(height: 12),
            _InfoRow(
              icon: Icons.emoji_emotions_outlined,
              label: '反馈风格',
              value: policy.feedbackStyle.label,
            ),
            _InfoRow(
              icon: Icons.animation_rounded,
              label: '动画级别',
              value: policy.animationLevel.label,
            ),
            _InfoRow(
              icon: Icons.touch_app_rounded,
              label: '触摸反馈',
              value: policy.touchFeedbackEnabled ? '开启' : '关闭',
            ),
            _InfoRow(
              icon: Icons.workspace_premium_rounded,
              label: '奖励特效',
              value: policy.rewardEffectsEnabled ? '开启' : '关闭',
            ),
            _InfoRow(
              icon: Icons.menu_book_rounded,
              label: '课程联动',
              value: policy.lessonLinkageEnabled ? '开启' : '关闭',
            ),
            _InfoRow(
              icon: Icons.timer_outlined,
              label: '单次时长',
              value: '${policy.maxSessionMinutes} 分钟',
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

  final LearningVisualizationState state;

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
              '学习中 ${state.runningCount}',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
        const SizedBox(height: 8),
        if (state.devices.isEmpty)
          const _EmptyCard(
            icon: Icons.hide_image_outlined,
            message: '还没有设备上报学习可视化状态。',
          )
        else
          for (final device in state.devices) _DeviceTile(device: device),
      ],
    );
  }
}

class _DeviceTile extends StatelessWidget {
  const _DeviceTile({required this.device});

  final LearningVisualizationDevice device;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        leading: Icon(
          device.running
              ? Icons.play_circle_outline_rounded
              : Icons.auto_awesome_outlined,
          color: device.running ? Theme.of(context).colorScheme.primary : null,
        ),
        title: Text(_shortId(device.deviceId)),
        subtitle: Text(
          device.supported
              ? '${visualStateLabel(device.visualState)} · '
                    '动画 ${animationLevelLabel(device.animationLevel)} · '
                    '反馈 ${device.feedbackEvents} · '
                    '${device.sessionMinutes} 分钟'
              : '此设备不支持学习可视化',
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

class _LearningVisualizationErrorState extends StatelessWidget {
  const _LearningVisualizationErrorState({
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
  LearningVisualizationPolicy policy, {
  bool? enabled,
}) async {
  final draft = LearningVisualizationDraft(
    enabled: enabled ?? policy.enabled,
    feedbackStyle: policy.feedbackStyle,
    animationLevel: policy.animationLevel,
    touchFeedbackEnabled: policy.touchFeedbackEnabled,
    rewardEffectsEnabled: policy.rewardEffectsEnabled,
    lessonLinkageEnabled: policy.lessonLinkageEnabled,
    maxSessionMinutes: policy.maxSessionMinutes,
  );
  try {
    await ref
        .read(learningVisualizationControllerProvider.notifier)
        .publishPolicy(draft);
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(LearningVisualizationController.messageFor(error)),
        ),
      );
    }
  }
}

Future<void> _openEditor(
  BuildContext context,
  WidgetRef ref,
  LearningVisualizationPolicy policy,
) {
  return Navigator.of(context).push<void>(
    MaterialPageRoute<void>(
      builder: (context) => _LearningVisualizationEditPage(policy: policy),
    ),
  );
}

String _shortId(String deviceId) {
  if (deviceId.length <= 12) {
    return deviceId.isEmpty ? '未知设备' : deviceId;
  }
  return '${deviceId.substring(0, 6)}…${deviceId.substring(deviceId.length - 4)}';
}

class _LearningVisualizationEditPage extends ConsumerStatefulWidget {
  const _LearningVisualizationEditPage({required this.policy});

  final LearningVisualizationPolicy policy;

  @override
  ConsumerState<_LearningVisualizationEditPage> createState() =>
      _LearningVisualizationEditPageState();
}

class _LearningVisualizationEditPageState
    extends ConsumerState<_LearningVisualizationEditPage> {
  late bool _enabled;
  late LearningFeedbackStyle _feedbackStyle;
  late LearningAnimationLevel _animationLevel;
  late bool _touchFeedbackEnabled;
  late bool _rewardEffectsEnabled;
  late bool _lessonLinkageEnabled;
  late double _maxSessionMinutes;
  bool _isSubmitting = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    final policy = widget.policy;
    _enabled = policy.enabled;
    _feedbackStyle = policy.feedbackStyle;
    _animationLevel = policy.animationLevel;
    _touchFeedbackEnabled = policy.touchFeedbackEnabled;
    _rewardEffectsEnabled = policy.rewardEffectsEnabled;
    _lessonLinkageEnabled = policy.lessonLinkageEnabled;
    _maxSessionMinutes = policy.maxSessionMinutes.toDouble();
  }

  Future<void> _submit() async {
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    final draft = LearningVisualizationDraft(
      enabled: _enabled,
      feedbackStyle: _feedbackStyle,
      animationLevel: _animationLevel,
      touchFeedbackEnabled: _touchFeedbackEnabled,
      rewardEffectsEnabled: _rewardEffectsEnabled,
      lessonLinkageEnabled: _lessonLinkageEnabled,
      maxSessionMinutes: _maxSessionMinutes.round(),
    );
    try {
      await ref
          .read(learningVisualizationControllerProvider.notifier)
          .publishPolicy(draft);
      if (mounted) {
        Navigator.of(context).pop();
      }
    } on Object catch (error) {
      if (mounted) {
        setState(
          () =>
              _errorMessage = LearningVisualizationController.messageFor(error),
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
      appBar: AppBar(title: const Text('学习可视化设置')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('开启学习可视化'),
              subtitle: const Text('关闭后设备不会显示学习动画和反馈效果'),
              value: _enabled,
              onChanged: (value) => setState(() => _enabled = value),
            ),
            const Divider(),
            DropdownButtonFormField<LearningFeedbackStyle>(
              initialValue: _feedbackStyle,
              decoration: const InputDecoration(labelText: '反馈风格'),
              items: [
                for (final style in LearningFeedbackStyle.values)
                  DropdownMenuItem<LearningFeedbackStyle>(
                    value: style,
                    child: Text(style.label),
                  ),
              ],
              onChanged: _enabled
                  ? (value) =>
                        setState(() => _feedbackStyle = value ?? _feedbackStyle)
                  : null,
            ),
            const SizedBox(height: 16),
            DropdownButtonFormField<LearningAnimationLevel>(
              initialValue: _animationLevel,
              decoration: const InputDecoration(labelText: '动画级别'),
              items: [
                for (final level in LearningAnimationLevel.values)
                  DropdownMenuItem<LearningAnimationLevel>(
                    value: level,
                    child: Text(level.label),
                  ),
              ],
              onChanged: _enabled
                  ? (value) => setState(
                      () => _animationLevel = value ?? _animationLevel,
                    )
                  : null,
            ),
            const SizedBox(height: 12),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('触摸反馈'),
              subtitle: const Text('孩子点击屏幕时给出视觉反馈'),
              value: _touchFeedbackEnabled && _enabled,
              onChanged: _enabled
                  ? (value) => setState(() => _touchFeedbackEnabled = value)
                  : null,
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('奖励特效'),
              subtitle: const Text('完成学习任务时显示庆祝动画'),
              value: _rewardEffectsEnabled && _enabled,
              onChanged: _enabled
                  ? (value) => setState(() => _rewardEffectsEnabled = value)
                  : null,
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('课程联动'),
              subtitle: const Text('可视化内容跟随当前课程'),
              value: _lessonLinkageEnabled && _enabled,
              onChanged: _enabled
                  ? (value) => setState(() => _lessonLinkageEnabled = value)
                  : null,
            ),
            const SizedBox(height: 8),
            Text('单次时长：${_maxSessionMinutes.round()} 分钟'),
            Slider(
              value: _maxSessionMinutes,
              min: 1,
              max: 60,
              divisions: 59,
              label: '${_maxSessionMinutes.round()} 分钟',
              onChanged: _enabled
                  ? (value) => setState(() => _maxSessionMinutes = value)
                  : null,
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
