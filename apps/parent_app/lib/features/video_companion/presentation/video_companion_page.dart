import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/video_companion_controller.dart';
import '../domain/video_companion.dart';

/// Guardian view of the family's video contacts and recent calls.
///
/// Only call metadata is shown; media never flows through this page. Ending a
/// call records a bounded reason, and a revoked camera or microphone permission
/// ends the session from the device side.
class VideoCompanionPage extends ConsumerWidget {
  const VideoCompanionPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(videoCompanionControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('视频陪伴'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (_) => 'video-data',
          error: (_, _) => 'video-error',
          loading: () => 'video-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('video-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('video-error'),
            message: VideoCompanionController.messageFor(error),
            onRetry: () =>
                ref.read(videoCompanionControllerProvider.notifier).refresh(),
          ),
          data: (value) => _VideoBody(
            key: const ValueKey<String>('video-data'),
            state: value,
            onAddContact: () => _addContact(context, ref),
            onDeleteContact: (id) => _deleteContact(context, ref, id),
            onEndCall: (id) => _endCall(context, ref, id),
          ),
        ),
      ),
    );
  }

  Future<void> _addContact(BuildContext context, WidgetRef ref) async {
    final messenger = ScaffoldMessenger.of(context);
    final contact = await showDialog<_ContactDraft>(
      context: context,
      builder: (dialogContext) => const _ContactDialog(),
    );
    if (contact == null) {
      return;
    }
    try {
      await ref
          .read(videoCompanionControllerProvider.notifier)
          .createContact(
            VideoContact(
              id: '',
              childId: '',
              displayName: contact.displayName,
              role: contact.role,
              enabled: true,
            ),
          );
      messenger.showSnackBar(const SnackBar(content: Text('联系人已保存')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(VideoCompanionController.messageFor(error))),
      );
    }
  }

  Future<void> _deleteContact(
    BuildContext context,
    WidgetRef ref,
    String contactId,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(videoCompanionControllerProvider.notifier)
          .deleteContact(contactId);
      messenger.showSnackBar(const SnackBar(content: Text('联系人已删除')));
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(VideoCompanionController.messageFor(error))),
      );
    }
  }

  Future<void> _endCall(
    BuildContext context,
    WidgetRef ref,
    String callId,
  ) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(videoCompanionControllerProvider.notifier)
          .endCall(callId);
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(VideoCompanionController.messageFor(error))),
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

class _VideoBody extends StatelessWidget {
  const _VideoBody({
    required this.state,
    required this.onAddContact,
    required this.onDeleteContact,
    required this.onEndCall,
    super.key,
  });

  final VideoContactsState state;
  final VoidCallback onAddContact;
  final ValueChanged<String> onDeleteContact;
  final ValueChanged<String> onEndCall;

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
                        '视频联系人',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      IconButton(
                        tooltip: '添加联系人',
                        onPressed: onAddContact,
                        icon: const Icon(Icons.person_add_alt_rounded),
                      ),
                    ],
                  ),
                  if (state.contacts.isEmpty)
                    const Text('还没有视频联系人。')
                  else
                    for (final contact in state.contacts)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: const Icon(Icons.account_circle_outlined),
                        title: Text(contact.displayName),
                        subtitle: Text(_roleLabel(contact.role)),
                        trailing: IconButton(
                          tooltip: '删除联系人',
                          onPressed: () => onDeleteContact(contact.id),
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
          delay: const Duration(milliseconds: 40),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '最近通话',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 8),
                  if (state.calls.isEmpty)
                    const Text('还没有通话记录。')
                  else
                    for (final call in state.calls)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: Icon(_callIcon(call.state)),
                        title: Text(_stateLabel(call.state)),
                        subtitle: Text(_callSubtitle(call)),
                        trailing: call.isActive
                            ? TextButton(
                                onPressed: () => onEndCall(call.id),
                                child: const Text('结束'),
                              )
                            : null,
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

class _ContactDraft {
  const _ContactDraft({required this.displayName, required this.role});

  final String displayName;
  final String role;
}

class _ContactDialog extends StatefulWidget {
  const _ContactDialog();

  @override
  State<_ContactDialog> createState() => _ContactDialogState();
}

class _ContactDialogState extends State<_ContactDialog> {
  final TextEditingController _name = TextEditingController();
  String _role = 'guardian';

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('添加视频联系人'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: _name,
            decoration: const InputDecoration(labelText: '名称'),
          ),
          const SizedBox(height: 12),
          DropdownButtonFormField<String>(
            initialValue: _role,
            decoration: const InputDecoration(labelText: '角色'),
            items: const [
              DropdownMenuItem(value: 'guardian', child: Text('监护人')),
              DropdownMenuItem(value: 'family', child: Text('家人')),
              DropdownMenuItem(value: 'device', child: Text('设备')),
            ],
            onChanged: (value) => setState(() => _role = value ?? 'guardian'),
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
            _ContactDraft(displayName: _name.text.trim(), role: _role),
          ),
          child: const Text('保存'),
        ),
      ],
    );
  }
}

String _roleLabel(String role) {
  return (
    {
      'guardian': '监护人',
      'family': '家人',
      'device': '设备',
    }[role] ?? role);
}

String _stateLabel(String state) {
  return (
    {
      'idle': '空闲',
      'ringing': '呼叫中',
      'active': '通话中',
      'ended': '已结束',
      'rejected': '已拒绝',
      'missed': '未接听',
      'failed': '已中断',
    }[state] ?? state);
}

String _callSubtitle(VideoCall call) {
  final quality = (
    {
      'good': '画质良好',
      'fair': '画质一般',
      'poor': '画质较差',
      'unknown': '画质未知',
    }[call.quality] ?? call.quality);
  if (call.endReason.isEmpty) {
    return quality;
  }
  final reason = (
    {
      'hangup': '已挂断',
      'rejected': '被拒绝',
      'missed': '未接听',
      'timeout': '超时',
      'network': '网络中断',
      'permission_revoked': '权限被撤回',
      'device_offline': '设备离线',
    }[call.endReason] ?? call.endReason);
  return '$quality · $reason';
}

IconData _callIcon(String state) {
  switch (state) {
    case 'active':
      return Icons.videocam_rounded;
    case 'ringing':
      return Icons.videocam_outlined;
    case 'rejected':
    case 'missed':
      return Icons.videocam_off_outlined;
    case 'failed':
      return Icons.error_outline_rounded;
    default:
      return Icons.call_end_rounded;
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
