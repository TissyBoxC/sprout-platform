import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
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
  late int _policyVersion;
  bool _isSaving = false;
  String? _errorMessage;
  String? _dailyLimitError;
  String? _volumeError;
  String? _disabledPeriodError;
  String? _categoryError;
  bool _showVersionConflict = false;

  @override
  void initState() {
    super.initState();
    _dailyLimitMinutes = widget.initialPolicy.dailyLimitMinutes;
    _allowedCategories = widget.initialPolicy.allowedCategories.toSet();
    _disabledPeriods = [...widget.initialPolicy.disabledPeriods];
    _maxVolumePercent = widget.initialPolicy.maxVolumePercent;
    _policyVersion = widget.initialPolicy.policyVersion;
  }

  bool get _hasUnsavedChanges {
    final initial = widget.initialPolicy;
    return _dailyLimitMinutes != initial.dailyLimitMinutes ||
        _maxVolumePercent != initial.maxVolumePercent ||
        !_sameCategories(_allowedCategories, initial.allowedCategories) ||
        !_samePeriods(_disabledPeriods, initial.disabledPeriods);
  }

  Future<void> _save() async {
    if (!_validateForm()) {
      return;
    }
    setState(() {
      _isSaving = true;
      _errorMessage = null;
      _showVersionConflict = false;
    });
    try {
      await ref
          .read(parentPolicyControllerProvider(widget.childId).notifier)
          .savePolicy(
            ParentPolicyDraft(
              policyVersion: _policyVersion,
              dailyLimitMinutes: _dailyLimitMinutes,
              allowedCategories: _allowedCategories.toList(growable: false),
              disabledPeriods: _disabledPeriods,
              maxVolumePercent: _maxVolumePercent,
            ),
          );
      if (mounted) {
        final savedPolicy = ref
            .read(parentPolicyControllerProvider(widget.childId))
            .value;
        if (savedPolicy != null) {
          _policyVersion = savedPolicy.policyVersion;
        }
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('时间与内容设置已保存')));
      }
    } on Object catch (error) {
      if (mounted) {
        final isConflict =
            error is AppException && error.kind == AppErrorKind.conflict;
        setState(() {
          _showVersionConflict = isConflict;
          _errorMessage = isConflict
              ? '设置已在其他页面更新，请重新加载后再修改'
              : parentPolicyErrorMessage(error);
        });
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
      setState(() => _disabledPeriodError = '开始时间和结束时间不能相同');
      return;
    }
    final period = DisabledPeriod(startTime: startTime, endTime: endTime);
    final alreadyExists = _disabledPeriods.any(
      (item) =>
          item.startTime == period.startTime && item.endTime == period.endTime,
    );
    if (alreadyExists) {
      setState(() => _disabledPeriodError = '这个免打扰时段已经存在');
      return;
    }
    setState(() {
      _disabledPeriods = [..._disabledPeriods, period];
      _errorMessage = null;
      _disabledPeriodError = null;
    });
  }

  bool _validateForm() {
    final dailyLimitError = _dailyLimitMinutes < 0 || _dailyLimitMinutes > 720
        ? '每日时长需要在 0 到 720 分钟之间'
        : null;
    final volumeError = _maxVolumePercent < 0 || _maxVolumePercent > 100
        ? '最大音量需要在 0 到 100 之间'
        : null;
    final categoryError = _allowedCategories.isEmpty ? '请至少保留一个内容分类' : null;
    final periodError = _validateDisabledPeriods(_disabledPeriods);
    setState(() {
      _dailyLimitError = dailyLimitError;
      _volumeError = volumeError;
      _categoryError = categoryError;
      _disabledPeriodError = periodError;
      _errorMessage = null;
    });
    return dailyLimitError == null &&
        volumeError == null &&
        categoryError == null &&
        periodError == null;
  }

  Future<void> _reloadAfterConflict() async {
    await ref
        .read(parentPolicyControllerProvider(widget.childId).notifier)
        .refresh();
    if (!mounted) {
      return;
    }
    final refreshed = ref
        .read(parentPolicyControllerProvider(widget.childId))
        .value;
    if (refreshed == null) {
      return;
    }
    setState(() {
      _dailyLimitMinutes = refreshed.dailyLimitMinutes;
      _allowedCategories = refreshed.allowedCategories.toSet();
      _disabledPeriods = [...refreshed.disabledPeriods];
      _maxVolumePercent = refreshed.maxVolumePercent;
      _policyVersion = refreshed.policyVersion;
      _showVersionConflict = false;
      _errorMessage = null;
    });
  }

  Future<bool> _confirmLeave() async {
    if (!_hasUnsavedChanges) {
      return true;
    }
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('放弃未保存的修改？'),
        content: const Text('离开后这次修改不会保存。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('继续编辑'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('放弃修改'),
          ),
        ],
      ),
    );
    return confirmed == true;
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) async {
        if (!didPop && await _confirmLeave() && context.mounted) {
          Navigator.of(context).pop();
        }
      },
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          if (_showVersionConflict)
            Padding(
              padding: const EdgeInsets.only(bottom: 14),
              child: Material(
                color: Theme.of(context).colorScheme.errorContainer,
                borderRadius: BorderRadius.circular(18),
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '设置已被更新',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 6),
                      const Text('请重新加载最新设置后再修改，以免覆盖其他入口的调整。'),
                      const SizedBox(height: 12),
                      OutlinedButton.icon(
                        onPressed: _reloadAfterConflict,
                        icon: const Icon(Icons.refresh_rounded),
                        label: const Text('重新加载设置'),
                      ),
                    ],
                  ),
                ),
              ),
            ),
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
                              () => _dailyLimitMinutes =
                                  (value / 30).round() * 30,
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
                  if (_dailyLimitError != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Text(
                        _dailyLimitError!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
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
                  if (_categoryError != null)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 8),
                      child: Text(
                        _categoryError!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
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
                    for (
                      var index = 0;
                      index < _disabledPeriods.length;
                      index++
                    )
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: const Icon(Icons.do_not_disturb_on_outlined),
                        title: Text(_periodLabel(_disabledPeriods[index])),
                        trailing: IconButton(
                          tooltip: '移除这个时段',
                          onPressed: () {
                            setState(() {
                              _disabledPeriods = [..._disabledPeriods]
                                ..removeAt(index);
                              _disabledPeriodError = null;
                            });
                          },
                          icon: const Icon(Icons.close_rounded),
                        ),
                      ),
                  if (_disabledPeriodError != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Text(
                        _disabledPeriodError!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
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
                  if (_volumeError != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Text(
                        _volumeError!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
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
      ),
    );
  }
}

bool _sameCategories(
  Set<ChildContentCategory> selected,
  List<ChildContentCategory> initial,
) {
  return selected.length == initial.length && selected.every(initial.contains);
}

bool _samePeriods(List<DisabledPeriod> selected, List<DisabledPeriod> initial) {
  if (selected.length != initial.length) {
    return false;
  }
  for (final period in selected) {
    if (!initial.any(
      (item) =>
          item.startTime == period.startTime && item.endTime == period.endTime,
    )) {
      return false;
    }
  }
  return true;
}

String? _validateDisabledPeriods(List<DisabledPeriod> periods) {
  final seen = <String>{};
  for (final period in periods) {
    if (period.startTime == period.endTime) {
      return '开始时间和结束时间不能相同';
    }
    final key = '${period.startTime}-${period.endTime}';
    if (!seen.add(key)) {
      return '这个免打扰时段已经存在';
    }
  }
  return null;
}

String _periodLabel(DisabledPeriod period) {
  final crossesMidnight = period.startTime.compareTo(period.endTime) > 0;
  final suffix = crossesMidnight ? '（跨天）' : '';
  return '${period.startTime} 至 ${period.endTime}$suffix';
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
