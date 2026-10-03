import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_state_switcher.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/parent_policy_controller.dart';
import '../domain/parent_policy.dart';

/// Edits one child's daily time, content, quiet hours, and volume limits.
class ParentPolicyPage extends ConsumerWidget {
  const ParentPolicyPage({required this.childId, super.key});

  final String childId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final policy = ref.watch(parentPolicyControllerProvider(childId));
    final children = ref.watch(childProfileControllerProvider).value;
    final child = children
        ?.where((profile) => profile.childId == childId)
        .firstOrNull;
    return Scaffold(
      appBar: AppBar(
        title: Text(child == null ? '时间与内容' : '${child.nickname}的时间与内容'),
      ),
      body: AppStateSwitcher(
        stateKey: policy.when(
          data: (_) => 'policy-content',
          error: (_, _) => 'policy-error',
          loading: () => 'policy-loading',
        ),
        child: policy.when(
          loading: () => const Center(
            key: ValueKey<String>('policy-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _PolicyErrorState(
            key: const ValueKey<String>('policy-error'),
            message: parentPolicyErrorMessage(error),
            onRetry: () => ref
                .read(parentPolicyControllerProvider(childId).notifier)
                .refresh(),
          ),
          data: (value) => _ParentPolicyForm(
            key: const ValueKey<String>('policy-content'),
            childId: childId,
            initialPolicy: value,
          ),
        ),
      ),
    );
  }
}

class _PolicyErrorState extends StatelessWidget {
  const _PolicyErrorState({
    required this.message,
    required this.onRetry,
    super.key,
  });

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
            const Icon(Icons.cloud_off_outlined, size: 42),
            const SizedBox(height: 12),
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

class _ParentPolicyForm extends ConsumerStatefulWidget {
  const _ParentPolicyForm({
    required this.childId,
    required this.initialPolicy,
    super.key,
  });

  final String childId;
  final ParentPolicy initialPolicy;

  @override
  ConsumerState<_ParentPolicyForm> createState() => _ParentPolicyFormState();
}

class _ParentPolicyFormState extends ConsumerState<_ParentPolicyForm> {
  late int _dailyLimitMinutes;
  late Set<ChildContentCategory> _allowedCategories;
  late List<DisabledPeriod> _disabledPeriods;
  late int _maxVolumePercent;
  bool _isSaving = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _dailyLimitMinutes = widget.initialPolicy.dailyLimitMinutes;
    _allowedCategories = widget.initialPolicy.allowedCategories.toSet();
    _disabledPeriods = [...widget.initialPolicy.disabledPeriods];
    _maxVolumePercent = widget.initialPolicy.maxVolumePercent;
  }

  Future<void> _save() async {
    if (_allowedCategories.isEmpty) {
      setState(() => _errorMessage = '请至少保留一个内容分类');
      return;
    }
    setState(() {
      _isSaving = true;
      _errorMessage = null;
    });
    try {
      await ref
          .read(parentPolicyControllerProvider(widget.childId).notifier)
          .savePolicy(
            ParentPolicyDraft(
              dailyLimitMinutes: _dailyLimitMinutes,
              allowedCategories: _allowedCategories.toList(growable: false),
              disabledPeriods: _disabledPeriods,
              maxVolumePercent: _maxVolumePercent,
            ),
          );
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('时间与内容设置已保存')));
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = parentPolicyErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isSaving = false);
      }
    }
  }

  Future<void> _pickDisabledPeriod() async {
    final start = await showTimePicker(
      context: context,
      initialTime: const TimeOfDay(hour: 12, minute: 0),
      helpText: '选择开始时间',
    );
    if (start == null || !mounted) {
      return;
    }
    final end = await showTimePicker(
      context: context,
      initialTime: const TimeOfDay(hour: 14, minute: 0),
      helpText: '选择结束时间',
    );
    if (end == null) {
      return;
    }
    final startTime = _formatTimeOfDay(start);
    final endTime = _formatTimeOfDay(end);
    if (startTime == endTime) {
      setState(() => _errorMessage = '开始时间和结束时间不能相同');
      return;
    }
    final period = DisabledPeriod(startTime: startTime, endTime: endTime);
    final alreadyExists = _disabledPeriods.any(
      (item) =>
          item.startTime == period.startTime && item.endTime == period.endTime,
    );
    if (alreadyExists) {
      setState(() => _errorMessage = '这个免打扰时段已经存在');
      return;
    }
    setState(() {
      _disabledPeriods = [..._disabledPeriods, period];
      _errorMessage = null;
    });
  }

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: [
        Card(
          child: Padding(
            padding: const EdgeInsets.all(18),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    const Icon(Icons.schedule_rounded),
                    const SizedBox(width: 10),
                    Text(
                      '每日使用时长',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                const Text('0 分钟表示不限制，最长可以设置为 720 分钟。'),
                const SizedBox(height: 12),
                Row(
                  children: [
                    Expanded(
                      child: Slider(
                        value: _dailyLimitMinutes.toDouble(),
                        min: 0,
                        max: 720,
                        divisions: 24,
                        label: _dailyLimitLabel(_dailyLimitMinutes),
                        onChanged: (value) {
                          setState(
                            () =>
                                _dailyLimitMinutes = (value / 30).round() * 30,
                          );
                        },
                      ),
                    ),
                    SizedBox(
                      width: 92,
                      child: Text(
                        _dailyLimitLabel(_dailyLimitMinutes),
                        textAlign: TextAlign.end,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 14),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(18),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    const Icon(Icons.library_music_outlined),
                    const SizedBox(width: 10),
                    Text(
                      '允许的内容',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                const Text('关闭分类后，设备不会播放该分类的内容。'),
                const SizedBox(height: 8),
                for (final category in ChildContentCategory.values)
                  SwitchListTile(
                    value: _allowedCategories.contains(category),
                    onChanged: (enabled) {
                      setState(() {
                        if (enabled) {
                          _allowedCategories.add(category);
                        } else {
                          _allowedCategories.remove(category);
                        }
                      });
                    },
                    contentPadding: EdgeInsets.zero,
                    title: Text(category.label),
                  ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 14),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(18),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    const Icon(Icons.bedtime_outlined),
                    const SizedBox(width: 10),
                    Text(
                      '免打扰时段',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                const Text('例如午休或夜间，免打扰期间设备保持安静。'),
                const SizedBox(height: 8),
                if (_disabledPeriods.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 10),
                    child: Text('还没有设置免打扰时段。'),
                  )
                else
                  for (var index = 0; index < _disabledPeriods.length; index++)
                    ListTile(
                      contentPadding: EdgeInsets.zero,
                      leading: const Icon(Icons.do_not_disturb_on_outlined),
                      title: Text(
                        '${_disabledPeriods[index].startTime} 至 ${_disabledPeriods[index].endTime}',
                      ),
                      trailing: IconButton(
                        tooltip: '移除这个时段',
                        onPressed: () {
                          setState(() {
                            _disabledPeriods = [..._disabledPeriods]
                              ..removeAt(index);
                          });
                        },
                        icon: const Icon(Icons.close_rounded),
                      ),
                    ),
                const SizedBox(height: 6),
                OutlinedButton.icon(
                  onPressed: _pickDisabledPeriod,
                  icon: const Icon(Icons.add_rounded),
                  label: const Text('添加免打扰时段'),
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 14),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(18),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    const Icon(Icons.volume_down_rounded),
                    const SizedBox(width: 10),
                    Text(
                      '最大音量',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                const Text('孩子仍然可以调低音量，但不能超过这里设置的上限。'),
                const SizedBox(height: 12),
                Row(
                  children: [
                    Expanded(
                      child: Slider(
                        value: _maxVolumePercent.toDouble(),
                        min: 0,
                        max: 100,
                        divisions: 20,
                        label: '$_maxVolumePercent',
                        onChanged: (value) {
                          setState(
                            () => _maxVolumePercent = (value / 5).round() * 5,
                          );
                        },
                      ),
                    ),
                    SizedBox(
                      width: 56,
                      child: Text(
                        '$_maxVolumePercent%',
                        textAlign: TextAlign.end,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
        if (_errorMessage != null) ...[
          const SizedBox(height: 14),
          Text(
            _errorMessage!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        ],
        const SizedBox(height: 24),
        FilledButton(
          onPressed: _isSaving ? null : _save,
          child: Text(_isSaving ? '正在保存…' : '保存时间与内容设置'),
        ),
      ],
    );
  }
}

String _dailyLimitLabel(int minutes) {
  if (minutes == 0) {
    return '不限制';
  }
  return '$minutes 分钟';
}

String _formatTimeOfDay(TimeOfDay time) {
  final hour = time.hour.toString().padLeft(2, '0');
  final minute = time.minute.toString().padLeft(2, '0');
  return '$hour:$minute';
}
