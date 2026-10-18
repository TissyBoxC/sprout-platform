import '../../child_profile/domain/child_profile.dart';

/// One local-time window when the device must stay quiet.
///
/// The platform rejects periods whose start and end are identical because the
/// intent would be ambiguous. Periods may cross midnight.
class DisabledPeriod {
  const DisabledPeriod({required this.startTime, required this.endTime});

  final String startTime;
  final String endTime;

  factory DisabledPeriod.fromJson(Map<String, Object?> json) {
    return DisabledPeriod(
      startTime: _requiredTime(json['start_time'], 'start_time'),
      endTime: _requiredTime(json['end_time'], 'end_time'),
    );
  }

  Map<String, Object?> toJson() {
    return {'start_time': startTime, 'end_time': endTime};
  }
}

/// Time, content, and volume limits applied to one child.
///
/// Realtime voice behaviour (continuous conversation, barge-in, far-field) is
/// carried alongside the time and content limits so a guardian can tune how
/// the device holds a conversation without a separate screen.
class VoiceConversationPolicy {
  const VoiceConversationPolicy({
    required this.continuousConversationEnabled,
    required this.idleWindowSeconds,
    required this.bargeInEnabled,
    required this.farFieldEnabled,
  });

  final bool continuousConversationEnabled;
  final int idleWindowSeconds;
  final bool bargeInEnabled;
  final bool farFieldEnabled;

  factory VoiceConversationPolicy.fromJson(Map<String, Object?> json) {
    final idleWindow = json['idle_window_seconds'];
    return VoiceConversationPolicy(
      continuousConversationEnabled: _optionalBool(
        json['continuous_conversation_enabled'],
        fallback: false,
      ),
      idleWindowSeconds: idleWindow is num && idleWindow.isFinite
          ? idleWindow.toInt().clamp(1, 120)
          : 8,
      bargeInEnabled: _optionalBool(json['barge_in_enabled'], fallback: false),
      farFieldEnabled: _optionalBool(
        json['far_field_enabled'],
        fallback: false,
      ),
    );
  }

  Map<String, Object?> toJson() {
    return {
      'continuous_conversation_enabled': continuousConversationEnabled,
      'idle_window_seconds': idleWindowSeconds,
      'barge_in_enabled': bargeInEnabled,
      'far_field_enabled': farFieldEnabled,
    };
  }
}

class ParentPolicy {
  const ParentPolicy({
    required this.policyId,
    required this.familyId,
    required this.childId,
    required this.policyVersion,
    required this.dailyLimitMinutes,
    required this.allowedCategories,
    required this.disabledPeriods,
    required this.maxVolumePercent,
    required this.voiceConversation,
    required this.updatedAt,
  });

  final String policyId;
  final String familyId;
  final String childId;
  final int policyVersion;
  final int dailyLimitMinutes;
  final List<ChildContentCategory> allowedCategories;
  final List<DisabledPeriod> disabledPeriods;
  final int maxVolumePercent;
  final VoiceConversationPolicy voiceConversation;
  final DateTime updatedAt;

  factory ParentPolicy.fromJson(Map<String, Object?> json) {
    return ParentPolicy(
      policyId: _requiredString(json['policy_id'], 'policy_id'),
      familyId: _requiredString(json['family_id'], 'family_id'),
      childId: _requiredString(json['child_id'], 'child_id'),
      policyVersion: _requiredInt(json['policy_version'], 'policy_version'),
      dailyLimitMinutes: _requiredInt(
        json['daily_limit_minutes'],
        'daily_limit_minutes',
      ),
      allowedCategories: _stringList(
        json['allowed_categories'],
      ).map(ChildContentCategoryMetadata.fromWireValue).toList(growable: false),
      disabledPeriods: _mapList(
        json['disabled_periods'],
      ).map(DisabledPeriod.fromJson).toList(growable: false),
      maxVolumePercent: _requiredInt(
        json['max_volume_percent'],
        'max_volume_percent',
      ),
      voiceConversation: _voiceConversation(json['voice_conversation']),
      updatedAt: _requiredDateTime(json['updated_at'], 'updated_at'),
    );
  }
}

VoiceConversationPolicy _voiceConversation(Object? value) {
  if (value is Map) {
    return VoiceConversationPolicy.fromJson(Map<String, Object?>.from(value));
  }
  // Older platforms predate the voice controls. Disable features that may
  // change microphone behavior and keep the existing eight-second idle default.
  return const VoiceConversationPolicy(
    continuousConversationEnabled: false,
    idleWindowSeconds: 8,
    bargeInEnabled: false,
    farFieldEnabled: false,
  );
}

/// Validated values collected by the time and content policy form.
class ParentPolicyDraft {
  const ParentPolicyDraft({
    required this.policyVersion,
    required this.dailyLimitMinutes,
    required this.allowedCategories,
    required this.disabledPeriods,
    required this.maxVolumePercent,
    required this.voiceConversation,
  });

  final int policyVersion;
  final int dailyLimitMinutes;
  final List<ChildContentCategory> allowedCategories;
  final List<DisabledPeriod> disabledPeriods;
  final int maxVolumePercent;
  final VoiceConversationPolicy voiceConversation;

  Map<String, Object?> toJson() {
    return {
      'policy_version': policyVersion,
      'daily_limit_minutes': dailyLimitMinutes,
      'allowed_categories': allowedCategories
          .map((category) => category.wireValue)
          .toList(growable: false),
      'disabled_periods': disabledPeriods
          .map((period) => period.toJson())
          .toList(growable: false),
      'max_volume_percent': maxVolumePercent,
      'voice_conversation': voiceConversation.toJson(),
    };
  }
}

String _requiredString(Object? value, String field) {
  if (value is String && value.trim().isNotEmpty) {
    return value.trim();
  }
  throw FormatException('missing $field');
}

int _requiredInt(Object? value, String field) {
  if (value is num) {
    return value.toInt();
  }
  throw FormatException('missing $field');
}

bool _optionalBool(Object? value, {required bool fallback}) {
  if (value is bool) {
    return value;
  }
  return fallback;
}

DateTime _requiredDateTime(Object? value, String field) {
  if (value is String) {
    final parsed = DateTime.tryParse(value);
    if (parsed != null) {
      return parsed;
    }
  }
  throw FormatException('missing $field');
}

String _requiredTime(Object? value, String field) {
  if (value is String && value.isNotEmpty) {
    return value;
  }
  throw FormatException('missing $field');
}

List<String> _stringList(Object? value) {
  if (value is! List) {
    return const [];
  }
  return value.whereType<String>().toList(growable: false);
}

List<Map<String, Object?>> _mapList(Object? value) {
  if (value is! List) {
    return const [];
  }
  return value
      .whereType<Map>()
      .map((item) => Map<String, Object?>.from(item))
      .where(_isDisabledPeriod)
      .toList(growable: false);
}

bool _isDisabledPeriod(Map<String, Object?> period) {
  final start = period['start_time'];
  final end = period['end_time'];
  return start is String &&
      start.isNotEmpty &&
      end is String &&
      end.isNotEmpty &&
      start != end;
}
