/// Guardian-facing learning-visualization contract types.
///
/// The platform stores only the family policy and bounded visualization status
/// scalars; no frame buffers, touch traces or captured media flow through this
/// client.
library;

/// Feedback-style wire values shared with the platform.
enum LearningFeedbackStyle {
  neutral('neutral', '中性'),
  encouraging('encouraging', '鼓励'),
  playful('playful', '活泼'),
  celebration('celebration', '庆祝');

  const LearningFeedbackStyle(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static LearningFeedbackStyle fromApiValue(String value) {
    for (final style in LearningFeedbackStyle.values) {
      if (style.apiValue == value) {
        return style;
      }
    }
    return LearningFeedbackStyle.neutral;
  }
}

/// Animation-level wire values shared with the platform.
enum LearningAnimationLevel {
  off('off', '关闭'),
  subtle('subtle', '轻微'),
  full('full', '完整');

  const LearningAnimationLevel(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static LearningAnimationLevel fromApiValue(String value) {
    for (final level in LearningAnimationLevel.values) {
      if (level.apiValue == value) {
        return level;
      }
    }
    return LearningAnimationLevel.off;
  }
}

/// One validated learning-visualization policy for the signed-in family.
class LearningVisualizationPolicy {
  const LearningVisualizationPolicy({
    required this.enabled,
    required this.feedbackStyle,
    required this.animationLevel,
    required this.touchFeedbackEnabled,
    required this.rewardEffectsEnabled,
    required this.lessonLinkageEnabled,
    required this.maxSessionMinutes,
    required this.revision,
  });

  const LearningVisualizationPolicy.conservative()
    : enabled = false,
      feedbackStyle = LearningFeedbackStyle.neutral,
      animationLevel = LearningAnimationLevel.off,
      touchFeedbackEnabled = true,
      rewardEffectsEnabled = true,
      lessonLinkageEnabled = true,
      maxSessionMinutes = 20,
      revision = 0;

  final bool enabled;
  final LearningFeedbackStyle feedbackStyle;
  final LearningAnimationLevel animationLevel;
  final bool touchFeedbackEnabled;
  final bool rewardEffectsEnabled;
  final bool lessonLinkageEnabled;
  final int maxSessionMinutes;
  final int revision;

  factory LearningVisualizationPolicy.fromJson(Map<String, Object?> json) {
    return LearningVisualizationPolicy(
      enabled: json['enabled'] == true,
      feedbackStyle: LearningFeedbackStyle.fromApiValue(
        json['feedback_style']?.toString() ?? 'neutral',
      ),
      animationLevel: LearningAnimationLevel.fromApiValue(
        json['animation_level']?.toString() ?? 'off',
      ),
      touchFeedbackEnabled: json['touch_feedback_enabled'] != false,
      rewardEffectsEnabled: json['reward_effects_enabled'] != false,
      lessonLinkageEnabled: json['lesson_linkage_enabled'] != false,
      maxSessionMinutes: _int(json['max_session_minutes'], fallback: 20),
      revision: _int(json['revision'], fallback: 0),
    );
  }
}

/// Editable policy fields accepted when publishing a new revision.
class LearningVisualizationDraft {
  const LearningVisualizationDraft({
    required this.enabled,
    required this.feedbackStyle,
    required this.animationLevel,
    required this.touchFeedbackEnabled,
    required this.rewardEffectsEnabled,
    required this.lessonLinkageEnabled,
    required this.maxSessionMinutes,
  });

  final bool enabled;
  final LearningFeedbackStyle feedbackStyle;
  final LearningAnimationLevel animationLevel;
  final bool touchFeedbackEnabled;
  final bool rewardEffectsEnabled;
  final bool lessonLinkageEnabled;
  final int maxSessionMinutes;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'enabled': enabled,
      'feedback_style': feedbackStyle.apiValue,
      'animation_level': animationLevel.apiValue,
      'touch_feedback_enabled': touchFeedbackEnabled,
      'reward_effects_enabled': rewardEffectsEnabled,
      'lesson_linkage_enabled': lessonLinkageEnabled,
      'max_session_minutes': maxSessionMinutes,
    };
  }
}

/// One device's latest bounded learning-visualization status.
class LearningVisualizationDevice {
  const LearningVisualizationDevice({
    required this.deviceId,
    required this.supported,
    required this.touchSupported,
    required this.linkedLessonId,
    required this.visualState,
    required this.feedbackStyle,
    required this.animationLevel,
    required this.feedbackEvents,
    required this.sessionMinutes,
    required this.upToDate,
  });

  final String deviceId;
  final bool supported;
  final bool touchSupported;
  final String linkedLessonId;
  final String visualState;
  final String feedbackStyle;
  final String animationLevel;
  final int feedbackEvents;
  final int sessionMinutes;
  final bool upToDate;

  bool get running => visualState == 'lesson' || visualState == 'feedback';

  factory LearningVisualizationDevice.fromJson(Map<String, Object?> json) {
    return LearningVisualizationDevice(
      deviceId: json['device_id']?.toString() ?? '',
      supported: json['visualization_supported'] == true,
      touchSupported: json['touch_supported'] == true,
      linkedLessonId: json['linked_lesson_id']?.toString() ?? '',
      visualState: json['visual_state']?.toString() ?? 'idle',
      feedbackStyle: json['feedback_style']?.toString() ?? 'neutral',
      animationLevel: json['animation_level']?.toString() ?? 'off',
      feedbackEvents: _int(json['feedback_events'], fallback: 0),
      sessionMinutes: _int(json['session_minutes'], fallback: 0),
      upToDate: json['up_to_date'] == true,
    );
  }
}

/// Stable Chinese label for a visual state read from the platform.
String visualStateLabel(String state) {
  return switch (state) {
    'idle' => '空闲',
    'lesson' => '学习中',
    'feedback' => '反馈中',
    'reward' => '奖励中',
    'paused' => '已暂停',
    'error' => '异常',
    _ => '未知',
  };
}

/// Stable Chinese label for an animation level read from the platform.
String animationLevelLabel(String level) {
  return switch (level) {
    'off' => '关闭',
    'subtle' => '轻微',
    'full' => '完整',
    _ => '未知',
  };
}

/// Stable Chinese label for a feedback style read from the platform.
String feedbackStyleLabel(String style) {
  return switch (style) {
    'neutral' => '中性',
    'encouraging' => '鼓励',
    'playful' => '活泼',
    'celebration' => '庆祝',
    _ => style,
  };
}

int _int(Object? value, {required int fallback}) {
  if (value is num) {
    return value.toInt();
  }
  return fallback;
}
