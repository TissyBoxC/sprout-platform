import '../../../core/error/app_exception.dart';
import '../domain/assessment.dart';

/// Every sentence the assessment screens show.
///
/// Keeping the copy in one place makes the promises checkable: no sentence may
/// name a transport, a credential, or a field of the stored run, and every
/// sentence either explains what is happening or tells the guardian what to do.
abstract final class AssessmentCopy {
  static const pageTitle = '学习测评';
  static const entryTitle = '学习测评';
  static const entryBody = '用 10 道题看看孩子这节课掌握得怎么样。'
      '您陪着孩子一起读，读到后由您来打分。';
  static const startButton = '开始测评';
  static const startingButton = '正在准备题目…';

  /// The English page's entry card action, which opens the assessment screen.
  static const entryAction = '进入学习测评';
  static const finishButton = '结束测评';
  static const finishingButton = '正在结束…';
  static const nextLessonButton = '再练一课';
  static const retryButton = '重新加载';
  static const backButton = '返回';

  static const lessonLabel = '选择课程';
  static const lessonEmpty = '暂时没有可以测评的课程。请先在英语学习里确认已有课程上线，再回来开始测评。';
  static const lessonEmptyTitle = '还没有可测评的课程';
  static const lessonRetry = '重新获取课程';

  static const childLabel = '这次是哪个孩子';
  static const childUnnamed = '不指定孩子';
  static const childEmpty = '还没有填写孩子资料。不指定孩子也可以开始，记录会保存在家庭里。';

  /// Shown before anything is sent, so the guardian knows the bounds first.
  static const preflightTitle = '开始前请知道';
  static const preflightItems = 10;
  static const preflightMinutes = 10;
  static const preflightOne = '每次测评一共 10 道题，最多 10 分钟，到时间会自动结束。';
  static const preflightTwo = '难度会跟着孩子的表现调整：连续答对会变难，答错会回到之前更容易的题。';
  static const preflightThree = '孩子想休息时可以随时停下，点“结束测评”就会结束，已经答过的题会保留。';
  static const preflightFour = '请陪着孩子一起读，读完由您来打分。孩子不需要自己看屏幕打分。';
  static const preflightConfirmTitle = '开始本次测评';
  static const preflightConfirmBody = '一共 10 道题，最多 10 分钟。'
      '题目由家长陪着孩子一起读，读完由您打分。孩子想休息时可以随时结束。';

  static const confirmStart = '开始答题';
  static const cancelStart = '先不开始';

  // The run in progress.
  static const progressTitle = '正在测评';
  static const itemProgress = '题号';
  static const difficultyLabel = '当前难度';
  static const correctLabel = '答对数';
  static const remainingLabel = '剩余时间';
  static const readingHint = '请把下面的内容读给孩子听，听孩子读完后再打分。';
  static const scoringHint = '大多数情况点一下就好；孩子读得差不多时再给中间分。';
  static const correctButton = '答对了';
  static const wrongButton = '答错了';
  static const partialScoreToggle = '给个中间分';
  static const partialScoreHide = '收起中间分';
  static const partialScoreHint = '拖动滑杆，越接近正确答案分数越高。';
  static const scoreLabel = '这一题的分数';
  static const submitAnswerButton = '记录这道题';
  static const submittingAnswerButton = '正在记录…';
  static const scorePreviewPrefix = '将记录为';
  static const answerRecorded = '已记录这道题，继续下一题。';
  static const waitingForItem = '题目正在准备，请稍等。';

  static const finishConfirmTitle = '结束这次测评？';
  static const finishConfirmBody = '结束后不能再继续答题，已经答过的题会保留并生成结果。';
  static const finishKeepGoing = '继续答题';
  static const finishConfirm = '结束测评';

  // Results.
  static const resultTitle = '这次测评的结果';
  static const resultMasteryLabel = '掌握程度';
  static const resultCorrectLabel = '答对题数';
  static const resultDifficultyLabel = '难度变化';
  static const resultEndedByGuardian = '您结束了这次测评，已经答过的题都记下来了。';
  static const resultEndedByTime = '这次测评用满了 10 分钟，到时间自动结束了。';
  static const resultEndedByItems = '10 道题都完成了，这是一次完整的测评。';
  static const resultEndedByPlatform = '这次测评之前没有答完就结束了，结果按已经答过的题计算。';
  static const resultNoAnswers = '这次还没有答任何题目，所以没有掌握程度可以参考。';

  /// Advice for a run that went well.
  static const tipHigh = '孩子掌握得不错，可以换一课新的内容继续练，过几天再回来复习这一课。';

  /// Advice for a run with room to improve.
  static const tipMiddle = '大部分内容已经会了。可以先把这一课再练一遍，再开始下一次测评。';

  /// Advice for a run that struggled; it never blames the child.
  static const tipLow = '这节课还有一点难，回到英语学习里多听几遍再来也不迟，孩子慢慢来就好。';

  /// Advice for a run without answers.
  static const tipNone = '下次可以陪孩子先读完一两题再开始，这样测评结果会更准确。';

  // History.
  static const historyTitle = '最近测评';
  static const historyEmptyTitle = '还没有测评记录';
  static const historyEmpty = '完成第一次测评后，每次的掌握程度会显示在这里，方便对比孩子的进步。';
  static const historyFailed = '测评记录没有加载出来。请检查网络后重试。';

  // Loading and empty states.
  static const loading = '正在加载…';

  // Refusals.
  static const alreadyActive = '还有一次测评没有结束。请先回到那次测评把它答完或结束，再开始新的测评。';
  static const rateLimited = '今天开始的测评有点多，请休息一会儿再来。';
  static const expired = '这次测评已经超时了。已经答过的题会保留，可以重新开始一次新的测评。';
  static const stateChanged = '这次测评的状态已经变了。请刷新页面后看看最新的结果。';
  static const lessonNotFound = '没有找到这节课，可能是课程已经下架。请重新选择一节课程。';
  static const childNotFound = '没有找到这个孩子的资料。请重新选择孩子，或者不指定孩子再开始。';
  static const sessionNotFound = '没有找到这次测评。可以刷新页面，重新开始一次新的测评。';

  static const genericFailure = '测评暂时无法开始，请检查网络后重试';
  static const finishFailure = '结束测评没有完成，请检查网络后再试一次。';
  static const answerFailure = '这道题没有记录成功，请检查网络后再试一次。';

  /// Difficulty is described in words, never as a bare number a parent has to
  /// interpret.
  static String difficultyName(int difficulty) {
    return switch (difficulty) {
      <= 1 => '简单',
      2 => '入门',
      3 => '适中',
      4 => '有点难',
      _ => '挑战',
    };
  }

  /// `3 / 10` progress text.
  static String itemCounter(int index, int maxItems) {
    return '${index + 1} / $maxItems';
  }

  /// `第 3 题` label.
  static String itemPosition(int index) {
    return '第 ${index + 1} 题';
  }

  /// The item type in a parent's words.
  static String itemType(AssessmentItem item) {
    if (item.isWord) {
      return '单词';
    }
    if (item.itemType == 'sentence') {
      return '句型';
    }
    return '内容';
  }

  /// Remaining time, phrased as a limit rather than a countdown in progress.
  static String remaining(int seconds) {
    final safe = seconds.isNegative ? 0 : seconds;
    // The clock counts whole seconds left, so one second short of ten minutes
    // still reads as 10:00 instead of jumping to 9:59 before anything happened.
    final rounded = safe > 0 ? (safe + 59) ~/ 60 * 60 : 0;
    final minutes = rounded ~/ 60;
    final rest = (rounded % 60).toString().padLeft(2, '0');
    return '$remainingLabel $minutes:$rest';
  }

  /// `简单 → 适中`, describing how the level moved over the whole run.
  static String difficultyRange(int from, int to) {
    final start = difficultyName(from);
    final end = difficultyName(to);
    if (from == to) {
      return '$start（没有变化）';
    }
    return '$start → $end';
  }

  /// The one sentence that explains why a run stopped.
  static String resultReason(AssessmentSession session) {
    return switch (session.finishReason) {
      AssessmentFinishReason.itemLimit => resultEndedByItems,
      AssessmentFinishReason.expired => resultEndedByTime,
      AssessmentFinishReason.ended => resultEndedByGuardian,
      AssessmentFinishReason.abandoned => resultEndedByPlatform,
      // A closed run without a stated reason still gets a calm sentence
      // instead of an empty line.
      AssessmentFinishReason.unknown => session.reachedItemLimit
          ? resultEndedByItems
          : resultEndedByGuardian,
    };
  }

  /// The next step the guardian can take, chosen from the run's own numbers.
  static String tip(AssessmentSession session) {
    if (session.answeredCount <= 0) {
      return tipNone;
    }
    if (session.mastery >= 80) {
      return tipHigh;
    }
    if (session.mastery >= 50) {
      return tipMiddle;
    }
    return tipLow;
  }

  /// Whether the advice offers another lesson.
  static bool suggestsAnotherLesson(AssessmentSession session) =>
      session.answeredCount > 0 && session.mastery >= 80;

  /// One history row: `掌握 80% · 答对 8/10 · 适中`.
  static String historyDetail(AssessmentSession session) {
    if (session.answeredCount <= 0) {
      return session.isActive ? '正在进行' : '没有作答';
    }
    return '答对 ${session.correctCount}/${session.answeredCount}'
        ' · 难度${difficultyName(session.difficulty)}';
  }

  /// A short status word for one history row.
  static String historyStatus(AssessmentSession session) {
    return switch (session.state) {
      AssessmentState.active => '正在进行',
      AssessmentState.completed => '已完成',
      AssessmentState.abandoned => '没有答完',
      AssessmentState.unknown => '已结束',
    };
  }

  /// Formats a moment as `10-22 15:30` in the guardian's own time zone.
  static String formatDateTime(DateTime value) {
    final local = value.toLocal();
    final month = local.month.toString().padLeft(2, '0');
    final day = local.day.toString().padLeft(2, '0');
    final hour = local.hour.toString().padLeft(2, '0');
    final minute = local.minute.toString().padLeft(2, '0');
    return '$month-$day $hour:$minute';
  }
}

/// A refusal the platform reports with a code of its own.
///
/// It stays a failure type so the screen can tell "this will not work right
/// now" from "something unexpected happened", while still showing one calm
/// sentence.
class AssessmentRefused implements Exception {
  const AssessmentRefused(this.refusal);

  final AssessmentRefusal refusal;

  String get message => switch (refusal) {
    AssessmentRefusal.alreadyActive => AssessmentCopy.alreadyActive,
    AssessmentRefusal.rateLimited => AssessmentCopy.rateLimited,
    AssessmentRefusal.expired => AssessmentCopy.expired,
    AssessmentRefusal.stateChanged => AssessmentCopy.stateChanged,
    AssessmentRefusal.lessonNotFound => AssessmentCopy.lessonNotFound,
    AssessmentRefusal.childNotFound => AssessmentCopy.childNotFound,
    AssessmentRefusal.sessionNotFound => AssessmentCopy.sessionNotFound,
  };

  @override
  String toString() => 'AssessmentRefused($refusal)';
}

/// Turns any failure into the guardian's sentence.
///
/// The data layer replaces a coded refusal with an [AssessmentRefused] before
/// the error reaches the interface, so a code, a body, or a field name never
/// appears on screen.
String assessmentMessageFor(Object? error) {
  if (error is AssessmentRefused) {
    return error.message;
  }
  if (error is AppException) {
    return error.message;
  }
  return AssessmentCopy.genericFailure;
}
