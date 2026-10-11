import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/alarm_reminder_controller.dart';
import '../domain/alarm_reminder.dart';

/// Guardian view of the family alarm schedule and its recent firing history.
class AlarmReminderPage extends ConsumerWidget {
  const AlarmReminderPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(alarmReminderControllerProvider);
    final children = ref.watch(childProfileControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('定时提醒'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _openEditor(
          context,
          ref,
          children.value ?? const <ChildProfile>[],
          null,
        ),
        icon: const Icon(Icons.add_alarm_rounded),
        label: const Text('新建'),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (value) =>
              value.isEmpty ? 'alarm-reminder-empty' : 'alarm-reminder-content',
          error: (_, _) => 'alarm-reminder-error',
          loading: () => 'alarm-reminder-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('alarm-reminder-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _AlarmReminderErrorState(
            key: const ValueKey<String>('alarm-reminder-error'),
            message: AlarmReminderController.messageFor(error),
            onRetry: () =>
                ref.read(alarmReminderControllerProvider.notifier).refresh(),
          ),
          data: (value) => _AlarmReminderContent(
            key: ValueKey<String>(
              value.isEmpty ? 'alarm-reminder-empty' : 'alarm-reminder-content',
            ),
            state: value,
            children: children.value ?? const <ChildProfile>[],
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

class _AlarmReminderContent extends ConsumerWidget {
  const _AlarmReminderContent({
    required this.state,
    required this.children,
    super.key,
  });

  final AlarmReminderState state;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(alarmReminderControllerProvider.notifier);
    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 96),
        children: [
          AppReveal(child: _ScheduleHeader(state: state)),
          const SizedBox(height: 12),
          AppReveal(
            child: _AlarmListSection(state: state, children: children),
          ),
          const SizedBox(height: 24),
          AppReveal(child: _EventHistorySection(state: state)),
        ],
      ),
    );
  }
}

class _ScheduleHeader extends StatelessWidget {
  const _ScheduleHeader({required this.state});

  final AlarmReminderState state;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.alarm_rounded),
                const SizedBox(width: 10),
                Text('我的闹钟', style: Theme.of(context).textTheme.titleMedium),
                const Spacer(),
                Text(
                  '${state.alarms.length} 个',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              '闹钟由设备本地响铃，到点提醒孩子起床、吃饭或休息。',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
          ],
        ),
      ),
    );
  }
}

class _AlarmListSection extends ConsumerWidget {
  const _AlarmListSection({required this.state, required this.children});

  final AlarmReminderState state;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (state.alarms.isEmpty) {
      return const _EmptyCard(
        icon: Icons.alarm_add_rounded,
        message: '还没有闹钟，点击右下角新建第一个吧。',
      );
    }
    final alarms = [...state.alarms]
      ..sort((a, b) => a.alarmTime.compareTo(b.alarmTime));
    return Column(
      children: [
        for (final alarm in alarms)
          _AlarmTile(alarm: alarm, children: children),
      ],
    );
  }
}

class _AlarmTile extends ConsumerWidget {
  const _AlarmTile({required this.alarm, required this.children});

  final Alarm alarm;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(alarmReminderControllerProvider.notifier);
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        leading: SizedBox(
          width: 56,
          child: Text(
            alarm.alarmTime,
            style: Theme.of(context).textTheme.titleMedium?.copyWith(
              color: alarm.enabled
                  ? Theme.of(context).colorScheme.primary
                  : Theme.of(context).disabledColor,
            ),
          ),
        ),
        title: Text(alarm.label),
        subtitle: Text(
          '${formatWeekdays(alarm.weekdays)} · ${_childName(children, alarm.childId)} · '
          '贪睡 ${alarm.snoozeMinutes} 分钟',
        ),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Switch(
              value: alarm.enabled,
              onChanged: (value) => controller.setEnabled(alarm, value),
            ),
            IconButton(
              tooltip: '编辑',
              icon: const Icon(Icons.edit_outlined),
              onPressed: () => _openEditor(context, ref, children, alarm),
            ),
          ],
        ),
        onTap: () => _openEditor(context, ref, children, alarm),
      ),
    );
  }
}

class _EventHistorySection extends StatelessWidget {
  const _EventHistorySection({required this.state});

  final AlarmReminderState state;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Icon(Icons.history_rounded),
            const SizedBox(width: 10),
            Text('最近响铃记录', style: Theme.of(context).textTheme.titleMedium),
          ],
        ),
        const SizedBox(height: 8),
        if (state.events.isEmpty)
          const _EmptyCard(
            icon: Icons.notifications_none_rounded,
            message: '设备还没有上报响铃记录。',
          )
        else
          for (final event in state.events)
            _EventTile(
              event: event,
              label: _alarmLabel(state.alarms, event.alarmId),
            ),
      ],
    );
  }
}

class _EventTile extends StatelessWidget {
  const _EventTile({required this.event, required this.label});

  final AlarmEvent event;
  final String label;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        leading: Icon(switch (event.state) {
          AlarmState.ringing => Icons.alarm_on_rounded,
          AlarmState.snoozed => Icons.snooze_rounded,
          AlarmState.dismissed => Icons.check_circle_outline_rounded,
          AlarmState.missed => Icons.error_outline_rounded,
        }),
        title: Text(label),
        subtitle: Text('${event.alarmDate} · ${event.state.label}'),
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

class _AlarmReminderErrorState extends StatelessWidget {
  const _AlarmReminderErrorState({
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

Future<void> _openEditor(
  BuildContext context,
  WidgetRef ref,
  List<ChildProfile> children,
  Alarm? alarm,
) {
  return Navigator.of(context).push<void>(
    MaterialPageRoute<void>(
      builder: (context) => _AlarmEditPage(children: children, alarm: alarm),
    ),
  );
}

String _alarmLabel(List<Alarm> alarms, String alarmId) {
  for (final alarm in alarms) {
    if (alarm.id == alarmId) {
      return alarm.label;
    }
  }
  return '已删除的闹钟';
}

String _childName(List<ChildProfile> children, String childId) {
  if (childId.isEmpty) {
    return '全家';
  }
  for (final child in children) {
    if (child.childId == childId) {
      return child.nickname;
    }
  }
  return '宝贝';
}

class _AlarmEditPage extends ConsumerStatefulWidget {
  const _AlarmEditPage({required this.children, this.alarm});

  final List<ChildProfile> children;
  final Alarm? alarm;

  @override
  ConsumerState<_AlarmEditPage> createState() => _AlarmEditPageState();
}

class _AlarmEditPageState extends ConsumerState<_AlarmEditPage> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _labelController;
  late final TextEditingController _promptController;
  late String _alarmTime;
  late Set<int> _weekdays;
  late String _childId;
  late int _snoozeMinutes;
  bool _enabled = true;
  bool _isSubmitting = false;
  String? _errorMessage;

  bool get _isEditing => widget.alarm != null;

  @override
  void initState() {
    super.initState();
    final alarm = widget.alarm;
    _labelController = TextEditingController(text: alarm?.label ?? '');
    _promptController = TextEditingController(text: alarm?.prompt ?? '');
    _alarmTime = alarm?.alarmTime ?? '07:00';
    _weekdays = {...?alarm?.weekdays};
    _childId = alarm?.childId ?? _defaultChildId();
    _snoozeMinutes = alarm?.snoozeMinutes ?? 5;
    _enabled = alarm?.enabled ?? true;
  }

  String _defaultChildId() {
    for (final child in widget.children) {
      if (child.childId.isNotEmpty) {
        return child.childId;
      }
    }
    return '';
  }

  @override
  void dispose() {
    _labelController.dispose();
    _promptController.dispose();
    super.dispose();
  }

  Future<void> _pickTime() async {
    final parts = _alarmTime.split(':');
    final initial = TimeOfDay(
      hour: int.tryParse(parts.first) ?? 7,
      minute: parts.length > 1 ? int.tryParse(parts[1]) ?? 0 : 0,
    );
    final picked = await showTimePicker(context: context, initialTime: initial);
    if (picked != null) {
      setState(() {
        _alarmTime =
            '${picked.hour.toString().padLeft(2, '0')}:${picked.minute.toString().padLeft(2, '0')}';
      });
    }
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) {
      return;
    }
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    final draft = AlarmDraft(
      childId: _childId,
      label: _labelController.text.trim(),
      prompt: _promptController.text.trim(),
      alarmTime: _alarmTime,
      weekdays: (_weekdays.toList()..sort()),
      enabled: _enabled,
      snoozeMinutes: _snoozeMinutes,
    );
    try {
      final controller = ref.read(alarmReminderControllerProvider.notifier);
      final alarm = widget.alarm;
      if (alarm == null) {
        await controller.createAlarm(draft);
      } else {
        await controller.updateAlarm(alarm.id, draft);
      }
      if (mounted) {
        Navigator.of(context).pop();
      }
    } on Object catch (error) {
      if (mounted) {
        setState(
          () => _errorMessage = AlarmReminderController.messageFor(error),
        );
      }
    } finally {
      if (mounted) {
        setState(() => _isSubmitting = false);
      }
    }
  }

  Future<void> _confirmDelete() async {
    final alarm = widget.alarm;
    if (alarm == null) {
      return;
    }
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除闹钟'),
        content: Text('确定要删除「${alarm.label}」吗？删除后无法恢复。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true) {
      return;
    }
    await ref
        .read(alarmReminderControllerProvider.notifier)
        .deleteAlarm(alarm.id);
    if (mounted) {
      Navigator.of(context).pop();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(_isEditing ? '编辑闹钟' : '新建闹钟'),
        actions: [
          if (_isEditing)
            IconButton(
              tooltip: '删除',
              icon: const Icon(Icons.delete_outline_rounded),
              onPressed: _isSubmitting ? null : _confirmDelete,
            ),
        ],
      ),
      body: SafeArea(
        child: Form(
          key: _formKey,
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              if (widget.children.length > 1)
                DropdownButtonFormField<String>(
                  initialValue: _childId.isEmpty ? null : _childId,
                  decoration: const InputDecoration(labelText: '提醒哪个宝贝'),
                  items: [
                    for (final child in widget.children)
                      DropdownMenuItem<String>(
                        value: child.childId,
                        child: Text(child.nickname),
                      ),
                  ],
                  onChanged: (value) =>
                      setState(() => _childId = value ?? _childId),
                ),
              if (widget.children.length > 1) const SizedBox(height: 16),
              TextFormField(
                controller: _labelController,
                decoration: const InputDecoration(
                  labelText: '闹钟名称',
                  hintText: '例如：起床、午睡、刷牙',
                ),
                validator: (value) {
                  final label = value?.trim() ?? '';
                  if (label.isEmpty) {
                    return '请输入闹钟名称';
                  }
                  if (label.length > 48) {
                    return '闹钟名称不能超过 48 个字';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: _promptController,
                maxLines: 2,
                decoration: const InputDecoration(
                  labelText: '提醒语（可选）',
                  hintText: '例如：该起床啦，今天也要开心哦',
                ),
                validator: (value) {
                  if ((value?.trim().length ?? 0) > 240) {
                    return '提醒语不能超过 240 个字';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 16),
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.schedule_rounded),
                title: const Text('响铃时间'),
                subtitle: Text(_alarmTime),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: _pickTime,
              ),
              const SizedBox(height: 8),
              Text('重复', style: Theme.of(context).textTheme.bodySmall),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final entry in weekdayLabels.entries)
                    FilterChip(
                      label: Text(entry.value),
                      selected: _weekdays.contains(entry.key),
                      onSelected: (selected) => setState(() {
                        if (selected) {
                          _weekdays.add(entry.key);
                        } else {
                          _weekdays.remove(entry.key);
                        }
                      }),
                    ),
                ],
              ),
              Text(
                '不选择星期表示每天响铃。',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 16),
              DropdownButtonFormField<int>(
                initialValue: _snoozeMinutes,
                decoration: const InputDecoration(labelText: '贪睡时长'),
                items: [
                  for (final minutes in const [1, 3, 5, 10, 15, 30])
                    DropdownMenuItem<int>(
                      value: minutes,
                      child: Text('$minutes 分钟'),
                    ),
                ],
                onChanged: (value) =>
                    setState(() => _snoozeMinutes = value ?? _snoozeMinutes),
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('启用闹钟'),
                value: _enabled,
                onChanged: (value) => setState(() => _enabled = value),
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
                onPressed: _isSubmitting ? null : _submit,
                child: Text(_isSubmitting ? '正在保存…' : '保存闹钟'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
