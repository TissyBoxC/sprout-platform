import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/error/app_exception.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../family/application/family_controller.dart';
import '../application/billing_controllers.dart';
import '../domain/billing_errors.dart';
import '../presentation/billing_widgets.dart';

/// 家长端「消费限额」页：看本月花了多少，并设置每月的金额上限。
///
/// 这里只负责家庭维度的月度上限；购买流程仍由结算页负责。保存后立即用
/// 服务端返回的余额刷新进度条，家长不需要手动重新加载。
class SpendingLimitPage extends ConsumerStatefulWidget {
  const SpendingLimitPage({super.key});

  @override
  ConsumerState<SpendingLimitPage> createState() => _SpendingLimitPageState();
}

class _SpendingLimitPageState extends ConsumerState<SpendingLimitPage> {
  final _formKey = GlobalKey<FormState>();
  final _amountController = TextEditingController();

  /// 最近一次从服务端拿到的限额，输入框只在它变化时整体重置。
  SpendingLimit? _syncedLimit;
  bool _isSaving = false;
  String? _actionError;

  @override
  void dispose() {
    _amountController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final limit = ref.watch(spendingLimitControllerProvider);
    final family = ref.watch(familyControllerProvider);
    _syncAmountField(limit.value);

    final canManage = _canManageLimit(family);
    return Scaffold(
      appBar: AppBar(
        title: const Text('消费限额'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: _stateKey(limit, family, canManage),
        child: _buildBody(limit, family, canManage),
      ),
    );
  }

  Widget _buildBody(
    AsyncValue<SpendingLimit> limit,
    AsyncValue<FamilyMembershipState> family,
    bool canManage,
  ) {
    if (family.hasError) {
      return _LimitErrorCard(
        key: const ValueKey<String>('spending-limit-family-error'),
        message: '暂时无法读取家庭信息，消费限额没有加载出来',
        retryLabel: '重新加载家庭信息',
        onRetry: () => ref.invalidate(familyControllerProvider),
      );
    }
    if (!family.hasValue) {
      return const BillingLoadingState(
        key: ValueKey<String>('spending-limit-loading'),
      );
    }
    if (!canManage) {
      return const _NoPermissionState(
        key: ValueKey<String>('spending-limit-no-permission'),
      );
    }
    return limit.when(
      loading: () => const BillingLoadingState(
        key: ValueKey<String>('spending-limit-loading'),
      ),
      error: (error, _) => _limitErrorState(error),
      data: (value) => _content(value),
    );
  }

  Widget _limitErrorState(Object error) {
    if (error is StaleBillingResultException) {
      return const BillingLoadingState(
        key: ValueKey<String>('spending-limit-loading'),
      );
    }
    // 家庭存在但还没有保存过上限时，后端可能按「未找到」返回；这不是故障，
    // 用空态说明为什么空以及下一步该填什么。
    final isMissing =
        (error is AppException && error.kind == AppErrorKind.notFound) ||
        error is FormatException;
    if (isMissing) {
      return BillingEmptyState(
        key: const ValueKey<String>('spending-limit-empty'),
        icon: Icons.savings_outlined,
        title: '还没有设置消费限额',
        message: '设置每月上限后，这里会显示本月已花金额和剩余额度。填 0 表示不限额。',
        actionLabel: '重新读取',
        onAction: () =>
            ref.read(spendingLimitControllerProvider.notifier).refresh(),
      );
    }
    return _LimitErrorCard(
      key: const ValueKey<String>('spending-limit-error'),
      message: _loadErrorMessage(error),
      onRetry: () =>
          ref.read(spendingLimitControllerProvider.notifier).refresh(),
    );
  }

  Widget _content(SpendingLimit limit) {
    final theme = Theme.of(context);
    return Form(
      key: _formKey,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(child: _LimitOverviewCard(limit: limit)),
          const SizedBox(height: 14),
          AppReveal(
            delay: const Duration(milliseconds: 50),
            child: Card(
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('每月限额', style: theme.textTheme.titleMedium),
                    const SizedBox(height: 6),
                    const Text('输入金额，单位是元。填写 0 表示不限额，次月会按新的设置重新计算。'),
                    const SizedBox(height: 14),
                    TextFormField(
                      key: const ValueKey<String>('spending-limit-input'),
                      controller: _amountController,
                      keyboardType: const TextInputType.numberWithOptions(
                        decimal: true,
                      ),
                      inputFormatters: [
                        FilteringTextInputFormatter.allow(
                          RegExp(r'^\d{0,7}(\.\d{0,2})?$'),
                        ),
                      ],
                      decoration: const InputDecoration(
                        labelText: '每月上限（元）',
                        hintText: '例如 200',
                        prefixText: '¥ ',
                        helperText: '最多 7 位整数和 2 位小数',
                      ),
                      validator: _validateAmount,
                    ),
                    const SizedBox(height: 6),
                    Align(
                      alignment: Alignment.centerLeft,
                      child: TextButton.icon(
                        onPressed: () => setState(
                          () => _amountController.text = '0',
                        ),
                        icon: const Icon(Icons.all_inclusive_rounded, size: 18),
                        label: const Text('改为不限额'),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
          if (_actionError != null) ...[
            const SizedBox(height: 14),
            BillingInlineNotice(message: _actionError!, isWarning: true),
          ],
          const SizedBox(height: 18),
          FilledButton.icon(
            key: const ValueKey<String>('spending-limit-save'),
            onPressed: _isSaving ? null : _save,
            icon: _isSaving
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.savings_outlined),
            label: Text(_isSaving ? '正在保存…' : '保存限额'),
          ),
          const SizedBox(height: 12),
          const BillingInlineNotice(
            icon: Icons.lock_outline_rounded,
            message: '达到上限后，新的套餐订单会暂时无法创建，已经付过款的订单不受影响。',
          ),
        ],
      ),
    );
  }

  /// 服务端数据变化后把输入框重置为最新限额，避免旧草稿覆盖新值。
  void _syncAmountField(SpendingLimit? limit) {
    if (limit == null || identical(limit, _syncedLimit)) {
      return;
    }
    _syncedLimit = limit;
    // 输入框只在数据初始化时赋值；不限额时留空，让家长自己决定填多少。
    _amountController.text = limit.isUnlimited
        ? ''
        : limit.monthlyLimit.yuanString();
  }

  String? _validateAmount(String? raw) {
    final text = (raw ?? '').trim();
    if (text.isEmpty) {
      return '请输入每月上限，填 0 表示不限额';
    }
    final Money amount;
    try {
      amount = Money.fromYuanString(text);
    } on MoneyFormatException {
      return '请输入正确的金额，例如 200 或 199.90';
    }
    if (amount.cents < 0) {
      return '金额不能是负数';
    }
    // 上限过大通常是把「分」当成了「元」，拦下来比默默保存更安全。
    if (amount.cents > _maxMonthlyLimitCents) {
      return '每月上限不能超过 ¥99999.99';
    }
    return null;
  }

  Future<void> _save() async {
    if (_isSaving || !(_formKey.currentState?.validate() ?? false)) {
      return;
    }
    final cents = Money.fromYuanString(_amountController.text.trim()).cents;
    setState(() {
      _isSaving = true;
      _actionError = null;
    });
    try {
      await ref
          .read(spendingLimitControllerProvider.notifier)
          .saveMonthlyLimit(cents);
      if (!mounted) {
        return;
      }
      FocusScope.of(context).unfocus();
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(
          SnackBar(
            content: Text(cents == 0 ? '已保存，本月不再限制消费金额' : '已保存，本月限额已更新'),
          ),
        );
    } on Object catch (error) {
      if (mounted) {
        setState(() => _actionError = _saveErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isSaving = false);
      }
    }
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
    } else {
      Navigator.of(context).maybePop();
    }
  }
}

/// 每月上限的最大值（分），与输入框的位数限制保持一致。
const _maxMonthlyLimitCents = 9999999;

String _stateKey(
  AsyncValue<SpendingLimit> limit,
  AsyncValue<FamilyMembershipState> family,
  bool canManage,
) {
  if (family.hasError) {
    return 'spending-limit-family-error';
  }
  if (!family.hasValue) {
    return 'spending-limit-loading';
  }
  if (!canManage) {
    return 'spending-limit-no-permission';
  }
  return limit.when(
    loading: () => 'spending-limit-loading',
    error: (_, _) => 'spending-limit-error',
    data: (_) => 'spending-limit-content',
  );
}

/// 只有家庭所有者可以改消费上限；其他成员看到只读说明。
///
/// 权限以家庭能力摘要里的 `spending_limit_manage` 为准；旧版本摘要还没
/// 下发这项声明时退回到「是否所有者」判断，避免把有权限的家长挡在外面。
bool _canManageLimit(AsyncValue<FamilyMembershipState> family) {
  final state = family.value;
  if (state == null) {
    return true;
  }
  final capability = state.capability;
  if (capability.permissions.containsKey(_managePermission)) {
    return capability.allows(_managePermission);
  }
  return state.members.any(
    (member) => member.isOwner && member.userId == capability.ownerUserId,
  );
}

/// 后端声明的「修改消费上限」权限名。
const _managePermission = 'spending_limit_manage';

String _saveErrorMessage(Object error) {
  if (error is AppException) {
    return switch (error.kind) {
      AppErrorKind.insufficientPermission => '只有家庭所有者可以修改消费限额，当前设置没有变化',
      AppErrorKind.validation => '这个金额暂时不能保存，请换一个金额再试',
      AppErrorKind.conflict => '限额刚在其他页面改过，请重新加载后再保存',
      AppErrorKind.unauthenticated => '登录已过期，金额没有保存，请重新登录后再试',
      AppErrorKind.network => '网络连接不稳定，金额没有保存，请检查后重试',
      AppErrorKind.serviceUnavailable => '服务暂时不可用，金额没有保存，请稍后重试',
      _ => '金额没有保存，请稍后重试',
    };
  }
  if (error is FormatException || error is MoneyFormatException) {
    return '返回的金额无法识别，请重新加载后再试';
  }
  return '金额没有保存，请稍后重试';
}

/// 读取失败的提示，区分网络与服务状态，并说明数据没有改动。
String _loadErrorMessage(Object error) {
  if (error is AppException) {
    return switch (error.kind) {
      AppErrorKind.network => '网络连接不稳定，消费限额没有加载出来，请检查网络后重试',
      AppErrorKind.serviceUnavailable => '服务暂时不可用，消费限额没有加载出来，请稍后重试',
      AppErrorKind.unauthenticated => '登录已过期，消费限额没有加载出来，请重新登录后再试',
      _ => '暂时无法读取消费限额，请稍后重试',
    };
  }
  return '暂时无法读取消费限额，请稍后重试';
}

/// 读取失败的卡片：说明发生了什么、提供重试，并保留返回上一页的出口。
class _LimitErrorCard extends StatelessWidget {
  const _LimitErrorCard({
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
            const SizedBox(height: 18),
            FilledButton.icon(
              onPressed: onRetry,
              icon: const Icon(Icons.refresh_rounded),
              label: Text(retryLabel),
            ),
            const SizedBox(height: 8),
            TextButton(
              onPressed: () => Navigator.of(context).maybePop(),
              child: const Text('返回上一页'),
            ),
          ],
        ),
      ),
    );
  }
}

/// 本月消费概览：已花、上限与剩余，并用进度条表示已用比例。
class _LimitOverviewCard extends StatelessWidget {
  const _LimitOverviewCard({required this.limit});

  final SpendingLimit limit;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;
    final progressColor = limit.isReached
        ? colorScheme.error
        : colorScheme.primary;
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
                  child: Icon(
                    Icons.account_balance_wallet_outlined,
                    color: colorScheme.primary,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('本月已消费', style: theme.textTheme.bodySmall),
                      Text(
                        limit.monthSpent.format(),
                        key: const ValueKey<String>('spending-limit-spent'),
                        style: theme.textTheme.headlineSmall?.copyWith(
                          fontWeight: FontWeight.w800,
                          color: colorScheme.primary,
                        ),
                      ),
                    ],
                  ),
                ),
                BillingStatusBadge(
                  label: limit.isUnlimited ? '不限额' : '每月限额',
                  isEmphasized: !limit.isUnlimited,
                ),
              ],
            ),
            const SizedBox(height: 16),
            ClipRRect(
              borderRadius: BorderRadius.circular(99),
              child: LinearProgressIndicator(
                key: const ValueKey<String>('spending-limit-progress'),
                // 不限额时没有比例可画，用一条完整的浅色底表示没有约束。
                value: limit.isUnlimited ? 1 : limit.usedRatio,
                minHeight: 10,
                color: limit.isUnlimited
                    ? colorScheme.primary.withValues(alpha: 0.25)
                    : progressColor,
                backgroundColor: colorScheme.primaryContainer.withValues(
                  alpha: 0.6,
                ),
              ),
            ),
            const SizedBox(height: 10),
            Text(
              limit.isUnlimited
                  ? '当前没有设置上限，购买套餐不受金额限制。'
                  : '上限 ${limit.monthlyLimit.format()}，'
                        '${limit.isReached ? '本月额度已经用完' : '还可以用 ${limit.remaining.format()}'}',
              key: const ValueKey<String>('spending-limit-summary'),
              style: theme.textTheme.bodyMedium,
            ),
            if (!limit.isUnlimited && limit.usedRatio >= 0.8) ...[
              const SizedBox(height: 6),
              Text(
                limit.isReached
                    ? '想继续购买，可以把上限调高，或者等下个月额度重置。'
                    : '本月额度快用完了，可以提前调整上限。',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: colorScheme.onSurfaceVariant,
                ),
              ),
            ],
            if (limit.updatedAt != null) ...[
              const SizedBox(height: 8),
              Text(
                '上次调整 ${billingDateLabel(limit.updatedAt!)}',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// 非家庭所有者看到只读说明，并说明谁能修改。
class _NoPermissionState extends StatelessWidget {
  const _NoPermissionState({super.key});

  @override
  Widget build(BuildContext context) {
    return Center(
      key: const ValueKey<String>('spending-limit-no-permission'),
      child: Padding(
        padding: const EdgeInsets.all(28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.lock_person_outlined,
              size: 44,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 14),
            Text(
              '只有家庭所有者可以设置消费限额',
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: 8),
            const Text(
              '你当前的成员角色只能查看购买记录。需要调整限额时，请让家庭所有者在自己的账号里修改。',
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 18),
            OutlinedButton.icon(
              onPressed: () => Navigator.of(context).maybePop(),
              icon: const Icon(Icons.arrow_back_rounded),
              label: const Text('返回上一页'),
            ),
          ],
        ),
      ),
    );
  }
}
