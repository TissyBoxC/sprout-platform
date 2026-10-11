import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/multi_device_controller.dart';
import '../domain/multi_device.dart';

/// Guardian view of the family device groups, linkage and content sync.
///
/// Removing a device from a group only changes that membership; the device's
/// other groups and data are untouched. Content converges by version, and a
/// same-version conflict is surfaced instead of being silently applied.
class MultiDevicePage extends ConsumerWidget {
  const MultiDevicePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(multiDeviceControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('多设备'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (_) => 'multi-device-data',
          error: (_, _) => 'multi-device-error',
          loading: () => 'multi-device-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('multi-device-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('multi-device-error'),
            message: MultiDeviceController.messageFor(error),
            onRetry: () =>
                ref.read(multiDeviceControllerProvider.notifier).refresh(),
          ),
          data: (value) => _MultiDeviceBody(
            key: const ValueKey<String>('multi-device-data'),
            state: value,
            onAddGroup: () => _addGroup(context, ref),
            onDeleteGroup: (id) => _deleteGroup(context, ref, id),
            onAddMember: (groupId) => _addMember(context, ref, groupId),
            onRouteMessage: () => _routeMessage(context, ref, value),
          ),
        ),
      ),
    );
  }

  Future<void> _addGroup(BuildContext context, WidgetRef ref) async {
    final messenger = ScaffoldMessenger.of(context);
    final draft = await showDialog<_GroupDraft>(
      context: context,
      builder: (dialogContext) => const _GroupDialog(),
    );
    if (draft == null) {
      return;
    }
    try {
      await ref
          .read(multiDeviceControllerProvider.notifier)
          .createGroup(name: draft.name, routeScope: draft.routeScope);
      messenger.showSnackBar(const SnackBar(content: Text('设备组已创建')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(MultiDeviceController.messageFor(error))),
      );
    }
  }

  Future<void> _deleteGroup(
    BuildContext context,
    WidgetRef ref,
    String groupId,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(multiDeviceControllerProvider.notifier)
          .deleteGroup(groupId);
      messenger.showSnackBar(const SnackBar(content: Text('设备组已删除')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(MultiDeviceController.messageFor(error))),
      );
    }
  }

  Future<void> _addMember(
    BuildContext context,
    WidgetRef ref,
    String groupId,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    final deviceId = await showDialog<String>(
      context: context,
      builder: (dialogContext) => const _MemberDialog(),
    );
    if (deviceId == null || deviceId.isEmpty) {
      return;
    }
    try {
      await ref
          .read(multiDeviceControllerProvider.notifier)
          .addMember(groupId: groupId, deviceId: deviceId);
      messenger.showSnackBar(const SnackBar(content: Text('设备已加入分组')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(MultiDeviceController.messageFor(error))),
      );
    }
  }

  Future<void> _routeMessage(
    BuildContext context,
    WidgetRef ref,
    MultiDeviceState current,
  ) async {
    if (current.groups.isEmpty) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先创建设备组')));
      return;
    }
    final messenger = ScaffoldMessenger.of(context);
    final draft = await showDialog<_MessageDraft>(
      context: context,
      builder: (dialogContext) => _MessageDialog(groups: current.groups),
    );
    if (draft == null) {
      return;
    }
    try {
      await ref
          .read(multiDeviceControllerProvider.notifier)
          .routeMessage(
            RoutedMessage(
              id: '',
              sourceDevice: '',
              scope: draft.scope,
              target: draft.target,
              body: draft.body,
            ),
          );
      messenger.showSnackBar(const SnackBar(content: Text('消息已发送')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(MultiDeviceController.messageFor(error))),
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

class _MultiDeviceBody extends StatelessWidget {
  const _MultiDeviceBody({
    required this.state,
    required this.onAddGroup,
    required this.onDeleteGroup,
    required this.onAddMember,
    required this.onRouteMessage,
    super.key,
  });

  final MultiDeviceState state;
  final VoidCallback onAddGroup;
  final ValueChanged<String> onDeleteGroup;
  final ValueChanged<String> onAddMember;
  final VoidCallback onRouteMessage;

  @override
  Widget build(BuildContext context) {
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
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      Text(
                        '设备组',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      IconButton(
                        tooltip: '创建设备组',
                        onPressed: onAddGroup,
                        icon: const Icon(Icons.add_rounded),
                      ),
                    ],
                  ),
                  if (state.groups.isEmpty)
                    const Text('还没有设备组。')
                  else
                    for (final group in state.groups)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: const Icon(Icons.devices_other_rounded),
                        title: Text(group.name),
                        subtitle: Text(
                          '${_scopeLabel(group.routeScope)} · ${group.memberCount} 台设备',
                        ),
                        trailing: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            IconButton(
                              tooltip: '添加设备',
                              onPressed: () => onAddMember(group.id),
                              icon: const Icon(
                                Icons.add_circle_outline_rounded,
                              ),
                            ),
                            IconButton(
                              tooltip: '删除分组',
                              onPressed: () => onDeleteGroup(group.id),
                              icon: const Icon(Icons.delete_outline_rounded),
                            ),
                          ],
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
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      Text(
                        '家庭联动消息',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      TextButton.icon(
                        onPressed: onRouteMessage,
                        icon: const Icon(Icons.send_rounded),
                        label: const Text('发送'),
                      ),
                    ],
                  ),
                  if (state.messages.isEmpty)
                    const Text('还没有联动消息。')
                  else
                    for (final message in state.messages.take(10))
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: const Icon(Icons.forum_outlined),
                        title: Text(message.body),
                        subtitle: Text(_scopeLabel(message.scope)),
                      ),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('内容同步', style: Theme.of(context).textTheme.titleMedium),
                  const SizedBox(height: 8),
                  if (state.content.isEmpty)
                    const Text('当前没有同步中的内容。')
                  else
                    for (final content in state.content)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: Icon(
                          content.hasConflict
                              ? Icons.sync_problem_rounded
                              : Icons.sync_rounded,
                        ),
                        title: Text(content.contentKey),
                        subtitle: Text(
                          '${_syncLabel(content.syncState)} · 版本 ${content.version}',
                        ),
                      ),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }
}

class _GroupDraft {
  const _GroupDraft({required this.name, required this.routeScope});

  final String name;
  final String routeScope;
}

class _GroupDialog extends StatefulWidget {
  const _GroupDialog();

  @override
  State<_GroupDialog> createState() => _GroupDialogState();
}

class _GroupDialogState extends State<_GroupDialog> {
  final TextEditingController _name = TextEditingController();
  String _scope = 'group';

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('创建设备组'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: _name,
            decoration: const InputDecoration(labelText: '名称'),
          ),
          const SizedBox(height: 12),
          DropdownButtonFormField<String>(
            initialValue: _scope,
            decoration: const InputDecoration(labelText: '联动范围'),
            items: const [
              DropdownMenuItem(value: 'group', child: Text('组内')),
              DropdownMenuItem(value: 'family', child: Text('全家')),
            ],
            onChanged: (value) => setState(() => _scope = value ?? 'group'),
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
          ).pop(_GroupDraft(name: _name.text.trim(), routeScope: _scope)),
          child: const Text('保存'),
        ),
      ],
    );
  }
}

class _MemberDialog extends StatefulWidget {
  const _MemberDialog();

  @override
  State<_MemberDialog> createState() => _MemberDialogState();
}

class _MemberDialogState extends State<_MemberDialog> {
  final TextEditingController _deviceId = TextEditingController();

  @override
  void dispose() {
    _deviceId.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('添加设备'),
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
          child: const Text('添加'),
        ),
      ],
    );
  }
}

class _MessageDraft {
  const _MessageDraft({
    required this.scope,
    required this.target,
    required this.body,
  });

  final String scope;
  final String target;
  final String body;
}

class _MessageDialog extends StatefulWidget {
  const _MessageDialog({required this.groups});

  final List<DeviceGroup> groups;

  @override
  State<_MessageDialog> createState() => _MessageDialogState();
}

class _MessageDialogState extends State<_MessageDialog> {
  final TextEditingController _body = TextEditingController();
  late String _target = widget.groups.first.id;

  @override
  void dispose() {
    _body.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('发送联动消息'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          DropdownButtonFormField<String>(
            initialValue: _target,
            decoration: const InputDecoration(labelText: '目标设备组'),
            items: [
              for (final group in widget.groups)
                DropdownMenuItem(value: group.id, child: Text(group.name)),
            ],
            onChanged: (value) =>
                setState(() => _target = value ?? widget.groups.first.id),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _body,
            decoration: const InputDecoration(labelText: '消息内容'),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(
            _MessageDraft(
              scope: 'group',
              target: _target,
              body: _body.text.trim(),
            ),
          ),
          child: const Text('发送'),
        ),
      ],
    );
  }
}

String _scopeLabel(String scope) {
  return ({'group': '组内联动', 'family': '全家联动', 'device': '单设备'}[scope] ?? scope);
}

String _syncLabel(String state) {
  return ({
        'applied': '已同步',
        'pending': '待同步',
        'conflict': '有冲突',
        'failed': '同步失败',
      }[state] ??
      state);
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
