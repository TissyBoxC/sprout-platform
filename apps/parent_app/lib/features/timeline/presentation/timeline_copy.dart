import '../../../core/error/app_exception.dart';
import '../domain/timeline.dart';

/// Every sentence the 成长时间轴 screen shows.
///
/// Keeping the copy in one place makes the promise checkable: no sentence may
/// name a transport, a field, or a credential, and every sentence either
/// explains what the screen contains or tells the guardian what to do next.
abstract final class TimelineCopy {
  static const pageTitle = '成长时间轴';
  static const backButton = '返回';
  static const retryButton = '重新加载';

  // Structure.
  static const childTitle = '看哪个孩子';
  static const milestonesTitle = '孩子的成长累积';
  static const listTitle = '按时间记下来';
  static const loadMoreButton = '看更早的记录';
  static const loadingMore = '正在加载更早的记录…';
  static const reachedEnd = '已经是最早的记录。';
  static const childEmptyTitle = '还没有儿童档案';
  static const childEmptyBody = '在儿童档案里添加孩子后，完成测评、练习和内容是这里的起点。';
  static const childMissing = '没有找到这个孩子的资料，请重新选择孩子再试。';
  static const childRequired = '请选择一个孩子，才能看到专属的时间轴。';
  static const childSelectionHint = '先选一个孩子';
  static const childTimelineHint = '时间轴按孩子记录。上面选一个孩子后，这里会按时间列出他完成的事。';

  // Loading and empty states.
  static const loading = '正在加载…';
  static const emptyTitle = '时间轴还是空的';
  static const emptyBody = '孩子完成测评、练习或听完内容后，这里会按时间记下来。';
  static const milestonesEmpty = '完成第一次测评或练习后，这里会开始累积。';
  static const milestonesFailed = '上面的数字暂时没有读出来，事件列表不受影响。';

  // Failures.
  static const eventsFailed = '这段时间的记录没有读出来，请稍后再试。';
  static const loadMoreFailed = '更早的记录没有读出来，请稍后再试。';
  static const cursorExpired = '页面停在的位置已经用不上了，请回到时间轴开头重新看看。';
  static const invalidLimit = '一次能显示的内容有限，请重新加载。';
  static const unavailable = '成长记录暂时无法查看，请稍后再试。';
  static const genericFailure = '成长时间轴暂时无法打开，请稍后再试。';

  /// The label of one counter, in a parent's words.
  static String milestoneLabel(MilestoneMetric metric) {
    return switch (metric) {
      MilestoneMetric.completedAssessments => '完成测评',
      MilestoneMetric.practiceDays => '练习天数',
      MilestoneMetric.currentStreakDays => '当前连续',
      MilestoneMetric.longestStreakDays => '最长连续',
      MilestoneMetric.publishedReports => '成长报告',
      MilestoneMetric.markedMilestones => '里程碑',
      MilestoneMetric.completedContent => '听完内容',
    };
  }

  /// The unit a counter is counted in.
  static String milestoneUnit(MilestoneMetric metric) {
    return switch (metric) {
      MilestoneMetric.practiceDays ||
      MilestoneMetric.currentStreakDays ||
      MilestoneMetric.longestStreakDays => '天',
      MilestoneMetric.completedAssessments => '次',
      MilestoneMetric.publishedReports ||
      MilestoneMetric.markedMilestones ||
      MilestoneMetric.completedContent => '个',
    };
  }

  /// The kind of one event, used as its small caption.
  static String kindName(TimelineEventKind kind) {
    return switch (kind) {
      TimelineEventKind.assessmentCompleted => '学习测评',
      TimelineEventKind.englishPractice => '英语练习',
      TimelineEventKind.reportPublished => '成长报告',
      TimelineEventKind.milestone => '里程碑',
      TimelineEventKind.contentCompleted => '听完内容',
      // An event the app does not have a name for still happened; calling it a
      // 成长记录 is true and does not expose the raw kind.
      TimelineEventKind.other => '成长记录',
    };
  }

  /// Renders one moment as a relative time, falling back to a date.
  ///
  /// [now] is passed in so the sentence is stable while a page is open and
  /// predictable in a test. A future stamp is described as 刚刚 rather than as
  /// a negative duration, because a device clock a minute ahead is not news.
  static String relativeTime(DateTime occurredAt, DateTime now) {
    final elapsed = now.toUtc().difference(occurredAt.toUtc());
    if (elapsed.isNegative || elapsed.inMinutes < 1) {
      return '刚刚';
    }
    if (elapsed.inMinutes < 60) {
      return '${elapsed.inMinutes} 分钟前';
    }
    if (elapsed.inHours < 24) {
      return '${elapsed.inHours} 小时前';
    }
    // Beyond a week a relative phrase stops helping, so the calendar date is
    // shown instead of "23 天前".
    const longestRelativeDays = 7;
    if (elapsed.inDays <= longestRelativeDays) {
      return '${elapsed.inDays} 天前';
    }
    return date(occurredAt);
  }

  /// `2026 年 10 月 22 日`, in the guardian's own time zone.
  static String date(DateTime value) {
    final local = value.toLocal();
    return '${local.year} 年 ${local.month} 月 ${local.day} 日';
  }

  /// `3 天`, the value half of one counter.
  static String milestoneValue(MilestoneMetric metric, int value) {
    return '$value ${milestoneUnit(metric)}';
  }
}

/// The counters the timeline shows above the list.
enum MilestoneMetric {
  completedAssessments,
  practiceDays,
  currentStreakDays,
  longestStreakDays,
  publishedReports,
  markedMilestones,
  completedContent,
}

/// The value of one counter in the order the screen shows them.
int milestoneValueOf(TimelineMilestones milestones, MilestoneMetric metric) {
  return switch (metric) {
    MilestoneMetric.completedAssessments => milestones.completedAssessments,
    MilestoneMetric.practiceDays => milestones.practiceDays,
    MilestoneMetric.currentStreakDays => milestones.currentStreakDays,
    MilestoneMetric.longestStreakDays => milestones.longestStreakDays,
    MilestoneMetric.publishedReports => milestones.publishedReports,
    MilestoneMetric.markedMilestones => milestones.markedMilestones,
    MilestoneMetric.completedContent => milestones.completedContent,
  };
}

/// A refusal the platform reported with a code of its own.
class TimelineRefused implements Exception {
  const TimelineRefused(this.refusal);

  final TimelineRefusal refusal;

  String get message => switch (refusal) {
    TimelineRefusal.childRequired => TimelineCopy.childRequired,
    TimelineRefusal.childNotFound => TimelineCopy.childMissing,
    TimelineRefusal.cursorExpired => TimelineCopy.cursorExpired,
    TimelineRefusal.invalidLimit => TimelineCopy.invalidLimit,
    TimelineRefusal.unavailable => TimelineCopy.unavailable,
  };

  @override
  String toString() => 'TimelineRefused($refusal)';
}

/// Turns any failure into the guardian's sentence.
///
/// A refusal that arrived at this build already carries its own sentence;
/// anything else is a transport failure the mapper has already translated.
String timelineMessageFor(Object? error) {
  if (error is TimelineRefused) {
    return error.message;
  }
  if (error is AppException) {
    return error.message;
  }
  return TimelineCopy.genericFailure;
}
