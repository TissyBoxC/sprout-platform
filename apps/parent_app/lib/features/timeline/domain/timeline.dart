/// One entry in a child's growth timeline.
///
/// It is a finished piece of work the platform already recorded elsewhere: an
/// assessment, a practice round, a report, a milestone a guardian marked, or a
/// content item played to the end. The event carries a sentence, never raw
/// material, so nothing here can be replayed or quoted back at the child.
class TimelineEvent {
  const TimelineEvent({
    required this.eventId,
    required this.kind,
    required this.occurredAt,
    required this.title,
    required this.description,
    this.subject = '',
  });

  final String eventId;
  final TimelineEventKind kind;
  final DateTime occurredAt;
  final String title;
  final String description;

  /// A short noun the title refers to, empty when the kind needs none.
  final String subject;

  factory TimelineEvent.fromJson(Map<String, Object?> json) {
    return TimelineEvent(
      eventId: _string(json['event_id']),
      kind: timelineKindFromWire(_string(json['kind'])),
      occurredAt:
          DateTime.tryParse(_string(json['occurred_at']))?.toUtc() ??
          DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
      title: _string(json['title']),
      description: _string(json['description']),
      subject: _string(json['subject']),
    );
  }

  static String _string(Object? value) => value is String ? value : '';
}

/// The stable event types a client switches on.
enum TimelineEventKind {
  assessmentCompleted,
  englishPractice,
  reportPublished,
  milestone,
  contentCompleted,

  /// A kind this build does not know. It still renders, with the neutral
  /// marker, because an event the platform added is news worth reading.
  other,
}

/// One page of a child's timeline.
class TimelinePage {
  const TimelinePage({required this.events, required this.nextCursor});

  final List<TimelineEvent> events;

  /// Empty when this page reached the newest entry.
  final String nextCursor;

  bool get hasMore => nextCursor.isNotEmpty;

  factory TimelinePage.fromJson(Map<String, Object?> json) {
    final rawEvents = json['events'];
    final events = <TimelineEvent>[];
    if (rawEvents is List) {
      for (final entry in rawEvents) {
        if (entry is! Map) {
          continue;
        }
        final event = TimelineEvent.fromJson(Map<String, Object?>.from(entry));
        // An entry without an identity cannot be de-duplicated across pages,
        // and one without a title has nothing to say, so both are dropped.
        if (event.eventId.isEmpty || event.title.isEmpty) {
          continue;
        }
        events.add(event);
      }
    }
    final cursor = json['next_cursor'];
    return TimelinePage(
      events: events,
      nextCursor: cursor is String ? cursor.trim() : '',
    );
  }
}

/// The counters shown above the timeline.
///
/// Every number counts finished work, which is what makes a count worth showing
/// a child: a started-but-abandoned assessment is not an achievement.
class TimelineMilestones {
  const TimelineMilestones({
    this.completedAssessments = 0,
    this.practiceDays = 0,
    this.currentStreakDays = 0,
    this.longestStreakDays = 0,
    this.publishedReports = 0,
    this.markedMilestones = 0,
    this.completedContent = 0,
    this.total = 0,
  });

  final int completedAssessments;
  final int practiceDays;
  final int currentStreakDays;
  final int longestStreakDays;
  final int publishedReports;
  final int markedMilestones;
  final int completedContent;
  final int total;

  /// True when there is nothing to count yet.
  bool get isEmpty => total == 0;

  factory TimelineMilestones.fromJson(Map<String, Object?> json) {
    return TimelineMilestones(
      completedAssessments: _int(json['completed_assessments']),
      practiceDays: _int(json['practice_days']),
      currentStreakDays: _int(json['current_streak_days']),
      longestStreakDays: _int(json['longest_streak_days']),
      publishedReports: _int(json['published_reports']),
      markedMilestones: _int(json['marked_milestones']),
      completedContent: _int(json['completed_content']),
      total: _int(json['total']),
    );
  }

  static int _int(Object? value) => value is int ? value : 0;
}

/// Known refusals, each with its own guardian-facing sentence.
enum TimelineRefusal {
  /// The request did not name a child. The platform requires one.
  childRequired,
  childNotFound,
  cursorExpired,
  invalidLimit,
  unavailable,
}

/// Maps a wire kind to the enum. An unknown value keeps the event instead of
/// dropping it, because the sentence is still true even if the app cannot
/// draw a specific icon for it.
TimelineEventKind timelineKindFromWire(String value) {
  return switch (value) {
    'assessment_completed' => TimelineEventKind.assessmentCompleted,
    'english_practice' => TimelineEventKind.englishPractice,
    'report_published' => TimelineEventKind.reportPublished,
    'milestone' => TimelineEventKind.milestone,
    'content_completed' => TimelineEventKind.contentCompleted,
    _ => TimelineEventKind.other,
  };
}
