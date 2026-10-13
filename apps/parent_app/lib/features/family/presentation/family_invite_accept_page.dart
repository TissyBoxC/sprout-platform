import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../application/family_controller.dart';
import '../domain/family_membership.dart';

/// Redeems a family invitation that was shared as a link carrying the family id
/// and invitation id. The platform still matches the signed-in account email,
/// so a forwarded link cannot add the wrong login.
class FamilyInviteAcceptPage extends ConsumerStatefulWidget {
  const FamilyInviteAcceptPage({
    required this.familyId,
    required this.invitationId,
    super.key,
  });

  final String familyId;
  final String invitationId;

  @override
  ConsumerState<FamilyInviteAcceptPage> createState() =>
      _FamilyInviteAcceptPageState();
}

class _FamilyInviteAcceptPageState
    extends ConsumerState<FamilyInviteAcceptPage> {
  bool _isSubmitting = false;
  FamilyMember? _member;
  String? _errorMessage;

  Future<void> _accept() async {
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    try {
      final member = await ref
          .read(familyControllerProvider.notifier)
          .acceptInvitation(
            familyId: widget.familyId,
            invitationId: widget.invitationId,
          );
      if (mounted) {
        setState(() => _member = member);
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = FamilyController.messageFor(error));
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
      appBar: AppBar(
        title: const Text('接受家庭邀请'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: _content(context),
        ),
      ),
    );
  }

  Widget _content(BuildContext context) {
    final member = _member;
    if (member != null) {
      return Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.check_circle_rounded,
            size: 56,
            color: Theme.of(context).colorScheme.primary,
          ),
          const SizedBox(height: 16),
          Text('已加入家庭', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 8),
          Text('你的角色是${FamilyRole.label(member.role)}。'),
          const SizedBox(height: 20),
          FilledButton(
            onPressed: () => context.go('/me/family'),
            child: const Text('查看家庭成员'),
          ),
        ],
      );
    }
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const Icon(Icons.family_restroom_rounded, size: 48),
        const SizedBox(height: 16),
        Text('加入这个家庭', style: Theme.of(context).textTheme.titleLarge),
        const SizedBox(height: 8),
        const Text(
          '接受后，你就能一起查看家庭设备和内容。',
          textAlign: TextAlign.center,
        ),
        if (_errorMessage != null) ...[
          const SizedBox(height: 14),
          Text(
            _errorMessage!,
            textAlign: TextAlign.center,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        ],
        const SizedBox(height: 20),
        FilledButton(
          onPressed: _isSubmitting ? null : _accept,
          child: Text(_isSubmitting ? '正在加入…' : '接受邀请'),
        ),
      ],
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
