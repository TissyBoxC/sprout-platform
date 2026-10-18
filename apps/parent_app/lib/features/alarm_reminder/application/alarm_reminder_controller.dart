import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/alarm_reminder_api.dart';
import '../domain/alarm_reminder.dart';

/// Complete state rendered by the alarm reminder page.
class AlarmReminderState {
  const AlarmReminderState({required this.alarms, required this.events});

  const AlarmReminderState.initial()
    : alarms = const <Alarm>[],
      events = const <AlarmEvent>[];

  final List<Alarm> alarms;
  final List<AlarmEvent> events;

  bool get isEmpty => alarms.isEmpty;

  /// Enabled alarms ordered by wall-clock time for a quick daily overview.
  List<Alarm> get enabledAlarms {
    final result = alarms.where((alarm) => alarm.enabled).toList()
      ..sort((a, b) => a.alarmTime.compareTo(b.alarmTime));
    return result;
  }
}

/// Owns the family alarm schedule so the page can refresh and mutate it without
/// holding a device session.
class AlarmReminderController extends AsyncNotifier<AlarmReminderState> {
  late AlarmReminderApi _api;

  @override
  Future<AlarmReminderState> build() async {
    _api = ref.read(alarmReminderApiProvider);
    return _load();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  /// Creates one alarm and reloads the schedule.
  Future<Alarm> createAlarm(AlarmDraft draft) async {
    final created = await _api.createAlarm(draft);
    await refresh();
    return created;
  }

  /// Replaces one alarm and reloads the schedule.
  Future<Alarm> updateAlarm(String alarmId, AlarmDraft draft) async {
    final updated = await _api.updateAlarm(alarmId, draft);
    await refresh();
    return updated;
  }

  /// Flips only the enabled flag of one alarm, preserving its other fields.
  Future<Alarm> setEnabled(Alarm alarm, bool enabled) async {
    final updated = await _api.updateAlarm(
      alarm.id,
      AlarmDraft(
        childId: alarm.childId,
        label: alarm.label,
        prompt: alarm.prompt,
        alarmTime: alarm.alarmTime,
        weekdays: alarm.weekdays,
        enabled: enabled,
        snoozeMinutes: alarm.snoozeMinutes,
      ),
    );
    await refresh();
    return updated;
  }

  /// Deletes one alarm and reloads the schedule.
  Future<void> deleteAlarm(String alarmId) async {
    await _api.deleteAlarm(alarmId);
    await refresh();
  }

  Future<AlarmReminderState> _load() async {
    final alarms = await _api.fetchAlarms();
    final events = await _api.fetchEvents(limit: 20);
    return AlarmReminderState(alarms: alarms, events: events);
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final alarmReminderApiProvider = Provider<AlarmReminderApi>((ref) {
  return HttpAlarmReminderApi(ref.read(apiClientProvider));
});

final alarmReminderControllerProvider =
    AsyncNotifierProvider<AlarmReminderController, AlarmReminderState>(
      AlarmReminderController.new,
    );
