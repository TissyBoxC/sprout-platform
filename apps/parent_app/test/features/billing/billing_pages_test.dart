import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/features/billing/application/billing_controllers.dart';
import 'package:parent_app/features/billing/data/billing_api.dart';
import 'package:parent_app/features/billing/domain/billing_errors.dart';
import 'package:parent_app/features/billing/presentation/billing_checkout_page.dart';
import 'package:parent_app/features/billing/presentation/billing_orders_page.dart';
import 'package:parent_app/features/billing/presentation/billing_plans_page.dart';

void main() {
  group('BillingPlansPage', () {
    testWidgets('shows plan price, period, and a strike-through original price', (
      tester,
    ) async {
      await tester.pumpWidget(
        _plansApp(AsyncData([_plan()])),
      );
      await tester.pumpAndSettle();

      expect(find.text('初芽体验包'), findsOneWidget);
      expect(find.text('¥9.90'), findsOneWidget);
      expect(find.text('¥19.90'), findsOneWidget);
      expect(find.text('1 个月'), findsOneWidget);
      expect(find.text('选择这个套餐'), findsOneWidget);

      final struck = tester.widget<Text>(find.text('¥19.90'));
      expect(struck.style?.decoration, TextDecoration.lineThrough);
    });

    testWidgets('hides the original price when it is zero', (tester) async {
      await tester.pumpWidget(
        _plansApp(
          AsyncData([
            _plan(originalPriceCents: 0),
          ]),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('¥9.90'), findsOneWidget);
      expect(find.text('¥19.90'), findsNothing);
      expect(find.textContaining('已优惠'), findsNothing);
    });

    testWidgets('shows the loading copy while plans are being read', (
      tester,
    ) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            billingPlansControllerProvider.overrideWith(
              () => _PendingPlansController(),
            ),
            billingConsentControllerProvider.overrideWith(
              () => _FixedConsentController(const BillingConsent(isGranted: true)),
            ),
          ],
          child: MaterialApp(theme: AppTheme.light, home: const BillingPlansPage()),
        ),
      );
      await tester.pump();

      expect(find.text('正在加载…'), findsOneWidget);
    });

    testWidgets('explains an empty plan list and offers a next step', (
      tester,
    ) async {
      await tester.pumpWidget(_plansApp(const AsyncData(<BillingPlan>[])));
      await tester.pumpAndSettle();

      expect(find.text('暂时还没有可选套餐'), findsOneWidget);
      expect(find.textContaining('稍后再来查看'), findsOneWidget);
      expect(find.text('返回首页'), findsOneWidget);
    });

    testWidgets('shows a retry state without leaking the raw error', (
      tester,
    ) async {
      await tester.pumpWidget(
        _plansApp(
          AsyncError(
            const AppException(
              kind: AppErrorKind.serviceUnavailable,
              message: '服务暂时不可用，请稍后重试',
              retryable: true,
            ),
            StackTrace.empty,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('服务暂时不可用，请稍后重试'), findsOneWidget);
      expect(find.text('重新加载'), findsOneWidget);
      expect(find.textContaining('Exception'), findsNothing);
    });
  });

  group('BillingCheckoutPage', () {
    testWidgets('asks the guardian to consent and creates no order', (
      tester,
    ) async {
      final api = _RecordingBillingApi();
      await tester.pumpWidget(
        _checkoutApp(api, const BillingConsent(isGranted: false)),
      );
      await tester.pumpAndSettle();

      expect(find.text('请家长同意后再购买'), findsOneWidget);
      expect(find.textContaining('使用真实的钱'), findsOneWidget);
      expect(find.text('同意并继续'), findsOneWidget);
      // 没有同意时不出现支付按钮，也没有创建订单。
      expect(find.text('确认支付'), findsNothing);
      expect(api.createdOrders, isEmpty);
    });

    testWidgets('granting consent moves to the order summary', (tester) async {
      final api = _RecordingBillingApi();
      await tester.pumpWidget(
        _checkoutApp(api, const BillingConsent(isGranted: false)),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('同意并继续'));
      await tester.pumpAndSettle();

      expect(api.consentGrants, 1);
      expect(find.text('订单信息'), findsOneWidget);
      expect(find.text('确认支付'), findsOneWidget);
      expect(find.text('¥9.90'), findsWidgets);
    });

    testWidgets('payment not open shows the friendly message and keeps 待支付', (
      tester,
    ) async {
      final api = _RecordingBillingApi(payError: const PaymentNotOpenException());
      await tester.pumpWidget(
        _checkoutApp(api, const BillingConsent(isGranted: true)),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('确认支付'));
      await tester.pumpAndSettle();
      // 确认对话框再点一次同名的按钮，才真正发起支付。
      await tester.tap(find.text('确认支付').last);
      await tester.pumpAndSettle();

      expect(api.createdOrders, hasLength(1));
      expect(api.payAttempts, 1);
      expect(
        find.textContaining('支付功能尚未开放，请稍后再试'),
        findsOneWidget,
      );
      expect(find.textContaining('Exception'), findsNothing);
      // 订单仍然保留，状态是待支付，可以稍后继续支付。
      expect(find.text('SP20261020A1B2'), findsOneWidget);
      expect(find.text('待支付'), findsOneWidget);
      expect(find.text('确认支付'), findsOneWidget);
    });

    testWidgets('a normal payment failure stays readable', (tester) async {
      final api = _RecordingBillingApi(
        payError: const AppException(
          kind: AppErrorKind.network,
          message: '网络连接不稳定，请检查后重试',
          retryable: true,
        ),
      );
      await tester.pumpWidget(
        _checkoutApp(api, const BillingConsent(isGranted: true)),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('确认支付'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认支付').last);
      await tester.pumpAndSettle();

      expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
      expect(find.textContaining('Exception'), findsNothing);
    });
  });

  group('BillingOrdersPage', () {
    testWidgets('labels every order status in Chinese', (tester) async {
      // 订单列表是 ListView.separated（虚拟化），默认 800x600 的测试画布放不下
      // 七张订单卡片，屏幕外的行尚未构建，按 key 查找会落空。这里把逻辑尺寸
      // 放大到足以容纳全部卡片，让每一行都真实构建后再统一断言。
      tester.view.physicalSize = const Size(1200, 4000);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      await tester.pumpWidget(
        _ordersApp(
          AsyncData(
            BillingOrdersState(
              orders: [
                _order(status: 'pending', orderNo: 'SP1'),
                _order(status: 'paid', orderNo: 'SP2'),
                _order(status: 'cancelled', orderNo: 'SP3'),
                _order(status: 'expired', orderNo: 'SP4'),
                _order(status: 'refunded', orderNo: 'SP5'),
                _order(status: 'partially_refunded', orderNo: 'SP6'),
                _order(status: 'chargeback_pending', orderNo: 'SP7'),
              ],
              total: 7,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      final expectedLabels = <String, String>{
        'SP1': '待支付',
        'SP2': '已支付',
        'SP3': '已取消',
        'SP4': '已过期',
        'SP5': '已退款',
        'SP6': '部分退款',
        'SP7': '处理中',
      };
      for (final entry in expectedLabels.entries) {
        final row = find.byKey(ValueKey<String>('order-${entry.key}'));
        expect(row, findsOneWidget, reason: '${entry.key} 的订单卡片应当已经构建');
        expect(
          find.descendant(of: row, matching: find.text(entry.value)),
          findsOneWidget,
          reason: '${entry.key} 应显示 ${entry.value}',
        );
      }
      // 未知状态必须降级为可读文案，绝不原样显示后端标识。
      expect(find.textContaining('chargeback'), findsNothing);
      expect(find.textContaining('pending'), findsNothing);
    });

    testWidgets('a paid order offers 申请退款 with a reason field', (
      tester,
    ) async {
      final api = _RecordingBillingApi();
      await tester.pumpWidget(
        _ordersApp(
          AsyncData(
            BillingOrdersState(
              orders: [_order(status: 'paid')],
              total: 1,
            ),
          ),
          api: api,
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('申请退款'));
      await tester.pumpAndSettle();

      expect(find.text('退款原因'), findsOneWidget);
      expect(find.text('提交退款申请'), findsOneWidget);

      await tester.tap(find.text('提交退款申请'));
      await tester.pumpAndSettle();
      expect(find.text('请填写退款原因'), findsOneWidget);
      expect(api.refundReasons, isEmpty);

      await tester.enterText(
        find.byKey(const ValueKey<String>('refund-reason')),
        '买错了套餐',
      );
      await tester.tap(find.text('提交退款申请'));
      await tester.pumpAndSettle();

      expect(api.refundReasons, ['买错了套餐']);
    });

    testWidgets('an empty order list explains why and what to do next', (
      tester,
    ) async {
      await tester.pumpWidget(
        _ordersApp(const AsyncData(BillingOrdersState.initial())),
      );
      await tester.pumpAndSettle();

      expect(find.text('还没有订单'), findsOneWidget);
      expect(find.textContaining('支付后，订单会显示在这里'), findsOneWidget);
      expect(find.text('查看服务套餐'), findsOneWidget);
    });

    testWidgets('shows a retry state without leaking the raw error', (
      tester,
    ) async {
      await tester.pumpWidget(
        _ordersApp(
          AsyncError(
            const AppException(
              kind: AppErrorKind.network,
              message: '网络连接不稳定，请检查后重试',
              retryable: true,
            ),
            StackTrace.empty,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
      expect(find.text('重新加载'), findsOneWidget);
      expect(find.textContaining('Exception'), findsNothing);
    });
  });
}

Widget _plansApp(AsyncValue<List<BillingPlan>> state) {
  return ProviderScope(
    overrides: [
      billingPlansControllerProvider.overrideWith(
        () => _FixedPlansController(state),
      ),
      billingConsentControllerProvider.overrideWith(
        () => _FixedConsentController(const BillingConsent(isGranted: true)),
      ),
    ],
    child: MaterialApp(theme: AppTheme.light, home: const BillingPlansPage()),
  );
}

Widget _checkoutApp(_RecordingBillingApi api, BillingConsent consent) {
  return ProviderScope(
    overrides: [
      billingApiProvider.overrideWithValue(api),
      billingConsentControllerProvider.overrideWith(
        () => _RecordingConsentController(api, consent),
      ),
    ],
    child: MaterialApp(theme: AppTheme.light, home: BillingCheckoutPage(plan: _plan())),
  );
}

Widget _ordersApp(AsyncValue<BillingOrdersState> state, {BillingApi? api}) {
  return ProviderScope(
    overrides: [
      billingOrdersControllerProvider.overrideWith(
        () => _FixedOrdersController(state, hasApi: api != null),
      ),
      if (api != null) billingApiProvider.overrideWithValue(api),
    ],
    child: MaterialApp(theme: AppTheme.light, home: const BillingOrdersPage()),
  );
}

BillingPlan _plan({int originalPriceCents = 1990}) {
  return BillingPlan.fromJson({
    'id': 'plan_001',
    'code': 'starter_monthly',
    'display_name': '初芽体验包',
    'description': '适合第一次使用的家庭',
    'billing_period': 'monthly',
    'period_months': 1,
    'price_cny_cents': 990,
    'original_price_cny_cents': originalPriceCents,
  });
}

BillingOrder _order({required String status, String orderNo = 'SP20261020A1B2'}) {
  return BillingOrder.fromJson({
    'order_no': orderNo,
    'plan_code': 'starter_monthly',
    'plan_display_name': '初芽体验包',
    'billing_period': 'monthly',
    'period_months': 1,
    'unit_price_cny_cents': 990,
    'discount_cny_cents': 0,
    'payable_cny_cents': 990,
    'paid_cny_cents': status == 'pending' ? 0 : 990,
    'refunded_cny_cents': 0,
    'status': status,
    'expires_at': '2026-10-20T10:30:00Z',
    'paid_at': status == 'pending' ? null : '2026-10-20T10:05:00Z',
    'created_at': '2026-10-20T10:00:00Z',
  });
}

/// 把套餐列表固定为测试给定状态。
///
/// 只能用 `build()` 返回固定值：`AsyncNotifier.state` 在 `build()` 返回之前
/// 是 loading，所以父类若返回一个永不完成的 Future，页面会一直停在加载态。
class _FixedPlansController extends BillingPlansController {
  _FixedPlansController(this._fixed);

  final AsyncValue<List<BillingPlan>> _fixed;

  @override
  Future<List<BillingPlan>> build() async => _fixed.when(
    data: (plans) => plans,
    error: (error, stackTrace) => Error.throwWithStackTrace(
      error,
      stackTrace,
    ),
    loading: () => const <BillingPlan>[],
  );
}

class _PendingPlansController extends BillingPlansController {
  @override
  Future<List<BillingPlan>> build() => Completer<List<BillingPlan>>().future;
}

class _FixedConsentController extends BillingConsentController {
  _FixedConsentController(this.value);

  final BillingConsent value;

  @override
  Future<BillingConsent> build() async => value;
}

/// 记录授权动作，并把状态切换为已同意，模拟真实控制器的行为。
class _RecordingConsentController extends BillingConsentController {
  _RecordingConsentController(this._api, this._initial);

  final _RecordingBillingApi _api;
  final BillingConsent _initial;

  @override
  Future<BillingConsent> build() async => _initial;

  @override
  Future<BillingConsent> grant() async {
    _api.consentGrants += 1;
    const granted = BillingConsent(isGranted: true);
    state = const AsyncData(granted);
    return granted;
  }
}

class _FixedOrdersController extends BillingOrdersController {
  _FixedOrdersController(this.value, {required this.hasApi});

  final AsyncValue<BillingOrdersState> value;
  final bool hasApi;

  /// The list state is pinned by the test, but the action methods still run for
  /// real and need the controller's API handle, which only `build()` sets. When
  /// the test injected a fake API, `super.build()` runs to wire it and its
  /// fetched page is discarded; otherwise it is skipped so a pinned error state
  /// does not trigger a real request.
  @override
  Future<BillingOrdersState> build() async {
    final live = hasApi
        ? await super.build()
        : const BillingOrdersState.initial();
    return value.when(
      data: (state) => state,
      error: (error, stackTrace) => throw error,
      loading: () => live,
    );
  }
}

/// 记录下单、支付与退款调用的假数据源。
class _RecordingBillingApi implements BillingApi {
  _RecordingBillingApi({this.payError});

  final Object? payError;
  final List<String> createdOrders = [];
  final List<String> refundReasons = [];
  int payAttempts = 0;
  int consentGrants = 0;

  @override
  Future<BillingOrder> createOrder({
    required String planCode,
    required String idempotencyKey,
  }) async {
    createdOrders.add(planCode);
    return _order(status: 'pending');
  }

  @override
  Future<BillingPaymentResult> payOrder({
    required String orderNo,
    required String channel,
  }) async {
    payAttempts += 1;
    final error = payError;
    if (error != null) {
      throw error;
    }
    return const BillingPaymentResult(order: null);
  }

  @override
  Future<BillingOrder> requestRefund({
    required String orderNo,
    required String reason,
  }) async {
    refundReasons.add(reason);
    return _order(status: 'paid');
  }

  @override
  Future<BillingOrder> cancelOrder(String orderNo) async =>
      _order(status: 'cancelled');

  @override
  Future<BillingOrder> fetchOrder(String orderNo) async =>
      _order(status: 'pending');

  @override
  Future<BillingOrderPage> fetchOrders({
    required int page,
    required int pageSize,
  }) async {
    return BillingOrderPage(orders: [_order(status: 'paid')], total: 1);
  }

  @override
  Future<List<BillingPlan>> fetchPlans() async => [_plan()];

  @override
  Future<BillingConsent> fetchConsent() async =>
      const BillingConsent(isGranted: false);

  @override
  Future<BillingConsent> grantConsent() async {
    consentGrants += 1;
    return const BillingConsent(isGranted: true);
  }

  @override
  Future<BillingConsent> revokeConsent() async =>
      const BillingConsent(isGranted: false);
}
