import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/billing/domain/billing_errors.dart';

void main() {
  test('maps every order status to a Simplified Chinese label', () {
    expect(BillingOrderStatus.fromWireValue('pending').label, '待支付');
    expect(BillingOrderStatus.fromWireValue('paid').label, '已支付');
    expect(BillingOrderStatus.fromWireValue('cancelled').label, '已取消');
    expect(BillingOrderStatus.fromWireValue('expired').label, '已过期');
    expect(BillingOrderStatus.fromWireValue('refunded').label, '已退款');
    expect(
      BillingOrderStatus.fromWireValue('partially_refunded').label,
      '部分退款',
    );
  });

  test('degrades an unknown status to a safe neutral label', () {
    final status = BillingOrderStatus.fromWireValue('chargeback_pending');
    expect(status, BillingOrderStatus.unknown);
    expect(status.label, '处理中');
    expect(status.label, isNot(contains('chargeback')));
    expect(status.canPay, isFalse);
    expect(status.canCancel, isFalse);
    expect(status.canRequestRefund, isFalse);
  });

  test('an empty or missing status never leaks the raw value', () {
    expect(BillingOrderStatus.fromWireValue(null), BillingOrderStatus.unknown);
    expect(BillingOrderStatus.fromWireValue(''), BillingOrderStatus.unknown);
  });

  test('only a pending order can be paid or cancelled', () {
    expect(BillingOrderStatus.pending.canPay, isTrue);
    expect(BillingOrderStatus.pending.canCancel, isTrue);
    for (final status in [
      BillingOrderStatus.paid,
      BillingOrderStatus.cancelled,
      BillingOrderStatus.expired,
      BillingOrderStatus.refunded,
      BillingOrderStatus.partiallyRefunded,
    ]) {
      expect(status.canPay, isFalse, reason: status.wireValue);
    }
  });

  test('paid and partially refunded orders can request a refund', () {
    expect(BillingOrderStatus.paid.canRequestRefund, isTrue);
    expect(BillingOrderStatus.partiallyRefunded.canRequestRefund, isTrue);
    expect(BillingOrderStatus.refunded.canRequestRefund, isFalse);
    expect(BillingOrderStatus.cancelled.canRequestRefund, isFalse);
  });

  test('parses an order with integer cent amounts only', () {
    final order = BillingOrder.fromJson({
      'order_no': 'SP20261020A1B2C3D4',
      'plan_code': 'starter_monthly',
      'plan_display_name': '初芽体验包',
      'billing_period': 'monthly',
      'period_months': 1,
      'unit_price_cny_cents': 990,
      'discount_cny_cents': 90,
      'payable_cny_cents': 900,
      'paid_cny_cents': 900,
      'refunded_cny_cents': 300,
      'status': 'partially_refunded',
      'expires_at': '2026-10-20T10:30:00Z',
      'paid_at': '2026-10-20T10:05:00Z',
      'created_at': '2026-10-20T10:00:00Z',
    });

    expect(order.unitPrice.format(), '¥9.90');
    expect(order.discount.format(), '¥0.90');
    expect(order.payable.format(), '¥9.00');
    expect(order.paid.format(), '¥9.00');
    expect(order.hasDiscount, isTrue);
    expect(order.refundableAmount.format(), '¥6.00');
    expect(order.periodLabel, '1 个月');
    expect(order.status.label, '部分退款');
  });

  test('rejects a fractional cent amount from the wire', () {
    expect(
      () => BillingOrder.fromJson({
        'order_no': 'SP1',
        'plan_code': 'starter_monthly',
        'plan_display_name': '初芽体验包',
        'billing_period': 'monthly',
        'period_months': 1,
        'unit_price_cny_cents': 9.9,
        'payable_cny_cents': 990,
        'status': 'pending',
        'created_at': '2026-10-20T10:00:00Z',
      }),
      throwsA(isA<MoneyFormatException>()),
    );
  });

  test('a plan without an original price hides the strike-through price', () {
    final plan = BillingPlan.fromJson({
      'id': 'plan_1',
      'code': 'starter_monthly',
      'display_name': '初芽体验包',
      'description': '适合第一次使用的家庭',
      'billing_period': 'monthly',
      'period_months': 1,
      'price_cny_cents': 990,
      'original_price_cny_cents': 0,
    });
    expect(plan.price.format(), '¥9.90');
    expect(plan.hasOriginalPrice, isFalse);
    expect(plan.periodLabel, '1 个月');
  });

  test('maps 409 business states to friendly Chinese copy', () {
    expect(
      billingActionErrorMessage(const PaymentNotOpenException()),
      '支付功能尚未开放，请稍后再试',
    );
    expect(
      billingActionErrorMessage(const SpendingConsentRequiredException()),
      contains('监护人'),
    );
    expect(isPaymentNotOpen(const PaymentNotOpenException()), isTrue);
    expect(isSpendingConsentRequired(const SpendingConsentRequiredException()), isTrue);
  });
}
