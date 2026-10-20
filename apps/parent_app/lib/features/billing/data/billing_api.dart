import 'package:dio/dio.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/network/api_client.dart';
import '../domain/billing_errors.dart';

/// 计费域的 HTTP 数据源。
///
/// 所有金额字段都以整数分解析，不经过浮点；金额或结构不符合约定时抛出
/// [FormatException]，由控制器转成普通提示。支付未开放、缺少消费同意这类
/// 业务结果用 [PaymentNotOpenException] 与 [SpendingConsentRequiredException]
/// 表达，不向界面泄露状态码或后端原文。
class BillingApi {
  const BillingApi(this._apiClient);

  final ApiClient _apiClient;

  Future<List<BillingPlan>> fetchPlans() async {
    final response = await _guard(() => _apiClient.get('/api/v1/billing/plans'));
    final data = _requiredMap(response['data'], 'data');
    final plans = data['plans'];
    if (plans == null) {
      return const <BillingPlan>[];
    }
    return _mapList(plans, 'plans')
        .map(BillingPlan.fromJson)
        .toList(growable: false);
  }

  /// 创建订单。同一 [idempotencyKey] 重复提交会得到同一张订单。
  Future<BillingOrder> createOrder({
    required String planCode,
    required String idempotencyKey,
  }) async {
    final response = await _guard(
      () => _apiClient.post(
        '/api/v1/billing/orders',
        body: {'plan_code': planCode, 'idempotency_key': idempotencyKey},
      ),
    );
    return _readOrder(response);
  }

  Future<BillingOrderPage> fetchOrders({
    required int page,
    required int pageSize,
  }) async {
    final response = await _guard(
      () => _apiClient.get(
        '/api/v1/billing/orders',
        queryParameters: {
          'page': page.toString(),
          'page_size': pageSize.toString(),
        },
      ),
    );
    final data = _requiredMap(response['data'], 'data');
    final orders = data['orders'];
    final total = data['total'];
    return BillingOrderPage(
      orders: orders == null
          ? const <BillingOrder>[]
          : _mapList(
              orders,
              'orders',
            ).map(BillingOrder.fromJson).toList(growable: false),
      total: total is int && total >= 0 ? total : 0,
    );
  }

  Future<BillingOrder> fetchOrder(String orderNo) async {
    final response = await _guard(
      () => _apiClient.get(_orderPath(orderNo)),
    );
    return _readOrder(response);
  }

  /// 取消待支付订单。后端只回订单时按订单解析，否则沿用本地单号。
  Future<BillingOrder> cancelOrder(String orderNo) async {
    final response = await _guard(
      () => _apiClient.post('${_orderPath(orderNo)}/cancel'),
    );
    return _readOrder(response, fallbackOrderNo: orderNo);
  }

  /// 发起支付。
  ///
  /// 渠道未开放时后端返回 409，这里转成 [PaymentNotOpenException]，
  /// 调用方据此保留订单并提示稍后再试。
  Future<BillingPaymentResult> payOrder({
    required String orderNo,
    required String channel,
  }) async {
    final response = await _guard(
      () => _apiClient.post(
        '${_orderPath(orderNo)}/pay',
        body: {'channel': channel},
      ),
    );
    final data = _requiredMap(response['data'], 'data');
    if (data['is_open'] == false) {
      throw const PaymentNotOpenException();
    }
    final order = data['order'];
    return BillingPaymentResult(
      order: order is Map
          ? BillingOrder.fromJson(Map<String, Object?>.from(order))
          : null,
    );
  }

  Future<BillingOrder> requestRefund({
    required String orderNo,
    required String reason,
  }) async {
    final response = await _guard(
      () => _apiClient.post(
        '${_orderPath(orderNo)}/refund',
        body: {'reason': reason},
      ),
    );
    return _readOrder(response, fallbackOrderNo: orderNo);
  }

  Future<BillingConsent> fetchConsent() async {
    final response = await _guard(
      () => _apiClient.get('/api/v1/billing/consent'),
    );
    return _readConsent(response);
  }

  Future<BillingConsent> grantConsent() async {
    final response = await _guard(
      () => _apiClient.post('/api/v1/billing/consent'),
    );
    return _readConsent(response);
  }

  Future<BillingConsent> revokeConsent() async {
    final response = await _guard(
      () => _apiClient.delete('/api/v1/billing/consent'),
    );
    return _readConsent(response, revoked: true);
  }

  String _orderPath(String orderNo) =>
      '/api/v1/billing/orders/${Uri.encodeComponent(orderNo)}';

  BillingOrder _readOrder(
    Map<String, Object?> response, {
    String? fallbackOrderNo,
  }) {
    final data = _requiredMap(response['data'], 'data');
    final order = data['order'];
    if (order is Map) {
      return BillingOrder.fromJson(Map<String, Object?>.from(order));
    }
    if (fallbackOrderNo != null) {
      throw BillingPartialOrderException(fallbackOrderNo);
    }
    throw const FormatException('missing order');
  }

  BillingConsent _readConsent(
    Map<String, Object?> response, {
    bool revoked = false,
  }) {
    final data = _requiredMap(response['data'], 'data');
    final consent = data['consent'];
    final envelope = consent is Map
        ? Map<String, Object?>.from(consent)
        : data;
    if (revoked) {
      return BillingConsent(
        isGranted: false,
        version: envelope['consent_version'] is String
            ? envelope['consent_version']! as String
            : null,
        grantedAt: null,
      );
    }
    return BillingConsent.fromJson(envelope);
  }

  /// 把传输失败统一映射为业务状态，避免原始异常文本流出到界面。
  Future<Map<String, Object?>> _guard(
    Future<Map<String, Object?>> Function() request,
  ) async {
    try {
      return await request();
    } on AppException catch (error) {
      final errorCode = _billingErrorCode(error);
      if (errorCode != null && paymentNotOpenErrorCodes.contains(errorCode)) {
        throw const PaymentNotOpenException();
      }
      if (errorCode != null &&
          spendingConsentRequiredErrorCodes.contains(errorCode)) {
        throw const SpendingConsentRequiredException();
      }
      rethrow;
    }
  }

  /// 读取后端错误码。
  ///
  /// 通用错误映射已把 409 统一折叠成 conflict，无法区分「渠道未开放」与
  /// 「缺少消费同意」，因此这里回到原始失败原因里取业务码。
  String? _billingErrorCode(AppException error) {
    final cause = error.cause;
    if (cause is! DioException) {
      return null;
    }
    return readApiErrorCode(cause.response?.data);
  }
}

/// 后端表示「支付渠道暂未开放」的业务错误码。
const paymentNotOpenErrorCodes = <String>{
  'payment_not_open',
  'payment_channel_unavailable',
  'billing_payment_unavailable',
};

/// 后端表示「缺少监护人消费同意」的业务错误码。
const spendingConsentRequiredErrorCodes = <String>{
  'spending_consent_required',
  'billing_consent_required',
};

/// 一页订单。
class BillingOrderPage {
  const BillingOrderPage({required this.orders, required this.total});

  final List<BillingOrder> orders;
  final int total;
}

/// 支付动作的结果；[order] 可能为空，此时界面沿用本地订单。
class BillingPaymentResult {
  const BillingPaymentResult({required this.order});

  final BillingOrder? order;
}

/// 后端只回状态、没有回完整订单时用于保留本地单号。
class BillingPartialOrderException implements Exception {
  const BillingPartialOrderException(this.orderNo);

  final String orderNo;
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
}

List<Map<String, Object?>> _mapList(Object? value, String field) {
  if (value is! List) {
    throw FormatException('missing $field');
  }
  final items = <Map<String, Object?>>[];
  for (final item in value) {
    if (item is! Map) {
      throw FormatException('invalid $field');
    }
    items.add(Map<String, Object?>.from(item));
  }
  return items;
}
