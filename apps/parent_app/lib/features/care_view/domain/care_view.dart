/// Lifecycle position of one live look at a device.
///
/// Only [CareViewState.streaming] may show a picture. The parent app must never
/// render a previous frame while the view is requested, ended, declined, or
/// failed: a stale frame looks exactly like a live camera.
enum CareViewState { requested, streaming, ended, declined, failed, unknown }

/// One live look, as the platform reports it.
class CareView {
  const CareView({
    required this.viewId,
    required this.deviceId,
    required this.state,
    required this.startedAt,
    required this.deadline,
    this.childId = '',
    this.finishedAt,
    this.durationSeconds = 0,
    this.finishReason = '',
    this.framesDelivered = 0,
    this.allowsFrames = false,
    this.streamPath = '',
    this.isStreamUnavailable = false,
  });

  final String viewId;
  final String deviceId;
  final String childId;
  final CareViewState state;
  final DateTime startedAt;

  /// Hard stop for this look. The platform and the relay both enforce it.
  final DateTime deadline;
  final DateTime? finishedAt;
  final int durationSeconds;
  final String finishReason;
  final int framesDelivered;
  final bool allowsFrames;

  /// Where the parent app receives pictures, without any credential.
  final String streamPath;

  /// Set when the view is streaming but no picture can be delivered.
  final bool isStreamUnavailable;

  bool get isRequested => state == CareViewState.requested;
  bool get isStreaming => state == CareViewState.streaming;
  bool get isFinished =>
      state == CareViewState.ended ||
      state == CareViewState.declined ||
      state == CareViewState.failed;

  /// True when the view is over or has run out of time.
  bool isExpiredAt(DateTime now) => !now.isBefore(deadline);

  bool get canShowPicture =>
      isStreaming && allowsFrames && !isStreamUnavailable && streamPath.isNotEmpty;

  factory CareView.fromJson(Map<String, Object?> json) {
    return CareView(
      viewId: _string(json['session_id']),
      deviceId: _string(json['device_id']),
      childId: _string(json['child_id']),
      state: _stateFrom(_string(json['state'])),
      startedAt: _instant(json['started_at']) ?? DateTime.now().toUtc(),
      deadline: _instant(json['deadline']) ?? DateTime.now().toUtc(),
      finishedAt: _instant(json['finished_at']),
      durationSeconds: _int(json['duration_seconds']),
      finishReason: _string(json['finish_reason']),
      framesDelivered: _int(json['frames_delivered']),
      allowsFrames: json['allows_frames'] == true,
      streamPath: _string(json['stream_path']),
      isStreamUnavailable: json['stream_unavailable'] == true,
    );
  }

  /// Returns a copy that is over as of [now], for a look that ran out of time.
  ///
  /// A view the platform already reported as ended keeps its own reason, so the
  /// guardian is told what actually happened rather than always "time is up".
  /// This view with its deadline moved no later than [limit].
  ///
  /// Refreshes use it so a platform answer cannot stretch a bounded look.
  CareView cappedAt(DateTime limit) {
    if (deadline.isBefore(limit) || deadline.isAtSameMomentAs(limit)) {
      return this;
    }
    return CareView(
      viewId: viewId,
      deviceId: deviceId,
      childId: childId,
      state: state,
      startedAt: startedAt,
      deadline: limit,
      finishedAt: finishedAt,
      durationSeconds: durationSeconds,
      finishReason: finishReason,
      framesDelivered: framesDelivered,
      streamPath: streamPath,
      isStreamUnavailable: isStreamUnavailable,
    );
  }

  CareView expiringAt(DateTime now) {
    if (isFinished) {
      return this;
    }
    return CareView(
      viewId: viewId,
      deviceId: deviceId,
      childId: childId,
      state: CareViewState.ended,
      startedAt: startedAt,
      deadline: deadline,
      finishedAt: now,
      durationSeconds: now.difference(startedAt).inSeconds,
      finishReason: 'expired',
      framesDelivered: framesDelivered,
      allowsFrames: false,
      isStreamUnavailable: isStreamUnavailable,
    );
  }

  static String _string(Object? value) => value is String ? value : '';

  static int _int(Object? value) => value is int ? value : 0;

  static DateTime? _instant(Object? value) =>
      value is String ? DateTime.tryParse(value)?.toUtc() : null;

  static CareViewState _stateFrom(String value) {
    return switch (value) {
      'requested' => CareViewState.requested,
      'streaming' => CareViewState.streaming,
      'ended' => CareViewState.ended,
      'declined' => CareViewState.declined,
      'failed' => CareViewState.failed,
      // An unreadable state is treated as unknown and never as streaming,
      // because showing a picture would claim a privacy state nobody confirmed.
      _ => CareViewState.unknown,
    };
  }
}

/// Known refusals that each need their own guardian-facing sentence.
enum CareViewRefusal {
  disabled,
  contentConsentRequired,
  deviceBusy,
  stateChanged,
  rateLimited,
  relayUnavailable,
  deviceNotFound,
  viewNotFound,
}
