import 'package:flutter/material.dart';

import '../domain/money.dart';

/// 价格文本：整数分渲染为 `¥9.90`，永不出现小数以外的格式。
class BillingPriceText extends StatelessWidget {
  const BillingPriceText({
    required this.money,
    this.style,
    this.semanticPrefix,
    super.key,
  });

  final Money money;
  final TextStyle? style;
  final String? semanticPrefix;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: semanticPrefix == null ? null : '$semanticPrefix${money.format()}',
      excludeSemantics: semanticPrefix != null,
      child: Text(money.format(), style: style),
    );
  }
}

/// 有原价时的划线价；没有原价时不占位。
class BillingOriginalPrice extends StatelessWidget {
  const BillingOriginalPrice({required this.money, super.key});

  final Money money;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Semantics(
      label: '原价 ${money.format()}',
      excludeSemantics: true,
      child: Text(
        money.format(),
        style: Theme.of(context).textTheme.bodySmall?.copyWith(
          color: colorScheme.onSurfaceVariant,
          decoration: TextDecoration.lineThrough,
          decorationColor: colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }
}

/// 订单状态徽标，颜色随状态变化但始终使用主题色。
class BillingStatusBadge extends StatelessWidget {
  const BillingStatusBadge({
    required this.label,
    this.isEmphasized = false,
    this.isMuted = false,
    super.key,
  });

  final String label;
  final bool isEmphasized;
  final bool isMuted;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    final color = isMuted
        ? colorScheme.onSurfaceVariant
        : isEmphasized
        ? colorScheme.primary
        : colorScheme.onSurfaceVariant;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(99),
      ),
      child: Text(
        label,
        style: Theme.of(
          context,
        ).textTheme.labelMedium?.copyWith(color: color),
      ),
    );
  }
}

/// 页面级的加载状态。
class BillingLoadingState extends StatelessWidget {
  const BillingLoadingState({this.label = '正在加载…', super.key});

  final String label;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            SizedBox(
              width: 34,
              height: 34,
              child: CircularProgressIndicator(
                strokeWidth: 3,
                color: colorScheme.primary,
              ),
            ),
            const SizedBox(height: 14),
            Text(label, style: Theme.of(context).textTheme.bodyMedium),
          ],
        ),
      ),
    );
  }
}

/// 页面级的空状态：说明为什么为空，并给出下一步。
class BillingEmptyState extends StatelessWidget {
  const BillingEmptyState({
    required this.title,
    required this.message,
    required this.icon,
    this.actionLabel,
    this.onAction,
    super.key,
  });

  final String title;
  final String message;
  final IconData icon;
  final String? actionLabel;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 78,
              height: 78,
              decoration: BoxDecoration(
                color: colorScheme.primaryContainer,
                shape: BoxShape.circle,
              ),
              child: Icon(icon, size: 38, color: colorScheme.primary),
            ),
            const SizedBox(height: 18),
            Text(title, style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 8),
            Text(message, textAlign: TextAlign.center),
            if (actionLabel != null && onAction != null) ...[
              const SizedBox(height: 18),
              OutlinedButton.icon(
                onPressed: onAction,
                icon: const Icon(Icons.arrow_forward_rounded),
                label: Text(actionLabel!),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// 页面级的失败状态：说明发生了什么，并提供重新加载。
class BillingErrorState extends StatelessWidget {
  const BillingErrorState({
    required this.message,
    required this.onRetry,
    this.retryLabel = '重新加载',
    super.key,
  });

  final String message;
  final VoidCallback onRetry;
  final String retryLabel;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.cloud_off_outlined,
              size: 44,
              color: colorScheme.primary,
            ),
            const SizedBox(height: 14),
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: onRetry,
              icon: const Icon(Icons.refresh_rounded),
              label: Text(retryLabel),
            ),
          ],
        ),
      ),
    );
  }
}

/// 表单内的行内提示，用于动作失败后的解释。
class BillingInlineNotice extends StatelessWidget {
  const BillingInlineNotice({
    required this.message,
    this.icon = Icons.info_outline_rounded,
    this.isWarning = false,
    super.key,
  });

  final String message;
  final IconData icon;
  final bool isWarning;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    final accent = isWarning ? colorScheme.error : colorScheme.primary;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: accent.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: accent.withValues(alpha: 0.35)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 20, color: accent),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              message,
              style: Theme.of(
                context,
              ).textTheme.bodyMedium?.copyWith(color: colorScheme.onSurface),
            ),
          ),
        ],
      ),
    );
  }
}

/// 时间文本，只展示到分钟，避免把时区细节抛给家长。
String billingDateLabel(DateTime value) {
  final local = value.toLocal();
  String two(int number) => number.toString().padLeft(2, '0');
  return '${local.year}-${two(local.month)}-${two(local.day)} '
      '${two(local.hour)}:${two(local.minute)}';
}
