import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../auth/application/auth_controller.dart';
import '../application/family_controller.dart';
import '../domain/family_membership.dart';

/// Guardian view of the family members, roles and pending invitations.
class FamilyMembersPage extends ConsumerWidget {
  const FamilyMembersPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(familyControllerProvider);
    final account = ref.watch(authControllerProvider).value?.account;
    return Scaffold(
      appBar: AppBar(
        title: const Text('家庭成员'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _openInviteEditor(context, ref),
        icon: const Icon(Icons.person_add_alt_1_rounded),
        label: const Text('邀请家人'),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (_) => 'family-members-content',
          error: (_, _) => 'family-members-error',
          loading: () => 'family-members-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('family-members-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _FamilyErrorState(
            key: const ValueKey<String>('family-members-error'),
            message: FamilyController.messageFor(error),
            onRetry: () =>
                ref.read(familyControllerProvider.notifier).refresh(),
          ),
          data: (value) => _FamilyContent(
            key: const ValueKey<String>('family-members-content'),
            state: value,
            currentUserId: account?.id ?? '',
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
    context.go('/me');
  }
}

class _FamilyContent extends ConsumerWidget {
  const _FamilyContent({
    required this.state,
    required this.currentUserId,
    super.key,
  });

  final FamilyMembershipState state;
  final String currentUserId;

  bool get _isOwner {
    final owner = state.capability.ownerUserId;
    return currentUserId.isNotEmpty && owner == currentUserId;
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(familyControllerProvider.notifier);
    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 96),
        children: [
          AppReveal(child: _SummaryCard(state: state)),
          const SizedBox(height: 20),
          AppReveal(
            delay: const Duration(milliseconds: 60),
            child: _SectionHeader(
              title: '家庭成员',
              trailing: '${state.members.length} 人',
            ),
          ),
          const SizedBox(height: 8),
          for (final member in state.orderedMembers)
            _MemberTile(
              member: member,
              isMe: member.userId == currentUserId,
              isOwnerViewer: _isOwner,
              currentUserId: currentUserId,
            ),
          const SizedBox(height: 20),
          AppReveal(
            delay: const Duration(milliseconds: 120),
            child: _SectionHeader(
              title: '待接受邀请',
              trailing: '${state.pendingInvitations.length} 个',
            ),
          ),
          const SizedBox(height: 8),
          if (state.pendingInvitations.isEmpty)
            const _EmptyCard(
              icon: Icons.mark_email_unread_outlined,
              message: '还没有待接受的邀请，点击右下角邀请家人吧。',
            )
          else
            for (final invitation in state.pendingInvitations)
              _InvitationTile(invitation: invitation),
        ],
      ),
    );
  }
}

class _SummaryCard extends StatelessWidget {
  const _SummaryCard({required this.state});

  final FamilyMembershipState state;

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
                const Icon(Icons.family_restroom_rounded),
                const SizedBox(width: 10),
                Text('家庭协作', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              '家庭成员可以一起照看设备和内容。所有者管成员和角色，守护者可以邀请家人，查看者只能浏览。',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            const SizedBox(height: 18),
            Row(
              children: [
                Expanded(
                  child: _SummaryMetric(
                    label: '成员',
                    value: '${state.capability.memberCount} 人',
                  ),
                ),
                Container(
                  width: 1,
                  height: 42,
                  color: Theme.of(context).colorScheme.outlineVariant,
                ),
                Expanded(
                  child: _SummaryMetric(
                    label: '待接受',
                    value: '${state.capability.pendingInviteCount} 个',
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _SummaryMetric extends StatelessWidget {
  const _SummaryMetric({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: Theme.of(context).textTheme.bodySmall),
        const SizedBox(height: 4),
        FittedBox(
          fit: BoxFit.scaleDown,
          alignment: Alignment.centerLeft,
          child: Text(
            value,
            style: Theme.of(
              context,
            ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
          ),
        ),
      ],
    );
  }
}

class _SectionHeader extends StatelessWidget {
  const _SectionHeader({required this.title, required this.trailing});

  final String title;
  final String trailing;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Text(title, style: Theme.of(context).textTheme.titleMedium),
        const Spacer(),
        Text(trailing, style: Theme.of(context).textTheme.bodySmall),
      ],
    );
  }
}

class _MemberTile extends ConsumerWidget {
  const _MemberTile({
    required this.member,
    required this.isMe,
    required this.isOwnerViewer,
    required this.currentUserId,
  });

  final FamilyMember member;
  final bool isMe;
  final bool isOwnerViewer;
  final String currentUserId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Only the current owner may manage peers, and the owner row is never
    // edited or removed from this page to protect the unique-owner invariant.
    final canManage =
        isOwnerViewer && !member.isOwner && member.userId != currentUserId;
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        leading: CircleAvatar(
          backgroundColor: Theme.of(context).colorScheme.primaryContainer,
          child: Icon(
            member.isOwner
                ? Icons.workspace_premium_rounded
                : Icons.person_outline_rounded,
            color: Theme.of(context).colorScheme.primary,
          ),
        ),
        title: Text(_memberTitle(member, isMe)),
        subtitle: Text(FamilyRole.label(member.role)),
        trailing: canManage
            ? PopupMenuButton<String>(
                tooltip: '管理成员',
                onSelected: (value) async {
                  if (value == 'remove') {
                    await _confirmRemove(context, ref, member);
                    return;
                  }
                  await _changeRole(context, ref, member, value);
                },
                itemBuilder: (context) => const [
                  PopupMenuItem<String>(
                    value: FamilyRole.guardian,
                    child: Text('设为守护者'),
                  ),
                  PopupMenuItem<String>(
                    value: FamilyRole.viewer,
                    child: Text('设为查看者'),
                  ),
                  PopupMenuDivider(),
                  PopupMenuItem<String>(
                    value: 'remove',
                    child: Text('移出家庭'),
                  ),
                ],
              )
            : null,
      ),
    );
  }
}

String _memberTitle(FamilyMember member, bool isMe) {
  if (isMe) {
    return '我';
  }
  final shortId = member.userId.length > 8
      ? member.userId.substring(0, 8)
      : member.userId;
  return '成员 $shortId';
}

class _InvitationTile extends ConsumerWidget {
  const _InvitationTile({required this.invitation});

  final FamilyInvitation invitation;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        leading: const Icon(Icons.mail_outline_rounded),
        title: Text(invitation.email),
        subtitle: Text(
          '${FamilyRole.label(invitation.role)} · '
          '${FamilyInvitationStatus.label(invitation.status)}',
        ),
        trailing: IconButton(
          tooltip: '撤销邀请',
          icon: const Icon(Icons.close_rounded),
          onPressed: () => _confirmRevoke(context, ref, invitation),
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
        padding: const EdgeInsets.all(24),
        child: Column(
          children: [
            Icon(icon, size: 36, color: Theme.of(context).colorScheme.primary),
            const SizedBox(height: 12),
            Text(message, textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}

class _FamilyErrorState extends StatelessWidget {
  const _FamilyErrorState({
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

Future<void> _openInviteEditor(BuildContext context, WidgetRef ref) async {
  final result = await showModalBottomSheet<_InviteDraft>(
    context: context,
    showDragHandle: true,
    isScrollControlled: true,
    builder: (context) => const _InviteSheet(),
  );
  if (result == null || !context.mounted) {
    return;
  }
  try {
    await ref
        .read(familyControllerProvider.notifier)
        .invite(email: result.email, role: result.role);
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('邀请已发送给 ${result.email}')));
    }
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(FamilyController.messageFor(error))),
      );
    }
  }
}

class _InviteDraft {
  const _InviteDraft({required this.email, required this.role});

  final String email;
  final String role;
}

class _InviteSheet extends StatefulWidget {
  const _InviteSheet();

  @override
  State<_InviteSheet> createState() => _InviteSheetState();
}

class _InviteSheetState extends State<_InviteSheet> {
  final _formKey = GlobalKey<FormState>();
  final _emailController = TextEditingController();
  String _role = FamilyRole.guardian;

  @override
  void dispose() {
    _emailController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.fromLTRB(
          20,
          0,
          20,
          24 + MediaQuery.of(context).viewInsets.bottom,
        ),
        child: Form(
          key: _formKey,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('邀请家人', style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 6),
              const Text('家人用这个邮箱注册或登录后，就可以接受邀请加入家庭。'),
              const SizedBox(height: 16),
              TextFormField(
                controller: _emailController,
                autofocus: true,
                keyboardType: TextInputType.emailAddress,
                decoration: const InputDecoration(
                  labelText: '家人邮箱',
                  hintText: '例如 name@example.com',
                ),
                validator: (value) {
                  final email = value?.trim() ?? '';
                  if (email.isEmpty || !email.contains('@')) {
                    return '请输入有效的邮箱地址';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 12),
              SegmentedButton<String>(
                segments: const [
                  ButtonSegment<String>(
                    value: FamilyRole.guardian,
                    label: Text('守护者'),
                    icon: Icon(Icons.shield_outlined),
                  ),
                  ButtonSegment<String>(
                    value: FamilyRole.viewer,
                    label: Text('查看者'),
                    icon: Icon(Icons.visibility_outlined),
                  ),
                ],
                selected: <String>{_role},
                onSelectionChanged: (selection) =>
                    setState(() => _role = selection.first),
              ),
              const SizedBox(height: 20),
              FilledButton(
                onPressed: () {
                  if (!_formKey.currentState!.validate()) {
                    return;
                  }
                  Navigator.of(context).pop(
                    _InviteDraft(
                      email: _emailController.text.trim(),
                      role: _role,
                    ),
                  );
                },
                child: const Text('发送邀请'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

Future<void> _changeRole(
  BuildContext context,
  WidgetRef ref,
  FamilyMember member,
  String role,
) async {
  if (member.role == role) {
    return;
  }
  try {
    await ref
        .read(familyControllerProvider.notifier)
        .updateRole(userId: member.userId, role: role);
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('已设为${FamilyRole.label(role)}')),
      );
    }
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(FamilyController.messageFor(error))),
      );
    }
  }
}

Future<void> _confirmRemove(
  BuildContext context,
  WidgetRef ref,
  FamilyMember member,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('移出家庭'),
      content: const Text('移出后，这位家人将无法再查看家庭设备和内容。'),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('移出'),
        ),
      ],
    ),
  );
  if (confirmed != true || !context.mounted) {
    return;
  }
  try {
    await ref
        .read(familyControllerProvider.notifier)
        .removeMember(member.userId);
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(FamilyController.messageFor(error))),
      );
    }
  }
}

Future<void> _confirmRevoke(
  BuildContext context,
  WidgetRef ref,
  FamilyInvitation invitation,
) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('撤销邀请'),
      content: Text('撤销后，${invitation.email} 将无法再用这封邀请加入家庭。'),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('保留'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('撤销'),
        ),
      ],
    ),
  );
  if (confirmed != true || !context.mounted) {
    return;
  }
  try {
    await ref
        .read(familyControllerProvider.notifier)
        .revokeInvitation(invitation.id);
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(FamilyController.messageFor(error))),
      );
    }
  }
}
