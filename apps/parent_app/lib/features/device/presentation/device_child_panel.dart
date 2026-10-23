import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/device_child_controller.dart';
import '../data/device_child_api.dart';

/// Shows and changes the child a device currently serves.
///
/// Multi-child families use one device in turn, so this block answers "who is
/// this device for right now" and lets the guardian switch. It never shows the
/// device's raw state codes: each state maps to one concrete next action.
class DeviceChildPanel extends ConsumerWidget {
  const DeviceChildPanel({super.key, required this.deviceId});

  final String deviceId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final status = ref.watch(deviceChildControllerProvider(deviceId));
    final children = ref.watch(childProfileControllerProvider);

    return status.when(
      loading: () => const _PanelShell(
        child: Text('正在读取设备当前使用的儿童…'),
      ),
      error: (error, _) => _PanelShell(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(_errorMessage(error)),
            const SizedBox(height: 8),
            TextButton(
              onPressed: () => ref
                  .read(deviceChildControllerProvider(deviceId).notifier)
                  .refresh(),
              child: const Text('重新加载'),
            ),
          ],
        ),
      ),
      data: (value) => _PanelShell(
        child: children.when(
          loading: () => const Text('正在读取儿童档案…'),
          error: (error, _) => Text(_errorMessage(error)),
          data: (profiles) => _content(context, ref, value, profiles),
        ),
      ),
    );
  }

  Widget _content(
    BuildContext context,
    WidgetRef ref,
    DeviceChildStatus status,
    List<ChildProfile> profiles,
  ) {
    final theme = Theme.of(context);
    final activeChild = profiles
        .where((child) => child.childId == status.childId)
        .firstOrNull;

    if (profiles.isEmpty) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text('当前使用的儿童', style: theme.textTheme.titleSmall),
          const SizedBox(height: 6),
          const Text('这个家庭还没有儿童档案。添加儿童后，初芽才知道在和谁对话。'),
        ],
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text('当前使用的儿童', style: theme.textTheme.titleSmall),
        const SizedBox(height: 6),
        Text(
          activeChild != null
              ? '初芽现在和 ${activeChild.nickname} 对话。'
              : '还没有选择儿童。选一个之后，对话和记忆都会记到该儿童名下。',
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: <Widget>[
            for (final child in profiles)
              ChoiceChip(
                label: Text(child.nickname),
                selected: child.childId == status.childId,
                onSelected: (selected) {
                  if (!selected || child.childId == status.childId) {
                    return;
                  }
                  _activate(context, ref, child);
                },
              ),
          ],
        ),
        if (activeChild != null) ...<Widget>[
          const SizedBox(height: 8),
          TextButton(
            onPressed: () => _clear(context, ref, activeChild),
            child: const Text('取消这台设备的儿童绑定'),
          ),
        ],
        const SizedBox(height: 4),
        TextButton(
          onPressed: () => _showHistory(context, ref, profiles),
          child: const Text('查看这台设备用过哪些儿童'),
        ),
      ],
    );
  }

  /// Shows who this device has served. A guardian switching between siblings
  /// otherwise has no way to tell whether an earlier child's records followed
  /// them, and the history is the only place that answers it.
  Future<void> _showHistory(
    BuildContext context,
    WidgetRef ref,
    List<ChildProfile> profiles,
  ) async {
    final nameOf = <String, String>{
      for (final child in profiles) child.childId: child.nickname,
    };
    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (sheetContext) => _ChildHistorySheet(
        deviceId: deviceId,
        nameOf: nameOf,
      ),
    );
  }

  Future<void> _activate(
    BuildContext context,
    WidgetRef ref,
    ChildProfile child,
  ) async {
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      await ref
          .read(deviceChildControllerProvider(deviceId).notifier)
          .activate(child.childId);
      messenger?.showSnackBar(
        SnackBar(content: Text('已切换到${child.nickname}')),
      );
    } catch (error) {
      messenger?.showSnackBar(SnackBar(content: Text(_errorMessage(error))));
    }
  }

  Future<void> _clear(
    BuildContext context,
    WidgetRef ref,
    ChildProfile child,
  ) async {
    final messenger = ScaffoldMessenger.maybeOf(context);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('取消儿童绑定'),
        content: Text(
          '取消后初芽将不再属于某个儿童，直到重新选择。'
          '${child.nickname}的对话记忆不会被删除。',
        ),
        actions: <Widget>[
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('保留绑定'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('取消绑定'),
          ),
        ],
      ),
    );
    if (confirmed != true) {
      return;
    }
    try {
      await ref
          .read(deviceChildControllerProvider(deviceId).notifier)
          .clear();
      messenger?.showSnackBar(const SnackBar(content: Text('已取消儿童绑定')));
    } catch (error) {
      messenger?.showSnackBar(SnackBar(content: Text(_errorMessage(error))));
    }
  }
}

class _PanelShell extends StatelessWidget {
  const _PanelShell({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(top: 12),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: const Color(0xFFFFF9FA),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: const Color(0xFFF7D8E2)),
      ),
      child: child,
    );
  }
}

/// Maps a failure to text a guardian can act on.
String _errorMessage(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '暂时无法读取设备当前使用的儿童，请稍后重试。';
}

/// The children this device has served, newest first.
class _ChildHistorySheet extends ConsumerWidget {
  const _ChildHistorySheet({required this.deviceId, required this.nameOf});

  final String deviceId;
  final Map<String, String> nameOf;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final history = ref.watch(deviceChildHistoryProvider(deviceId));
    final theme = Theme.of(context);

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text('这台设备用过的儿童', style: theme.textTheme.titleMedium),
            const SizedBox(height: 12),
            history.when(
              loading: () => const Text('正在读取使用记录…'),
              error: (error, _) => Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  const Text('暂时无法读取使用记录，请稍后重试。'),
                  const SizedBox(height: 8),
                  TextButton(
                    onPressed: () => ref.invalidate(
                      deviceChildHistoryProvider(deviceId),
                    ),
                    child: const Text('重新读取'),
                  ),
                ],
              ),
              data: (entries) => entries.isEmpty
                  ? const Text('这台设备还没有换过儿童。')
                  : Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        for (final entry in entries)
                          _ChildHistoryRow(
                            // An unbind leaves no name on the record, and showing
                            // the raw identifier would tell the guardian nothing.
                            name: nameOf[entry.childId] ?? '已删除的儿童档案',
                            isActive: entry.isActive,
                            activatedAt: entry.activatedAt,
                            deactivatedAt: entry.deactivatedAt,
                          ),
                      ],
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ChildHistoryRow extends StatelessWidget {
  const _ChildHistoryRow({
    required this.name,
    required this.isActive,
    required this.activatedAt,
    required this.deactivatedAt,
  });

  final String name;
  final bool isActive;
  final DateTime? activatedAt;
  final DateTime? deactivatedAt;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final span = _spanText();
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(name, style: theme.textTheme.bodyMedium),
                if (span != null)
                  Text(
                    span,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
              ],
            ),
          ),
          if (isActive)
            Text('当前使用中', style: theme.textTheme.labelSmall),
        ],
      ),
    );
  }

  String? _spanText() {
    final start = activatedAt;
    if (start == null) {
      return null;
    }
    final end = deactivatedAt;
    final startText = _dayText(start);
    if (end == null) {
      return '从 $startText 开始使用';
    }
    return '$startText 至 ${_dayText(end)}';
  }

  String _dayText(DateTime value) {
    final local = value.toLocal();
    return '${local.year}年${local.month}月${local.day}日';
  }
}
