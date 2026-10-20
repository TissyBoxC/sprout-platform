import '../../../core/error/app_exception.dart';
import 'money.dart';

export 'billing_models.dart';
export 'money.dart';

/// 支付渠道未开放时，后端以 HTTP 409 返回的业务结果。
///
/// 这是一个「正常的业务状态」而不是错误：订单仍然保留为待支付，
/// 界面只需要告诉家长稍后再试。
class PaymentNotOpenException implements Exception {
  const PaymentNotOpenException();

  @override
  String toString() => 'PaymentNotOpenException';
}

/// 尚未同意监护人消费条款，后端拒绝下单或支付。
class SpendingConsentRequiredException implements Exception {
  const SpendingConsentRequiredException();

  @override
  String toString() => 'SpendingConsentRequiredException';
}

bool isPaymentNotOpen(Object error) => error is PaymentNotOpenException;

bool isSpendingConsentRequired(Object error) =>
    error is SpendingConsentRequiredException;

/// 把支付动作的失败转成家长可读的文案。
///
/// 任何原始异常文本都不会传给界面。
String billingActionErrorMessage(Object? error) {
  if (error is PaymentNotOpenException) {
    return '支付功能尚未开放，请稍后再试';
  }
  if (error is SpendingConsentRequiredException) {
    return '购买前需要监护人同意消费条款';
  }
  if (error is AppException) {
    return error.message;
  }
  return '操作没有完成，请稍后重试';
}

/// 把套餐、订单读取失败转成家长可读的文案。
String billingLoadErrorMessage(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '暂时无法读取付费信息，请稍后重试';
}

/// 显示用金额：始终带 `¥` 和两位小数。
String formatCents(Money money) => money.format();
