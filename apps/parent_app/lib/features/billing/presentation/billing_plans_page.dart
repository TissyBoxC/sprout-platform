import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/billing_controllers.dart';
import '../domain/billing_errors.dart';
import '../presentation/billing_widgets.dart';

/// 套餐列表页：展示价格、周期与优惠，并引导家长进入结算。
class BillingPlansPage extends ConsumerWidget {
  const BillingPlansPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final plans = ref.watch(billingPlansControllerProvider);
    final consent = ref.watch(billingConsentControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('服务套餐'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        actions: [
          IconButton(
            tooltip: '我的订单',
            onPressed: () => context.push('/me/billing/orders'),
            icon: const Icon(Icons.receipt_long_outlined),
          ),
        ],
      ),
      body: AppStateSwitcher(
        stateKey: plans.when(
          data: (value) => value.isEmpty ? 'plans-empty' : 'plans-content',
          error: (_, _) => 'plans-error',
          loading: () => 'plans-loading',
        ),
        child: plans.when(
          loading: () => const BillingLoadingState(
            key: ValueKey<String>('plans-loading'),
          ),
          error: (error, _) => BillingErrorState(
            key: const ValueKey<String>('plans-error'),
            message: billingStateErrorMessage(error),
            onRetry: () =>
                ref.read(billingPlansControllerProvider.notifier).refresh(),
          ),
          data: (value) => value.isEmpty
              ? BillingEmptyState(
                  key: const ValueKey<String>('plans-empty'),
                  icon: Icons.spa_outlined,
                  title: '暂时还没有可选套餐',
                  message: '服务套餐还在准备中。你可以先返回首页，稍后再来查看。',
                  actionLabel: '返回首页',
                  onAction: () => context.go('/home'),
                )
              : _PlansContent(
                  key: const ValueKey<String>('plans-content'),
                  plans: value,
                  isConsentGranted: consent.value?.isGranted ?? false,
                  hasConsentState: consent.hasValue,
                  onConsentManage: () => context.push('/me/billing/consent'),
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

class _PlansContent extends ConsumerWidget {
  const _PlansContent({
    required this.plans,
    required this.isConsentGranted,
    required this.hasConsentState,
    required this.onConsentManage,
    super.key,
  });

  final List<BillingPlan> plans;
  final bool isConsentGranted;
  final bool hasConsentState;
  final VoidCallback onConsentManage;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return RefreshIndicator(
      onRefresh: () => ref.read(billingPlansControllerProvider.notifier).refresh(),
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(child: _BillingIntroCard(onOrders: () => context.push('/me/billing/orders'))),
          const SizedBox(height: 14),
          if (hasConsentState && !isConsentGranted) ...[
            AppReveal(
              delay: const Duration(milliseconds: 40),
              child: _ConsentHint(onOpen: onConsentManage),
            ),
            const SizedBox(height: 14),
          ],
          for (var index = 0; index < plans.length; index++) ...[
            AppReveal(
              delay: Duration(milliseconds: 60 + index * 40),
              child: _PlanCard(
                plan: plans[index],
                onBuy: () => context.push(
                  '/me/billing/checkout',
                  extra: plans[index],
                ),
              ),
            ),
            const SizedBox(height: 14),
          ],
        ],
      ),
    );
  }
}

class _BillingIntroCard extends StatelessWidget {
  const _BillingIntroCard({required this.onOrders});

  final VoidCallback onOrders;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  width: 44,
                  height: 44,
                  decoration: BoxDecoration(
                    color: colorScheme.primaryContainer,
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: Icon(Icons.auto_awesome_rounded, color: colorScheme.primary),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    '选择适合家庭的陪伴服务',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            const Text('购买成功后，服务会在有效期内为家庭提供额度，到期前会提醒续费。'),
            const SizedBox(height: 6),
            TextButton.icon(
              onPressed: onOrders,
              icon: const Icon(Icons.receipt_long_outlined),
              label: const Text('查看我的订单'),
            ),
          ],
        ),
      ),
    );
  }
}

class _ConsentHint extends StatelessWidget {
  const _ConsentHint({required this.onOpen});

  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: colorScheme.primaryContainer.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.shield_outlined, size: 20, color: colorScheme.primary),
              const SizedBox(width: 8),
              Text('需要监护人先同意消费条款', style: Theme.of(context).textTheme.titleSmall),
            ],
          ),
          const SizedBox(height: 6),
          const Text('购买会使用真实的钱。为了孩子的安全，只有监护人同意后才能购买。'),
          const SizedBox(height: 8),
          OutlinedButton(onPressed: onOpen, child: const Text('查看消费条款')),
        ],
      ),
    );
  }
}

class _PlanCard extends StatelessWidget {
  const _PlanCard({required this.plan, required this.onBuy});

  final BillingPlan plan;
  final VoidCallback onBuy;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
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
                        plan.displayName,
                        style: theme.textTheme.titleLarge?.copyWith(
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(plan.periodLabel, style: theme.textTheme.bodySmall),
                    ],
                  ),
                ),
                BillingStatusBadge(label: plan.billingPeriod.label, isEmphasized: true),
              ],
            ),
            if (plan.description.isNotEmpty) ...[
              const SizedBox(height: 10),
              Text(plan.description),
            ],
            const SizedBox(height: 16),
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                BillingPriceText(
                  money: plan.price,
                  semanticPrefix: '价格',
                  style: theme.textTheme.headlineSmall?.copyWith(
                    fontWeight: FontWeight.w800,
                    color: colorScheme.primary,
                  ),
                ),
                if (plan.hasOriginalPrice) ...[
                  const SizedBox(width: 8),
                  Padding(
                    padding: const EdgeInsets.only(bottom: 4),
                    child: BillingOriginalPrice(money: plan.originalPrice),
                  ),
                ],
                const Spacer(),
                if (plan.hasOriginalPrice)
                  Text(
                    '已优惠 ${plan.discountAmount.format()}',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: colorScheme.primary,
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 16),
            FilledButton.icon(
              key: ValueKey<String>('plan-buy-${plan.code}'),
              onPressed: onBuy,
              icon: const Icon(Icons.shopping_bag_outlined),
              label: const Text('选择这个套餐'),
            ),
          ],
        ),
      ),
    );
  }
}
