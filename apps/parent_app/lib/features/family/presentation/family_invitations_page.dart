import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/family_controller.dart';
import '../domain/family_membership.dart';

/// Invitee view: lists the pending invitations addressed to the signed-in
/// account's email so it can accept one without already knowing the family id.
class FamilyInvitationsPage extends ConsumerWidget {
  const FamilyInvitationsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final invitations = ref.watch(myInvitationsControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('家庭邀请'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: invitations.when(
          data: (value) => value.isEmpty
              ? 'family-invitations-empty'
              : 'family-invitations-content',
          error: (_, _) => 'family-invitations-error',
          loading: () => 'family-invitations-loading',
        ),
        child: invitations.when(
          loading: () => const Center(
            key: ValueKey<String>('family-invitations-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('family-invitations-error'),
            message: FamilyController.messageFor(error),
            onRetry: () =>
                ref.read(myInvitationsControllerProvider.notifier).refresh(),
          ),
          data: (value) => RefreshIndicator(
            key: ValueKey<String>(
              value.isEmpty
                  ? 'family-invitations-empty'
                  : 'family-invitations-content',
            ),
            onRefresh: () =>
                ref.read(myInvitationsControllerProvider.notifier).refresh(),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
              children: [
                if (value.isEmpty)
                  const _EmptyCard()
                else ...[
                  AppReveal(
                    child: Text(
                      '家人邀请你加入家庭',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ),
                  const SizedBox(height: 8),
                  for (final invitation in value)
                    _InvitationCard(invitation: invitation),
                ],
              ],
            ),
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

class _InvitationCard extends ConsumerStatefulWidget {
  const _InvitationCard({required this.invitation});

  final FamilyInvitation invitation;

  @override
  ConsumerState<_InvitationCard> createState() => _InvitationCardState();
}

class _InvitationCardState extends ConsumerState<_InvitationCard> {
  bool _isAccepting = false;
  String? _errorMessage;

  Future<void> _accept() async {
    setState(() {
      _isAccepting = true;
      _errorMessage = null;
    });
    try {
      final member = await ref
          .read(myInvitationsControllerProvider.notifier)
          .accept(widget.invitation);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('已加入家庭（${FamilyRole.label(member.role)}）')),
        );
        context.go('/me/family');
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = FamilyController.messageFor(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isAccepting = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final invitation = widget.invitation;
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.family_restroom_rounded),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    invitation.email,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text('角色：${FamilyRole.label(invitation.role)}'),
            if (_errorMessage != null) ...[
              const SizedBox(height: 8),
              Text(
                _errorMessage!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 12),
            Align(
              alignment: Alignment.centerRight,
              child: FilledButton.icon(
                onPressed: _isAccepting ? null : _accept,
                icon: const Icon(Icons.check_rounded),
                label: Text(_isAccepting ? '正在加入…' : '接受邀请'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _EmptyCard extends StatelessWidget {
  const _EmptyCard();

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          children: [
            Icon(
              Icons.mark_email_read_outlined,
              size: 36,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 12),
            const Text('暂时没有待接受的家庭邀请', textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({
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
