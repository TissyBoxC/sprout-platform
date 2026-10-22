import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:parent_app/core/error/api_error_mapper.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/features/assessment/application/assessment_controller.dart';
import 'package:parent_app/features/assessment/data/assessment_api.dart';
import 'package:parent_app/features/assessment/domain/assessment.dart';
import 'package:parent_app/features/assessment/presentation/assessment_copy.dart';
import 'package:parent_app/features/assessment/presentation/assessment_page.dart';
import 'package:parent_app/features/child_profile/application/child_profile_controller.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';
import 'package:parent_app/features/english/application/english_controller.dart';
import 'package:parent_app/features/english/data/english_api.dart';
import 'package:parent_app/features/english/domain/english.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';

void main() {
  group('开始前的说明', () {
    testWidgets('states the limits and the scoring rule before starting', (
      tester,
    ) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      expect(find.text(AssessmentCopy.entryTitle), findsWidgets);
      expect(find.text(AssessmentCopy.preflightOne), findsOneWidget);
      expect(find.text(AssessmentCopy.preflightTwo), findsOneWidget);
      expect(find.text(AssessmentCopy.preflightThree), findsOneWidget);
      expect(find.text(AssessmentCopy.preflightFour), findsOneWidget);
      // The catalog is the English one, so the course names match that page.
      expect(find.text('水果和颜色'), findsOneWidget);
      expect(api.startedLessonIds, isEmpty);
    });

    testWidgets('declining the notice never asks the platform', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.text(AssessmentCopy.startButton));
      await tester.pumpAndSettle();
      expect(find.text(AssessmentCopy.preflightConfirmTitle), findsOneWidget);

      await tester.tap(find.text(AssessmentCopy.cancelStart));
      await tester.pumpAndSettle();

      expect(api.startedLessonIds, isEmpty);
      expect(find.text(AssessmentCopy.startButton), findsOneWidget);
    });
  });

  group('进行中的答题', () {
    testWidgets('the first item is shown with its counters', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      expect(api.startedLessonIds, <String>['lesson-1']);
      expect(find.text('第 1 题'), findsOneWidget);
      expect(find.text('1 / 10'), findsOneWidget);
      expect(find.text(AssessmentCopy.difficultyName(2)), findsOneWidget);
      // The budget is whole minutes, so a fresh run reads 10:00, not 9:59.
      expect(find.text('剩余时间 10:00'), findsOneWidget);
      expect(find.text('apple'), findsOneWidget);
      expect(finishButton, findsOneWidget);
      // No device recognition is claimed: the guardian is told to score.
      expect(find.text(AssessmentCopy.scoringHint), findsOneWidget);
    });

    testWidgets('answering correctly records 100 and moves on', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      await tester.tap(find.text(AssessmentCopy.correctButton));
      await tester.pumpAndSettle();

      expect(api.scores, <int>[100]);
      expect(api.answeredItemIds, <String>['word-1']);
      expect(find.text('banana'), findsOneWidget);
      expect(find.text('2 / 10'), findsOneWidget);
    });

    testWidgets('answering wrongly records 0', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      await tester.tap(find.text(AssessmentCopy.wrongButton));
      await tester.pumpAndSettle();

      expect(api.scores, <int>[0]);
      expect(find.text('0 / 1'), findsOneWidget);
    });

    testWidgets('the slider sends a middle score', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      // The middle score is folded away by default, so opening it is the
      // guardian's explicit choice rather than part of every answer.
      final toggle = find.byKey(
        const ValueKey<String>('assessment-partial-toggle'),
      );
      await tester.ensureVisible(toggle);
      await tester.pumpAndSettle();
      await tester.tap(toggle);
      await tester.pumpAndSettle();

      final slider = find.byKey(
        const ValueKey<String>('assessment-score-slider'),
      );
      // A half-way drag expresses "close but not exact", which the two fixed
      // buttons cannot.
      await tester.drag(slider, const Offset(-150, 0));
      await tester.pumpAndSettle();
      final submit = find.byKey(
        const ValueKey<String>('assessment-submit-score'),
      );
      await tester.ensureVisible(submit);
      await tester.pumpAndSettle();
      await tester.tap(submit);
      await tester.pumpAndSettle();

      expect(api.scores, hasLength(1));
      expect(api.scores.single, inInclusiveRange(1, 99));
    });

    testWidgets('a middle score stays folded until it is asked for', (
      tester,
    ) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      // Two buttons should be the whole of a normal answer, so the slider must
      // not compete with them until the guardian opens it.
      expect(
        find.byKey(const ValueKey<String>('assessment-score-slider')),
        findsNothing,
      );
      expect(
        find.byKey(const ValueKey<String>('assessment-submit-score')),
        findsNothing,
      );
      expect(
        find.byKey(const ValueKey<String>('assessment-partial-toggle')),
        findsOneWidget,
      );
    });

    testWidgets('a sentence item is labelled as a sentence', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      await tester.tap(find.text(AssessmentCopy.correctButton));
      await tester.pumpAndSettle();
      await tester.tap(find.text(AssessmentCopy.correctButton));
      await tester.pumpAndSettle();

      expect(find.text('I like apples.'), findsOneWidget);
      expect(find.text('句型'), findsOneWidget);
      expect(api.answeredItemIds, <String>['word-1', 'word-2']);
    });

    testWidgets('ending the run calls finish and shows the result', (
      tester,
    ) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);
      await tester.tap(find.text(AssessmentCopy.correctButton));
      await tester.pumpAndSettle();

      await tester.tap(finishButton);
      await tester.pumpAndSettle();
      expect(find.text(AssessmentCopy.finishConfirmTitle), findsOneWidget);

      await tester.tap(
        find.byKey(const ValueKey<String>('assessment-confirm-finish')),
      );
      await tester.pumpAndSettle();

      expect(api.finishedSessionIds, <String>['session-1']);
      expect(find.text(AssessmentCopy.resultTitle), findsOneWidget);
    });

    testWidgets('a failed answer keeps the item and explains the retry', (
      tester,
    ) async {
      final api = _FakeAssessmentApi(compileFailure: true);
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      await tester.tap(find.text(AssessmentCopy.correctButton));
      await tester.pumpAndSettle();

      expect(find.text('apple'), findsOneWidget);
      // A refusal caused by this item is repeated above the card, so it cannot
      // sit below a question the guardian has already scrolled past.
      expect(find.text(AssessmentCopy.stateChanged), findsWidgets);
      expect(_findLeakedTerms(tester), isNull);
    });
  });

  group('结果页', () {
    testWidgets('shows mastery, the correct count and the level change', (
      tester,
    ) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);
      for (var index = 0; index < 10; index++) {
        await tester.tap(find.text(AssessmentCopy.correctButton));
        await tester.pumpAndSettle();
      }

      expect(api.scores, hasLength(10));
      expect(find.text(AssessmentCopy.resultTitle), findsOneWidget);
      expect(
        find.text('${AssessmentCopy.resultMasteryLabel}：100%'),
        findsOneWidget,
      );
      expect(find.text('10 / 10'), findsOneWidget);
      expect(
        find.text(AssessmentCopy.difficultyRange(2, _FakeAssessmentApi.endLevel)),
        findsOneWidget,
      );
      expect(find.text(AssessmentCopy.tipHigh), findsOneWidget);
      expect(find.text(AssessmentCopy.nextLessonButton), findsOneWidget);
    });

    testWidgets('a run with no answers says why the result is empty', (
      tester,
    ) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      await tester.tap(finishButton);
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const ValueKey<String>('assessment-confirm-finish')),
      );
      await tester.pumpAndSettle();

      expect(find.text(AssessmentCopy.resultNoAnswers), findsOneWidget);
      expect(find.text(AssessmentCopy.tipNone), findsOneWidget);
    });

    testWidgets('an expired run explains the ten minute limit', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api, seeded: _expiredSession()));
      await tester.pumpAndSettle();

      expect(find.text(AssessmentCopy.resultEndedByTime), findsOneWidget);
    });

    testWidgets('a run the guardian closed is described as their choice', (
      tester,
    ) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api, seeded: _closedSession()));
      await tester.pumpAndSettle();

      expect(find.text(AssessmentCopy.resultReason(_closedSession())), findsWidgets);
    });
  });

  group('历史记录', () {
    testWidgets('lists recent runs with their mastery', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api, seeded: _closedSession()));
      await tester.pumpAndSettle();

      expect(find.text(AssessmentCopy.historyTitle), findsOneWidget);
      expect(find.text('已完成 · 80%'), findsOneWidget);
      expect(find.textContaining('答对 8/10'), findsWidgets);
    });

    testWidgets('an empty history explains what to do next', (tester) async {
      final api = _FakeAssessmentApi(history: const <AssessmentSession>[]);
      await tester.pumpWidget(_page(api: api, seeded: _closedSession()));
      await tester.pumpAndSettle();

      expect(find.text(AssessmentCopy.historyEmptyTitle), findsOneWidget);
      expect(find.text(AssessmentCopy.historyEmpty), findsOneWidget);
    });

    testWidgets('a failed history read offers a retry', (tester) async {
      final api = _FakeAssessmentApi(failHistory: true);
      await tester.pumpWidget(_page(api: api, seeded: _closedSession()));
      await tester.pumpAndSettle();

      expect(find.text(AssessmentCopy.historyFailed), findsOneWidget);
      expect(find.text(AssessmentCopy.retryButton), findsOneWidget);
    });
  });

  group('五种错误状态', () {
    // Opening a run can answer with a refusal of its own, or with the codes the
    // transport produces before the feature ever sees the request: a body it
    // cannot read, or a sign-in that has lapsed.
    const cases = <String>[
      'assessment_already_active',
      'assessment_lesson_not_found',
      'assessment_child_not_found',
      'assessment_state_changed',
      'assessment_failed',
      'invalid_request',
      'unauthenticated',
    ];

    for (final code in cases) {
      final expected = _expectedStartMessage(code);
      testWidgets('$code is shown as its own Chinese sentence', (tester) async {
        final api = _FakeAssessmentApi(startFailureCode: code);
        await tester.pumpWidget(_page(api: api));
        await tester.pumpAndSettle();
        await _startRun(tester);

        expect(find.text(expected), findsOneWidget);
        // The entry card is still there, so the guardian can try again.
        expect(find.text(AssessmentCopy.startButton), findsOneWidget);
        expect(_findLeakedTerms(tester), isNull);
      });
    }

    testWidgets('a run that expires mid-answer asks for a new one', (
      tester,
    ) async {
      final api = _FakeAssessmentApi(
        compileFailure: true,
        compileFailureCode: 'assessment_expired',
      );
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      await tester.tap(find.text(AssessmentCopy.correctButton));
      await tester.pumpAndSettle();

      expect(find.text(AssessmentCopy.expired), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('a family that started too many runs is told to wait', (
      tester,
    ) async {
      final api = _FakeAssessmentApi(
        compileFailure: true,
        compileFailureCode: 'assessment_rate_limited',
      );
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      await tester.tap(find.text(AssessmentCopy.correctButton));
      await tester.pumpAndSettle();

      expect(find.text(AssessmentCopy.rateLimited), findsOneWidget);
    });

    testWidgets('a second start opens the run that is still unfinished', (
      tester,
    ) async {
      final api = _FakeAssessmentApi(
        startFailureCode: 'assessment_already_active',
        history: <AssessmentSession>[_activeSession()],
      );
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      // The refusal is not a dead end: the family's open run is what the screen
      // continues, so the guardian is never told to wait for a screen they
      // cannot reach.
      expect(find.text(AssessmentCopy.progressTitle), findsOneWidget);
      expect(find.text('apple'), findsOneWidget);
      expect(find.text(AssessmentCopy.alreadyActive), findsNothing);
    });

    testWidgets('a second start with no open run shows the refusal', (
      tester,
    ) async {
      final api = _FakeAssessmentApi(
        startFailureCode: 'assessment_already_active',
      );
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      expect(find.text(AssessmentCopy.alreadyActive), findsOneWidget);
      expect(find.text(AssessmentCopy.startButton), findsOneWidget);
    });

    testWidgets('a failed start stays calm and generic', (tester) async {
      final api = _FakeAssessmentApi(startFailure: mapApiError(_offline()));
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      expect(_findLeakedTerms(tester), isNull);
      expect(find.text('网络连接不稳定，请检查后重试'), findsOneWidget);
    });

    testWidgets('a loading state and an empty catalog each explain themselves', (
      tester,
    ) async {
      final api = _FakeAssessmentApi(lessons: const <EnglishLessonSummary>[]);
      await tester.pumpWidget(_page(api: api));
      await tester.pump();
      expect(find.text(AssessmentCopy.loading), findsOneWidget);

      await tester.pumpAndSettle();
      expect(find.text(AssessmentCopy.lessonEmptyTitle), findsOneWidget);
      expect(find.text(AssessmentCopy.lessonEmpty), findsOneWidget);
      expect(find.text(AssessmentCopy.startButton), findsNothing);
    });
  });

  group('离开页面', () {
    testWidgets('leaving ends the run on the platform', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api));
      await tester.pumpAndSettle();
      await _startRun(tester);

      await tester.tap(find.byTooltip(AssessmentCopy.backButton));
      await tester.pumpAndSettle();

      expect(api.finishedSessionIds, <String>['session-1']);
      expect(find.text('英语学习占位'), findsOneWidget);
    });

    testWidgets('leaving a closed run does not end it twice', (tester) async {
      final api = _FakeAssessmentApi();
      await tester.pumpWidget(_page(api: api, seeded: _closedSession()));
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip(AssessmentCopy.backButton));
      await tester.pumpAndSettle();

      expect(api.finishedSessionIds, isEmpty);
    });
  });

  group('Assessment model', () {
    test('an expired run is closed even while it still says active', () {
      final now = DateTime.now().toUtc();
      final session = AssessmentSession(
        sessionId: 'session-1',
        lessonId: 'lesson-1',
        state: AssessmentState.active,
        difficulty: 2,
        itemIndex: 1,
        correctCount: 1,
        answeredCount: 1,
        streak: 1,
        mastery: 100,
        maxItems: 10,
        progress: 10,
        startedAt: now.subtract(const Duration(minutes: 11)),
        expiresAt: now.subtract(const Duration(minutes: 1)),
        acceptsAnswers: false,
      );

      expect(session.isClosedAt(now), isTrue);
      expect(session.remainingItems, 9);
    });

    test('an unreadable state is never treated as active', () {
      final session = AssessmentSession.fromJson(<String, Object?>{
        'session_id': 'session-1',
        'state': 'something_new',
      });

      expect(session.state, AssessmentState.unknown);
      expect(session.isActive, isFalse);
    });

    test('a weak run gets advice that does not blame the child', () {
      final low = _closedSession(mastery: 30, correctCount: 3);
      expect(AssessmentCopy.tip(low), AssessmentCopy.tipLow);
      expect(AssessmentCopy.suggestsAnotherLesson(low), isFalse);
    });
  });
}

/// The run's own end button; it sits below the fold on a phone-sized screen.
final Finder finishButton = find.byKey(
  const ValueKey<String>('assessment-finish'),
);

/// Starts a run through the real confirmation dialog.
Future<void> _startRun(WidgetTester tester) async {
  await tester.tap(find.text(AssessmentCopy.startButton));
  await tester.pumpAndSettle();
  await tester.tap(
    find.byKey(const ValueKey<String>('assessment-confirm-start')),
  );
  await tester.pumpAndSettle();
}

/// Returns the first technical term found on screen, or null when clean.
String? _findLeakedTerms(WidgetTester tester) {
  const banned = <String>[
    'API',
    'token',
    'Token',
    'session',
    'Session',
    'status',
    'assessment_',
    '127.0.0.1',
    '[object Object]',
    'undefined',
    'null',
    'NaN',
    'Exception',
  ];
  // The item text is lesson content, so the fixture's own English words are
  // not part of what this check is looking for.
  const lessonContent = <String>{'apple', 'banana', 'I like apples.'};
  final texts = tester
      .widgetList<Text>(find.byType(Text))
      .map((text) => text.data ?? text.textSpan?.toPlainText() ?? '')
      .toList(growable: false);
  for (final text in texts) {
    if (lessonContent.contains(text)) {
      continue;
    }
    for (final word in banned) {
      if (text.contains(word)) {
        return word;
      }
    }
  }
  return null;
}

/// The translated refusal a coded failure becomes at the data boundary.
AssessmentRefusal _refusalFor(String code) => switch (code) {
  'assessment_already_active' => AssessmentRefusal.alreadyActive,
  'assessment_rate_limited' => AssessmentRefusal.rateLimited,
  'assessment_expired' => AssessmentRefusal.expired,
  'assessment_state_changed' => AssessmentRefusal.stateChanged,
  'assessment_lesson_not_found' => AssessmentRefusal.lessonNotFound,
  'assessment_child_not_found' => AssessmentRefusal.childNotFound,
  'assessment_not_found' => AssessmentRefusal.sessionNotFound,
  _ => throw StateError('not a coded refusal: $code'),
};

/// The failure the transport stores after mapping a coded start failure.
Object _refused(String code) {
  if (const <String>{
    'assessment_already_active',
    'assessment_rate_limited',
    'assessment_expired',
    'assessment_state_changed',
    'assessment_lesson_not_found',
    'assessment_child_not_found',
    'assessment_not_found',
  }.contains(code)) {
    return AssessmentRefused(_refusalFor(code));
  }
  return mapApiError(_refusal(code, status: _refusalStatus(code)));
}

/// The sentence an unmapped start failure ends up showing.
String _expectedStartMessage(String code) => switch (code) {
  'assessment_already_active' => AssessmentCopy.alreadyActive,
  'assessment_lesson_not_found' => AssessmentCopy.lessonNotFound,
  'assessment_child_not_found' => AssessmentCopy.childNotFound,
  'assessment_state_changed' => AssessmentCopy.stateChanged,
  _ => assessmentMessageFor(
    mapApiError(_refusal(code, status: _refusalStatus(code))),
  ),
};

/// A connection that never reaches the service, as the transport reports it.
DioException _offline() {
  return DioException(
    requestOptions: RequestOptions(path: '/api/v1/families/family-1'),
    type: DioExceptionType.connectionError,
    message: 'SocketException: connection refused (127.0.0.1:8081)',
  );
}

/// The status a refusal arrives with; only the code decides the sentence.
int _refusalStatus(String code) => switch (code) {
  'invalid_request' || 'unauthenticated' => 400,
  'assessment_failed' => 500,
  _ => 409,
};

/// A coded refusal exactly as the transport hands it to the error mapper.
DioException _refusal(String code, {int status = 409}) {
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

AssessmentSession _activeSession({
  int itemIndex = 0,
  int correctCount = 0,
  int answeredCount = 0,
  int difficulty = 2,
  bool isCompleted = false,
}) {
  final now = DateTime.now().toUtc();
  return AssessmentSession(
    sessionId: 'session-1',
    lessonId: 'lesson-1',
    state: isCompleted ? AssessmentState.completed : AssessmentState.active,
    difficulty: difficulty,
    itemIndex: itemIndex,
    correctCount: correctCount,
    answeredCount: answeredCount,
    streak: 0,
    mastery: answeredCount == 0
        ? 0
        : (correctCount * 100 / answeredCount).round(),
    maxItems: 10,
    progress: itemIndex * 10,
    startedAt: now,
    expiresAt: now.add(const Duration(minutes: 10)),
    finishedAt: isCompleted ? now : null,
    finishReason: isCompleted
        ? AssessmentFinishReason.itemLimit
        : AssessmentFinishReason.unknown,
    acceptsAnswers: !isCompleted,
  )..firstItem = itemIndex < _items.length
      ? _items[itemIndex]
      : AssessmentItem.empty;
}

AssessmentSession _closedSession({int mastery = 80, int correctCount = 8}) {
  final now = DateTime.now().toUtc();
  return AssessmentSession(
    sessionId: 'session-closed',
    lessonId: 'lesson-1',
    state: AssessmentState.completed,
    difficulty: 4,
    itemIndex: 10,
    correctCount: correctCount,
    answeredCount: 10,
    streak: 0,
    mastery: mastery,
    maxItems: 10,
    progress: 100,
    startedAt: now.subtract(const Duration(hours: 1)),
    expiresAt: now.subtract(const Duration(minutes: 50)),
    finishedAt: now.subtract(const Duration(hours: 1)),
    finishReason: AssessmentFinishReason.itemLimit,
  );
}

AssessmentSession _expiredSession() {
  final now = DateTime.now().toUtc();
  return AssessmentSession(
    sessionId: 'session-1',
    lessonId: 'lesson-1',
    state: AssessmentState.abandoned,
    difficulty: 3,
    itemIndex: 2,
    correctCount: 2,
    answeredCount: 2,
    streak: 0,
    mastery: 100,
    maxItems: 10,
    progress: 20,
    startedAt: now.subtract(const Duration(minutes: 11)),
    expiresAt: now.subtract(const Duration(minutes: 1)),
    finishedAt: now,
    finishReason: AssessmentFinishReason.expired,
  );
}

Widget _page({
  required _FakeAssessmentApi api,
  AssessmentSession? seeded,
}) {
  final router = GoRouter(
    initialLocation: '/me/english/assessment',
    routes: <RouteBase>[
      GoRoute(
        path: '/me/english',
        builder: (context, state) =>
            const Scaffold(body: Text('英语学习占位')),
      ),
      GoRoute(
        path: '/me/english/assessment',
        builder: (context, state) => const AssessmentPage(),
      ),
    ],
  );
  return ProviderScope(
    overrides: [
      // Every provider the page reaches is replaced: a future provider left
      // alone would never settle and no screen would ever appear.
      assessmentApiProvider.overrideWithValue(api),
      assessmentFamilyIdProvider.overrideWith((ref) async => 'family-1'),
      assessmentLessonsProvider.overrideWith((ref) async => api.lessons),
      assessmentChildrenProvider.overrideWith(
        (ref) async => const <ChildProfile>[],
      ),
      assessmentHistoryProvider.overrideWith((ref) => api.readHistory()),
      englishApiProvider.overrideWithValue(_FakeEnglishApi()),
      familyControllerProvider.overrideWith(_SeededFamilyController.new),
      childProfileControllerProvider.overrideWith(
        _SeededChildController.new,
      ),
      if (seeded != null)
        assessmentRunControllerProvider.overrideWith(
          () => _SeededRunController(seeded),
        ),
    ],
    child: MaterialApp.router(theme: AppTheme.light, routerConfig: router),
  );
}

/// The production controller with a starting run instead of an empty one.
class _SeededRunController extends AssessmentRunController {
  _SeededRunController(this.seeded);

  final AssessmentSession seeded;

  @override
  AssessmentRunState get initialState => AssessmentRunState(
    session: seeded,
    items: _lessonItems,
    startDifficulty: 2,
    remainingSeconds: seeded.state == AssessmentState.active ? 600 : 0,
  );
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
  Future<List<ChildProfile>> build() async => const <ChildProfile>[];
}

class _FakeEnglishApi implements EnglishApi {
  @override
  Future<List<EnglishLessonSummary>> fetchLessons({String? ageTier}) async {
    return const <EnglishLessonSummary>[
      EnglishLessonSummary(
        lessonId: 'lesson-1',
        ageTier: 'age_5_6',
        title: '水果和颜色',
        summary: '练习常见的水果单词。',
        sequence: 1,
        wordCount: 2,
        sentenceCount: 1,
      ),
    ];
  }

  @override
  Future<EnglishLesson> fetchLesson(String lessonId) async {
    return EnglishLesson(
      lessonId: lessonId,
      ageTier: 'age_5_6',
      title: '水果和颜色',
      summary: '',
      sequence: 1,
      words: const <EnglishWord>[
        EnglishWord(
          id: 'word-1',
          position: 1,
          term: 'apple',
          phonetic: '',
          translation: '苹果',
          example: '',
        ),
        EnglishWord(
          id: 'word-2',
          position: 2,
          term: 'banana',
          phonetic: '',
          translation: '香蕉',
          example: '',
        ),
      ],
      sentences: const <EnglishSentence>[
        EnglishSentence(
          id: 'sentence-1',
          position: 1,
          pattern: 'I like …',
          prompt: '说出喜欢的水果',
          modelAnswer: 'I like apples.',
        ),
      ],
    );
  }

  @override
  Future<EnglishProgress> fetchProgress({String? childId, String? lessonId}) {
    return Future.value(const EnglishProgress.empty());
  }

  @override
  Future<List<EnglishPracticeAttempt>> fetchAttempts({
    String? childId,
    String? lessonId,
  }) async {
    return const <EnglishPracticeAttempt>[];
  }
}

class _FakeAssessmentApi implements AssessmentApi {
  _FakeAssessmentApi({
    Object? startFailure,
    String? startFailureCode,
    this.compileFailure = false,
    this.compileFailureCode = 'assessment_state_changed',
    this.failHistory = false,
    List<AssessmentSession>? history,
    List<EnglishLessonSummary>? lessons,
  }) : historyRuns = history ?? <AssessmentSession>[_closedSession()],
       lessons = lessons ?? _catalogLessons,
       startFailure = startFailureCode == null
           ? startFailure
           : _refused(startFailureCode);

  /// The level a run reaches once an answer is correct, matching the curve the
  /// platform applies (two correct answers raise the level).
  static const endLevel = 4;

  /// What opening a run answers with. A coded refusal is translated here
  /// because the transport stores whatever the mapper handed it, not a Dio
  /// failure the fixture can construct.
  final Object? startFailure;
  final bool compileFailure;
  final String compileFailureCode;
  final bool failHistory;

  /// The runs `history` answers with; it is kept apart from the method so the
  /// fixture can hand an empty list without naming it again.
  final List<AssessmentSession> historyRuns;
  final List<EnglishLessonSummary> lessons;

  final List<String> startedLessonIds = <String>[];
  final List<int> scores = <int>[];
  final List<String> answeredItemIds = <String>[];
  final List<String> finishedSessionIds = <String>[];

  int _answered = 0;
  int _correct = 0;
  int _level = 2;

  @override
  Future<AssessmentSession> start({
    required String lessonId,
    String childId = '',
  }) async {
    if (startFailure != null) {
      throw startFailure!;
    }
    startedLessonIds.add(lessonId);
    _answered = 0;
    _correct = 0;
    _level = 2;
    return _activeSession(difficulty: _level);
  }

  @override
  Future<AssessmentSession> fetch(String sessionId) async {
    return _activeSession(
      itemIndex: _answered,
      correctCount: _correct,
      answeredCount: _answered,
      difficulty: _level,
    );
  }

  @override
  Future<AssessmentAnswer> answer({
    required String sessionId,
    required AssessmentItem item,
    required int score,
  }) async {
    if (compileFailure) {
      // The guard translates the code in production; after that the failure is
      // already a guardian-facing refusal.
      throw AssessmentRefused(_refusalFor(compileFailureCode));
    }
    scores.add(score);
    answeredItemIds.add(item.itemId);
    _answered++;
    if (score >= 60) {
      _correct++;
      _level = endLevel;
    }
    final isLast = _answered >= 10;
    return AssessmentAnswer(
      session: _activeSession(
        itemIndex: _answered,
        correctCount: _correct,
        answeredCount: _answered,
        difficulty: _level,
        isCompleted: isLast,
      ),
      answeredCount: _answered,
      correctCount: _correct,
      difficulty: _level,
      nextItem: isLast
          ? AssessmentItem.empty
          : _items[_answered % _items.length],
    );
  }

  @override
  Future<AssessmentSession> finish(String sessionId) async {
    finishedSessionIds.add(sessionId);
    final now = DateTime.now().toUtc();
    return AssessmentSession(
      sessionId: sessionId,
      lessonId: 'lesson-1',
      state: AssessmentState.completed,
      difficulty: _level,
      itemIndex: _answered,
      correctCount: _correct,
      answeredCount: _answered,
      streak: 0,
      mastery: _answered == 0 ? 0 : (_correct * 100 / _answered).round(),
      maxItems: 10,
      progress: _answered * 10,
      startedAt: now,
      expiresAt: now.add(const Duration(minutes: 10)),
      finishedAt: now,
      finishReason: AssessmentFinishReason.ended,
    );
  }

  @override
  Future<List<AssessmentSession>> history({int limit = 20}) =>
      readHistory();

  /// The same answer the provider receives, so a failing or empty history can
  /// be arranged once.
  Future<List<AssessmentSession>> readHistory() async {
    if (failHistory) {
      // History is not a coded refusal of its own: the transport reports it
      // the same way it reports an unreachable service.
      throw mapApiError(_offline());
    }
    return historyRuns;
  }
}

const _catalogLessons = <EnglishLessonSummary>[
  EnglishLessonSummary(
    lessonId: 'lesson-1',
    ageTier: 'age_5_6',
    title: '水果和颜色',
    summary: '练习常见的水果单词。',
    sequence: 1,
    wordCount: 2,
    sentenceCount: 1,
  ),
];

final _lessonItems = _items
    .map(
      (item) => AssessmentLessonItem(
        itemId: item.itemId,
        itemType: item.itemType,
        text: item.text,
      ),
    )
    .toList(growable: false);

const _items = <AssessmentItem>[
  AssessmentItem(
    itemId: 'word-1',
    itemType: 'word',
    text: 'apple',
    difficulty: 2,
  ),
  AssessmentItem(
    itemId: 'word-2',
    itemType: 'word',
    text: 'banana',
    difficulty: 3,
  ),
  AssessmentItem(
    itemId: 'sentence-1',
    itemType: 'sentence',
    text: 'I like apples.',
    difficulty: 4,
  ),
];
