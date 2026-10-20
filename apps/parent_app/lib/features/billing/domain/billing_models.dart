import 'money.dart';

/// 套餐的计费周期，与后端 `billing_period` 一一对应。
enum BillingPeriod {
  monthly('monthly', '按月'),
  quarterly('quarterly', '按季'),
  yearly('yearly', '按年'),
  oneTime('one_time', '一次性'),
  unknown('unknown', '有效期内');

  const BillingPeriod(this.wireValue, this.label);

  final String wireValue;
  final String label;

  static BillingPeriod fromWireValue(String? value) {
    for (final period in values) {
      if (period != unknown && period.wireValue == value) {
        return period;
      }
    }
    return unknown;
  }

  /// 周期描述，例如「按月」或「12 个月」。
  String describe(int periodMonths) {
    if (this == oneTime || periodMonths <= 0) {
      return '一次性购买';
    }
    return '$periodMonths 个月';
  }
}

/// 可购买的服务套餐。
class BillingPlan {
  const BillingPlan({
    required this.id,
    required this.code,
    required this.displayName,
    required this.description,
    required this.billingPeriod,
    required this.periodMonths,
    required this.price,
    required this.originalPrice,
  });

  final String id;
  final String code;
  final String displayName;
  final String description;
  final BillingPeriod billingPeriod;
  final int periodMonths;
  final Money price;

  /// 划线价；为零表示当前不展示原价。
  final Money originalPrice;

  /// 只有原价高于售价时才有划线意义。
  bool get hasOriginalPrice =>
      originalPrice.isPositive && originalPrice.compareTo(price) > 0;

  /// 省下的金额，仅在展示原价时使用。
  Money get discountAmount =>
      hasOriginalPrice ? originalPrice - price : Money.zero;

  String get periodLabel => billingPeriod.describe(periodMonths);

  factory BillingPlan.fromJson(Map<String, Object?> json) {
    return BillingPlan(
      id: _requiredString(json['id'], 'id'),
      code: _requiredString(json['code'], 'code'),
      displayName: _requiredString(json['display_name'], 'display_name'),
      description: _optionalString(json['description']),
      billingPeriod: BillingPeriod.fromWireValue(
        _optionalString(json['billing_period']),
      ),
      periodMonths: _requiredInt(json['period_months'], 'period_months'),
      price: Money.fromNonNegativeCents(json['price_cny_cents']),
      originalPrice: Money.fromNonNegativeCents(
        json['original_price_cny_cents'] ?? 0,
      ),
    );
  }
}

/// 订单的完整状态机，与后端 `status` 取值一一对应。
enum BillingOrderStatus {
  pending('pending', '待支付'),
  paid('paid', '已支付'),
  cancelled('cancelled', '已取消'),
  expired('expired', '已过期'),
  refunded('refunded', '已退款'),
  partiallyRefunded('partially_refunded', '部分退款'),
  unknown('unknown', '处理中');

  const BillingOrderStatus(this.wireValue, this.label);

  final String wireValue;
  final String label;

  /// 后端新增状态时退化为中性文案，绝不直接显示原始取值。
  static BillingOrderStatus fromWireValue(String? value) {
    for (final status in values) {
      if (status != unknown && status.wireValue == value) {
        return status;
      }
    }
    return unknown;
  }

  bool get canPay => this == pending;
  bool get canCancel => this == pending;

  /// 已支付或部分退款后仍可申请剩余金额的退款。
  bool get canRequestRefund =>
      this == paid || this == partiallyRefunded;
}

/// 一条订单记录。
class BillingOrder {
  const BillingOrder({
    required this.orderNo,
    required this.planCode,
    required this.planDisplayName,
    required this.billingPeriod,
    required this.periodMonths,
    required this.unitPrice,
    required this.discount,
    required this.payable,
    required this.paid,
    required this.refunded,
    required this.status,
    required this.createdAt,
    this.expiresAt,
    this.paidAt,
  });

  final String orderNo;
  final String planCode;
  final String planDisplayName;
  final BillingPeriod billingPeriod;
  final int periodMonths;
  final Money unitPrice;
  final Money discount;
  final Money payable;
  final Money paid;
  final Money refunded;
  final BillingOrderStatus status;
  final DateTime createdAt;
  final DateTime? expiresAt;
  final DateTime? paidAt;

  bool get hasDiscount => discount.isPositive;

  /// 仍可退款的金额。
  Money get refundableAmount {
    final remaining = paid - refunded;
    return remaining.isPositive ? remaining : Money.zero;
  }

  String get periodLabel => billingPeriod.describe(periodMonths);

  BillingOrder copyWith({BillingOrderStatus? status}) {
    return BillingOrder(
      orderNo: orderNo,
      planCode: planCode,
      planDisplayName: planDisplayName,
      billingPeriod: billingPeriod,
      periodMonths: periodMonths,
      unitPrice: unitPrice,
      discount: discount,
      payable: payable,
      paid: paid,
      refunded: refunded,
      status: status ?? this.status,
      createdAt: createdAt,
      expiresAt: expiresAt,
      paidAt: paidAt,
    );
  }

  factory BillingOrder.fromJson(Map<String, Object?> json) {
    return BillingOrder(
      orderNo: _requiredString(json['order_no'], 'order_no'),
      planCode: _requiredString(json['plan_code'], 'plan_code'),
      planDisplayName: _requiredString(
        json['plan_display_name'],
        'plan_display_name',
      ),
      billingPeriod: BillingPeriod.fromWireValue(
        _optionalString(json['billing_period']),
      ),
      periodMonths: _requiredInt(json['period_months'], 'period_months'),
      unitPrice: Money.fromNonNegativeCents(json['unit_price_cny_cents']),
      discount: Money.fromNonNegativeCents(json['discount_cny_cents'] ?? 0),
      payable: Money.fromNonNegativeCents(json['payable_cny_cents']),
      paid: Money.fromNonNegativeCents(json['paid_cny_cents'] ?? 0),
      refunded: Money.fromNonNegativeCents(json['refunded_cny_cents'] ?? 0),
      status: BillingOrderStatus.fromWireValue(
        _optionalString(json['status']),
      ),
      createdAt: _requiredDate(json['created_at'], 'created_at'),
      expiresAt: _optionalDate(json['expires_at']),
      paidAt: _optionalDate(json['paid_at']),
    );
  }
}

/// 监护人对家庭消费的授权状态。没有有效授权时不允许下单。
class BillingConsent {
  const BillingConsent({
    required this.isGranted,
    this.version,
    this.grantedAt,
  });

  const BillingConsent.none()
    : isGranted = false,
      version = null,
      grantedAt = null;

  final bool isGranted;

  /// 条款版本号，只用于展示「已同意的时间」，不对外暴露内部标识。
  final String? version;
  final DateTime? grantedAt;

  factory BillingConsent.fromJson(Map<String, Object?> json) {
    final isGranted = json['granted'] == true || json['is_granted'] == true;
    return BillingConsent(
      isGranted: isGranted,
      version: _optionalString(json['consent_version']),
      grantedAt: _optionalDate(json['granted_at']),
    );
  }
}

String _requiredString(Object? value, String field) {
  if (value is String && value.isNotEmpty) {
    return value;
  }
  throw FormatException('missing $field');
}

String _optionalString(Object? value) {
  return value is String ? value : '';
}

int _requiredInt(Object? value, String field) {
  if (value is int) {
    return value;
  }
  throw FormatException('missing $field');
}

DateTime _requiredDate(Object? value, String field) {
  final parsed = _optionalDate(value);
  if (parsed == null) {
    throw FormatException('missing $field');
  }
  return parsed;
}

DateTime? _optionalDate(Object? value) {
  if (value is! String || value.isEmpty) {
    return null;
  }
  return DateTime.tryParse(value)?.toLocal();
}
