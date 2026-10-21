import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../../family/application/family_controller.dart';
import '../data/billing_api.dart';
import '../domain/billing_errors.dart';

/// 把本次生成号与最新生成号比较，旧请求的结果一律丢弃。
bool isStaleBillingResult(int requestGeneration, int latestGeneration) =>
    requestGeneration != latestGeneration;

final billingApiProvider = Provider<BillingApi>((ref) {
  return BillingApi(ref.read(apiClientProvider));
});

/// 套餐列表：加载中、空列表与失败都由状态本身表达。
class BillingPlansController extends AsyncNotifier<List<BillingPlan>> {
  late BillingApi _api;
  int _requestGeneration = 0;
  List<BillingPlan>? _lastSuccessful;

  /// 当前请求生成号，用于测试与调试陈旧结果。
  int get requestGeneration => _requestGeneration;

  @override
  Future<List<BillingPlan>> build() async {
    _api = ref.read(billingApiProvider);
    _lastSuccessful = null;
    final generation = ++_requestGeneration;
    final plans = await _api.fetchPlans();
    if (isStaleBillingResult(generation, _requestGeneration)) {
      // 已经被更新的一次加载取代，直接沿用最新状态，不再覆盖界面。
      return state.value ?? _lastSuccessful ?? const <BillingPlan>[];
    }
    _lastSuccessful = plans;
    return plans;
  }

  Future<void> refresh() async {
    final generation = ++_requestGeneration;
    final next = await AsyncValue.guard(() => _api.fetchPlans());
    // 加载期间又发起了更新的一次请求时，这次结果已经过期。
    if (isStaleBillingResult(generation, _requestGeneration)) {
      return;
    }
    state = _keepLastSuccessfulOnError(next);
  }

  AsyncValue<List<BillingPlan>> _keepLastSuccessfulOnError(
    AsyncValue<List<BillingPlan>> next,
  ) {
    if (next.hasValue) {
      _lastSuccessful = next.value;
      return next;
    }
    // 网络抖动不应该让家长看到空页面，保留上一次成功的套餐。
    final previous = _lastSuccessful;
    if (previous != null) {
      return AsyncData(previous);
    }
    return next;
  }
}

final billingPlansControllerProvider =
    AsyncNotifierProvider<BillingPlansController, List<BillingPlan>>(
      BillingPlansController.new,
    );

/// 监护人消费条款的授权状态。
class BillingConsentController extends AsyncNotifier<BillingConsent> {
  late BillingApi _api;
  int _requestGeneration = 0;
  BillingConsent? _lastSuccessful;

  @override
  Future<BillingConsent> build() async {
    _api = ref.read(billingApiProvider);
    _lastSuccessful = null;
    final generation = ++_requestGeneration;
    final consent = await _api.fetchConsent();
    if (isStaleBillingResult(generation, _requestGeneration)) {
      throw const StaleBillingResultException();
    }
    _lastSuccessful = consent;
    return consent;
  }

  /// 重新读取授权状态；失败时保留上一次已知状态。
  Future<void> refresh() async {
    final generation = ++_requestGeneration;
    final next = await AsyncValue.guard(() => _api.fetchConsent());
    if (isStaleBillingResult(generation, _requestGeneration)) {
      return;
    }
    if (next.hasValue) {
      _lastSuccessful = next.value;
      state = next;
      return;
    }
    final previous = _lastSuccessful;
    state = previous == null ? next : AsyncData(previous);
  }

  /// 监护人明确同意消费条款。
  Future<BillingConsent> grant() async {
    final generation = ++_requestGeneration;
    final consent = await _api.grantConsent();
    if (isStaleBillingResult(generation, _requestGeneration)) {
      return consent;
    }
    _lastSuccessful = consent;
    state = AsyncData(consent);
    return consent;
  }

  /// 监护人撤回消费同意；之后再次购买需要重新同意。
  Future<BillingConsent> revoke() async {
    final generation = ++_requestGeneration;
    final consent = await _api.revokeConsent();
    if (isStaleBillingResult(generation, _requestGeneration)) {
      return consent;
    }
    _lastSuccessful = consent;
    state = AsyncData(consent);
    return consent;
  }

  /// 读取当前授权；读取失败时视为未授权，购买前会再次要求同意。
  Future<bool> ensureGranted() async {
    final current = state.value;
    if (current != null) {
      return current.isGranted;
    }
    try {
      await refresh();
    } on Object {
      return false;
    }
    return state.value?.isGranted ?? false;
  }
}

final billingConsentControllerProvider =
    AsyncNotifierProvider<BillingConsentController, BillingConsent>(
      BillingConsentController.new,
    );

/// 订单列表与单条订单详情的可渲染状态。
class BillingOrdersState {
  const BillingOrdersState({
    required this.orders,
    required this.total,
    this.selectedOrder,
    this.isLoadingMore = false,
    this.loadMoreError,
  });

  const BillingOrdersState.initial()
    : orders = const <BillingOrder>[],
      total = 0,
      selectedOrder = null,
      isLoadingMore = false,
      loadMoreError = null;

  final List<BillingOrder> orders;
  final int total;

  /// 详情页当前关注的订单，可能来自列表，也可能是单独读取的。
  final BillingOrder? selectedOrder;
  final bool isLoadingMore;
  final String? loadMoreError;

  bool get isEmpty => orders.isEmpty;
  bool get hasMore => orders.length < total;

  BillingOrdersState copyWith({
    List<BillingOrder>? orders,
    int? total,
    BillingOrder? selectedOrder,
    bool? isLoadingMore,
    String? loadMoreError,
    bool clearLoadMoreError = false,
  }) {
    return BillingOrdersState(
      orders: orders ?? this.orders,
      total: total ?? this.total,
      selectedOrder: selectedOrder ?? this.selectedOrder,
      isLoadingMore: isLoadingMore ?? this.isLoadingMore,
      loadMoreError: clearLoadMoreError
          ? null
          : (loadMoreError ?? this.loadMoreError),
    );
  }

  /// 用最新订单替换列表中的同单号条目。
  BillingOrdersState replaceOrder(BillingOrder order) {
    final index = orders.indexWhere(
      (item) => item.orderNo == order.orderNo,
    );
    final nextOrders = List<BillingOrder>.of(orders);
    if (index >= 0) {
      nextOrders[index] = order;
    } else {
      nextOrders.insert(0, order);
    }
    return BillingOrdersState(
      orders: nextOrders,
      total: index >= 0 ? total : total + 1,
      selectedOrder: selectedOrder?.orderNo == order.orderNo
          ? order
          : (selectedOrder ?? order),
      isLoadingMore: isLoadingMore,
      loadMoreError: loadMoreError,
    );
  }
}

/// 订单历史分页加载、详情读取，以及取消与退款动作。
class BillingOrdersController extends AsyncNotifier<BillingOrdersState> {
  static const pageSize = 20;

  late BillingApi _api;
  int _requestGeneration = 0;
  int _page = 1;

  int get requestGeneration => _requestGeneration;

  @override
  Future<BillingOrdersState> build() async {
    _api = ref.read(billingApiProvider);
    _page = 1;
    final generation = ++_requestGeneration;
    final page = await _api.fetchOrders(page: 1, pageSize: pageSize);
    if (isStaleBillingResult(generation, _requestGeneration)) {
      throw const StaleBillingResultException();
    }
    return BillingOrdersState(orders: page.orders, total: page.total);
  }

  Future<void> refresh() async {
    final generation = ++_requestGeneration;
    _page = 1;
    final next = await AsyncValue.guard(
      () => _api.fetchOrders(page: 1, pageSize: pageSize),
    );
    if (isStaleBillingResult(generation, _requestGeneration)) {
      return;
    }
    state = next.whenData(
      (page) => BillingOrdersState(
        orders: page.orders,
        total: page.total,
        selectedOrder: state.value?.selectedOrder,
      ),
    );
  }

  /// 追加下一页；失败只在列表底部提示，不清空已加载的订单。
  Future<void> loadMore() async {
    final current = state.value;
    if (current == null || current.isLoadingMore || !current.hasMore) {
      return;
    }
    state = AsyncData(
      current.copyWith(isLoadingMore: true, clearLoadMoreError: true),
    );
    final generation = _requestGeneration;
    final nextPage = _page + 1;
    try {
      final page = await _api.fetchOrders(
        page: nextPage,
        pageSize: pageSize,
      );
      if (isStaleBillingResult(generation, _requestGeneration)) {
        return;
      }
      _page = nextPage;
      final latest = state.value ?? current;
      final known = latest.orders.map((order) => order.orderNo).toSet();
      state = AsyncData(
        latest.copyWith(
          orders: [
            ...latest.orders,
            ...page.orders.where((order) => !known.contains(order.orderNo)),
          ],
          total: page.total,
          isLoadingMore: false,
          clearLoadMoreError: true,
        ),
      );
    } on Object catch (error) {
      if (isStaleBillingResult(generation, _requestGeneration)) {
        return;
      }
      state = AsyncData(
        (state.value ?? current).copyWith(
          isLoadingMore: false,
          loadMoreError: billingLoadErrorMessage(error),
        ),
      );
    }
  }

  /// 读取一条订单详情并记为当前关注订单。
  Future<BillingOrder> loadOrder(String orderNo) async {
    final generation = ++_requestGeneration;
    final order = await _api.fetchOrder(orderNo);
    if (isStaleBillingResult(generation, _requestGeneration)) {
      throw const StaleBillingResultException();
    }
    final current = state.value ?? const BillingOrdersState.initial();
    state = AsyncData(current.copyWith(selectedOrder: order));
    return order;
  }

  /// 取消待支付订单。
  Future<BillingOrder> cancel(String orderNo) async {
    final generation = _requestGeneration;
    final updated = await _applyOrder(_api.cancelOrder(orderNo), orderNo);
    if (isStaleBillingResult(generation, _requestGeneration)) {
      return updated;
    }
    final current = state.value ?? const BillingOrdersState.initial();
    state = AsyncData(current.replaceOrder(updated));
    return updated;
  }

  /// 申请退款，[reason] 会原样交给平台审核。
  Future<BillingOrder> requestRefund({
    required String orderNo,
    required String reason,
  }) async {
    final generation = _requestGeneration;
    final updated = await _applyOrder(
      _api.requestRefund(orderNo: orderNo, reason: reason),
      orderNo,
    );
    if (isStaleBillingResult(generation, _requestGeneration)) {
      return updated;
    }
    final current = state.value ?? const BillingOrdersState.initial();
    state = AsyncData(current.replaceOrder(updated));
    return updated;
  }

  /// 后端只回状态时按本地已知订单更新，避免详情丢失。
  Future<BillingOrder> _applyOrder(
    Future<BillingOrder> pending,
    String orderNo,
  ) async {
    try {
      return await pending;
    } on BillingPartialOrderException {
      final current = state.value;
      final known = current?.orders
          .where((order) => order.orderNo == orderNo)
          .firstOrNull;
      final selected = current?.selectedOrder;
      if (known != null) {
        return known;
      }
      if (selected != null) {
        return selected;
      }
      throw const FormatException('missing order');
    }
  }
}

final billingOrdersControllerProvider =
    AsyncNotifierProvider<BillingOrdersController, BillingOrdersState>(
      BillingOrdersController.new,
    );

/// 陈旧请求被丢弃时抛出的内部信号，不会出现在界面上。
class StaleBillingResultException implements Exception {
  const StaleBillingResultException();

  @override
  String toString() => 'StaleBillingResultException';
}

/// 家庭每月消费限额的读写状态。
///
/// 页面按当前登录家庭读取；家庭信息还没准备好时先等 `familyId` 完成，
/// 期间不发出限额请求，界面停留在加载态。保存成功后直接用服务端返回的
/// 余额覆盖本地状态，不再补一次读取，避免进度条闪烁。
class SpendingLimitController extends AsyncNotifier<SpendingLimit> {
  late BillingApi _api;
  int _requestGeneration = 0;

  /// 当前请求生成号，用于丢弃过期的读取结果。
  int get requestGeneration => _requestGeneration;

  @override
  Future<SpendingLimit> build() async {
    _api = ref.read(billingApiProvider);
    final familyId = await ref.watch(spendingFamilyIdProvider.future);
    final generation = ++_requestGeneration;
    final limit = await _api.fetchSpendingLimit(familyId);
    if (isStaleBillingResult(generation, _requestGeneration)) {
      throw const StaleBillingResultException();
    }
    return limit;
  }

  /// 重新读取限额。
  Future<void> refresh() async {
    final generation = ++_requestGeneration;
    final next = await AsyncValue.guard(() async {
      final familyId = await ref.read(spendingFamilyIdProvider.future);
      return _api.fetchSpendingLimit(familyId);
    });
    if (isStaleBillingResult(generation, _requestGeneration)) {
      return;
    }
    state = next;
  }

  /// 保存每月上限。[monthlyLimitCents] 为零表示不限额。
  Future<SpendingLimit> saveMonthlyLimit(int monthlyLimitCents) async {
    final generation = ++_requestGeneration;
    final familyId = await ref.read(spendingFamilyIdProvider.future);
    final saved = await _api.updateSpendingLimit(
      familyId,
      monthlyLimitCents: monthlyLimitCents,
    );
    if (isStaleBillingResult(generation, _requestGeneration)) {
      return saved;
    }
    state = AsyncData(saved);
    return saved;
  }
}

final spendingLimitControllerProvider =
    AsyncNotifierProvider<SpendingLimitController, SpendingLimit>(
      SpendingLimitController.new,
    );

/// 当前家庭的 family id，来自家庭能力摘要。
///
/// 限额接口按家庭维度寻址，客户端不自行拼接家庭标识；摘要里没有家庭时
/// 返回空字符串，页面据此显示空状态而不是发出注定失败的请求。
final spendingFamilyIdProvider = FutureProvider<String>((ref) async {
  final family = await ref.watch(familyControllerProvider.future);
  return family.capability.familyId;
});

/// 只有真正的业务失败才转成提示，陈旧结果静默丢弃。
String billingStateErrorMessage(Object? error) {
  if (error is StaleBillingResultException) {
    return '';
  }
  if (error is AppException) {
    return error.message;
  }
  if (error == null) {
    return billingLoadErrorMessage(null);
  }
  // 解析失败等非传输错误也要给出家长能读懂的说明。
  return mapApiError(error).message;
}
