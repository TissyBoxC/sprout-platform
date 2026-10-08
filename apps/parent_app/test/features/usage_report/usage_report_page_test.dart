import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';
import 'package:parent_app/features/usage_report/application/usage_report_controller.dart';
import 'package:parent_app/features/usage_report/domain/usage_report.dart';
import 'package:parent_app/features/usage_report/presentation/usage_report_page.dart';

void main() {
  testWidgets('shows the summary, trend, category, and device breakdown', (
    tester,
  ) async {
    await tester.pumpWidget(
      _usageApp(
        AsyncData(
          UsageReport(
            days: [
              UsageReportDay(
                schemaVersion: '1.0.0',
                reportDate: _reportDate,
                timezoneOffsetMinutes: 480,
                activeMinutes: 42,
                conversationCount: 18,
                conversationMinutes: 24,
                contentPlayCount: 6,
                contentMinutes: 18,
                dailyLimitMinutes: 60,
                remainingMinutes: 18,
                limitReached: false,
                categories: [
                  UsageReportCategory(
                    category: ChildContentCategory.story,
                    playCount: 3,
                    minutes: 9,
                  ),
                ],
                blocked: UsageReportBlocked(
                  disabledPeriod: 1,
                  dailyLimit: 0,
                  categoryDenied: 2,
                  timeUntrusted: 0,
                ),
                devices: [
                  UsageReportDevice(
                    deviceId: 'device_demo_001',
                    deviceName: '初芽',
                    activeMinutes: 42,
                    conversationCount: 18,
                    contentPlayCount: 6,
                  ),
                ],
                updatedAt: _updatedAt,
              ),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('今日陪伴'), findsOneWidget);
    expect(find.text('近 30 天'), findsOneWidget);
    expect(find.text('42'), findsWidgets);
    expect(find.text('每日使用趋势'), findsOneWidget);

    await tester.scrollUntilVisible(find.text('故事'), 200);
    await tester.pumpAndSettle();
    expect(find.text('故事'), findsOneWidget);
    expect(find.text('9 分钟 · 3 次'), findsOneWidget);
    expect(find.text('共 3 次'), findsOneWidget);
    expect(find.text('初芽'), findsOneWidget);
    expect(find.text('42 分钟'), findsOneWidget);
  });

  testWidgets('shows a branded empty state before any usage exists', (
    tester,
  ) async {
    await tester.pumpWidget(_usageApp(const AsyncData(UsageReport(days: []))));
    await tester.pumpAndSettle();

    expect(find.text('还没有使用记录'), findsOneWidget);
    expect(find.textContaining('设备联网并开始陪伴后'), findsOneWidget);
    expect(find.text('查看设备'), findsOneWidget);
  });

  testWidgets('shows a retry state without exposing the raw error', (
    tester,
  ) async {
    await tester.pumpWidget(
      _usageApp(
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

  testWidgets('changing the range reloads the selected window', (tester) async {
    final controller = _RecordingUsageReportController(
      UsageReport(
        days: [
          UsageReportDay(
            schemaVersion: '1.0.0',
            reportDate: _reportDate,
            timezoneOffsetMinutes: 480,
            activeMinutes: 10,
            conversationCount: 1,
            conversationMinutes: 2,
            contentPlayCount: 1,
            contentMinutes: 3,
            dailyLimitMinutes: 60,
            remainingMinutes: 50,
            limitReached: false,
            categories: const [],
            blocked: const UsageReportBlocked(
              disabledPeriod: 0,
              dailyLimit: 0,
              categoryDenied: 0,
              timeUntrusted: 0,
            ),
            devices: const [],
            updatedAt: _updatedAt,
          ),
        ],
      ),
    );
    await tester.pumpWidget(_usageAppWithController(controller));
    await tester.pumpAndSettle();

    await tester.tap(find.text('近 7 天'));
    await tester.pumpAndSettle();

    expect(controller.days, 7);
    expect(find.text('查看范围'), findsOneWidget);
  });
}

final _reportDate = DateTime(2026, 10, 7);
final _updatedAt = DateTime.utc(2026, 10, 7, 12);

Widget _usageApp(AsyncValue<UsageReport> state) {
  return _usageAppWithController(_FakeUsageReportController(state));
}

Widget _usageAppWithController(UsageReportController controller) {
  return ProviderScope(
    overrides: [usageReportControllerProvider.overrideWith(() => controller)],
    child: const MaterialApp(home: UsageReportPage()),
  );
}

class _FakeUsageReportController extends UsageReportController {
  _FakeUsageReportController(this.initialState);

  final AsyncValue<UsageReport> initialState;

  @override
  Future<UsageReport> build() async {
    return initialState.when(
      data: (value) => value,
      error: (error, stackTrace) => throw error,
      loading: () => const UsageReport(days: []),
    );
  }
}

class _RecordingUsageReportController extends UsageReportController {
  _RecordingUsageReportController(this.value);

  final UsageReport value;

  @override
  int days = 30;

  @override
  Future<UsageReport> build() async => value;

  @override
  Future<void> setRange(int days) async {
    this.days = days;
    state = AsyncData(value);
  }
}
