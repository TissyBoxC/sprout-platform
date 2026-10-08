import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/usage_report_api.dart';
import '../domain/usage_report.dart';

/// Loads the signed-in guardian usage report with a bounded day range.
class UsageReportController extends AsyncNotifier<UsageReport> {
  late final UsageReportApi _api;
  int _days = 30;
  UsageReport? _lastSuccessful;

  int get days => _days;

  @override
  Future<UsageReport> build() {
    _api = ref.read(usageReportApiProvider);
    return _api.list(days: _days);
  }

  Future<void> refresh() async {
    final next = await AsyncValue.guard(() => _api.list(days: _days));
    state = _keepLastSuccessfulOnError(next);
  }

  Future<void> setRange(int days) async {
    final boundedDays = days.clamp(1, 90);
    if (boundedDays == _days) {
      return;
    }
    _days = boundedDays;
    state = const AsyncLoading<UsageReport>();
    final next = await AsyncValue.guard(() => _api.list(days: _days));
    state = _keepLastSuccessfulOnError(next);
  }

  AsyncValue<UsageReport> _keepLastSuccessfulOnError(
    AsyncValue<UsageReport> next,
  ) {
    if (next.hasValue) {
      _lastSuccessful = next.value;
      return next;
    }
    if (_lastSuccessful != null) {
      return AsyncData(_lastSuccessful!);
    }
    return next;
  }
}

final usageReportApiProvider = Provider<UsageReportApi>((ref) {
  return UsageReportApi(ref.read(apiClientProvider));
});

final usageReportControllerProvider =
    AsyncNotifierProvider<UsageReportController, UsageReport>(
      UsageReportController.new,
    );

/// Returns copy safe to display for the usage-report module.
String usageReportErrorMessage(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '暂时无法读取使用记录，请稍后重试';
}
