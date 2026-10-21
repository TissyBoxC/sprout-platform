import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/app_exception.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/features/child_profile/application/child_profile_controller.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';
import 'package:parent_app/features/growth_report/application/growth_report_controller.dart';
import 'package:parent_app/features/growth_report/data/growth_report_api.dart';
import 'package:parent_app/features/growth_report/domain/growth_report.dart';
import 'package:parent_app/features/growth_report/presentation/growth_report_page.dart';

void main() {
  group('GrowthReportPage', () {
    testWidgets('shows the window, summary and comparison', (tester) async {
      await tester.pumpWidget(_reportApp(reports: <GrowthReport>[_report()]));
      await tester.pumpAndSettle();

      // The period name appears on the filter chip and on the card heading, so
      // two matches is the intended shape rather than a duplicate render.
      expect(find.text('周报'), findsNWidgets(2));
      expect(find.text('10月19日 - 10月25日'), findsOneWidget);
      expect(find.textContaining('练习了 6 次'), findsOneWidget);
      expect(find.text('75%（3/4）'), findsOneWidget);
      // A change is named with its direction so the number means something.
      expect(find.text('比上期多 2次'), findsOneWidget);
    });

    testWidgets('explains an empty list instead of showing nothing', (
      tester,
    ) async {
      await tester.pumpWidget(
        _reportApp(reports: const <GrowthReport>[]),
      );
      await tester.pumpAndSettle();

      expect(find.text('还没有成长报告'), findsOneWidget);
      expect(find.textContaining('自动生成'), findsOneWidget);
    });

    testWidgets('asks the guardian to add a child first', (tester) async {
      await tester.pumpWidget(
        _reportApp(reports: const <GrowthReport>[], children: const []),
      );
      await tester.pumpAndSettle();

      expect(find.text('还没有儿童档案'), findsOneWidget);
    });

    testWidgets('shows a readable retry state instead of raw errors', (
      tester,
    ) async {
      await tester.pumpWidget(
        _reportApp(
          error: const AppException(
            kind: AppErrorKind.serviceUnavailable,
            message: '暂时读不到成长报告，请稍后重新加载。',
            retryable: true,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('暂时读不到成长报告，请稍后重新加载。'), findsOneWidget);
      expect(find.text('重新加载'), findsOneWidget);
    });

    testWidgets('filters to one period without losing the others', (
      tester,
    ) async {
      await tester.pumpWidget(
        _reportApp(
          reports: <GrowthReport>[
            _report(),
            _report(period: GrowthReportPeriod.monthly, id: 'r-2'),
          ],
        ),
      );
      await tester.pumpAndSettle();

      // Both are visible before filtering.
      expect(find.byType(Card), findsNWidgets(2));

      await tester.tap(find.widgetWithText(ChoiceChip, '月报'));
      await tester.pumpAndSettle();

      expect(find.byType(Card), findsOneWidget);
    });
  });

  group('GrowthReport.fromJson', () {
    test('reads nested metrics and deltas', () {
      final report = GrowthReport.fromJson(<String, dynamic>{
        'id': 'r-1',
        'child_id': 'c-1',
        'period_type': 'monthly',
        'period_start': '2026-10-01',
        'period_end': '2026-11-01',
        'metrics': <String, dynamic>{
          'practice': <String, dynamic>{
            'attempts': 8,
            'passed': 6,
            'average_score': 82.5,
            'accuracy': 75.0,
          },
          'previous_practice': <String, dynamic>{
            'attempts': 4,
            'passed': 4,
            'average_score': 90.0,
            'accuracy': 100.0,
          },
          'usage': <String, dynamic>{
            'active_seconds': 1800,
            'conversation_count': 12,
            'conversation_seconds': 900,
          },
          'counts': <String, dynamic>{'growth_records': 3, 'memories': 5},
          'deltas': <String, dynamic>{
            'practice': 4,
            'accuracy': -25.0,
            'active_seconds': 600,
          },
          'accuracy_phrase': '75%（6/8）',
        },
        'summary': '这个月练习了 8 次。',
        'generated_at': '2026-11-01T00:05:00Z',
      });

      expect(report.period, GrowthReportPeriod.monthly);
      expect(report.practice.attempts, 8);
      expect(report.previousPractice.attempts, 4);
      expect(report.usage.activeMinutes, 30);
      expect(report.counts.memories, 5);
      expect(report.practiceDelta, 4);
      expect(report.accuracyDelta, -25);
      expect(report.activeSecondsDelta, 600);
      expect(report.isQuiet, isFalse);
    });

    test('survives missing metrics and unknown period', () {
      final report = GrowthReport.fromJson(<String, dynamic>{
        'id': 'r-2',
        'period_type': 'quarterly',
      });

      // An unknown period must not crash the list; it degrades to weekly.
      expect(report.period, GrowthReportPeriod.weekly);
      expect(report.practice.isEmpty, isTrue);
      expect(report.usage.activeMinutes, 0);
      expect(report.periodLabel, '');
      expect(report.isQuiet, isTrue);
    });
  });
}

class _FixedChildController extends ChildProfileController {
  _FixedChildController(this._profiles);

  final List<ChildProfile> _profiles;

  @override
  Future<List<ChildProfile>> build() async => _profiles;
}

class _FixedReportController extends GrowthReportController {
  _FixedReportController({required this.reports, this.error});

  final List<GrowthReport> reports;
  final Object? error;

  @override
  Future<GrowthReportState> build() async {
    final failure = error;
    if (failure != null) {
      throw failure;
    }
    return GrowthReportState(childId: 'child-1', reports: reports);
  }

  @override
  Future<void> load(String childId) async {
    state = AsyncValue<GrowthReportState>.data(
      GrowthReportState(childId: childId, reports: reports),
    );
  }
}

Widget _reportApp({
  List<GrowthReport> reports = const <GrowthReport>[],
  List<ChildProfile>? children,
  Object? error,
}) {
  final profiles = children ?? <ChildProfile>[_child()];
  return ProviderScope(
    overrides: [
      childProfileControllerProvider.overrideWith(
        () => _FixedChildController(profiles),
      ),
      growthReportControllerProvider.overrideWith(
        () => _FixedReportController(reports: reports, error: error),
      ),
    ],
    child: MaterialApp(
      theme: AppTheme.light,
      home: const GrowthReportPage(),
    ),
  );
}

ChildProfile _child() {
  final now = DateTime.utc(2026);
  return ChildProfile(
    childId: 'child-1',
    familyId: 'family-1',
    nickname: '小芽',
    ageTier: ChildAgeTier.age3To4,
    interests: const <String>[],
    contentCategories: const <ChildContentCategory>[],
    guardianConsent: true,
    createdAt: now,
    updatedAt: now,
  );
}

GrowthReport _report({
  String id = 'r-1',
  GrowthReportPeriod period = GrowthReportPeriod.weekly,
}) {
  return GrowthReport(
    id: id,
    childId: 'child-1',
    period: period,
    periodStart: DateTime.utc(2026, 10, 19),
    periodEnd: DateTime.utc(2026, 10, 26),
    practice: const PracticeTotals(
      attempts: 6,
      passed: 3,
      averageScore: 88.5,
      accuracy: 75,
    ),
    previousPractice: const PracticeTotals(
      attempts: 4,
      passed: 2,
      averageScore: 70,
      accuracy: 50,
    ),
    usage: const UsageTotals(
      activeSeconds: 900,
      conversationCount: 5,
      conversationSeconds: 300,
    ),
    counts: const ReportCounts(growthRecords: 2, memories: 3),
    practiceDelta: 2,
    accuracyDelta: 25,
    activeSecondsDelta: 300,
    accuracyPhrase: '75%（3/4）',
    summary: '这一周练习了 6 次，完成 3 次，平均得分 88.5。',
    generatedAt: DateTime.utc(2026, 10, 26, 0, 5),
  );
}
