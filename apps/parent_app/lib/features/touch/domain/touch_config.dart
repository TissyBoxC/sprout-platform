/// Guardian-facing touch input contract types.
///
/// The platform stores only bounded configuration scalars and bounded device
/// touch status; raw touch coordinates never flow through this client.
library;

/// One validated touch configuration for the family.
class TouchConfig {
  const TouchConfig({
    required this.enabled,
    required this.tapEnabled,
    required this.swipeEnabled,
    required this.longPressEnabled,
    required this.hapticEnabled,
    required this.sensitivity,
    required this.rotationDegrees,
    required this.longPressMs,
    required this.doubleTapMs,
    required this.swipeDistancePx,
    required this.revision,
  });

  final bool enabled;
  final bool tapEnabled;
  final bool swipeEnabled;
  final bool longPressEnabled;
  final bool hapticEnabled;
  final int sensitivity;
  final int rotationDegrees;
  final int longPressMs;
  final int doubleTapMs;
  final int swipeDistancePx;
  final int revision;

  TouchConfig copyWith({
    bool? enabled,
    bool? tapEnabled,
    bool? swipeEnabled,
    bool? longPressEnabled,
    bool? hapticEnabled,
    int? sensitivity,
    int? rotationDegrees,
    int? longPressMs,
    int? doubleTapMs,
    int? swipeDistancePx,
  }) {
    return TouchConfig(
      enabled: enabled ?? this.enabled,
      tapEnabled: tapEnabled ?? this.tapEnabled,
      swipeEnabled: swipeEnabled ?? this.swipeEnabled,
      longPressEnabled: longPressEnabled ?? this.longPressEnabled,
      hapticEnabled: hapticEnabled ?? this.hapticEnabled,
      sensitivity: sensitivity ?? this.sensitivity,
      rotationDegrees: rotationDegrees ?? this.rotationDegrees,
      longPressMs: longPressMs ?? this.longPressMs,
      doubleTapMs: doubleTapMs ?? this.doubleTapMs,
      swipeDistancePx: swipeDistancePx ?? this.swipeDistancePx,
      revision: revision,
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'enabled': enabled,
      'tap_enabled': tapEnabled,
      'swipe_enabled': swipeEnabled,
      'long_press_enabled': longPressEnabled,
      'haptic_enabled': hapticEnabled,
      'sensitivity': sensitivity,
      'rotation_degrees': rotationDegrees,
      'long_press_ms': longPressMs,
      'double_tap_ms': doubleTapMs,
      'swipe_distance_px': swipeDistancePx,
    };
  }

  factory TouchConfig.fromJson(Map<String, Object?> json) {
    return TouchConfig(
      enabled: json['enabled'] == true,
      tapEnabled: json['tap_enabled'] == true,
      swipeEnabled: json['swipe_enabled'] == true,
      longPressEnabled: json['long_press_enabled'] == true,
      hapticEnabled: json['haptic_enabled'] == true,
      sensitivity: _asInt(json['sensitivity']),
      rotationDegrees: _asInt(json['rotation_degrees']),
      longPressMs: _asInt(json['long_press_ms']),
      doubleTapMs: _asInt(json['double_tap_ms']),
      swipeDistancePx: _asInt(json['swipe_distance_px']),
      revision: _asInt(json['revision']),
    );
  }
}

/// The touch capability of one device as last reported.
class TouchCapability {
  const TouchCapability({
    required this.supported,
    required this.hapticSupported,
    required this.gestureCount,
    required this.policyRevision,
    required this.appliedRevision,
    required this.upToDate,
  });

  final bool supported;
  final bool hapticSupported;
  final int gestureCount;
  final int policyRevision;
  final int appliedRevision;
  final bool upToDate;

  factory TouchCapability.fromJson(Map<String, Object?> json) {
    return TouchCapability(
      supported: json['supported'] == true,
      hapticSupported: json['haptic_supported'] == true,
      gestureCount: _asInt(json['gesture_count']),
      policyRevision: _asInt(json['policy_revision']),
      appliedRevision: _asInt(json['applied_revision']),
      upToDate: json['up_to_date'] == true,
    );
  }
}

int _asInt(Object? value) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return 0;
}
