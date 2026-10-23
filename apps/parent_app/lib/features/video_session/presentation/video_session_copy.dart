import '../../../core/error/app_exception.dart';
import '../domain/video_session.dart';

/// Every sentence the 视频通话 screen shows.
///
/// The copy is kept in one place so the promises stay checkable. Nothing here
/// names a transport, a field, or a credential; every sentence either explains
/// what the screen contains, states what the device will and will not capture,
/// or tells the guardian what the next action is. There is no marketing
/// language, because the screen describes a permission over a child's camera
/// and microphone.
abstract final class VideoSessionCopy {
  static const pageTitle = '视频通话';
  static const backButton = '返回';
  static const retryButton = '重新加载';
  static const saveButton = '保存视频通话设置';
  static const savingButton = '正在保存…';
  static const loading = '正在加载…';

  // What the screen is and is not.
  static const introTitle = '通话设置在设备上生效';
  static const introBody = '这里只决定允不允许通话、能不能用摄像头和麦克风。通话本身不在手机上接打。';
  static const privacyBody = '通话不经过家长手机，平台也不保存通话画面和声音，只记录这一台设备是否允许通话。';
  static const noRecordingBody = '视频和声音都不会留存在平台，通话结束后设备停止使用摄像头和麦克风。';

  // Policy section.
  static const policyTitle = '这台设备的通话规则';
  static const enabledTitle = '允许视频通话';
  static const enabledBody = '关闭后设备不会响铃，也不会接受任何视频通话。';
  static const cameraTitle = '允许使用摄像头';
  static const cameraBody = '开启后孩子能和家人视频。默认关闭：设备不会自己打开摄像头。';
  static const microphoneTitle = '允许使用麦克风';
  static const microphoneBody = '视频通话需要麦克风收音。开启后通话期间设备会拾音，结束即停止。';
  static const consentTitle = '通话前先问孩子';
  static const consentBody = '由孩子在设备上确认后才会开始。建议保持开启。';
  static const consentOffConfirmTitle = '关闭通话前确认';
  static const consentOffConfirmBody = '关闭后，视频会在孩子看到提示的同时直接开始。确定关闭吗？';
  static const consentOffConfirmAction = '关闭通话前确认';
  static const consentOffCancelAction = '保持开启';
  static const captureBlockedHint = '摄像头和麦克风都关闭时，设备不能发起或接受视频通话。';
  static const qualityTitle = '最低清晰度';
  static const qualityBody = '网络变差时，画面先降到这一档；再差就结束通话，而不是继续勉强进行。';
  static const ringTitle = '响铃时长';
  static const ringBody = '设备响铃这么久没人接听就自动挂断。';
  static const durationTitle = '单次通话上限';
  static const durationBody = '时间到后通话自动结束，避免孩子长时间盯着屏幕。';

  // Device section.
  static const deviceTitle = '设备的通话状态';
  static const deviceEmptyTitle = '还没有设备上报通话状态';
  static const deviceEmptyBody = '设备联网并检查过设置后，这里会显示它能不能视频、摄像头和麦克风是开还是关。';
  static const deviceUnreported = '这台设备还没有上报通话状态，联网后会自动显示。';
  static const deviceUnsupported = '这台设备不支持视频通话，家人留言和语音对话不受影响。';
  static const deviceReady = '设置已同步到这台设备。';
  static const deviceOutdated = '设备还在用旧设置，下次联网时自动更新。';
  /// Shown when the device itself reports no video capability. The policy above
  /// can still be on, so the sentence names the device as the reason.
  static const capabilityUnsupported = '这台设备当前无法进行视频通话，设置会保留到设备支持为止。';
  static const cameraOn = '摄像头已允许';
  static const cameraOff = '摄像头未允许';
  static const microphoneOn = '麦克风已允许';
  static const microphoneOff = '麦克风未允许';
  static const callIdle = '当前没有通话';
  static const callRinging = '正在响铃，等待孩子接听';
  static const callActive = '正在视频通话';
  static const callEnded = '上一次通话已结束';
  static const callRejected = '上一次通话被拒绝';
  static const callFailed = '上一次通话中断了';
  static const callUnknown = '设备上报了一个这里还没有的通话状态';
  static const qualityGood = '画质良好';
  static const qualityFair = '画质一般';
  static const qualityPoor = '画质较差';
  static const qualityUnknown = '画质未知';
  static const endHangup = '由一方挂断';
  static const endRejected = '孩子没有接听';
  static const endTimeout = '响铃超时';
  static const endNetwork = '网络中断';
  static const endPermissionRevoked = '权限被关闭后中断';
  static const endOther = '原因未说明';
  static const reportedAt = '上报时间';

  // Results and failures.
  static const saveSucceeded = '视频通话设置已保存，设备联网后会同步。';
  static const noChangeHint = '没有改动需要保存。';
  static const settingsFailed = '通话设置没有读出来，请稍后再试。';
  static const devicesFailed = '设备状态没有读出来，通话设置仍可查看和保存。';
  static const devicesRetryButton = '重新加载设备状态';
  static const invalidPolicy = '有一项设置超出允许范围，请调整后再保存。';
  static const invalidPolicyHint = '多数情况下把响铃时长或通话上限调回范围内即可。';
  static const unavailable = '视频通话设置暂时无法读取，请稍后再试。';
  static const genericFailure = '视频通话设置暂时无法打开，请稍后再试。';

  /// One quality band in a parent's words.
  static String qualityName(VideoQualityFloor floor) {
    return switch (floor) {
      VideoQualityFloor.good => '良好，网络差时自动降低',
      VideoQualityFloor.fair => '一般，网络差时自动降低',
      VideoQualityFloor.poor => '较低也能继续',
    };
  }

  /// The one-line consequence of a quality floor.
  static String qualityHint(VideoQualityFloor floor) {
    return switch (floor) {
      VideoQualityFloor.good => '画面稍微变差就结束通话，最省流量。',
      VideoQualityFloor.fair => '画面较差时才结束通话，比较平衡。',
      VideoQualityFloor.poor => '只要还能看清就继续通话。',
    };
  }

  /// A duration in seconds as a sentence a parent reads without converting.
  static String seconds(int value) {
    if (value < 60) {
      return '$value 秒';
    }
    final minutes = value ~/ 60;
    final remainder = value % 60;
    if (remainder == 0) {
      return '$minutes 分钟';
    }
    return '$minutes 分 $remainder 秒';
  }

  /// `2026 年 10 月 22 日 15:04`, in the guardian's own time zone.
  static String dateTime(DateTime value) {
    final local = value.toLocal();
    final hour = local.hour.toString().padLeft(2, '0');
    final minute = local.minute.toString().padLeft(2, '0');
    return '${local.year} 年 ${local.month} 月 ${local.day} 日 $hour:$minute';
  }
}

/// A refusal the platform reported with a code of its own.
class VideoSessionRefused implements Exception {
  const VideoSessionRefused(this.refusal);

  final VideoSessionRefusal refusal;

  String get message => switch (refusal) {
    VideoSessionRefusal.invalidPolicy => VideoSessionCopy.invalidPolicy,
    VideoSessionRefusal.unavailable => VideoSessionCopy.unavailable,
  };

  @override
  String toString() => 'VideoSessionRefused($refusal)';
}

/// Turns any failure into the guardian's sentence.
///
/// A refusal that arrived at this build already carries its own sentence;
/// anything else is a transport failure the mapper already translated.
String videoSessionMessageFor(Object? error) {
  if (error is VideoSessionRefused) {
    return error.message;
  }
  if (error is AppException) {
    return error.message;
  }
  return VideoSessionCopy.genericFailure;
}
