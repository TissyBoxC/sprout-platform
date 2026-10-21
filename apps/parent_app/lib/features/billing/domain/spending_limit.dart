import 'money.dart';

/// 家庭每月的消费金额上限与本月已花金额。
///
/// 限额与已花金额都以整数分保存；限额为零表示不限制。界面只在展示时换算成
/// `¥X.XX`，计算与比较始终使用分，避免浮点误差。
class SpendingLimit {
  const SpendingLimit({
    required this.monthlyLimit,
    required this.monthSpent,
    this.updatedAt,
  });

  final Money monthlyLimit;
  final Money monthSpent;
  final DateTime? updatedAt;

  /// 是否没有设置上限。
  bool get isUnlimited => !monthlyLimit.isPositive;

  /// 已花金额占上限的比例，限制在 0 到 1 之间供进度条使用。
  double get usedRatio {
    if (isUnlimited) {
      return 0;
    }
    final ratio = monthSpent.cents / monthlyLimit.cents;
    return ratio.clamp(0.0, 1.0).toDouble();
  }

  /// 本月还能花多少；未设上限或被扣减时返回零。
  Money get remaining {
    if (isUnlimited) {
      return Money.zero;
    }
    final left = monthlyLimit - monthSpent;
    return left.isPositive ? left : Money.zero;
  }

  /// 本月消费是否已经达到或超过上限。
  bool get isReached => !isUnlimited && monthSpent.cents >= monthlyLimit.cents;

  /// 后端约定 `updated_at` 是时间字符串；缺失时按「未记录」处理，不阻断展示。
  factory SpendingLimit.fromJson(Map<String, Object?> json) {
    final rawUpdatedAt = json['updated_at'];
    return SpendingLimit(
      monthlyLimit: Money.fromNonNegativeCents(json['monthly_limit_cents']),
      monthSpent: Money.fromNonNegativeCents(
        json['current_month_spent_cents'],
      ),
      updatedAt: rawUpdatedAt is String && rawUpdatedAt.isNotEmpty
          ? DateTime.tryParse(rawUpdatedAt)?.toLocal()
          : null,
    );
  }
}
