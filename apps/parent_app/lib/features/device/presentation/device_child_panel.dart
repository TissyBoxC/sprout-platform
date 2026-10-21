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
          const Text('这个家庭还没有儿童档案。添加儿童后，Robot 才知道在和谁对话。'),
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
              ? '这台 Robot 现在和 ${activeChild.nickname} 对话。'
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
      ],
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
          '取消后这台 Robot 将不再属于某个儿童，直到重新选择。'
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
