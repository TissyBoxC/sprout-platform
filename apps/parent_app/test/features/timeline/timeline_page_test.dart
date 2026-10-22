import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:parent_app/core/error/api_error_mapper.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/core/time/current_time.dart';
import 'package:parent_app/features/child_profile/application/child_profile_controller.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';
import 'package:parent_app/features/timeline/application/timeline_controller.dart';
import 'package:parent_app/features/timeline/data/timeline_api.dart';
import 'package:parent_app/features/timeline/domain/timeline.dart';
import 'package:parent_app/features/timeline/presentation/timeline_copy.dart';
import 'package:parent_app/features/timeline/presentation/timeline_page.dart';

void main() {
  group('必须先选孩子', () {
    testWidgets('asks for a child before any read is made', (tester) async {
      final api = _FakeTimelineApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(find.text(TimelineCopy.childSelectionHint), findsOneWidget);
      expect(find.text(TimelineCopy.childTimelineHint), findsOneWidget);
      // The platform refuses a read without a child, so none is sent.
      expect(api.eventCalls, isEmpty);
      expect(api.milestoneCalls, isEmpty);
    });

    testWidgets('a family without children explains the first step', (
      tester,
    ) async {
      await tester.pumpWidget(
        _page(api: _FakeTimelineApi(), children: const <ChildProfile>[]),
      );
      await tester.pumpAndSettle();

      expect(find.text(TimelineCopy.childEmptyTitle), findsOneWidget);
      expect(find.text(TimelineCopy.childEmptyBody), findsOneWidget);
    });
  });

  group('加载中', () {
    testWidgets('shows a loading state until the first page arrives', (
      tester,
    ) async {
      final api = _FakeTimelineApi(pending: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const ValueKey<String>('timeline-child-child-1')),
      );
      await tester.pump();
      await tester.pump();

      expect(find.text(TimelineCopy.loading), findsOneWidget);

      api.release();
      await tester.pumpAndSettle();
      expect(find.text('完成一次测评'), findsOneWidget);
    });
  });

  group('空状态', () {
    testWidgets('an empty timeline explains what will appear', (tester) async {
      final api = _FakeTimelineApi(hasEvents: false);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      expect(find.text(TimelineCopy.emptyTitle), findsOneWidget);
      expect(find.text(TimelineCopy.emptyBody), findsOneWidget);
    });
  });

  group('有数据', () {
    testWidgets('shows the counters and the events oldest first', (
      tester,
    ) async {
      final api = _FakeTimelineApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      expect(find.text(TimelineCopy.milestonesTitle), findsOneWidget);
      for (final metric in MilestoneMetric.values) {
        // 听完内容 is both a counter and the caption of a content event, so
        // the counter is asserted inside the counters card.
        expect(
          find.descendant(
            of: find.byKey(const ValueKey<String>('timeline-milestones')),
            matching: find.text(TimelineCopy.milestoneLabel(metric)),
          ),
          findsOneWidget,
        );
      }
      expect(find.text('完成一次测评'), findsOneWidget);
      expect(find.text('读完了一本故事'), findsOneWidget);
      // Oldest first: the assessment happened before the story.
      final assessment = tester.getTopLeft(find.text('完成一次测评')).dy;
      final story = tester.getTopLeft(find.text('读完了一本故事')).dy;
      expect(assessment, lessThan(story));
    });

    testWidgets('every event names its kind and a relative time', (
      tester,
    ) async {
      final api = _FakeTimelineApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      expect(
        find.text(TimelineCopy.kindName(TimelineEventKind.assessmentCompleted)),
        findsOneWidget,
      );
      // 听完内容 is also a counter label, so the event caption is looked for
      // beside the story rather than anywhere on the page.
      expect(
        find.descendant(
          of: find.byKey(
            const ValueKey<String>('timeline-event-content:child-1:1'),
          ),
          matching: find.text(
            TimelineCopy.kindName(TimelineEventKind.contentCompleted),
          ),
        ),
        findsOneWidget,
      );
      // 3 days ago is still a relative phrase, not a date.
      expect(find.text('3 天前'), findsOneWidget);
      // 40 days ago is far enough that the calendar date is clearer.
      expect(
        find.text(TimelineCopy.date(_now.subtract(const Duration(days: 40)))),
        findsOneWidget,
      );
    });

    testWidgets('an event kind this build does not know still renders', (
      tester,
    ) async {
      final api = _FakeTimelineApi(unknownKind: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      expect(find.text('初次见面'), findsOneWidget);
      expect(
        find.text(TimelineCopy.kindName(TimelineEventKind.other)),
        findsOneWidget,
      );
      expect(_findLeakedTerms(tester), isNull);
    });
  });

  group('分页', () {
    testWidgets('one cursor means one more page, then the end', (tester) async {
      final api = _FakeTimelineApi(pages: 2);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      final more = find.byKey(const ValueKey<String>('timeline-load-more'));
      await tester.scrollUntilVisible(more, 300);
      await tester.tap(more);
      await tester.pumpAndSettle();
      await tester.pumpAndSettle();

      expect(api.eventCalls, <String>['child-1||', 'child-1|cursor-1|']);
      _expand(tester);
      await tester.pumpAndSettle();
      expect(find.text('更早的一次练习'), findsOneWidget);
      // The second page had no cursor, so no button remains.
      _expand(tester);
      await tester.pumpAndSettle();
      expect(more, findsNothing);
      expect(
        find.byKey(const ValueKey<String>('timeline-reached-end')),
        findsOneWidget,
      );
    });

    testWidgets('the end of the list is not requested again', (tester) async {
      final api = _FakeTimelineApi(pages: 1);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      _expand(tester);
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey<String>('timeline-reached-end')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey<String>('timeline-load-more')),
        findsNothing,
      );
      // The exact number of reads, so a closed list cannot spin.
      expect(api.eventCalls, <String>['child-1||']);
    });

    testWidgets('an entry that arrives twice is shown once', (tester) async {
      final api = _FakeTimelineApi(pages: 3, overlapping: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      final more = find.byKey(const ValueKey<String>('timeline-load-more'));
      await tester.scrollUntilVisible(more, 300);
      await tester.tap(more);
      await tester.pumpAndSettle();
      await tester.pumpAndSettle();

      expect(find.text('完成一次测评'), findsOneWidget);
    });

    testWidgets('a failed page keeps the list and offers to read again', (
      tester,
    ) async {
      final api = _FakeTimelineApi(pages: 2, failSecondPage: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      final more = find.byKey(const ValueKey<String>('timeline-load-more'));
      await tester.scrollUntilVisible(more, 300);
      await tester.tap(more);
      await tester.pumpAndSettle();
      await tester.pumpAndSettle();

      expect(find.text('完成一次测评'), findsOneWidget);
      _expand(tester);
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey<String>('timeline-load-more-error')),
        findsOneWidget,
      );
      expect(find.text(mapApiError(_offline()).message), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });
  });

  group('切换孩子', () {
    testWidgets('switching children reads the new child and keeps the list', (
      tester,
    ) async {
      final api = _FakeTimelineApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);
      expect(find.text('完成一次测评'), findsOneWidget);

      await tester.tap(
        find.byKey(const ValueKey<String>('timeline-child-child-2')),
      );
      await tester.pumpAndSettle();

      expect(api.eventCalls.last, 'child-2||');
      expect(api.milestoneCalls.last, 'child-2');
      // The second child has their own counters, not the first one's.
      expect(
        find.descendant(
          of: find.byKey(const ValueKey<String>('timeline-milestones')),
          matching: find.text('3 次'),
        ),
        findsWidgets,
      );
    });
  });

  group('错误状态', () {
    testWidgets('a lapsed sign-in asks the guardian to sign in again', (
      tester,
    ) async {
      final api = _FakeTimelineApi(failure: _status(401));
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      expect(find.text('登录已过期，请重新登录'), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('an unreachable network offers a retry that works', (
      tester,
    ) async {
      final api = _FakeTimelineApi(failure: _offline());
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);

      api.failure = null;
      await tester.tap(find.text(TimelineCopy.retryButton));
      await tester.pumpAndSettle();

      expect(find.text('完成一次测评'), findsOneWidget);
    });

    testWidgets('counters that fail do not hide the events', (tester) async {
      final api = _FakeTimelineApi(failMilestones: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _selectFirstChild(tester);

      expect(find.text(TimelineCopy.milestonesFailed), findsOneWidget);
      expect(find.text('完成一次测评'), findsOneWidget);
    });
  });
}

/// Gives the list a tall viewport so a case can read its whole content.
///
/// A phone-sized viewport shows two cards, and the end of the list is a footer
/// below them; growing the window keeps the assertions about the list rather
/// than about scrolling.
void _expand(WidgetTester tester) {
  tester.view.physicalSize = const Size(1200, 3600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

/// The widget tree every case starts from: the real page, the real notifier,
/// and a fake only at the data boundary.
Widget _page({required _FakeTimelineApi api, List<ChildProfile>? children}) {
  final profiles = children ?? _twoChildren;
  final router = GoRouter(
    initialLocation: '/me/timeline',
    routes: <RouteBase>[
      GoRoute(
        path: '/me',
        builder: (context, state) => const Scaffold(body: Text('我的占位')),
      ),
      GoRoute(
        path: '/me/timeline',
        builder: (context, state) => const TimelinePageView(),
      ),
    ],
  );
  return ProviderScope(
    overrides: [
      // Every provider the page reaches is replaced: a future provider left
      // alone would never settle and no screen would ever appear.
      timelineApiProvider.overrideWithValue(api),
      timelineChildrenProvider.overrideWith((ref) async => profiles),
      // The relative times are rendered against a fixed clock so a case is
      // about the sentence, not about when the suite happened to run.
      currentTimeProvider.overrideWithValue(() => _now),
      familyControllerProvider.overrideWith(_SeededFamilyController.new),
      childProfileControllerProvider.overrideWith(_SeededChildController.new),
    ],
    child: MaterialApp.router(theme: AppTheme.light, routerConfig: router),
  );
}

/// Choosing the first child is the platform's required first step.
Future<void> _selectFirstChild(WidgetTester tester) async {
  await tester.tap(
    find.byKey(const ValueKey<String>('timeline-child-child-1')),
  );
  await tester.pumpAndSettle();
}

/// The instant the relative sentences are measured from.
final _now = DateTime.utc(2026, 10, 22, 12);

final _twoChildren = <ChildProfile>[
  ChildProfile(
    childId: 'child-1',
    familyId: 'family-1',
    nickname: '朵朵',
    ageTier: ChildAgeTier.age5To6,
    interests: const <String>[],
    contentCategories: const <ChildContentCategory>[],
    guardianConsent: true,
    createdAt: _now,
    updatedAt: _now,
  ),
  ChildProfile(
    childId: 'child-2',
    familyId: 'family-1',
    nickname: '豆豆',
    ageTier: ChildAgeTier.age3To4,
    interests: const <String>[],
    contentCategories: const <ChildContentCategory>[],
    guardianConsent: true,
    createdAt: _now,
    updatedAt: _now,
  ),
];

class _FakeTimelineApi implements TimelineApi {
  _FakeTimelineApi({
    this.pages = 1,
    this.hasEvents = true,
    this.unknownKind = false,
    this.overlapping = false,
    this.failSecondPage = false,
    this.failMilestones = false,
    this.pending = false,
    this.failure,
  });

  final int pages;
  final bool hasEvents;
  final bool unknownKind;
  final bool overlapping;
  final bool failSecondPage;
  final bool failMilestones;
  final bool pending;
  Object? failure;

  final List<String> eventCalls = <String>[];
  final List<String> milestoneCalls = <String>[];
  Completer<void>? _gate;

  void release() {
    _gate?.complete();
    _gate = null;
  }

  @override
  Future<TimelinePage> fetchEvents({
    required String childId,
    String before = '',
    int limit = 30,
  }) async {
    eventCalls.add('$childId|$before|');
    if (pending) {
      _gate ??= Completer<void>();
      await _gate!.future;
    }
    final failed = failure;
    if (failed != null) {
      throw mapApiError(failed);
    }
    if (before.isEmpty) {
      return TimelinePage(
        events: hasEvents ? _firstPage(childId) : const <TimelineEvent>[],
        nextCursor: pages > 1 ? 'cursor-1' : '',
      );
    }
    if (failSecondPage) {
      throw mapApiError(_offline());
    }
    if (before == 'cursor-1') {
      return TimelinePage(
        events: _secondPage(childId),
        nextCursor: pages > 2 ? 'cursor-2' : '',
      );
    }
    return const TimelinePage(events: <TimelineEvent>[], nextCursor: '');
  }

  @override
  Future<TimelineMilestones> fetchMilestones({required String childId}) async {
    milestoneCalls.add(childId);
    if (failMilestones) {
      throw mapApiError(_offline());
    }
    if (childId == 'child-2') {
      return const TimelineMilestones(
        completedAssessments: 3,
        practiceDays: 5,
        currentStreakDays: 2,
        longestStreakDays: 4,
        publishedReports: 1,
        markedMilestones: 1,
        completedContent: 2,
        total: 18,
      );
    }
    return const TimelineMilestones(
      completedAssessments: 8,
      practiceDays: 21,
      currentStreakDays: 6,
      longestStreakDays: 12,
      publishedReports: 4,
      markedMilestones: 2,
      completedContent: 15,
      total: 62,
    );
  }

  List<TimelineEvent> _firstPage(String childId) {
    if (unknownKind) {
      return <TimelineEvent>[
        TimelineEvent(
          eventId: 'milestone:1',
          kind: TimelineEventKind.other,
          occurredAt: _now.subtract(const Duration(days: 2)),
          title: '初次见面',
          description: '这一天孩子第一次和初芽打招呼。',
        ),
      ];
    }
    return <TimelineEvent>[
      TimelineEvent(
        eventId: 'assessment:$childId:1',
        kind: TimelineEventKind.assessmentCompleted,
        occurredAt: _now.subtract(const Duration(days: 40)),
        title: '完成一次测评',
        description: '水果和颜色这一课练完了一遍。',
      ),
      TimelineEvent(
        eventId: 'content:$childId:1',
        kind: TimelineEventKind.contentCompleted,
        occurredAt: _now.subtract(const Duration(days: 3)),
        title: '读完了一本故事',
        description: '《小熊的雨天》听完了。',
        subject: '小熊的雨天',
      ),
    ];
  }

  List<TimelineEvent> _secondPage(String childId) {
    return <TimelineEvent>[
      if (overlapping)
        TimelineEvent(
          eventId: 'assessment:$childId:1',
          kind: TimelineEventKind.assessmentCompleted,
          occurredAt: _now.subtract(const Duration(days: 40)),
          title: '完成一次测评',
          description: '水果和颜色这一课练完了一遍。',
        ),
      TimelineEvent(
        eventId: 'practice:$childId:2',
        kind: TimelineEventKind.englishPractice,
        occurredAt: _now.subtract(const Duration(days: 45)),
        title: '更早的一次练习',
        description: '跟着初芽读完了五个单词。',
      ),
    ];
  }
}

class _SeededFamilyController extends FamilyController {
  @override
  Future<FamilyMembershipState> build() async {
    return const FamilyMembershipState(
      capability: FamilyCapability(
        familyId: 'family-1',
        ownerUserId: 'user-1',
        memberCount: 1,
        pendingInviteCount: 0,
        schemaVersion: '1',
      ),
      members: <FamilyMember>[],
      invitations: <FamilyInvitation>[],
    );
  }
}

class _SeededChildController extends ChildProfileController {
  @override
  Future<List<ChildProfile>> build() async => _twoChildren;
}

DioException _offline() {
  return DioException(
    requestOptions: RequestOptions(path: '/api/v1/families/family-1'),
    type: DioExceptionType.connectionError,
    message: 'SocketException: connection refused (127.0.0.1:8081)',
  );
}

DioException _status(int status, {String code = 'unauthenticated'}) {
  final options = RequestOptions(path: '/api/v1/families/family-1');
  return DioException(
    requestOptions: options,
    type: DioExceptionType.badResponse,
    response: Response<Object?>(
      requestOptions: options,
      statusCode: status,
      data: <String, Object?>{
        'error': <String, Object?>{
          'code': code,
          'message': 'raw backend message',
          'retryable': false,
        },
      },
    ),
  );
}

String? _findLeakedTerms(WidgetTester tester) {
  const banned = <String>[
    'API',
    'token',
    'Token',
    'session',
    'Session',
    'status',
    'timeline_',
    'child_id',
    'next_cursor',
    '127.0.0.1',
    '[object Object]',
    'undefined',
    'null',
    'NaN',
    'Exception',
    'UUID',
  ];
  final texts = tester
      .widgetList<Text>(find.byType(Text))
      .map((text) => text.data ?? text.textSpan?.toPlainText() ?? '');
  for (final text in texts) {
    for (final word in banned) {
      if (text.contains(word)) {
        return word;
      }
    }
  }
  return null;
}
