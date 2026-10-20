import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/billing_controllers.dart';
import '../domain/billing_errors.dart';
import '../presentation/billing_widgets.dart';

/// 订单记录页：家长查看历史订单，并为已支付订单申请退款。
class BillingOrdersPage extends ConsumerWidget {
  const BillingOrdersPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final orders = ref.watch(billingOrdersControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('我的订单'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        actions: [
          IconButton(
            tooltip: '服务套餐',
            onPressed: () => context.push('/me/billing'),
            icon: const Icon(Icons.card_giftcard_outlined),
          ),
        ],
      ),
      body: AppStateSwitcher(
        stateKey: orders.when(
          data: (value) => value.isEmpty ? 'orders-empty' : 'orders-content',
          error: (_, _) => 'orders-error',
          loading: () => 'orders-loading',
        ),
        child: orders.when(
          loading: () => const BillingLoadingState(
            key: ValueKey<String>('orders-loading'),
          ),
          error: (error, _) => BillingErrorState(
            key: const ValueKey<String>('orders-error'),
            message: billingStateErrorMessage(error),
            onRetry: () =>
                ref.read(billingOrdersControllerProvider.notifier).refresh(),
          ),
          data: (value) => value.isEmpty
              ? BillingEmptyState(
                  key: const ValueKey<String>('orders-empty'),
                  icon: Icons.receipt_long_outlined,
                  title: '还没有订单',
                  message: '在服务套餐里选择并支付后，订单会显示在这里，方便随时查看和申请退款。',
                  actionLabel: '查看服务套餐',
                  onAction: () => context.push('/me/billing'),
                )
              : _OrdersList(
                  key: const ValueKey<String>('orders-content'),
                  state: value,
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

class _OrdersList extends ConsumerWidget {
  const _OrdersList({required this.state, super.key});

  final BillingOrdersState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return RefreshIndicator(
      onRefresh: () =>
          ref.read(billingOrdersControllerProvider.notifier).refresh(),
      child: ListView.separated(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        itemCount: state.orders.length + 1,
        separatorBuilder: (context, index) => const SizedBox(height: 12),
        itemBuilder: (context, index) {
          if (index == 0) {
            return AppReveal(child: _OrdersHeader(total: state.total));
          }
          final order = state.orders[index - 1];
          return AppReveal(
            delay: Duration(milliseconds: 30 * (index > 4 ? 4 : index)),
            child: _OrderCard(
              order: order,
              onRequestRefund: () => _requestRefund(context, ref, order),
              onCancel: () => _cancel(context, ref, order),
            ),
          );
        },
      ),
    );
  }

  Future<void> _requestRefund(
    BuildContext context,
    WidgetRef ref,
    BillingOrder order,
  ) async {
    final reason = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      builder: (sheetContext) => _RefundSheet(order: order),
    );
    if (reason == null || !context.mounted) {
      return;
    }
    try {
      await ref
          .read(billingOrdersControllerProvider.notifier)
          .requestRefund(orderNo: order.orderNo, reason: reason);
      if (context.mounted) {
        _showMessage(context, '退款申请已提交，审核结果会显示在订单状态里');
      }
    } on Object catch (error) {
      if (context.mounted) {
        _showMessage(context, billingActionErrorMessage(error));
      }
    }
  }

  Future<void> _cancel(
    BuildContext context,
    WidgetRef ref,
    BillingOrder order,
  ) async {
    final isConfirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('取消这笔订单吗？'),
        content: const Text('取消后订单不能再支付。需要时可以在服务套餐里重新选择。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('保留订单'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('取消订单'),
          ),
        ],
      ),
    );
    if (isConfirmed != true || !context.mounted) {
      return;
    }
    try {
      await ref
          .read(billingOrdersControllerProvider.notifier)
          .cancel(order.orderNo);
      if (context.mounted) {
        _showMessage(context, '订单已取消');
      }
    } on Object catch (error) {
      if (context.mounted) {
        _showMessage(context, billingActionErrorMessage(error));
      }
    }
  }

  void _showMessage(BuildContext context, String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }
}

class _OrdersHeader extends StatelessWidget {
  const _OrdersHeader({required this.total});

  final int total;

  @override
  Widget build(BuildContext context) {
    final isSingle = total <= 1;
    return Padding(
      padding: const EdgeInsets.only(bottom: 4),
      child: Text(
        isSingle ? '共 1 笔订单' : '共 $total 笔订单，按时间从新到旧排列',
        style: Theme.of(context).textTheme.bodySmall,
      ),
    );
  }
}

class _OrderCard extends StatelessWidget {
  const _OrderCard({
    required this.order,
    required this.onRequestRefund,
    required this.onCancel,
  });

  final BillingOrder order;
  final VoidCallback onRequestRefund;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;
    final status = order.status;
    return Card(
      key: ValueKey<String>('order-${order.orderNo}'),
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        order.planDisplayName,
                        style: theme.textTheme.titleMedium?.copyWith(
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 3),
                      Text(order.periodLabel, style: theme.textTheme.bodySmall),
                    ],
                  ),
                ),
                BillingStatusBadge(
                  label: status.label,
                  isEmphasized: status == BillingOrderStatus.paid,
                  isMuted: status == BillingOrderStatus.cancelled ||
                      status == BillingOrderStatus.expired,
                ),
              ],
            ),
            const SizedBox(height: 12),
            _AmountRow(label: '订单金额', value: order.unitPrice.format()),
            if (order.hasDiscount)
              _AmountRow(label: '优惠', value: '-${order.discount.format()}'),
            Row(
              children: [
                Text('应付金额', style: theme.textTheme.bodySmall),
                const Spacer(),
                Text(
                  order.payable.format(),
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w800,
                    color: colorScheme.primary,
                  ),
                ),
              ],
            ),
            if (order.paid.isPositive) ...[
              const SizedBox(height: 4),
              _AmountRow(label: '实付金额', value: order.paid.format()),
            ],
            if (order.refunded.isPositive) ...[
              const SizedBox(height: 4),
              _AmountRow(label: '已退金额', value: order.refunded.format()),
            ],
            const SizedBox(height: 10),
            Text(
              '订单号 ${order.orderNo}',
              style: theme.textTheme.bodySmall,
            ),
            Text(
              '下单时间 ${billingDateLabel(order.createdAt)}',
              style: theme.textTheme.bodySmall,
            ),
            if (status == BillingOrderStatus.pending && order.expiresAt != null)
              Text(
                '请在 ${billingDateLabel(order.expiresAt!)} 前完成支付',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: colorScheme.primary,
                ),
              ),
            if (status == BillingOrderStatus.paid && order.paidAt != null)
              Text(
                '支付时间 ${billingDateLabel(order.paidAt!)}',
                style: theme.textTheme.bodySmall,
              ),
            if (status == BillingOrderStatus.partiallyRefunded)
              Text(
                '剩余可退 ${order.refundableAmount.format()}',
                style: theme.textTheme.bodySmall,
              ),
            if (status.canRequestRefund) ...[
              const SizedBox(height: 12),
              OutlinedButton.icon(
                key: ValueKey<String>('order-refund-${order.orderNo}'),
                onPressed: onRequestRefund,
                icon: const Icon(Icons.assignment_return_outlined),
                label: const Text('申请退款'),
              ),
            ],
            if (status.canCancel) ...[
              const SizedBox(height: 8),
              TextButton(
                key: ValueKey<String>('order-cancel-${order.orderNo}'),
                onPressed: onCancel,
                child: const Text('取消订单'),
              ),
            ],
            if (status == BillingOrderStatus.unknown)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  '这笔订单正在处理中，稍后刷新可以看到最新状态。',
                  style: theme.textTheme.bodySmall,
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _AmountRow extends StatelessWidget {
  const _AmountRow({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: Row(
        children: [
          Text(label, style: theme.textTheme.bodySmall),
          const Spacer(),
          Text(
            value,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurface,
            ),
          ),
        ],
      ),
    );
  }
}

/// 退款申请表单：原因必填，避免空原因提交。
class _RefundSheet extends StatefulWidget {
  const _RefundSheet({required this.order});

  final BillingOrder order;

  @override
  State<_RefundSheet> createState() => _RefundSheetState();
}

class _RefundSheetState extends State<_RefundSheet> {
  static const _maxReasonLength = 200;

  final _controller = TextEditingController();
  String? _errorText;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;
    final insets = MediaQuery.viewInsetsOf(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(20, 8, 20, 20 + insets.bottom),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('申请退款', style: theme.textTheme.titleLarge),
          const SizedBox(height: 8),
          Text(
            '本次可退 ${widget.order.refundableAmount.format()}。'
            '提交后会由平台审核，审核结果会显示在订单状态里。',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 16),
          TextField(
            key: const ValueKey<String>('refund-reason'),
            controller: _controller,
            maxLines: 4,
            maxLength: _maxReasonLength,
            textInputAction: TextInputAction.newline,
            decoration: InputDecoration(
              labelText: '退款原因',
              hintText: '例如：买错了套餐，需要更换',
              errorText: _errorText,
            ),
            onChanged: (_) {
              if (_errorText != null) {
                setState(() => _errorText = null);
              }
            },
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              Expanded(
                child: OutlinedButton(
                  onPressed: () => Navigator.of(context).pop(),
                  child: Text(
                    '暂不退款',
                    style: TextStyle(color: colorScheme.onSurfaceVariant),
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: FilledButton(
                  key: const ValueKey<String>('refund-submit'),
                  onPressed: _submit,
                  child: const Text('提交退款申请'),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  void _submit() {
    final reason = _controller.text.trim();
    if (reason.isEmpty) {
      setState(() => _errorText = '请填写退款原因');
      return;
    }
    Navigator.of(context).pop(reason);
  }
}

/// 订单状态文案，供测试与页面共用。
String billingOrderStatusLabel(BillingOrderStatus status) => status.label;
