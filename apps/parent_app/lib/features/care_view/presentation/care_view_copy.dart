import '../../../core/error/app_exception.dart';
import '../domain/care_view.dart';

/// Every sentence the live view shows.
///
/// Keeping the copy in one place makes the promises checkable: no sentence may
/// name a transport, a credential, or a field of the stored view, and every
/// sentence either explains what is happening or tells the guardian what to do.
abstract final class CareViewCopy {
  static const pageTitle = '实时查看';
  static const entryTitle = '实时查看画面';
  static const entrySummary = '一次最多 5 分钟，孩子可以随时关闭。';
  static const startButton = '开始查看';
  static const startingButton = '正在联系设备…';
  static const cancelStartButton = '先不查看';
  static const endButton = '结束查看';
  static const backButton = '返回';

  static const confirmTitle = '开始查看设备画面';
  static const confirmBody = '会显示提示并播放提示音，孩子可以按键随时关闭。'
      '您也可以随时结束查看。一次查看最多持续 5 分钟，到时间会自动结束。';

  static const requestedTitle = '正在等待设备确认';
  static const requestedBody = '设备显示提示后才会开始传送画面。'
      '如果超过 20 秒没有反应，可能是设备没有联网。';

  static const streamingTitle = '正在查看';
  static const preparingTitle = '正在准备画面';
  static const picturePending = '设备已确认，画面马上就到。';
  static const pictureBroken = '画面没有显示出来，可以结束查看后重新开始。';
  static const pictureLabel = '设备画面';
  static const indicationReminder = '设备一直显示提示并播放提示音，孩子可以随时按键关闭。';

  static const streamUnavailableTitle = '暂时无法传送画面';
  static const streamUnavailableBody = '设备已确认，但画面没有传过来。可以先结束查看，稍后再试。';

  static const endedTitle = '查看已结束';
  static const expiredBody = '一次查看最多 5 分钟，到时间已自动结束。需要时可以再开始一次。';
  static const closedByChildTitle = '孩子关闭了这次查看';
  static const closedByChildBody = '孩子可以随时关闭查看，设备现在不会再传送画面。具体原因可以在设备上看到。';
  static const closedByGuardianBody = '已停止查看，设备不会再传送画面。';

  static const declinedTitle = '设备这次没有开始传送';
  static const declinedBody = '可能是孩子暂时不方便，或者设备屏幕没有确认。可以稍后再试一次。';

  static const failedTitle = '这次查看没有开始';
  static const failedBody = '设备在 20 秒内没有确认，可能是设备没有联网。请确认设备已联网后再试一次。';
  static const failedByDeadlineBody = '这次查看已到时限并自动结束。需要时可以再开始一次。';

  static const unknownStateTitle = '这次查看已经结束';
  static const unknownStateBody = '画面已经停止。如果还想看，可以重新开始查看。';

  static const disabled = '还没有开启查看功能，请先在设置里开启';
  static const consentRequired = '需要先同意图片授权才能查看';
  static const deviceBusy = '这台设备正在被查看，请稍后再试';
  static const stateChanged = '这次查看的状态已经变了，请刷新后再试';
  static const rateLimited = '查看太频繁了，请稍后再试';
  static const relayUnavailable = '暂时无法传送画面，请稍后再试';
  static const deviceNotFound = '没有找到这台设备，请确认设备已开机后重试';
  static const viewNotFound = '没有找到这次查看，请回到设备页重新开始';

  static const genericFailure = '暂时无法开始查看，请稍后再试';
  static const endingFailure = '结束查看没有完成，画面也会在时限内停止。';

  /// Remaining time, phrased as a limit rather than a countdown in progress.
  static String remaining(int seconds) {
    final safe = seconds.isNegative ? 0 : seconds;
    final minutes = safe ~/ 60;
    final rest = (safe % 60).toString().padLeft(2, '0');
    return '剩余 $minutes:$rest';
  }

  /// Title of a finished view.
  static String finishedTitle(CareView view) {
    final reason = view.finishReason;
    return switch (view.state) {
      CareViewState.declined => declinedTitle,
      CareViewState.failed =>
        reason == 'request_timeout' || reason.isEmpty
            ? failedTitle
            : failedByDeadlineBody,
      CareViewState.ended => reason == 'expired'
          ? endedTitle
          : _isChildStop(reason)
          ? closedByChildTitle
          : endedTitle,
      _ => unknownStateTitle,
    };
  }

  /// The reason a finished view stopped, in the guardian's words.
  static String finishedDetail(CareView view) {
    final reason = view.finishReason;
    return switch (view.state) {
      CareViewState.declined => declinedBody,
      CareViewState.failed => reason == 'request_timeout' || reason.isEmpty
          ? failedBody
          : failedByDeadlineBody,
      CareViewState.ended => reason == 'expired'
          ? expiredBody
          : _isChildStop(reason)
          ? closedByChildBody
          : closedByGuardianBody,
      _ => unknownStateBody,
    };
  }

  /// Whether the device reports that the child stopped the look.
  ///
  /// The platform records a short internal token, never free text, so this
  /// matches the part that identifies who stopped it.
  static bool _isChildStop(String reason) =>
      reason.contains('child') || reason.contains('device');
}

/// A refusal the platform reports with a code of its own.
///
/// It stays a failure type so the screen can tell "this will not work right
/// now" from "something unexpected happened", while still showing one calm
/// sentence.
class CareViewRefused implements Exception {
  const CareViewRefused(this.refusal);

  final CareViewRefusal refusal;

  String get message => switch (refusal) {
    CareViewRefusal.disabled => CareViewCopy.disabled,
    CareViewRefusal.contentConsentRequired => CareViewCopy.consentRequired,
    CareViewRefusal.deviceBusy => CareViewCopy.deviceBusy,
    CareViewRefusal.stateChanged => CareViewCopy.stateChanged,
    CareViewRefusal.rateLimited => CareViewCopy.rateLimited,
    CareViewRefusal.relayUnavailable => CareViewCopy.relayUnavailable,
    CareViewRefusal.deviceNotFound => CareViewCopy.deviceNotFound,
    CareViewRefusal.viewNotFound => CareViewCopy.viewNotFound,
  };

  @override
  String toString() => 'CareViewRefused($refusal)';
}

/// Turns any failure into the guardian's sentence.
///
/// The API layer replaces a coded refusal with a [CareViewRefused] before the
/// error reaches the interface, so a code, a body, or a field name never
/// appears on screen.
String careViewMessageFor(Object? error) {
  if (error is CareViewRefused) {
    return error.message;
  }
  if (error is AppException) {
    return error.message;
  }
  return CareViewCopy.genericFailure;
}
