/// Growth record categories shown on the guardian timeline.
enum GrowthCategory {
  milestone('milestone', '里程碑'),
  encouragement('encouragement', '鼓励'),
  habit('habit', '习惯'),
  emotion('emotion', '情绪');

  const GrowthCategory(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static GrowthCategory? fromApiValue(String value) {
    for (final category in GrowthCategory.values) {
      if (category.apiValue == value) {
        return category;
      }
    }
    return null;
  }
}

/// One guardian-visible growth timeline entry.
class GrowthRecord {
  const GrowthRecord({
    required this.id,
    required this.childId,
    required this.category,
    required this.title,
    required this.note,
    required this.recordedAt,
  });

  final String id;
  final String childId;
  final GrowthCategory category;
  final String title;
  final String note;
  final DateTime recordedAt;

  factory GrowthRecord.fromJson(Map<String, Object?> json) {
    final category = GrowthCategory.fromApiValue(
      _string(json['category']),
    );
    if (category == null) {
      throw const FormatException('unknown growth category');
    }
    return GrowthRecord(
      id: _string(json['id']),
      childId: _string(json['child_id']),
      category: category,
      title: _string(json['title']),
      note: _string(json['note']),
      recordedAt: _dateTime(json['recorded_at']),
    );
  }
}

/// One family-scoped habit reminder.
///
/// [weekdays] uses ISO-8601 numbering (1 = Monday ... 7 = Sunday) so the
/// guardian's mental model matches the stored set.
class HabitReminder {
  const HabitReminder({
    required this.id,
    required this.childId,
    required this.label,
    required this.prompt,
    required this.reminderTime,
    required this.weekdays,
    required this.enabled,
  });

  final String id;
  final String childId;
  final String label;
  final String prompt;
  final String reminderTime;
  final List<int> weekdays;
  final bool enabled;

  factory HabitReminder.fromJson(Map<String, Object?> json) {
    return HabitReminder(
      id: _string(json['id']),
      childId: _string(json['child_id']),
      label: _string(json['label']),
      prompt: _string(json['prompt']),
      reminderTime: _string(json['reminder_time']),
      weekdays: _intList(json['weekdays']),
      enabled: json['enabled'] == true,
    );
  }
}

/// Editable fields accepted when scheduling or editing a reminder.
class HabitReminderDraft {
  const HabitReminderDraft({
    required this.childId,
    required this.label,
    required this.prompt,
    required this.reminderTime,
    required this.weekdays,
    required this.enabled,
  });

  final String childId;
  final String label;
  final String prompt;
  final String reminderTime;
  final List<int> weekdays;
  final bool enabled;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'child_id': childId,
      'label': label,
      'prompt': prompt,
      'reminder_time': reminderTime,
      'weekdays': weekdays,
      'enabled': enabled,
    };
  }
}

/// Stable weekday labels for the reminder editor.
const Map<int, String> weekdayLabels = <int, String>{
  1: '周一',
  2: '周二',
  3: '周三',
  4: '周四',
  5: '周五',
  6: '周六',
  7: '周日',
};

/// Formats an ISO weekday set for display, collapsing the full week.
String formatWeekdays(List<int> weekdays) {
  final sorted = [...weekdays]..sort();
  if (sorted.length == 7) {
    return '每天';
  }
  return sorted
      .map((weekday) => weekdayLabels[weekday] ?? '')
      .where((label) => label.isNotEmpty)
      .join('、');
}

String _string(Object? value) => value is String ? value : '';

DateTime _dateTime(Object? value) {
  if (value is String) {
    return DateTime.tryParse(value)?.toLocal() ?? DateTime.fromMillisecondsSinceEpoch(0);
  }
  return DateTime.fromMillisecondsSinceEpoch(0);
}

List<int> _intList(Object? value) {
  if (value is! List) {
    return const <int>[];
  }
  return value
      .whereType<num>()
      .map((item) => item.toInt())
      .where((item) => item >= 1 && item <= 7)
      .toList(growable: false);
}
