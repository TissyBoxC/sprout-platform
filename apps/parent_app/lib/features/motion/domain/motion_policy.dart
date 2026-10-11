/// Guardian-facing motion, motor and servo policy contract types.
///
/// The platform stores only bounded policy scalars and a bounded motion
/// lifecycle state; raw motor telemetry never flows through this client.
library;

/// One validated motion policy for the family.
class MotionPolicy {
  const MotionPolicy({
    required this.enabled,
    required this.requireStopAck,
    required this.allowLocomotion,
    required this.maxSpeedPercent,
    required this.maxRuntimeMs,
    required this.stopGraceMs,
    required this.revision,
  });

  final bool enabled;
  final bool requireStopAck;
  final bool allowLocomotion;
  final int maxSpeedPercent;
  final int maxRuntimeMs;
  final int stopGraceMs;
  final int revision;

  MotionPolicy copyWith({
    bool? enabled,
    bool? requireStopAck,
    bool? allowLocomotion,
    int? maxSpeedPercent,
    int? maxRuntimeMs,
    int? stopGraceMs,
  }) {
    return MotionPolicy(
      enabled: enabled ?? this.enabled,
      requireStopAck: requireStopAck ?? this.requireStopAck,
      allowLocomotion: allowLocomotion ?? this.allowLocomotion,
      maxSpeedPercent: maxSpeedPercent ?? this.maxSpeedPercent,
      maxRuntimeMs: maxRuntimeMs ?? this.maxRuntimeMs,
      stopGraceMs: stopGraceMs ?? this.stopGraceMs,
      revision: revision,
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'enabled': enabled,
      'require_stop_ack': requireStopAck,
      'allow_locomotion': allowLocomotion,
      'max_speed_percent': maxSpeedPercent,
      'max_runtime_ms': maxRuntimeMs,
      'stop_grace_ms': stopGraceMs,
    };
  }

  factory MotionPolicy.fromJson(Map<String, Object?> json) {
    return MotionPolicy(
      enabled: json['enabled'] == true,
      requireStopAck: json['require_stop_ack'] == true,
      allowLocomotion: json['allow_locomotion'] == true,
      maxSpeedPercent: _asInt(json['max_speed_percent']),
      maxRuntimeMs: _asInt(json['max_runtime_ms']),
      stopGraceMs: _asInt(json['stop_grace_ms']),
      revision: _asInt(json['revision']),
    );
  }
}

/// The motion capability of one device as last reported.
class MotionCapability {
  const MotionCapability({
    required this.supported,
    required this.state,
    required this.action,
    required this.actuatorCount,
    required this.faulted,
    required this.allowLocomotion,
    required this.policyRevision,
    required this.appliedRevision,
    required this.upToDate,
  });

  final bool supported;
  final String state;
  final String action;
  final int actuatorCount;
  final bool faulted;
  final bool allowLocomotion;
  final int policyRevision;
  final int appliedRevision;
  final bool upToDate;

  factory MotionCapability.fromJson(Map<String, Object?> json) {
    return MotionCapability(
      supported: json['supported'] == true,
      state: json['state']?.toString() ?? 'idle',
      action: json['action']?.toString() ?? 'idle',
      actuatorCount: _asInt(json['actuator_count']),
      faulted: json['faulted'] == true,
      allowLocomotion: json['allow_locomotion'] == true,
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
