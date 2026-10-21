import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/api_error_mapper.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/billing/application/billing_controllers.dart';
import 'package:parent_app/features/billing/data/billing_api.dart';
import 'package:parent_app/features/billing/domain/billing_errors.dart';

void main() {
  group('BillingPlansController', () {
    test('loads the published plans', () async {
      final client = _FakeApiClient(
        getResponses: {
          '/api/v1/billing/plans': _plansResponse(_planJson()),
        },
      );
      final container = _container(client);

      final plans = await container.read(
        billingPlansControllerProvider.future,
      );

      expect(plans, hasLength(1));
      expect(plans.single.displayName, '初芽体验包');
      expect(plans.single.price.format(), '¥9.90');
      expect(plans.single.hasOriginalPrice, isTrue);
      expect(plans.single.originalPrice.format(), '¥19.90');
    });

    test('exposes an empty list when no plan is published', () async {
      final client = _FakeApiClient(
        getResponses: {
          '/api/v1/billing/plans': {
            'data': {'plans': <Object?>[]},
          },
        },
      );
      final container = _container(client);

      final plans = await container.read(
        billingPlansControllerProvider.future,
      );

      expect(plans, isEmpty);
      expect(container.read(billingPlansControllerProvider).hasError, isFalse);
    });

    test('exposes a friendly error state when loading fails', () async {
      final client = _FakeApiClient(
        getResponses: {
          '/api/v1/billing/plans': const AppException(
            kind: AppErrorKind.network,
            message: '网络连接不稳定，请检查后重试',
            retryable: true,
          ),
        },
      );
      final container = _container(client);

      // 读取时触发加载，失败通过 provider 状态暴露给界面。
      container.read(billingPlansControllerProvider);
      await pumpEventQueue();
      final state = container.read(billingPlansControllerProvider);
      expect(state.hasError, isTrue, reason: 'state=$state');
      expect(billingLoadErrorMessage(state.error), '网络连接不稳定，请检查后重试');
    });

    test('a stale response never overwrites a newer one', () async {
      final firstResponse = Completer<Map<String, Object?>>();
      final secondResponse = Completer<Map<String, Object?>>();
      final client = _QueueingApiClient([firstResponse, secondResponse]);
      final container = _container(client);

      container.read(billingPlansControllerProvider);
      await pumpEventQueue();
      expect(client.pendingRequests, 1);

      // 第一次加载还没返回时又触发一次刷新，第二次才是家长需要的结果。
      final refreshed = container
          .read(billingPlansControllerProvider.notifier)
          .refresh();
      await pumpEventQueue();
      expect(client.pendingRequests, 2);

      secondResponse.complete(
        _plansResponse(_planJson(code: 'newer_plan', displayName: '新的套餐')),
      );
      await refreshed;

      // 旧请求在之后才返回，内容必须被丢弃。
      firstResponse.complete(
        _plansResponse(_planJson(code: 'older_plan', displayName: '旧套餐')),
      );
      await pumpEventQueue();

      final state = container.read(billingPlansControllerProvider);
      expect(state.value, isNotNull);
      expect(state.value!.single.displayName, '新的套餐');
      expect(state.value!.single.code, 'newer_plan');
    });
  });

  group('billing business states', () {
    test('maps payment_not_open to the friendly state', () async {
      final client = _FakeApiClient(
        postErrors: {
          '/api/v1/billing/orders/SP1/pay': _conflict('payment_not_open'),
        },
      );
      final api = BillingApi(client);

      await expectLater(
        api.payOrder(orderNo: 'SP1', channel: 'wechat'),
        throwsA(isA<PaymentNotOpenException>()),
      );
      expect(
        billingActionErrorMessage(const PaymentNotOpenException()),
        '支付功能尚未开放，请稍后再试',
      );
    });

    test('maps spending_consent_required to the consent state', () async {
      final client = _FakeApiClient(
        postErrors: {
          '/api/v1/billing/orders': _conflict('spending_consent_required'),
        },
      );
      final api = BillingApi(client);

      await expectLater(
        api.createOrder(planCode: 'starter_monthly', idempotencyKey: 'key-1'),
        throwsA(isA<SpendingConsentRequiredException>()),
      );
    });

    test('keeps the original order when pay only returns a result', () async {
      final client = _FakeApiClient(
        postResponses: {
          '/api/v1/billing/orders/SP1/pay': {
            'data': {
              'payload': {'prepay_id': 'wx-prepay-1'},
            },
          },
        },
      );
      final api = BillingApi(client);

      final result = await api.payOrder(orderNo: 'SP1', channel: 'wechat');

      expect(result.order, isNull);
    });

    test('reads the business code from a real 409 failure', () {
      final responseData = {
        'error': {
          'code': 'payment_not_open',
          'message': 'payment channel not configured',
          'retryable': true,
        },
      };
      final dioError = DioException(
        requestOptions: RequestOptions(path: '/api/v1/billing/orders/SP1/pay'),
        response: Response<Object?>(
          requestOptions: RequestOptions(path: '/api/v1/billing/orders/SP1/pay'),
          statusCode: 409,
          data: responseData,
        ),
        type: DioExceptionType.badResponse,
      );

      expect(readApiErrorCode(responseData), 'payment_not_open');
      expect(mapApiError(dioError).kind, AppErrorKind.conflict);
    });

    test('parses the consent envelope used by the consent endpoints', () async {
      final client = _FakeApiClient(
        getResponses: {
          '/api/v1/billing/consent': {
            'data': {
              'consent': {
                'granted': true,
                'consent_version': '2026-10',
                'granted_at': '2026-10-20T10:00:00Z',
              },
            },
          },
        },
      );
      final api = BillingApi(client);

      final consent = await api.fetchConsent();

      expect(consent.isGranted, isTrue);
      expect(consent.grantedAt, isNotNull);
    });
  });

  group('BillingOrdersController', () {
    test('loads the first page and appends the next one', () async {
      final client = _FakeApiClient(
        getResponses: {
          '/api/v1/billing/orders': {
            'data': {
              'orders': [_orderJson(orderNo: 'SP1'), _orderJson(orderNo: 'SP2')],
              'total': 3,
            },
          },
        },
      );
      final container = _container(client);
      final initial = await container.read(
        billingOrdersControllerProvider.future,
      );
      expect(initial.orders, hasLength(2));
      expect(initial.total, 3);
      expect(initial.hasMore, isTrue);

      client.getResponses['/api/v1/billing/orders'] = {
        'data': {
          'orders': [_orderJson(orderNo: 'SP3')],
          'total': 3,
        },
      };
      await container
          .read(billingOrdersControllerProvider.notifier)
          .loadMore();

      final state = container.read(billingOrdersControllerProvider);
      expect(state.value!.orders.map((order) => order.orderNo), [
        'SP1',
        'SP2',
        'SP3',
      ]);
      expect(state.value!.hasMore, isFalse);
    });
  });

  group('Money wire safety', () {
    test('rejects a fractional cent value in a plan', () {
      final plan = <String, Object?>{
        ..._planJson(),
        'price_cny_cents': 9.9,
      };
      expect(
        () => BillingPlan.fromJson(plan),
        throwsA(isA<MoneyFormatException>()),
      );
    });
  });

  group('SpendingLimit', () {
    test('reads the family limit with integer cents', () async {
      final client = _FakeApiClient(
        getResponses: {
          '/api/v1/families/fam_001/spending-limit': _spendingLimitResponse(),
        },
      );
      final api = BillingApi(client);

      final limit = await api.fetchSpendingLimit('fam_001');

      expect(limit.monthlyLimit.format(), '¥200.00');
      expect(limit.monthSpent.format(), '¥49.90');
      expect(limit.remaining.format(), '¥150.10');
      expect(limit.isReached, isFalse);
      expect(limit.updatedAt, isNotNull);
    });

    test('treats a zero limit as unlimited', () async {
      final client = _FakeApiClient(
        getResponses: {
          '/api/v1/families/fam_001/spending-limit': _spendingLimitResponse(
            monthlyLimitCents: 0,
          ),
        },
      );
      final api = BillingApi(client);

      final limit = await api.fetchSpendingLimit('fam_001');

      expect(limit.isUnlimited, isTrue);
      expect(limit.usedRatio, 0);
    });

    test('sends integer cents when saving the limit', () async {
      final client = _FakeApiClient(
        putResponses: {
          '/api/v1/families/fam_001/spending-limit': _spendingLimitResponse(
            monthlyLimitCents: 19990,
          ),
        },
      );
      final api = BillingApi(client);

      final saved = await api.updateSpendingLimit(
        'fam_001',
        monthlyLimitCents: 19990,
      );

      expect(client.putBodies.single, {'monthly_limit_cents': 19990});
      expect(saved.monthlyLimit.format(), '¥199.90');
    });

    test('maps spending_limit_exceeded to the business state', () async {
      final client = _FakeApiClient(
        postErrors: {
          '/api/v1/billing/orders': _conflict('spending_limit_exceeded'),
        },
      );
      final api = BillingApi(client);

      await expectLater(
        api.createOrder(planCode: 'starter_monthly', idempotencyKey: 'key-1'),
        throwsA(isA<SpendingLimitExceededException>()),
      );
      expect(
        billingActionErrorMessage(const SpendingLimitExceededException()),
        '本月消费已达上限，请先调整限额或次月再试',
      );
    });

    test('rejects a fractional cent limit', () {
      expect(
        () => SpendingLimit.fromJson({
          ..._spendingLimitJson(),
          'monthly_limit_cents': 200.0,
        }),
        throwsA(isA<MoneyFormatException>()),
      );
    });
  });
}

Map<String, Object?> _spendingLimitJson({int monthlyLimitCents = 20000}) {
  return <String, Object?>{
    'monthly_limit_cents': monthlyLimitCents,
    'current_month_spent_cents': 4990,
    'updated_at': '2026-10-20T10:00:00Z',
  };
}

Map<String, Object?> _spendingLimitResponse({int monthlyLimitCents = 20000}) {
  return {'data': _spendingLimitJson(monthlyLimitCents: monthlyLimitCents)};
}

/// 构造与生产链路一致的 409：通用映射先折叠成 conflict，原始失败里带着业务码。
AppException _conflict(String code) {
  final responseData = {
    'error': {'code': code, 'message': 'conflict', 'retryable': true},
  };
  final request = RequestOptions(path: '/api/v1/billing/orders');
  final dioError = DioException(
    requestOptions: request,
    response: Response<Object?>(
      requestOptions: request,
      statusCode: 409,
      data: responseData,
    ),
    type: DioExceptionType.badResponse,
  );
  return mapApiError(dioError);
}

ProviderContainer _container(ApiClient client) {
  return ProviderContainer(
    overrides: [billingApiProvider.overrideWithValue(BillingApi(client))],
  );
}

Map<String, Object?> _planJson({
  String code = 'starter_monthly',
  String displayName = '初芽体验包',
}) {
  return <String, Object?>{
    'id': 'plan_001',
    'code': code,
    'display_name': displayName,
    'description': '适合第一次使用的家庭',
    'billing_period': 'monthly',
    'period_months': 1,
    'price_cny_cents': 990,
    'original_price_cny_cents': 1990,
  };
}

Map<String, Object?> _plansResponse(Map<String, Object?> plan) {
  return {
    'data': {
      'plans': [plan],
    },
  };
}

Map<String, Object?> _orderJson({required String orderNo}) {
  return {
    'order_no': orderNo,
    'plan_code': 'starter_monthly',
    'plan_display_name': '初芽体验包',
    'billing_period': 'monthly',
    'period_months': 1,
    'unit_price_cny_cents': 990,
    'discount_cny_cents': 0,
    'payable_cny_cents': 990,
    'paid_cny_cents': 0,
    'refunded_cny_cents': 0,
    'status': 'pending',
    'expires_at': '2026-10-20T10:30:00Z',
    'paid_at': null,
    'created_at': '2026-10-20T10:00:00Z',
  };
}

/// 按路径返回固定结果，或抛出预设的业务失败。
class _FakeApiClient implements ApiClient {
  _FakeApiClient({
    Map<String, Object?>? getResponses,
    Map<String, Object?>? postResponses,
    Map<String, Object?>? postErrors,
    Map<String, Object?>? putResponses,
  }) : getResponses = getResponses ?? <String, Object?>{},
       postResponses = postResponses ?? <String, Object?>{},
       postErrors = postErrors ?? <String, Object?>{},
       putResponses = putResponses ?? <String, Object?>{};

  final Map<String, Object?> getResponses;
  final Map<String, Object?> postResponses;
  final Map<String, Object?> postErrors;
  final Map<String, Object?> putResponses;
  final List<Object?> putBodies = <Object?>[];

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    final response = getResponses[path];
    if (response is AppException) {
      throw response;
    }
    if (response is Map) {
      return Map<String, Object?>.from(response);
    }
    throw StateError('unexpected get $path');
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    final error = postErrors[path];
    if (error is AppException) {
      throw error;
    }
    final response = postResponses[path];
    if (response is Map) {
      return Map<String, Object?>.from(response);
    }
    throw StateError('unexpected post $path');
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    putBodies.add(body);
    final response = putResponses[path];
    if (response is AppException) {
      throw response;
    }
    if (response is Map) {
      return Map<String, Object?>.from(response);
    }
    throw StateError('unexpected put $path');
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) async {
    throw StateError('unexpected bearer post $path');
  }

  @override
  Future<Map<String, Object?>> delete(String path) async {
    throw StateError('unexpected delete $path');
  }
}

/// 每次请求都挂起，直到测试手动补全，用来制造陈旧响应。
class _QueueingApiClient implements ApiClient {
  _QueueingApiClient(this.responses);

  final List<Completer<Map<String, Object?>>> responses;
  int pendingRequests = 0;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) {
    final response = responses[pendingRequests];
    pendingRequests += 1;
    return response.future;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    throw StateError('unexpected post $path');
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    throw StateError('unexpected put $path');
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) async {
    throw StateError('unexpected bearer post $path');
  }

  @override
  Future<Map<String, Object?>> delete(String path) async {
    throw StateError('unexpected delete $path');
  }
}
