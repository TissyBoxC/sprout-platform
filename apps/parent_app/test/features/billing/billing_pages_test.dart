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
import 'package:parent_app/features/billing/presentation/spending_limit_page.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';

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

    testWidgets('spending limit blocks the order and offers the limit page', (
      tester,
    ) async {
      final api = _RecordingBillingApi(
        createOrderError: const SpendingLimitExceededException(),
      );
      await tester.pumpWidget(
        _checkoutApp(api, const BillingConsent(isGranted: true)),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('确认支付'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认支付').last);
      await tester.pumpAndSettle();

      expect(api.createdOrders, hasLength(1));
      expect(find.text('本月消费已达上限，请先调整限额或次月再试'), findsOneWidget);
      expect(find.text('调整消费限额'), findsOneWidget);
      // 限额是保护而不是故障，不能出现技术词或原始错误。
      expect(find.textContaining('Exception'), findsNothing);
      expect(find.textContaining('409'), findsNothing);
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

  group('SpendingLimitPage', () {
    testWidgets('shows this month spending, the limit, and a progress bar', (
      tester,
    ) async {
      final api = _SpendingLimitApi(limit: _limit());
      await tester.pumpWidget(_spendingLimitApp(api));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const ValueKey<String>('spending-limit-spent')),
        findsOneWidget,
      );
      expect(find.text('¥49.90'), findsOneWidget);
      expect(find.text('上限 ¥200.00，还可以用 ¥150.10'), findsOneWidget);
      final progress = tester.widget<LinearProgressIndicator>(
        find.byKey(const ValueKey<String>('spending-limit-progress')),
      );
      expect(progress.value, closeTo(0.2495, 0.001));
      final input = tester.widget<TextFormField>(
        find.byKey(const ValueKey<String>('spending-limit-input')),
      );
      expect(input.controller?.text, '200.00');
    });

    testWidgets('saving 0 means no limit and refreshes in place', (
      tester,
    ) async {
      final api = _SpendingLimitApi(limit: _limit());
      await tester.pumpWidget(_spendingLimitApp(api));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const ValueKey<String>('spending-limit-input')),
        '0',
      );
      await tester.tap(
        find.byKey(const ValueKey<String>('spending-limit-save')),
      );
      await tester.pumpAndSettle();

      expect(api.savedCents, [0]);
      expect(find.text('当前没有设置上限，购买套餐不受金额限制。'), findsOneWidget);
      expect(find.text('已保存，本月不再限制消费金额'), findsOneWidget);
    });

    testWidgets('converts yuan input to cents and shows the saved limit', (
      tester,
    ) async {
      final api = _SpendingLimitApi(limit: _limit());
      await tester.pumpWidget(_spendingLimitApp(api));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const ValueKey<String>('spending-limit-input')),
        '199.9',
      );
      await tester.tap(
        find.byKey(const ValueKey<String>('spending-limit-save')),
      );
      await tester.pumpAndSettle();

      expect(api.savedCents, [19990]);
      expect(find.text('上限 ¥199.90，还可以用 ¥150.00'), findsOneWidget);
    });

    testWidgets('rejects an empty amount before saving', (tester) async {
      final api = _SpendingLimitApi(limit: _limit());
      await tester.pumpWidget(_spendingLimitApp(api));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const ValueKey<String>('spending-limit-input')),
        '',
      );
      await tester.tap(
        find.byKey(const ValueKey<String>('spending-limit-save')),
      );
      await tester.pumpAndSettle();

      expect(find.text('请输入每月上限，填 0 表示不限额'), findsOneWidget);
      expect(api.savedCents, isEmpty);
    });

    testWidgets('explains a reached limit and what to do next', (tester) async {
      final api = _SpendingLimitApi(
        limit: SpendingLimit(
          monthlyLimit: Money.fromNonNegativeCents(4990),
          monthSpent: Money.fromNonNegativeCents(4990),
        ),
      );
      await tester.pumpWidget(_spendingLimitApp(api));
      await tester.pumpAndSettle();

      expect(find.text('上限 ¥49.90，本月额度已经用完'), findsOneWidget);
      expect(find.textContaining('把上限调高'), findsOneWidget);
    });

    testWidgets('shows the loading state', (tester) async {
      final api = _SpendingLimitApi(limit: _limit(), isPending: true);
      await tester.pumpWidget(_spendingLimitApp(api));
      await tester.pump();

      expect(find.text('正在加载…'), findsOneWidget);
    });

    testWidgets('shows an empty state when no limit was ever saved', (
      tester,
    ) async {
      final api = _SpendingLimitApi(
        limit: _limit(),
        loadError: const AppException(
          kind: AppErrorKind.notFound,
          message: '没有找到这条内容',
          retryable: false,
        ),
      );
      await tester.pumpWidget(_spendingLimitApp(api));
      await tester.pumpAndSettle();

      expect(find.text('还没有设置消费限额'), findsOneWidget);
      expect(find.textContaining('填 0 表示不限额'), findsOneWidget);
      expect(find.textContaining('没有找到这条内容'), findsNothing);
    });

    testWidgets('shows a readable error with a retry action', (tester) async {
      final api = _SpendingLimitApi(
        limit: _limit(),
        loadError: const AppException(
          kind: AppErrorKind.network,
          message: '网络连接不稳定，请检查后重试',
          retryable: true,
        ),
      );
      await tester.pumpWidget(_spendingLimitApp(api));
      await tester.pumpAndSettle();

      expect(find.textContaining('网络连接不稳定'), findsOneWidget);
      expect(find.text('重新加载'), findsOneWidget);
      expect(find.textContaining('Exception'), findsNothing);
    });

    testWidgets('a member without the permission gets a read-only page', (
      tester,
    ) async {
      final api = _SpendingLimitApi(limit: _limit());
      await tester.pumpWidget(_spendingLimitApp(api, canManage: false));
      await tester.pumpAndSettle();

      expect(find.text('只有家庭所有者可以设置消费限额'), findsOneWidget);
      expect(
        find.byKey(const ValueKey<String>('spending-limit-save')),
        findsNothing,
      );
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

/// 本月已花 ¥49.90、上限 ¥200.00 的典型家庭。
SpendingLimit _limit() => SpendingLimit(
  monthlyLimit: Money.fromNonNegativeCents(20000),
  monthSpent: Money.fromNonNegativeCents(4990),
);

Widget _spendingLimitApp(
  _SpendingLimitApi api, {
  bool canManage = true,
}) {
  return ProviderScope(
    overrides: [
      billingApiProvider.overrideWithValue(api),
      familyControllerProvider.overrideWith(
        () => _FixedFamilyController(canManage: canManage),
      ),
      spendingLimitControllerProvider.overrideWith(
        () => _FixedSpendingLimitController(api),
      ),
    ],
    child: MaterialApp(theme: AppTheme.light, home: const SpendingLimitPage()),
  );
}

FamilyMembershipState _familyState({required bool canManage}) {
  return FamilyMembershipState(
    capability: FamilyCapability.fromJson({
      'family_id': 'fam_001',
      'owner_user_id': 'user_owner',
      'member_count': 2,
      'pending_invite_count': 0,
      'schema_version': '1',
      'permissions': {'spending_limit_manage': canManage},
    }),
    members: const <FamilyMember>[],
    invitations: const <FamilyInvitation>[],
  );
}

class _FixedFamilyController extends FamilyController {
  _FixedFamilyController({required this.canManage});

  final bool canManage;

  @override
  Future<FamilyMembershipState> build() async =>
      _familyState(canManage: canManage);
}

/// 用假数据源驱动真实的限额控制器，保留刷新与保存后的状态切换。
class _FixedSpendingLimitController extends SpendingLimitController {
  _FixedSpendingLimitController(this._api);

  final _SpendingLimitApi _api;

  @override
  Future<SpendingLimit> build() async {
    // 先让父类完成 build，把数据源和行为都接好，再按测试意图覆盖结果。
    final live = super.build();
    if (_api.isPending) {
      // 真实数据源也挂起时，父类请求同样不会完成；这里换成永不完成的结果，
      // 并把父类这次请求的错误吞掉，避免未处理的异步异常打断测试。
      unawaited(live.then((_) {}, onError: (Object _) {}));
      return Completer<SpendingLimit>().future;
    }
    unawaited(
      live.then((_) {}, onError: (Object _) {}),
    );
    final error = _api.loadError;
    if (error != null) {
      throw error;
    }
    return _api.limit;
  }
}

class _SpendingLimitApi implements BillingApi {
  _SpendingLimitApi({
    required this.limit,
    this.loadError,
    this.isPending = false,
  });

  SpendingLimit limit;
  final Object? loadError;
  final bool isPending;
  final List<int> savedCents = [];

  @override
  Future<SpendingLimit> fetchSpendingLimit(String familyId) async {
    if (isPending) {
      return Completer<SpendingLimit>().future;
    }
    final error = loadError;
    if (error != null) {
      throw error;
    }
    return limit;
  }

  @override
  Future<SpendingLimit> updateSpendingLimit(
    String familyId, {
    required int monthlyLimitCents,
  }) async {
    savedCents.add(monthlyLimitCents);
    limit = SpendingLimit(
      monthlyLimit: Money.fromNonNegativeCents(monthlyLimitCents),
      monthSpent: limit.monthSpent,
    );
    return limit;
  }

  @override
  Future<List<BillingPlan>> fetchPlans() async => const <BillingPlan>[];

  @override
  Future<BillingConsent> fetchConsent() async => const BillingConsent.none();

  @override
  Future<BillingConsent> grantConsent() async => const BillingConsent.none();

  @override
  Future<BillingConsent> revokeConsent() async => const BillingConsent.none();

  @override
  Future<BillingOrder> createOrder({
    required String planCode,
    required String idempotencyKey,
  }) async => _order(status: 'pending');

  @override
  Future<BillingPaymentResult> payOrder({
    required String orderNo,
    required String channel,
  }) async => const BillingPaymentResult(order: null);

  @override
  Future<BillingOrderPage> fetchOrders({
    required int page,
    required int pageSize,
  }) async => const BillingOrderPage(orders: <BillingOrder>[], total: 0);

  @override
  Future<BillingOrder> fetchOrder(String orderNo) async =>
      _order(status: 'pending');

  @override
  Future<BillingOrder> cancelOrder(String orderNo) async =>
      _order(status: 'cancelled');

  @override
  Future<BillingOrder> requestRefund({
    required String orderNo,
    required String reason,
  }) async => _order(status: 'paid');
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
  _RecordingBillingApi({this.payError, this.createOrderError});

  final Object? payError;
  final Object? createOrderError;
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
    final error = createOrderError;
    if (error != null) {
      throw error;
    }
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

  @override
  Future<SpendingLimit> fetchSpendingLimit(String familyId) async =>
      const SpendingLimit(
        monthlyLimit: Money.zero,
        monthSpent: Money.zero,
      );

  @override
  Future<SpendingLimit> updateSpendingLimit(
    String familyId, {
    required int monthlyLimitCents,
  }) async => SpendingLimit(
    monthlyLimit: Money.fromNonNegativeCents(monthlyLimitCents),
    monthSpent: Money.zero,
  );
}
