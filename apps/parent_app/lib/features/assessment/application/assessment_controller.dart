import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../../english/application/english_controller.dart';
import '../../english/domain/english.dart';
import '../../family/application/family_controller.dart';
import '../data/assessment_api.dart';
import '../domain/assessment.dart';
import '../presentation/assessment_copy.dart';

/// Resolves the signed-in family before a run can be addressed.
final assessmentFamilyIdProvider = FutureProvider<String>((ref) async {
  final family = await ref.watch(familyControllerProvider.future);
  return family.capability.familyId;
});

final assessmentApiProvider = Provider<AssessmentApi?>((ref) {
  final familyId = ref.watch(assessmentFamilyIdProvider).value;
  if (familyId == null || familyId.isEmpty) {
    return null;
  }
  return HttpAssessmentApi(ref.watch(apiClientProvider), familyId);
});

/// The published lessons a run can be opened on.
///
/// It reuses the English catalog, so a guardian sees the same course names as
/// in the English page rather than a second, drifting list.
final assessmentLessonsProvider = FutureProvider<List<EnglishLessonSummary>>(
  (ref) async {
    return ref.watch(englishApiProvider).fetchLessons();
  },
);

/// Children the run can be attributed to, for an optional selector.
final assessmentChildrenProvider = FutureProvider<List<ChildProfile>>(
  (ref) async {
    return ref.watch(childProfileControllerProvider.future);
  },
);

/// Decodes the lesson content an item is drawn from, mirroring the platform's
/// ordering: words first, then sentence patterns, each in lesson order.
List<AssessmentLessonItem> lessonItemsOf(EnglishLesson lesson) {
  final items = <AssessmentLessonItem>[
    for (final word in lesson.words)
      if (word.term.isNotEmpty)
        AssessmentLessonItem(
          itemId: word.id,
          itemType: 'word',
          text: word.term,
        ),
    for (final sentence in lesson.sentences)
      if (sentence.modelAnswer.isNotEmpty)
        AssessmentLessonItem(
          itemId: sentence.id,
          itemType: 'sentence',
          text: sentence.modelAnswer,
        ),
  ];
  return items;
}

/// The lesson names and children offered before a run starts, as the screens
/// read them.
final assessmentOfferedLessonsProvider = assessmentLessonsProvider;

final assessmentOfferedChildrenProvider = assessmentChildrenProvider;

/// The run controller the screens read.
///
/// It is an alias so the shell can replace what the screens show without every
/// widget changing; a release build has nothing behind it but the real
/// controller.
final assessmentScreenControllerProvider = assessmentRunControllerProvider;

/// One run as the parent screen shows it.
class AssessmentRunState {
  const AssessmentRunState({
    this.session,
    this.items = const <AssessmentLessonItem>[],
    this.item,
    this.itemIndex = 0,
    this.startDifficulty = 0,
    this.remainingSeconds = 0,
    this.isLoading = false,
    this.isSubmitting = false,
    this.isFinishing = false,
    this.error,
    this.notice,
  });

  /// The run as the platform last reported it.
  final AssessmentSession? session;

  /// The items of the lesson this run is practising.
  final List<AssessmentLessonItem> items;

  /// The item to show now, or null while it is unknown.
  final AssessmentItem? item;

  /// How many items the run had already served when this screen picked it up.
  final int itemIndex;

  /// The level the run started at, kept so the result can describe the change.
  final int startDifficulty;
  final int remainingSeconds;
  final bool isLoading;
  final bool isSubmitting;
  final bool isFinishing;

  /// A refusal or a transport failure, already safe to show.
  final Object? error;

  /// A short confirmation the guardian can read without leaving the screen.
  final String? notice;

  bool get hasSession => session != null;

  /// True when this screen must not offer another question.
  bool get isClosed => session?.state != AssessmentState.active;

  AssessmentRunState copyWith({
    AssessmentSession? session,
    List<AssessmentLessonItem>? items,
    AssessmentItem? item,
    bool clearItem = false,
    int? itemIndex,
    int? startDifficulty,
    int? remainingSeconds,
    bool? isLoading,
    bool? isSubmitting,
    bool? isFinishing,
    Object? error,
    bool clearError = false,
    String? notice,
    bool clearNotice = false,
  }) {
    return AssessmentRunState(
      session: session ?? this.session,
      items: items ?? this.items,
      item: clearItem ? null : (item ?? this.item),
      itemIndex: itemIndex ?? this.itemIndex,
      startDifficulty: startDifficulty ?? this.startDifficulty,
      remainingSeconds: remainingSeconds ?? this.remainingSeconds,
      isLoading: isLoading ?? this.isLoading,
      isSubmitting: isSubmitting ?? this.isSubmitting,
      isFinishing: isFinishing ?? this.isFinishing,
      error: clearError ? null : (error ?? this.error),
      notice: clearNotice ? null : (notice ?? this.notice),
    );
  }
}

/// Drives one learning assessment from the guardian's side.
///
/// The parent app belongs to the guardian, so every run this controller opens
/// is closed by it as well: leaving the page or ending the run always calls the
/// platform's finish, so a family is never left with a run that stays open and
/// blocks the next one.
class AssessmentRunController extends Notifier<AssessmentRunState> {
  Timer? _ticker;

  /// True while this controller is attached, so a timer that fires after the
  /// screen closed cannot touch a disposed notifier.
  bool _isMounted = false;

  /// True once a run on this screen has closed, so a refusal that arrives while
  /// the result is on screen belongs to the run that just ended rather than to
  /// the question that is no longer shown.
  bool _isRunClosed = false;

  @override
  AssessmentRunState build() {
    _isMounted = true;
    // The session is remembered rather than read from `state` on disposal: a
    // disposed notifier may no longer be read, and the cleanup still has to
    // know which run to close.
    ref.onDispose(() {
      _isMounted = false;
      _ticker?.cancel();
      // A page that is removed without its leave handler still cannot leave a
      // run open behind it. The API is captured here because a disposed
      // notifier may no longer read providers.
      final api = _apiOrNull;
      final sessionId = _openSessionId;
      if (api != null && sessionId != null && sessionId.isNotEmpty) {
        unawaited(_finishQuietly(api, sessionId));
      }
    });
    _rememberOpenRun(initialState.session);
    return initialState;
  }

  /// The run this controller most recently showed while it was open.
  String? _openSessionId;

  /// The API this controller used, kept so disposal can still close a run.
  AssessmentApi? _apiOrNull;

  /// Records and forgets the run whose cleanup is owed on disposal.
  void _rememberOpenRun(AssessmentSession? session) {
    _openSessionId = session != null && session.isActive
        ? session.sessionId
        : null;
  }

  /// The state this controller starts with. A screen that opens on an existing
  /// run overrides it instead of [build].
  @visibleForTesting
  AssessmentRunState get initialState => const AssessmentRunState();

  /// Opens a run and arranges for its first item to be shown ready to read.
  ///
  /// The platform reports the next item's text without its type, and an answer
  /// cannot be sent without one, so the type is filled in from the lesson the
  /// run is practising.
  Future<_OpenedRun> _open(
    AssessmentApi api, {
    required String lessonId,
    required String childId,
  }) async {
    final session = await api.start(lessonId: lessonId, childId: childId);
    final items = await _itemsFor(session.lessonId);
    return _OpenedRun(
      session: session,
      items: items,
      item: _typedItem(session, items, session.firstItem),
    );
  }

  /// Opens a run on [lessonId] and shows its first item ready to read.
  ///
  /// Throws the translated failure so the caller can show the reason while the
  /// entry card stays on screen; the same failure is stored in [state].
  @nonVirtual
  Future<void> start({required String lessonId, String childId = ''}) async {
    final api = _api();
    state = state.copyWith(isLoading: true, clearError: true);
    try {
      final opened = await _open(api, lessonId: lessonId, childId: childId);
      _show(opened.session, items: opened.items, item: opened.item);
    } catch (error) {
      state = state.copyWith(isLoading: false, error: error);
      rethrow;
    }
  }

  /// Picks an open run back up instead of opening a second one.
  ///
  /// A family may only have one run open, so this is the only way out of the
  /// refusal the platform gives when a guardian tries to start another.
  Future<void> resume(AssessmentSession session) async {
    final api = _api();
    state = state.copyWith(isLoading: true, clearError: true);
    try {
      final items = await _itemsFor(session.lessonId);
      if (session.isClosedAt(DateTime.now().toUtc())) {
        _show(session, items: items, item: null);
        return;
      }
      final detail = await api.fetch(session.sessionId);
      // A run picked back up continues at the item the platform says comes
      // next, so the guardian never re-reads a question already answered before
      // the page was left.
      _show(
        detail,
        items: items,
        item: _typedItem(detail, items, detail.firstItem),
      );
    } catch (error) {
      state = state.copyWith(isLoading: false, error: error);
    }
  }

  /// Records one item's score and moves to the next item.
  ///
  /// A failure keeps the current item on screen so the guardian can score it
  /// again instead of losing the answer.
  @nonVirtual
  Future<void> submitScore(int score) async {
    final current = state.session;
    final item = state.item;
    if (current == null || item == null || item.isEmpty) {
      return;
    }
    if (state.isSubmitting || current.isClosedAt(DateTime.now().toUtc())) {
      return;
    }
    final api = _api();
    state = state.copyWith(
      isSubmitting: true,
      clearError: true,
      clearNotice: true,
    );
    try {
      final answer = await api.answer(
        sessionId: current.sessionId,
        item: item,
        score: score,
      );
      _show(
        answer.session,
        items: state.items,
        item: answer.nextItem,
        notice: answer.nextItem.isEmpty ? null : AssessmentCopy.answerRecorded,
      );
    } catch (error) {
      // Leaving the page closes the run; the answer in flight then fails with a
      // refusal about a run the guardian already ended, which is not news.
      if (!_isRunClosed && _isMounted) {
        state = state.copyWith(isSubmitting: false, error: error);
      }
    }
  }

  /// Ends the run on the platform and keeps the result on screen.
  ///
  /// The platform call is always attempted, even past the local deadline,
  /// because the stored run is what actually frees the family for a new one.
  @nonVirtual
  Future<void> finish() async {
    final current = state.session;
    if (current == null) {
      return;
    }
    if (current.state != AssessmentState.active) {
      state = state.copyWith(clearItem: true);
      _stopTicker();
      return;
    }
    final api = _api();
    state = state.copyWith(isFinishing: true, clearError: true);
    try {
      final finished = await api.finish(current.sessionId);
      _show(finished, items: state.items, item: null);
      // The result lists the run that just closed, so the history is read again
      // rather than left showing the count from before this run.
      ref.invalidate(assessmentHistoryProvider);
    } catch (error) {
      // A run that reached the platform's own deadline is already over, so its
      // result is shown instead of a failure to end it.
      try {
        final latest = await api.fetch(current.sessionId);
        if (latest.state != AssessmentState.active) {
          _show(latest, items: state.items, item: null);
          return;
        }
      } on Object {
        // A failed read leaves the original failure as the message.
      }
      if (_isMounted) {
        state = state.copyWith(isFinishing: false, error: error);
      }
    }
  }

  /// Loads the latest state of an open run, for a guardian who wants to retry.
  Future<void> refresh() async {
    final current = state.session;
    if (current == null || current.state != AssessmentState.active) {
      return;
    }
    final api = _api();
    try {
      final detail = await api.fetch(current.sessionId);
      _show(detail, items: state.items, item: state.item);
    } catch (error) {
      state = state.copyWith(error: error);
    }
  }

  /// Clears the screen back to its entry card.
  @nonVirtual
  void reset() {
    _stopTicker();
    _isRunClosed = false;
    state = const AssessmentRunState();
  }

  AssessmentApi _api() {
    final api = ref.read(assessmentApiProvider);
    if (api == null) {
      throw StateError('assessment api is not ready');
    }
    _apiOrNull = api;
    return api;
  }

  Future<List<AssessmentLessonItem>> _itemsFor(String lessonId) async {
    final lesson = await ref.read(englishApiProvider).fetchLesson(lessonId);
    return lessonItemsOf(lesson);
  }

  /// Fills in the item type the platform did not report.
  AssessmentItem _typedItem(
    AssessmentSession session,
    List<AssessmentLessonItem> items,
    AssessmentItem reported,
  ) {
    if (session.state != AssessmentState.active ||
        session.isClosedAt(DateTime.now().toUtc()) ||
        reported.isEmpty) {
      return AssessmentItem.empty;
    }
    for (final item in items) {
      if (item.itemId == reported.itemId) {
        return AssessmentItem(
          itemId: reported.itemId,
          itemType: reported.itemType.isNotEmpty
              ? reported.itemType
              : item.itemType,
          text: reported.text,
          difficulty: reported.difficulty != 0
              ? reported.difficulty
              : session.difficulty,
        );
      }
    }
    return AssessmentItem.empty;
  }

  void _show(
    AssessmentSession session, {
    required List<AssessmentLessonItem> items,
    required AssessmentItem? item,
    String? notice,
  }) {
    final previous = state.session;
    final startDifficulty = state.hasSession && previous != null
        ? state.startDifficulty
        : session.difficulty;
    state = AssessmentRunState(
      session: session,
      items: items,
      item: item,
      itemIndex: session.itemIndex,
      startDifficulty: startDifficulty,
      remainingSeconds: _remainingSeconds(session),
      notice: notice,
    );
    _rememberOpenRun(session);
    _isRunClosed = session.state != AssessmentState.active;
    if (session.state != AssessmentState.active ||
        session.isClosedAt(DateTime.now().toUtc())) {
      _stopTicker();
      return;
    }
    _startTicker();
  }

  int _remainingSeconds(AssessmentSession session) {
    final remaining = session.expiresAt.difference(DateTime.now().toUtc());
    return remaining.isNegative ? 0 : remaining.inSeconds;
  }

  void _startTicker() {
    _ticker?.cancel();
    _ticker = Timer.periodic(const Duration(seconds: 1), (timer) {
      final session = state.session;
      if (!_isMounted || session == null) {
        timer.cancel();
        return;
      }
      final remaining = _remainingSeconds(session);
      state = state.copyWith(remainingSeconds: remaining);
      if (remaining <= 0) {
        _stopTicker();
        // The platform closes the run on its own deadline, so this only has to
        // stop offering questions; the result is read back on the next open.
        state = state.copyWith(clearItem: true);
      }
    });
  }

  void _stopTicker() {
    _ticker?.cancel();
    _ticker = null;
  }

  Future<void> _finishQuietly(AssessmentApi api, String sessionId) async {
    try {
      await api.finish(sessionId);
    } on Object {
      // The platform abandons a run at its own deadline, so a failed cleanup
      // cannot leave the family blocked forever and needs no message here.
    }
  }
}

final assessmentRunControllerProvider =
    NotifierProvider<AssessmentRunController, AssessmentRunState>(
      AssessmentRunController.new,
    );

/// One freshly opened run together with the lesson content it serves.
class _OpenedRun {
  const _OpenedRun({
    required this.session,
    required this.items,
    required this.item,
  });

  final AssessmentSession session;
  final List<AssessmentLessonItem> items;
  final AssessmentItem item;
}

/// Recent runs for the family, newest first.
final assessmentHistoryProvider = FutureProvider<List<AssessmentSession>>((
  ref,
) async {
  await ref.watch(assessmentFamilyIdProvider.future);
  final api = ref.watch(assessmentApiProvider);
  if (api == null) {
    return const <AssessmentSession>[];
  }
  return api.history();
});
