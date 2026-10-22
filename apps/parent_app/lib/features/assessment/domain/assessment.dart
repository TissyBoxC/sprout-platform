/// Lifecycle position of one learning assessment.
///
/// Two closed shapes exist on purpose: a run the guardian ended with the
/// 结束测评 button is [completed], while a run that ran out of time or was
/// left behind is [abandoned]. The result screen tells them apart so a parent
/// is never told "all done" about a run the child never finished.
enum AssessmentState { active, completed, abandoned, unknown }

/// Why a closed assessment stopped.
enum AssessmentFinishReason {
  /// The run used up its ten items.
  itemLimit,

  /// The run used up its ten minutes.
  expired,

  /// The guardian ended it, or the family left the page.
  ended,

  /// The platform closed a run nobody finished.
  abandoned,
  unknown,
}

/// One assessment run as the platform reports it.
///
/// It is mutable only in [firstItem], which the start reply carries beside the
/// run; every other value is fixed for the lifetime of this snapshot.
class AssessmentSession {
  AssessmentSession({
    required this.sessionId,
    required this.lessonId,
    required this.state,
    required this.difficulty,
    required this.itemIndex,
    required this.correctCount,
    required this.answeredCount,
    required this.streak,
    required this.mastery,
    required this.maxItems,
    required this.progress,
    required this.startedAt,
    required this.expiresAt,
    this.childId = '',
    this.finishedAt,
    this.finishReason = AssessmentFinishReason.unknown,
    this.acceptsAnswers = false,
  });

  final String sessionId;

  /// Empty for a run the family opened without naming a child.
  final String childId;
  final String lessonId;
  final AssessmentState state;

  /// Level 1 is the easiest lesson content and 5 is the hardest.
  final int difficulty;
  final int itemIndex;
  final int correctCount;
  final int answeredCount;
  final int streak;

  /// Correct answers as a 0 to 100 percentage, rounded by the platform.
  final int mastery;
  final int maxItems;
  final int progress;
  final DateTime startedAt;

  /// Hard stop for this run. The platform and the device both enforce it.
  final DateTime expiresAt;
  final DateTime? finishedAt;
  final AssessmentFinishReason finishReason;

  /// Whether the platform still accepts an answer for this run right now.
  final bool acceptsAnswers;

  /// The first item a start reply carries, or [AssessmentItem.empty] when it
  /// carries none. It belongs to the reply rather than to the stored run, so it
  /// is set once by the data layer instead of being decoded with the rest.
  AssessmentItem firstItem = AssessmentItem.empty;

  bool get isActive => state == AssessmentState.active;

  /// True when no further answer may be recorded: the run is closed, or it is
  /// still open but out of time on this device's clock.
  bool isClosedAt(DateTime now) =>
      !isActive || !now.isBefore(expiresAt);

  int get remainingItems {
    final remaining = maxItems - itemIndex;
    return remaining.isNegative ? 0 : remaining;
  }

  /// True when the run took every item it was allowed.
  bool get reachedItemLimit => itemIndex >= maxItems;

  factory AssessmentSession.fromJson(Map<String, Object?> json) {
    return AssessmentSession(
      sessionId: _string(json['session_id']),
      childId: _string(json['child_id']),
      lessonId: _string(json['lesson_id']),
      state: _stateFrom(_string(json['state'])),
      difficulty: _int(json['difficulty']),
      itemIndex: _int(json['item_index']),
      correctCount: _int(json['correct_count']),
      answeredCount: _int(json['answered_count']),
      streak: _int(json['streak']),
      mastery: _int(json['mastery']),
      maxItems: _int(json['max_items']),
      progress: _int(json['progress']),
      startedAt: _instant(json['started_at']) ?? DateTime.now().toUtc(),
      expiresAt: _instant(json['expires_at']) ?? DateTime.now().toUtc(),
      finishedAt: _instant(json['finished_at']),
      finishReason: _finishReasonFrom(_string(json['finish_reason'])),
      acceptsAnswers: json['accepts_answers'] == true,
    );
  }

  static String _string(Object? value) => value is String ? value : '';

  static int _int(Object? value) => value is int ? value : 0;

  static DateTime? _instant(Object? value) =>
      value is String ? DateTime.tryParse(value)?.toUtc() : null;

  static AssessmentState _stateFrom(String value) {
    return switch (value) {
      'active' => AssessmentState.active,
      'completed' => AssessmentState.completed,
      'abandoned' => AssessmentState.abandoned,
      // An unreadable state is treated as unknown and never as active, so the
      // screen cannot offer another question for a run nobody confirmed.
      _ => AssessmentState.unknown,
    };
  }

  static AssessmentFinishReason _finishReasonFrom(String value) {
    return switch (value) {
      'item_limit' => AssessmentFinishReason.itemLimit,
      'expired' => AssessmentFinishReason.expired,
      'completed' => AssessmentFinishReason.ended,
      'abandoned' => AssessmentFinishReason.abandoned,
      _ => AssessmentFinishReason.unknown,
    };
  }
}

/// One course item the child is asked to say.
class AssessmentItem {
  const AssessmentItem({
    required this.itemId,
    required this.itemType,
    required this.text,
    required this.difficulty,
  });

  final String itemId;

  /// `word` or `sentence`, as the platform stores the item.
  final String itemType;
  final String text;
  final int difficulty;

  bool get isWord => itemType == 'word';

  bool get isEmpty => itemId.isEmpty || text.isEmpty;

  /// Decodes a `next_item` object; an absent item becomes [empty].
  factory AssessmentItem.fromJson(Map<String, Object?> json) {
    return AssessmentItem(
      itemId: _string(json['item_id']),
      itemType: _string(json['item_type']),
      text: _string(json['text']),
      difficulty: _int(json['difficulty']),
    );
  }

  static const empty = AssessmentItem(
    itemId: '',
    itemType: '',
    text: '',
    difficulty: 0,
  );

  static String _string(Object? value) => value is String ? value : '';

  static int _int(Object? value) => value is int ? value : 0;
}

/// A guardian's answer for one item.
class AssessmentAnswer {
  const AssessmentAnswer({
    required this.session,
    required this.answeredCount,
    required this.correctCount,
    required this.difficulty,
    required this.nextItem,
  });

  /// The run after this answer was stored.
  final AssessmentSession session;
  final int answeredCount;
  final int correctCount;
  final int difficulty;

  /// The item to show next, or [AssessmentItem.empty] when the run is over.
  final AssessmentItem nextItem;
}

/// Known refusals, each with its own guardian-facing sentence.
enum AssessmentRefusal {
  alreadyActive,
  rateLimited,
  expired,
  stateChanged,
  lessonNotFound,
  childNotFound,
  sessionNotFound,
}
