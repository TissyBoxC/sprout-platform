import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/billing_controllers.dart';
import '../domain/billing_errors.dart';
import '../presentation/billing_widgets.dart';

/// 消费条款页：家长在这里查看、同意或撤回消费授权。
class BillingConsentPage extends ConsumerStatefulWidget {
  const BillingConsentPage({super.key});

  @override
  ConsumerState<BillingConsentPage> createState() => _BillingConsentPageState();
}

class _BillingConsentPageState extends ConsumerState<BillingConsentPage> {
  bool _isBusy = false;
  String? _actionError;

  @override
  Widget build(BuildContext context) {
    final consent = ref.watch(billingConsentControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('消费条款'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: consent.when(
          data: (value) => value.isGranted ? 'consent-granted' : 'consent-none',
          error: (_, _) => 'consent-error',
          loading: () => 'consent-loading',
        ),
        child: consent.when(
          loading: () => const BillingLoadingState(
            key: ValueKey<String>('consent-loading'),
          ),
          error: (error, _) => BillingErrorState(
            key: const ValueKey<String>('consent-error'),
            message: billingStateErrorMessage(error),
            onRetry: () =>
                ref.read(billingConsentControllerProvider.notifier).refresh(),
          ),
          data: (value) => _ConsentBody(
            key: ValueKey<String>(
              value.isGranted ? 'consent-granted' : 'consent-none',
            ),
            consent: value,
            isBusy: _isBusy,
            actionError: _actionError,
            onGrant: _grant,
            onRevoke: _revoke,
          ),
        ),
      ),
    );
  }

  Future<void> _grant() async {
    setState(() {
      _isBusy = true;
      _actionError = null;
    });
    try {
      await ref.read(billingConsentControllerProvider.notifier).grant();
      if (mounted) {
        _showMessage('已同意消费条款');
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _actionError = billingActionErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isBusy = false);
      }
    }
  }

  Future<void> _revoke() async {
    final isConfirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('撤回消费同意吗？'),
        content: const Text('撤回后不能再购买新套餐，已支付的订单不受影响。需要时可以再次同意。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('继续保留'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('撤回同意'),
          ),
        ],
      ),
    );
    if (isConfirmed != true || !mounted) {
      return;
    }
    setState(() {
      _isBusy = true;
      _actionError = null;
    });
    try {
      await ref.read(billingConsentControllerProvider.notifier).revoke();
      if (mounted) {
        _showMessage('已撤回消费同意');
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _actionError = billingActionErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isBusy = false);
      }
    }
  }

  void _showMessage(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
    } else {
      context.go('/me/billing');
    }
  }
}

class _ConsentBody extends StatelessWidget {
  const _ConsentBody({
    required this.consent,
    required this.isBusy,
    required this.actionError,
    required this.onGrant,
    required this.onRevoke,
    super.key,
  });

  final BillingConsent consent;
  final bool isBusy;
  final String? actionError;
  final Future<void> Function() onGrant;
  final Future<void> Function() onRevoke;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: [
        AppReveal(
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Icon(
                        Icons.shield_outlined,
                        color: colorScheme.primary,
                      ),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Text(
                          '为什么需要家长同意',
                          style: theme.textTheme.titleMedium?.copyWith(
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ),
                      BillingStatusBadge(
                        label: consent.isGranted ? '已同意' : '未同意',
                        isEmphasized: consent.isGranted,
                        isMuted: !consent.isGranted,
                      ),
                    ],
                  ),
                  const SizedBox(height: 12),
                  const Text(
                    '购买套餐会使用家庭的钱。为了孩子的安全，平台不会让孩子自己完成购买，'
                    '必须由家长确认这份消费条款后，才能下单和支付。',
                  ),
                  const SizedBox(height: 10),
                  _ConsentLine(text: '下单前会显示金额，支付前还会再次确认。'),
                  _ConsentLine(text: '已支付的订单可以申请退款，退款进度显示在订单记录里。'),
                  _ConsentLine(text: '你可以随时撤回同意，撤回后需要重新同意才能再次购买。'),
                  if (consent.grantedAt != null) ...[
                    const SizedBox(height: 10),
                    Text(
                      '同意时间 ${billingDateLabel(consent.grantedAt!)}',
                      style: theme.textTheme.bodySmall,
                    ),
                  ],
                ],
              ),
            ),
          ),
        ),
        if (actionError != null) ...[
          const SizedBox(height: 14),
          BillingInlineNotice(message: actionError!, isWarning: true),
        ],
        const SizedBox(height: 18),
        if (consent.isGranted)
          OutlinedButton.icon(
            key: const ValueKey<String>('consent-revoke'),
            onPressed: isBusy ? null : onRevoke,
            icon: const Icon(Icons.logout_rounded),
            label: const Text('撤回消费同意'),
            style: OutlinedButton.styleFrom(
              foregroundColor: colorScheme.error,
              side: BorderSide(color: colorScheme.error),
            ),
          )
        else
          FilledButton.icon(
            key: const ValueKey<String>('consent-grant'),
            onPressed: isBusy ? null : onGrant,
            icon: isBusy
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.verified_user_outlined),
            label: const Text('同意消费条款'),
          ),
        const SizedBox(height: 10),
        OutlinedButton(
          onPressed: () => context.push('/me/billing'),
          child: const Text('查看服务套餐'),
        ),
      ],
    );
  }
}

class _ConsentLine extends StatelessWidget {
  const _ConsentLine({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.check_circle_outline_rounded,
            size: 18,
            color: Theme.of(context).colorScheme.primary,
          ),
          const SizedBox(width: 8),
          Expanded(child: Text(text)),
        ],
      ),
    );
  }
}
