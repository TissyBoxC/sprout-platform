import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/billing_controllers.dart';
import '../domain/billing_errors.dart';
import '../presentation/billing_widgets.dart';

/// 结算页：先确认订单与监护人同意，再发起支付。
///
/// 没有监护人同意时，页面只展示同意步骤，绝不创建订单；因此孩子无法在
/// 没有家长参与的情况下触发购买。
class BillingCheckoutPage extends ConsumerStatefulWidget {
  const BillingCheckoutPage({required this.plan, super.key});

  final BillingPlan plan;

  @override
  ConsumerState<BillingCheckoutPage> createState() => _BillingCheckoutPageState();
}

class _BillingCheckoutPageState extends ConsumerState<BillingCheckoutPage> {
  /// 一次结算流程使用同一个幂等键，重复点击不会产生多张订单。
  late final String _idempotencyKey = _newIdempotencyKey();
  BillingOrder? _order;
  bool _isCreatingOrder = false;
  bool _isPaying = false;
  bool _hasPaymentOpen = true;
  String? _actionError;

  BillingPlan get _plan => widget.plan;

  @override
  Widget build(BuildContext context) {
    final consent = ref.watch(billingConsentControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('确认订单'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: '${consent.hasValue}-${consent.value?.isGranted}-$_order',
        child: consent.when(
          loading: () => const BillingLoadingState(key: ValueKey('checkout-loading')),
          error: (error, _) => BillingErrorState(
            key: const ValueKey('checkout-consent-error'),
            message: billingStateErrorMessage(error),
            onRetry: () =>
                ref.read(billingConsentControllerProvider.notifier).refresh(),
          ),
          data: (value) {
            if (!value.isGranted) {
              return _ConsentStep(
                key: const ValueKey('checkout-consent'),
                plan: _plan,
                onAgreed: _onConsentGranted,
              );
            }
            return _OrderStep(
              key: const ValueKey('checkout-order'),
              plan: _plan,
              order: _order,
              isCreatingOrder: _isCreatingOrder,
              isPaying: _isPaying,
              hasPaymentOpen: _hasPaymentOpen,
              actionError: _actionError,
              onConfirmPay: _confirmAndPay,
              onBackToOrders: () => context.push('/me/billing/orders'),
            );
          },
        ),
      ),
    );
  }

  Future<void> _onConsentGranted() async {
    setState(() => _actionError = null);
    try {
      await ref.read(billingConsentControllerProvider.notifier).grant();
    } on Object catch (error) {
      if (mounted) {
        setState(() => _actionError = billingActionErrorMessage(error));
      }
    }
  }

  Future<void> _confirmAndPay() async {
    if (_isPaying) {
      return;
    }
    // 支付是把真钱花出去的不可逆动作，必须由家长再次确认。
    final isConfirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('确认支付这笔订单吗？'),
        content: Text(
          '本次将支付 ${_plan.price.format()}，购买「${_plan.displayName}」。'
          '支付后订单会进入已支付状态。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('再想想'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('确认支付'),
          ),
        ],
      ),
    );
    if (isConfirmed != true || !mounted) {
      return;
    }
    setState(() {
      _isPaying = true;
      _actionError = null;
    });
    try {
      final order = _order ?? await _createOrder();
      if (order == null || !mounted) {
        return;
      }
      final result = await ref
          .read(billingApiProvider)
          .payOrder(orderNo: order.orderNo, channel: billingPaymentChannel);
      if (!mounted) {
        return;
      }
      // 后端可能只回支付结果，此时用本地订单进入订单记录，避免多一次失败。
      final latestOrder = result.order;
      setState(() {
        if (latestOrder != null) {
          _order = latestOrder;
        }
        _hasPaymentOpen = true;
      });
      _showMessage('支付请求已提交，订单会显示最新状态');
      context.push('/me/billing/orders');
    } on PaymentNotOpenException {
      if (!mounted) {
        return;
      }
      // 支付渠道尚未开放：保留订单，提示稍后再试。
      setState(() {
        _hasPaymentOpen = false;
        _actionError = null;
      });
    } on Object catch (error) {
      if (mounted) {
        setState(() => _actionError = billingActionErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isPaying = false);
      }
    }
  }

  Future<BillingOrder?> _createOrder() async {
    if (!_isCreatingOrder) {
      setState(() {
        _isCreatingOrder = true;
        _actionError = null;
      });
    }
    try {
      final order = await ref
          .read(billingApiProvider)
          .createOrder(
            planCode: _plan.code,
            idempotencyKey: _idempotencyKey,
          );
      if (mounted) {
        setState(() => _order = order);
      }
      return order;
    } on SpendingConsentRequiredException {
      if (!mounted) {
        return null;
      }
      // 服务端可能已经撤回了同意：重新读取，界面会回到同意步骤。
      await ref.read(billingConsentControllerProvider.notifier).refresh();
      if (mounted) {
        setState(() => _actionError = '购买前需要监护人同意消费条款');
      }
      return null;
    } on Object catch (error) {
      if (mounted) {
        setState(() => _actionError = billingActionErrorMessage(error));
      }
      return null;
    } finally {
      if (mounted && _isCreatingOrder) {
        setState(() => _isCreatingOrder = false);
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

/// 支付渠道标识。本轮只接入微信支付，渠道未配置时后端返回未开放结果。
const billingPaymentChannel = 'wechat';

class _ConsentStep extends ConsumerStatefulWidget {
  const _ConsentStep({required this.plan, required this.onAgreed, super.key});

  final BillingPlan plan;
  final Future<void> Function() onAgreed;

  @override
  ConsumerState<_ConsentStep> createState() => _ConsentStepState();
}

class _ConsentStepState extends ConsumerState<_ConsentStep> {
  bool _isGranting = false;

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
                      Container(
                        width: 48,
                        height: 48,
                        decoration: BoxDecoration(
                          color: colorScheme.primaryContainer,
                          borderRadius: BorderRadius.circular(18),
                        ),
                        child: Icon(Icons.family_restroom_rounded,
                            color: colorScheme.primary),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Text(
                          '请家长同意后再购买',
                          style: theme.textTheme.titleLarge?.copyWith(
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 14),
                  const Text(
                    '购买套餐会使用真实的钱。为了孩子的安全，只有家长本人同意这份消费条款后，'
                    '才能继续进入支付。设备上的孩子无法完成这一步。',
                  ),
                  const SizedBox(height: 14),
                  _ConsentBullet(text: '你可以在任何时候撤回同意，撤回后需要重新同意才能再次购买。'),
                  _ConsentBullet(text: '支付前会再次显示订单金额，由你确认后才真正付款。'),
                  _ConsentBullet(text: '已支付的订单可以在订单记录里申请退款。'),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 14),
        AppReveal(
          delay: const Duration(milliseconds: 50),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('本次要购买', style: theme.textTheme.titleMedium),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      Expanded(child: Text(widget.plan.displayName)),
                      Text(
                        widget.plan.price.format(),
                        style: theme.textTheme.titleLarge?.copyWith(
                          fontWeight: FontWeight.w800,
                          color: colorScheme.primary,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(widget.plan.periodLabel, style: theme.textTheme.bodySmall),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 20),
        FilledButton.icon(
          key: const ValueKey<String>('checkout-agree'),
          onPressed: _isGranting ? null : _grant,
          icon: _isGranting
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Icon(Icons.verified_user_outlined),
          label: const Text('同意并继续'),
        ),
        const SizedBox(height: 10),
        OutlinedButton(
          onPressed: () => context.push('/me/billing/consent'),
          child: const Text('查看消费条款'),
        ),
      ],
    );
  }

  Future<void> _grant() async {
    setState(() => _isGranting = true);
    try {
      await widget.onAgreed();
    } finally {
      if (mounted) {
        setState(() => _isGranting = false);
      }
    }
  }
}

class _ConsentBullet extends StatelessWidget {
  const _ConsentBullet({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.circle,
            size: 7,
            color: Theme.of(context).colorScheme.primary,
          ),
          const SizedBox(width: 10),
          Expanded(child: Text(text)),
        ],
      ),
    );
  }
}

class _OrderStep extends StatelessWidget {
  const _OrderStep({
    required this.plan,
    required this.order,
    required this.isCreatingOrder,
    required this.isPaying,
    required this.hasPaymentOpen,
    required this.actionError,
    required this.onConfirmPay,
    required this.onBackToOrders,
    super.key,
  });

  final BillingPlan plan;
  final BillingOrder? order;
  final bool isCreatingOrder;
  final bool isPaying;
  final bool hasPaymentOpen;
  final String? actionError;
  final Future<void> Function() onConfirmPay;
  final VoidCallback onBackToOrders;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;
    final currentOrder = order;
    final isBusy = isCreatingOrder || isPaying;
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
                  Text('订单信息', style: theme.textTheme.titleMedium),
                  const SizedBox(height: 14),
                  _SummaryRow(label: '套餐名称', value: plan.displayName),
                  _SummaryRow(label: '计费周期', value: plan.periodLabel),
                  _SummaryRow(
                    label: '单价',
                    value: currentOrder?.unitPrice.format() ?? plan.price.format(),
                  ),
                  if (currentOrder != null && currentOrder.hasDiscount)
                    _SummaryRow(
                      label: '优惠',
                      value: '-${currentOrder.discount.format()}',
                    ),
                  const Divider(height: 26),
                  Row(
                    children: [
                      Text('应付金额', style: theme.textTheme.titleMedium),
                      const Spacer(),
                      Text(
                        (currentOrder?.payable ?? plan.price).format(),
                        key: const ValueKey<String>('checkout-payable'),
                        style: theme.textTheme.headlineSmall?.copyWith(
                          fontWeight: FontWeight.w800,
                          color: colorScheme.primary,
                        ),
                      ),
                    ],
                  ),
                  if (currentOrder != null) ...[
                    const SizedBox(height: 10),
                    _SummaryRow(label: '订单号', value: currentOrder.orderNo),
                    _SummaryRow(
                      label: '订单状态',
                      value: currentOrder.status.label,
                    ),
                    if (currentOrder.expiresAt != null)
                      _SummaryRow(
                        label: '支付截止',
                        value: billingDateLabel(currentOrder.expiresAt!),
                      ),
                  ],
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 14),
        if (!hasPaymentOpen) ...[
          const AppReveal(
            child: BillingInlineNotice(
              message: '支付功能尚未开放，请稍后再试。订单已经为你保留，稍后回来可以继续支付。',
              icon: Icons.schedule_rounded,
            ),
          ),
          const SizedBox(height: 14),
        ],
        if (actionError != null) ...[
          BillingInlineNotice(message: actionError!, isWarning: true),
          const SizedBox(height: 14),
        ],
        const AppReveal(child: _SafetyNote()),
        const SizedBox(height: 18),
        FilledButton.icon(
          key: const ValueKey<String>('checkout-pay'),
          onPressed: isBusy ? null : onConfirmPay,
          icon: isBusy
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Icon(Icons.account_balance_wallet_outlined),
          label: Text(isPaying ? '正在发起支付…' : '确认支付'),
        ),
        if (currentOrder != null) ...[
          const SizedBox(height: 10),
          OutlinedButton.icon(
            onPressed: onBackToOrders,
            icon: const Icon(Icons.receipt_long_outlined),
            label: const Text('查看订单记录'),
          ),
        ],
      ],
    );
  }
}

class _SummaryRow extends StatelessWidget {
  const _SummaryRow({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: theme.textTheme.bodySmall),
          const SizedBox(width: 16),
          Expanded(
            child: Text(
              value,
              textAlign: TextAlign.right,
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurface,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _SafetyNote extends StatelessWidget {
  const _SafetyNote();

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: colorScheme.outlineVariant),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.lock_outline_rounded, size: 20, color: colorScheme.primary),
          const SizedBox(width: 10),
          const Expanded(child: Text('支付由家长本人完成。孩子使用的设备上不会出现支付入口。')),
        ],
      ),
    );
  }
}

String _newIdempotencyKey() {
  final random = Random.secure();
  final values = List<int>.generate(16, (_) => random.nextInt(256));
  final buffer = StringBuffer();
  for (final value in values) {
    buffer.write(value.toRadixString(16).padLeft(2, '0'));
  }
  return 'app-${buffer.toString()}';
}
