/// One family-scoped scheduled alarm.
///
/// [weekdays] uses ISO-8601 numbering (1 = Monday ... 7 = Sunday). An empty set
/// means "every day", matching the platform contract.
class Alarm {
  const Alarm({
    required this.id,
    required this.childId,
    required this.label,
    required this.prompt,
    required this.alarmTime,
    required this.weekdays,
    required this.enabled,
    required this.snoozeMinutes,
  });

  final String id;
  final String childId;
  final String label;
  final String prompt;

  /// Local wall-clock time formatted as `HH:MM`.
  final String alarmTime;
  final List<int> weekdays;
  final bool enabled;
  final int snoozeMinutes;

  factory Alarm.fromJson(Map<String, Object?> json) {
    return Alarm(
      id: _string(json['id']),
      childId: _string(json['child_id']),
      label: _string(json['label']),
      prompt: _string(json['prompt']),
      alarmTime: _string(json['alarm_time']),
      weekdays: _intList(json['weekdays']),
      enabled: json['enabled'] == true,
      snoozeMinutes: _int(json['snooze_minutes'], fallback: 5),
    );
  }
}

/// Editable fields accepted when scheduling or editing an alarm.
class AlarmDraft {
  const AlarmDraft({
    required this.childId,
    required this.label,
    required this.prompt,
    required this.alarmTime,
    required this.weekdays,
    required this.enabled,
    required this.snoozeMinutes,
  });

  final String childId;
  final String label;
  final String prompt;
  final String alarmTime;
  final List<int> weekdays;
  final bool enabled;
  final int snoozeMinutes;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'child_id': childId,
      'label': label,
      'prompt': prompt,
      'alarm_time': alarmTime,
      'weekdays': weekdays,
      'enabled': enabled,
      'snooze_minutes': snoozeMinutes,
    };
  }
}

/// Firing states the platform reports for one alarm on one day.
enum AlarmState {
  ringing('ringing', '响铃中'),
  snoozed('snoozed', '稍后提醒'),
  dismissed('dismissed', '已关闭'),
  missed('missed', '已错过');

  const AlarmState(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static AlarmState? fromApiValue(String value) {
    for (final state in AlarmState.values) {
      if (state.apiValue == value) {
        return state;
      }
    }
    return null;
  }
}

/// One bounded alarm firing acknowledgement reported by a device.
class AlarmEvent {
  const AlarmEvent({
    required this.id,
    required this.alarmId,
    required this.deviceId,
    required this.alarmDate,
    required this.state,
    required this.reportedAt,
  });

  final String id;
  final String alarmId;
  final String deviceId;
  final String alarmDate;
  final AlarmState state;
  final DateTime reportedAt;

  factory AlarmEvent.fromJson(Map<String, Object?> json) {
    final state = AlarmState.fromApiValue(_string(json['state']));
    if (state == null) {
      throw const FormatException('unknown alarm state');
    }
    return AlarmEvent(
      id: _string(json['id']),
      alarmId: _string(json['alarm_id']),
      deviceId: _string(json['device_id']),
      alarmDate: _string(json['alarm_date']),
      state: state,
      reportedAt: _dateTime(json['reported_at']),
    );
  }
}

/// Stable weekday labels for the alarm editor.
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
  if (sorted.isEmpty || sorted.length == 7) {
    return '每天';
  }
  return sorted
      .map((weekday) => weekdayLabels[weekday] ?? '')
      .where((label) => label.isNotEmpty)
      .join('、');
}

String _string(Object? value) => value is String ? value : '';

int _int(Object? value, {required int fallback}) {
  if (value is num) {
    return value.toInt();
  }
  return fallback;
}

DateTime _dateTime(Object? value) {
  if (value is String) {
    return DateTime.tryParse(value)?.toLocal() ??
        DateTime.fromMillisecondsSinceEpoch(0);
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
