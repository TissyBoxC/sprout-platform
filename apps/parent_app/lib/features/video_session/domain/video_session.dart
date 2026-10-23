/// Guardian-facing video-session contract types.
///
/// Two separate things live here. The policy is the family-wide rule the
/// guardian edits: whether a video call may happen at all, whether the device
/// camera and microphone may be used, whether the child has to agree first, the
/// lowest picture quality the call may drop to, how long the device rings, and
/// the hard ceiling on one call. The device snapshot is what a bound device last
/// reported over its own session: which child device it is, whether it can carry
/// a call, which permission it currently has, and whether it already applied the
/// newest policy revision.
///
/// The platform stores negotiation metadata only. No audio and no video is
/// stored or carried by this client, which is why nothing in this module can
/// hand back a frame, a clip, or a recording.
library;

/// The lowest picture quality a call may keep before it ends.
///
/// The device clamps a call down through these bands and ends it instead of
/// continuing below the floor the guardian chose.
enum VideoQualityFloor {
  good,
  fair,
  poor;

  static VideoQualityFloor fromWire(String value) {
    return switch (value) {
      'good' => VideoQualityFloor.good,
      'fair' => VideoQualityFloor.fair,
      _ => VideoQualityFloor.poor,
    };
  }

  String get wireValue {
    return switch (this) {
      VideoQualityFloor.good => 'good',
      VideoQualityFloor.fair => 'fair',
      VideoQualityFloor.poor => 'poor',
    };
  }
}

/// The normalized call state a device reported.
///
/// A device that cannot carry a call reports [idle], so a stale value never
/// reads as a live session.
enum VideoCallState {
  idle,
  ringing,
  active,
  ended,
  rejected,
  failed,

  /// A state this build does not know. It is kept rather than dropped because
  /// the guardian still needs to know something happened.
  unknown;

  static VideoCallState fromWire(String value) {
    return switch (value) {
      'idle' => VideoCallState.idle,
      'ringing' => VideoCallState.ringing,
      'active' => VideoCallState.active,
      'ended' => VideoCallState.ended,
      'rejected' => VideoCallState.rejected,
      'failed' => VideoCallState.failed,
      _ => VideoCallState.unknown,
    };
  }
}

/// Why the last call on a device ended.
enum VideoEndReason {
  none,
  hangup,
  rejected,
  timeout,
  network,
  permissionRevoked,

  /// A reason this build does not name. The screen says the call ended without
  /// repeating the raw value.
  other;

  static VideoEndReason fromWire(String value) {
    return switch (value) {
      'hangup' => VideoEndReason.hangup,
      'rejected' => VideoEndReason.rejected,
      'timeout' => VideoEndReason.timeout,
      'network' => VideoEndReason.network,
      'permission_revoked' => VideoEndReason.permissionRevoked,
      'none' || '' => VideoEndReason.none,
      _ => VideoEndReason.other,
    };
  }
}

/// How a device carries a call right now, in the guardian's own terms.
///
/// It is derived from the reported capability rather than from a raw code, so
/// the screen never has to decide what "supported but outdated" means twice.
enum VideoSessionReadiness {
  /// No device in this family has reported yet.
  unreported,

  /// The device reported that it cannot carry a video call at all.
  unsupported,

  /// The device can carry a call and already applied the newest revision.
  ready,

  /// The device can carry a call but still runs an older revision. The new
  /// setting reaches it the next time it checks in.
  outdated,
}

/// One bounded device snapshot as the platform stored it.
class VideoSessionDevice {
  const VideoSessionDevice({
    required this.deviceId,
    required this.sessionSupported,
    required this.cameraAllowed,
    required this.microphoneAllowed,
    required this.callState,
    required this.quality,
    required this.lastEndReason,
    required this.upToDate,
    this.reportedAt,
  });

  final String deviceId;

  /// Whether the device reports that it can carry a video call at all.
  final bool sessionSupported;

  /// Whether the device currently holds the camera permission.
  final bool cameraAllowed;

  /// Whether the device currently holds the microphone permission.
  final bool microphoneAllowed;
  final VideoCallState callState;

  /// The reported quality band, kept as its wire value because the guardian
  /// screen renders it through the copy table in the same way for the policy
  /// floor and for a live call.
  final String quality;
  final VideoEndReason lastEndReason;

  /// Whether the device applied at least the newest policy revision.
  final bool upToDate;

  /// When the device reported. Absent when the platform has no stamp.
  final DateTime? reportedAt;

  VideoSessionReadiness get readiness {
    if (!sessionSupported) {
      return VideoSessionReadiness.unsupported;
    }
    return upToDate
        ? VideoSessionReadiness.ready
        : VideoSessionReadiness.outdated;
  }

  /// The device is calling or being called right now. It is the only state in
  /// which the device is capturing, so the screen can say so plainly.
  bool get isInCall {
    return callState == VideoCallState.active ||
        callState == VideoCallState.ringing;
  }

  factory VideoSessionDevice.fromJson(Map<String, Object?> json) {
    final reported = json['reported_at'];
    return VideoSessionDevice(
      deviceId: valueAsString(json['device_id']),
      sessionSupported: json['session_supported'] == true,
      cameraAllowed: json['camera_allowed'] == true,
      microphoneAllowed: json['microphone_allowed'] == true,
      callState: VideoCallState.fromWire(valueAsString(json['call_state'])),
      quality: valueAsString(json['quality']),
      lastEndReason: VideoEndReason.fromWire(
        valueAsString(json['last_end_reason']),
      ),
      upToDate: json['up_to_date'] == true,
      reportedAt: reported is String ? DateTime.tryParse(reported) : null,
    );
  }
}

/// The family-wide video-call rule the guardian edits.
///
/// Every field is bounded by the platform: the ring window is 5 to 120 seconds
/// and one call may last 60 to 7200 seconds. The camera and the microphone start
/// off, so a device never captures before a guardian explicitly allows it.
class VideoSessionPolicy {
  const VideoSessionPolicy({
    required this.enabled,
    required this.cameraAllowed,
    required this.microphoneAllowed,
    required this.requireChildConsent,
    required this.qualityFloor,
    required this.ringTimeoutSeconds,
    required this.maxSessionSeconds,
    required this.revision,
  });

  /// The rule the platform serves before a guardian edits it: calls are allowed
  /// but the camera and the microphone stay off and the child is asked first.
  const VideoSessionPolicy.initial()
    : enabled = true,
      cameraAllowed = false,
      microphoneAllowed = false,
      requireChildConsent = true,
      qualityFloor = VideoQualityFloor.poor,
      ringTimeoutSeconds = defaultRingTimeoutSeconds,
      maxSessionSeconds = defaultSessionSeconds,
      revision = 0;

  /// The platform's bounds. They are repeated here because the screen has to
  /// keep the controls inside them rather than let the platform refuse the save.
  static const minimumRingTimeoutSeconds = 5;
  static const maximumRingTimeoutSeconds = 120;
  static const minimumSessionSeconds = 60;
  static const maximumSessionSeconds = 7200;
  static const defaultRingTimeoutSeconds = 30;
  static const defaultSessionSeconds = 1800;

  final bool enabled;
  final bool cameraAllowed;
  final bool microphoneAllowed;

  /// When true the device asks the child before it starts capturing. It stays
  /// on unless the guardian turns it off, and turning it off is confirmed first.
  final bool requireChildConsent;
  final VideoQualityFloor qualityFloor;
  final int ringTimeoutSeconds;
  final int maxSessionSeconds;

  /// The stored revision. A save returns the next one, which is why the screen
  /// always re-reads the policy instead of patching its own copy.
  final int revision;

  /// True while every capture permission is off, so the screen can say that the
  /// device will not capture anything.
  bool get isCaptureBlocked => !cameraAllowed && !microphoneAllowed;

  VideoSessionPolicy copyWith({
    bool? enabled,
    bool? cameraAllowed,
    bool? microphoneAllowed,
    bool? requireChildConsent,
    VideoQualityFloor? qualityFloor,
    int? ringTimeoutSeconds,
    int? maxSessionSeconds,
    int? revision,
  }) {
    return VideoSessionPolicy(
      enabled: enabled ?? this.enabled,
      cameraAllowed: cameraAllowed ?? this.cameraAllowed,
      microphoneAllowed: microphoneAllowed ?? this.microphoneAllowed,
      requireChildConsent: requireChildConsent ?? this.requireChildConsent,
      qualityFloor: qualityFloor ?? this.qualityFloor,
      ringTimeoutSeconds: ringTimeoutSeconds ?? this.ringTimeoutSeconds,
      maxSessionSeconds: maxSessionSeconds ?? this.maxSessionSeconds,
      revision: revision ?? this.revision,
    );
  }

  /// The editable half of the policy, in the shape the platform accepts.
  Map<String, Object?> toJson() {
    return <String, Object?>{
      'enabled': enabled,
      'camera_allowed': cameraAllowed,
      'microphone_allowed': microphoneAllowed,
      'require_child_consent': requireChildConsent,
      'quality_floor': qualityFloor.wireValue,
      'ring_timeout_seconds': ringTimeoutSeconds,
      'max_session_seconds': maxSessionSeconds,
    };
  }

  /// Reads one policy record.
  ///
  /// The platform omits nothing, but a value outside the documented range is
  /// clamped rather than shown, so a stored outlier cannot put the controls in
  /// a position the save would then be refused for.
  factory VideoSessionPolicy.fromJson(Map<String, Object?> json) {
    return VideoSessionPolicy(
      enabled: json['enabled'] == true,
      cameraAllowed: json['camera_allowed'] == true,
      microphoneAllowed: json['microphone_allowed'] == true,
      requireChildConsent: json['require_child_consent'] == true,
      qualityFloor: VideoQualityFloor.fromWire(
        valueAsString(json['quality_floor']),
      ),
      ringTimeoutSeconds: _clampInt(
        json['ring_timeout_seconds'],
        minimumRingTimeoutSeconds,
        maximumRingTimeoutSeconds,
        defaultRingTimeoutSeconds,
      ),
      maxSessionSeconds: _clampInt(
        json['max_session_seconds'],
        minimumSessionSeconds,
        maximumSessionSeconds,
        defaultSessionSeconds,
      ),
      revision: _clampInt(json['revision'], 0, 1 << 30, 0),
    );
  }
}

/// The refusal codes this module translates for the guardian.
enum VideoSessionRefusal {
  /// A policy value fell outside the bounds the platform accepts.
  invalidPolicy,

  /// The listed device snapshots are unavailable.
  unavailable,
}

String valueAsString(Object? value) => value is String ? value : '';

int _clampInt(Object? value, int minimum, int maximum, int fallback) {
  if (value is! num) {
    return fallback;
  }
  return value.toInt().clamp(minimum, maximum);
}
