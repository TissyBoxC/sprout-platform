import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/companionship_controller.dart';
import '../domain/companionship.dart';

/// Guardian view of the child's growth timeline and habit reminders.
class CompanionshipPage extends ConsumerWidget {
  const CompanionshipPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(companionshipControllerProvider);
    final children = ref.watch(childProfileControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('陪伴成长'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _showCreateSheet(
          context,
          ref,
          children.value ?? const <ChildProfile>[],
        ),
        icon: const Icon(Icons.add_rounded),
        label: const Text('记录'),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (value) => value.isEmpty ? 'companionship-empty' : 'companionship-content',
          error: (_, _) => 'companionship-error',
          loading: () => 'companionship-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('companionship-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _CompanionshipErrorState(
            key: const ValueKey<String>('companionship-error'),
            message: CompanionshipController.messageFor(error),
            onRetry: () =>
                ref.read(companionshipControllerProvider.notifier).refresh(),
          ),
          data: (value) => _CompanionshipContent(
            key: ValueKey<String>(
              value.isEmpty ? 'companionship-empty' : 'companionship-content',
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

class _CompanionshipContent extends ConsumerWidget {
  const _CompanionshipContent({
    required this.state,
    required this.children,
    super.key,
  });

  final CompanionshipState state;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(companionshipControllerProvider.notifier);
    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(child: _TimelineHeader(state: state, children: children)),
          const SizedBox(height: 12),
          AppReveal(child: _TimelineSection(state: state, children: children)),
          const SizedBox(height: 24),
          AppReveal(child: _ReminderSection(state: state, children: children)),
        ],
      ),
    );
  }
}

class _TimelineHeader extends ConsumerWidget {
  const _TimelineHeader({required this.state, required this.children});

  final CompanionshipState state;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(companionshipControllerProvider.notifier);
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
                Text('成长记录', style: Theme.of(context).textTheme.titleMedium),
                const Spacer(),
                Text(
                  '${state.records.length} 条',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              '记录孩子每一次尝试和进步，让陪伴看得见。',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            if (state.recordChildIds.isNotEmpty) ...[
              const SizedBox(height: 12),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  FilterChip(
                    label: const Text('全部'),
                    selected: state.childFilter == null,
                    onSelected: (_) => controller.filterByChild(null),
                  ),
                  for (final childId in state.recordChildIds)
                    FilterChip(
                      label: Text(_childName(children, childId)),
                      selected: state.childFilter == childId,
                      onSelected: (_) => controller.filterByChild(childId),
                    ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _TimelineSection extends StatelessWidget {
  const _TimelineSection({required this.state, required this.children});

  final CompanionshipState state;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context) {
    if (state.records.isEmpty) {
      return const _EmptyCard(
        icon: Icons.emoji_events_outlined,
        message: '还没有成长记录，点击右下角记录第一条吧。',
      );
    }
    return Column(
      children: [
        for (final record in state.records)
          _GrowthRecordTile(record: record, children: children),
      ],
    );
  }
}

class _GrowthRecordTile extends ConsumerWidget {
  const _GrowthRecordTile({required this.record, required this.children});

  final GrowthRecord record;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        isThreeLine: record.note.isNotEmpty,
        leading: CircleAvatar(
          backgroundColor: Theme.of(context).colorScheme.primaryContainer,
          child: Icon(_categoryIcon(record.category), size: 20),
        ),
        title: Text(record.title),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${record.category.label} · ${_childName(children, record.childId)} · ${_formatDate(record.recordedAt)}',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            if (record.note.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(record.note),
            ],
          ],
        ),
        trailing: IconButton(
          tooltip: '删除',
          icon: const Icon(Icons.delete_outline_rounded),
          onPressed: () => _confirmDelete(context, ref, record),
        ),
      ),
    );
  }
}

class _ReminderSection extends ConsumerWidget {
  const _ReminderSection({required this.state, required this.children});

  final CompanionshipState state;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Icon(Icons.alarm_rounded),
            const SizedBox(width: 10),
            Text('习惯提醒', style: Theme.of(context).textTheme.titleMedium),
          ],
        ),
        const SizedBox(height: 8),
        if (state.reminders.isEmpty)
          const _EmptyCard(
            icon: Icons.alarm_add_rounded,
            message: '还没有习惯提醒，设置后设备会按时提醒孩子。',
          )
        else
          for (final reminder in state.reminders)
            _HabitReminderTile(reminder: reminder, children: children),
      ],
    );
  }
}

class _HabitReminderTile extends ConsumerWidget {
  const _HabitReminderTile({required this.reminder, required this.children});

  final HabitReminder reminder;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        leading: Icon(
          reminder.enabled
              ? Icons.notifications_active_rounded
              : Icons.notifications_off_rounded,
        ),
        title: Text(reminder.label),
        subtitle: Text(
          '${reminder.reminderTime} · ${formatWeekdays(reminder.weekdays)} · ${_childName(children, reminder.childId)}',
        ),
        trailing: IconButton(
          tooltip: '编辑',
          icon: const Icon(Icons.edit_outlined),
          onPressed: () => _openReminderEditor(context, ref, children, reminder),
        ),
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

class _CompanionshipErrorState extends StatelessWidget {
  const _CompanionshipErrorState({
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

Future<void> _confirmDelete(
  BuildContext context,
  WidgetRef ref,
  GrowthRecord record,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('删除成长记录'),
      content: Text('确定要删除「${record.title}」吗？删除后无法恢复。'),
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
  if (confirmed == true) {
    await ref
        .read(companionshipControllerProvider.notifier)
        .deleteGrowthRecord(record.id);
  }
}

Future<void> _openReminderEditor(
  BuildContext context,
  WidgetRef ref,
  List<ChildProfile> children,
  HabitReminder? reminder,
) async {
  await Navigator.of(context).push<void>(
    MaterialPageRoute<void>(
      builder: (context) =>
          _ReminderEditPage(children: children, reminder: reminder),
    ),
  );
}

/// Lets the guardian choose whether to add a growth record or a habit
/// reminder, then opens the matching editor.
Future<void> _showCreateSheet(
  BuildContext context,
  WidgetRef ref,
  List<ChildProfile> children,
) async {
  final choice = await showModalBottomSheet<String>(
    context: context,
    showDragHandle: true,
    builder: (context) => SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          ListTile(
            leading: const Icon(Icons.auto_awesome_rounded),
            title: const Text('记录成长瞬间'),
            subtitle: const Text('里程碑、鼓励、习惯和情绪都可以记下来'),
            onTap: () => Navigator.of(context).pop('record'),
          ),
          ListTile(
            leading: const Icon(Icons.alarm_add_rounded),
            title: const Text('新建习惯提醒'),
            subtitle: const Text('设置设备按时提醒孩子'),
            onTap: () => Navigator.of(context).pop('reminder'),
          ),
        ],
      ),
    ),
  );
  if (!context.mounted) {
    return;
  }
  switch (choice) {
    case 'record':
      await _openGrowthRecordEditor(context, children);
    case 'reminder':
      await _openReminderEditor(context, ref, children, null);
  }
}

Future<void> _openGrowthRecordEditor(
  BuildContext context,
  List<ChildProfile> children,
) {
  return Navigator.of(context).push<void>(
    MaterialPageRoute<void>(
      builder: (context) => _GrowthRecordEditPage(children: children),
    ),
  );
}

IconData _categoryIcon(GrowthCategory category) {
  return switch (category) {
    GrowthCategory.milestone => Icons.emoji_events_rounded,
    GrowthCategory.encouragement => Icons.thumb_up_alt_rounded,
    GrowthCategory.habit => Icons.repeat_rounded,
    GrowthCategory.emotion => Icons.favorite_rounded,
  };
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

String _formatDate(DateTime value) {
  final local = value.toLocal();
  final month = local.month.toString().padLeft(2, '0');
  final day = local.day.toString().padLeft(2, '0');
  return '${local.year}-$month-$day';
}

class _GrowthRecordEditPage extends ConsumerStatefulWidget {
  const _GrowthRecordEditPage({required this.children});

  final List<ChildProfile> children;

  @override
  ConsumerState<_GrowthRecordEditPage> createState() =>
      _GrowthRecordEditPageState();
}

class _GrowthRecordEditPageState
    extends ConsumerState<_GrowthRecordEditPage> {
  final _formKey = GlobalKey<FormState>();
  final _titleController = TextEditingController();
  final _noteController = TextEditingController();
  GrowthCategory _category = GrowthCategory.milestone;
  late String _childId;
  bool _isSubmitting = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _childId = _defaultChildId();
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
    _titleController.dispose();
    _noteController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) {
      return;
    }
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    try {
      await ref
          .read(companionshipControllerProvider.notifier)
          .createGrowthRecord(
            childId: _childId,
            category: _category,
            title: _titleController.text.trim(),
            note: _noteController.text.trim(),
          );
      if (mounted) {
        Navigator.of(context).pop();
      }
    } on Object catch (error) {
      if (mounted) {
        setState(
          () => _errorMessage = CompanionshipController.messageFor(error),
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
      appBar: AppBar(title: const Text('记录成长瞬间')),
      body: SafeArea(
        child: Form(
          key: _formKey,
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              if (widget.children.length > 1)
                DropdownButtonFormField<String>(
                  initialValue: _childId.isEmpty ? null : _childId,
                  decoration: const InputDecoration(labelText: '记录哪个宝贝'),
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
              Text('分类', style: Theme.of(context).textTheme.bodySmall),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final category in GrowthCategory.values)
                    ChoiceChip(
                      label: Text(category.label),
                      selected: _category == category,
                      onSelected: (_) => setState(() => _category = category),
                    ),
                ],
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: _titleController,
                decoration: const InputDecoration(
                  labelText: '标题',
                  hintText: '例如：第一次自己穿鞋',
                ),
                validator: (value) {
                  final title = value?.trim() ?? '';
                  if (title.isEmpty) {
                    return '请填写标题';
                  }
                  if (title.length > 80) {
                    return '标题不能超过 80 个字';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: _noteController,
                maxLines: 3,
                decoration: const InputDecoration(
                  labelText: '备注（可选）',
                  hintText: '记下当时的场景或你的心情',
                ),
                validator: (value) {
                  if ((value?.trim().length ?? 0) > 500) {
                    return '备注不能超过 500 个字';
                  }
                  return null;
                },
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
                child: Text(_isSubmitting ? '正在保存…' : '保存记录'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ReminderEditPage extends ConsumerStatefulWidget {
  const _ReminderEditPage({required this.children, this.reminder});

  final List<ChildProfile> children;
  final HabitReminder? reminder;

  @override
  ConsumerState<_ReminderEditPage> createState() => _ReminderEditPageState();
}

class _ReminderEditPageState extends ConsumerState<_ReminderEditPage> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _labelController;
  late final TextEditingController _promptController;
  late String _reminderTime;
  late Set<int> _weekdays;
  late String _childId;
  bool _isSubmitting = false;
  String? _errorMessage;

  bool get _isEditing => widget.reminder != null;

  @override
  void initState() {
    super.initState();
    final reminder = widget.reminder;
    _labelController = TextEditingController(text: reminder?.label ?? '');
    _promptController = TextEditingController(text: reminder?.prompt ?? '');
    _reminderTime = reminder?.reminderTime ?? '08:00';
    _weekdays = {...?reminder?.weekdays};
    _childId = reminder?.childId ?? _defaultChildId();
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
    final parts = _reminderTime.split(':');
    final initial = TimeOfDay(
      hour: int.tryParse(parts.first) ?? 8,
      minute: parts.length > 1 ? int.tryParse(parts[1]) ?? 0 : 0,
    );
    final picked = await showTimePicker(context: context, initialTime: initial);
    if (picked != null) {
      setState(() {
        _reminderTime =
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
    final draft = HabitReminderDraft(
      childId: _childId,
      label: _labelController.text.trim(),
      prompt: _promptController.text.trim(),
      reminderTime: _reminderTime,
      weekdays: (_weekdays.toList()..sort()),
      enabled: true,
    );
    try {
      final controller = ref.read(companionshipControllerProvider.notifier);
      final reminder = widget.reminder;
      if (reminder == null) {
        await controller.createHabitReminder(draft);
      } else {
        await controller.updateHabitReminder(reminder.id, draft);
      }
      if (mounted) {
        Navigator.of(context).pop();
      }
    } on Object catch (error) {
      if (mounted) {
        setState(
          () => _errorMessage = CompanionshipController.messageFor(error),
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
      appBar: AppBar(title: Text(_isEditing ? '编辑习惯提醒' : '新建习惯提醒')),
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
                decoration: const InputDecoration(labelText: '提醒名称'),
                validator: (value) {
                  final label = value?.trim() ?? '';
                  if (label.isEmpty) {
                    return '请输入提醒名称';
                  }
                  if (label.length > 40) {
                    return '提醒名称不能超过 40 个字';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: _promptController,
                maxLines: 2,
                decoration: const InputDecoration(
                  labelText: '提醒内容（可选）',
                  hintText: '例如：该刷牙啦，我们去卫生间吧',
                ),
                validator: (value) {
                  if ((value?.trim().length ?? 0) > 200) {
                    return '提醒内容不能超过 200 个字';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 16),
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.schedule_rounded),
                title: const Text('提醒时间'),
                subtitle: Text(_reminderTime),
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
                child: Text(_isSubmitting ? '正在保存…' : '保存提醒'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
