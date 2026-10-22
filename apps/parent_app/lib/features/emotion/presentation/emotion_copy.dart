import '../../../core/error/app_exception.dart';
import '../domain/emotion.dart';

/// Every sentence the 情绪感知 screen shows.
///
/// Keeping the copy in one place makes the promise checkable: no sentence may
/// name a transport, a field, or a credential, and no sentence may describe a
/// single moment of a child's day. Where the platform reports that it cannot
/// answer, the screen says so instead of guessing.
abstract final class EmotionCopy {
  static const pageTitle = '情绪感知';
  static const backButton = '返回';
  static const retryButton = '重新加载';

  // The explanation shown before sensing is turned on.
  static const introTitle = '情绪感知可以做什么';
  static const introBody =
      '初芽会在陪伴孩子的时候，记录大致属于哪一类情绪，'
      '慢慢积累成最近一段时间的分布，让您看到孩子这段时间大致的状态。';
  static const privacyTitle = '关于孩子的数据';
  static const privacyTagOnly = '只记录情绪的类型，比如安静、开心、好奇，不会记录具体说了什么。';
  static const privacyNoMedia = '不录音，也不录像，不会有声音和画面的保存。';
  static const privacyGuardianOnly = '结果只给家长看，只有这份分布，不会推送给孩子。';
  static const privacyNoMoments = '不会记下「几点是什么情绪」，看的是一段时间里的分布。';
  static const privacyRetention = '数据按您选择的保留天数保存，到期由平台清理。';
  static const privacySwitchOff = '情绪感知默认关闭，随时可以关闭，关闭后就不再记录。';

  static const enableButton = '开启情绪感知';
  static const enablingButton = '正在开启…';

  // The switch on the card that shows the distribution.
  static const switchLabel = '情绪感知';
  static const switchOnHint = '正在记录大致情绪。关闭后不再记录。';
  static const switchOffHint = '这部分内容不会记录。';

  // Distribution.
  static const summaryTitle = '这段时间的情绪分布';
  static const window7 = '最近 7 天';
  static const window30 = '最近 30 天';
  static const trendTitle = '和上一个周期相比';
  static const trendNoComparison = '数据还太少，暂时看不出趋势。多陪伴几天再回来看看。';
  static const trendFlat = '基本持平';
  static const trendUp = '比上期多';
  static const trendDown = '比上期少';

  // Loading, empty and error states.
  static const loading = '正在加载…';
  static const emptyTitle = '还没有可以看的数据';
  static const emptyBody = '初芽会在陪伴过程中记录孩子的情绪变化，积累几天后这里就会有分布。';
  static const summaryError = '这段时间的分布没有读出来，请稍后再试。';
  static const settingsError = '情绪感知的设置没有读出来，请稍后再试。';
  static const enableFailure = '没有开启成功，请稍后重试。';
  static const disableFailure = '没有关闭成功，请稍后重试。';
  static const retentionFailure = '保留天数没有保存成功，请稍后重试。';
  static const genericFailure = '情绪感知暂时无法打开，请稍后再试。';

  // Children.
  static const childTitle = '看哪个孩子';
  static const childEmptyTitle = '还没有儿童档案';
  static const childEmptyBody = '在儿童档案里添加孩子后，就能看到这段时间的情绪分布。';
  static const childMissing = '没有找到这个孩子的资料，请重新选择孩子。';
  static const childFamilyWide = '全部孩子';
  static const childFamilyWideHint = '现在显示的是整个家庭记录到的情绪。';

  // Turning sensing off.
  static const disableConfirmTitle = '关闭情绪感知？';
  static const disableConfirmBody =
      '关闭后初芽会停止记录情绪，已有的分布数据不再增加。'
      '之后也可以随时重新开启。';
  static const disableConfirm = '关闭情绪感知';
  static const disableKeep = '继续开启';
  static const disabledNotice = '情绪感知已关闭，之后不再记录。';

  // Retention.
  static const retentionTitle = '数据保留';
  static const retentionBody = '记录到的情绪只保存这么久，到期后由平台清理。';
  static const retentionSaving = '正在保存保留天数…';
  static const retentionKept = '这个保留天数没有保存成功，已保持原来的设置。';

  /// The label of one emotion, described neutrally.
  ///
  /// Every word names what the child was doing or feeling without judging it,
  /// and none of them reads as a diagnosis.
  static String labelName(EmotionLabel label) {
    return switch (label) {
      EmotionLabel.calm => '安静',
      EmotionLabel.happy => '开心',
      EmotionLabel.curious => '好奇',
      EmotionLabel.tired => '疲倦',
      EmotionLabel.frustrated => '受挫',
      EmotionLabel.sad => '低落',
      EmotionLabel.upset => '不安',
      EmotionLabel.unknown => '',
    };
  }

  /// One sentence naming the period the distribution covers.
  static String windowPhrase(int days) => '最近 $days 天';

  /// `这周记录到的情绪以“开心”为主。`
  ///
  /// It states what was recorded, not what the child is like. A period of two
  /// days is called 这段时间 rather than 这周, so the sentence stays true.
  static String dominantPhrase(int days, EmotionLabel label) {
    final name = labelName(label);
    if (name.isEmpty) {
      return '';
    }
    final window = days > 7 ? '这段时间' : (days == 7 ? '这周' : '这几天');
    return '$window记录到的情绪以「$name」为主。';
  }

  /// `共记录 28 次`.
  static String sampleTotal(int samples) => '共记录 $samples 次';

  /// `开心 12 次 · 43%`.
  static String labelDetail(EmotionLabelShare share) {
    return '${labelName(share.label)} ${share.count} 次 · ${share.percentage}%';
  }

  /// The trend of one label, in a parent's words.
  static String trendPhrase(EmotionLabelShare share) {
    return switch (share.trend) {
      EmotionTrend.up => '${labelName(share.label)}$trendUp',
      EmotionTrend.down => '${labelName(share.label)}$trendDown',
      EmotionTrend.flat => '${labelName(share.label)}$trendFlat',
      // A label without a comparable trend is left out of the line rather than
      // shown as "unknown", which would read like an error.
      EmotionTrend.unknown => '',
    };
  }

  /// `保留 30 天`.
  static String retentionDays(int days) => '保留 $days 天';
}

/// A refusal the platform reported with a code of its own.
///
/// It stays a failure type so the screen can tell "this will not work right
/// now" from "something unexpected happened", while still showing one calm
/// sentence.
class EmotionRefused implements Exception {
  const EmotionRefused(this.refusal);

  final EmotionRefusal refusal;

  String get message => switch (refusal) {
    EmotionRefusal.disabled => EmotionCopy.emptyBody,
    EmotionRefusal.invalidRetention => EmotionCopy.retentionFailure,
    EmotionRefusal.invalidDays => EmotionCopy.summaryError,
    EmotionRefusal.childNotFound => EmotionCopy.childMissing,
    EmotionRefusal.retentionKept => EmotionCopy.retentionKept,
    EmotionRefusal.unavailable => EmotionCopy.summaryError,
  };

  @override
  String toString() => 'EmotionRefused($refusal)';
}

/// Turns any failure into the guardian's sentence.
///
/// The data layer replaces a coded refusal with an [EmotionRefused] before the
/// error reaches the interface, so a code, a body, or a field name never
/// appears on screen. The transport's own mapping already covers a lapsed
/// sign-in and an unreachable network.
String emotionMessageFor(Object? error) {
  if (error is EmotionRefused) {
    return error.message;
  }
  if (error is AppException) {
    return error.message;
  }
  return EmotionCopy.genericFailure;
}
