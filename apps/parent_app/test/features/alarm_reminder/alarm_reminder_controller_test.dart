import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/alarm_reminder/application/alarm_reminder_controller.dart';
import 'package:parent_app/features/alarm_reminder/data/alarm_reminder_api.dart';
import 'package:parent_app/features/alarm_reminder/domain/alarm_reminder.dart';

void main() {
  test('loads alarms and events on first build', () async {
    final api = _FakeAlarmReminderApi(
      alarms: [
        _alarm('alarm-1', 'child-1', '07:30', enabled: true),
        _alarm('alarm-2', 'child-2', '13:00', enabled: false),
      ],
      events: [_event('event-1', 'alarm-1', AlarmState.dismissed)],
    );
    final container = ProviderContainer(
      overrides: [alarmReminderApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(alarmReminderControllerProvider.future);

    expect(state.alarms, hasLength(2));
    expect(state.events, hasLength(1));
    expect(state.isEmpty, isFalse);
    expect(state.enabledAlarms.map((alarm) => alarm.id), ['alarm-1']);
  });

  test('creates an alarm then reloads the schedule', () async {
    final api = _FakeAlarmReminderApi(alarms: const []);
    final container = ProviderContainer(
      overrides: [alarmReminderApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(alarmReminderControllerProvider.future);
    final controller = container.read(alarmReminderControllerProvider.notifier);
    final created = await controller.createAlarm(_draft('起床', '07:00'));

    expect(created.label, '起床');
    final state = container.read(alarmReminderControllerProvider).value;
    expect(state?.alarms, hasLength(1));
  });

  test('updates an alarm then reloads', () async {
    final api = _FakeAlarmReminderApi(
      alarms: [_alarm('alarm-1', 'child-1', '07:00', enabled: true)],
    );
    final container = ProviderContainer(
      overrides: [alarmReminderApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(alarmReminderControllerProvider.future);
    final controller = container.read(alarmReminderControllerProvider.notifier);
    final updated = await controller.updateAlarm(
      'alarm-1',
      _draft('起床', '06:45', snoozeMinutes: 10),
    );

    expect(updated.alarmTime, '06:45');
    expect(updated.snoozeMinutes, 10);
  });

  test('setEnabled flips only the enabled flag', () async {
    final alarm = _alarm('alarm-1', 'child-1', '07:00', enabled: true);
    final api = _FakeAlarmReminderApi(alarms: [alarm]);
    final container = ProviderContainer(
      overrides: [alarmReminderApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(alarmReminderControllerProvider.future);
    final controller = container.read(alarmReminderControllerProvider.notifier);
    final updated = await controller.setEnabled(alarm, false);

    expect(updated.enabled, isFalse);
    expect(updated.label, alarm.label);
    expect(updated.alarmTime, '07:00');
    expect(api.lastDraft?.enabled, isFalse);
  });

  test('deletes an alarm then reloads', () async {
    final api = _FakeAlarmReminderApi(
      alarms: [_alarm('alarm-1', 'child-1', '07:00', enabled: true)],
    );
    final container = ProviderContainer(
      overrides: [alarmReminderApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(alarmReminderControllerProvider.future);
    final controller = container.read(alarmReminderControllerProvider.notifier);
    await controller.deleteAlarm('alarm-1');

    expect(api.deletedIds, ['alarm-1']);
    expect(
      container.read(alarmReminderControllerProvider).value?.alarms,
      isEmpty,
    );
  });

  test('propagates a load failure as an error state', () async {
    final api = _FakeAlarmReminderApi(alarms: const [], failLoad: true);
    final container = ProviderContainer(
      overrides: [alarmReminderApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final subscription = container.listen(
      alarmReminderControllerProvider,
      (_, _) {},
    );
    addTearDown(subscription.close);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(container.read(alarmReminderControllerProvider).hasError, isTrue);
  });
}

AlarmDraft _draft(
  String label,
  String alarmTime, {
  int snoozeMinutes = 5,
  bool enabled = true,
}) {
  return AlarmDraft(
    childId: 'child-1',
    label: label,
    prompt: '',
    alarmTime: alarmTime,
    weekdays: const [1, 2, 3, 4, 5, 6, 7],
    enabled: enabled,
    snoozeMinutes: snoozeMinutes,
  );
}

Alarm _alarm(
  String id,
  String childId,
  String alarmTime, {
  required bool enabled,
}) {
  return Alarm(
    id: id,
    childId: childId,
    label: '闹钟 $id',
    prompt: '',
    alarmTime: alarmTime,
    weekdays: const [1, 2, 3, 4, 5, 6, 7],
    enabled: enabled,
    snoozeMinutes: 5,
  );
}

AlarmEvent _event(String id, String alarmId, AlarmState state) {
  return AlarmEvent(
    id: id,
    alarmId: alarmId,
    deviceId: 'device-1',
    alarmDate: '2026-10-09',
    state: state,
    reportedAt: DateTime.utc(2026, 10, 9, 7, 35),
  );
}

class _FakeAlarmReminderApi implements AlarmReminderApi {
  _FakeAlarmReminderApi({
    required List<Alarm> alarms,
    List<AlarmEvent> events = const [],
    this.failLoad = false,
  }) : _alarms = [...alarms],
       _events = [...events];

  final List<Alarm> _alarms;
  final List<AlarmEvent> _events;
  final bool failLoad;
  AlarmDraft? lastDraft;
  final List<String> deletedIds = [];
  int _sequence = 0;

  @override
  Future<List<Alarm>> fetchAlarms() async {
    if (failLoad) {
      throw Exception('network down');
    }
    return List<Alarm>.unmodifiable(_alarms);
  }

  @override
  Future<List<AlarmEvent>> fetchEvents({int limit = 0}) async {
    if (failLoad) {
      throw Exception('network down');
    }
    return List<AlarmEvent>.unmodifiable(_events);
  }

  @override
  Future<Alarm> createAlarm(AlarmDraft draft) async {
    _sequence += 1;
    lastDraft = draft;
    final alarm = Alarm(
      id: 'alarm-new-$_sequence',
      childId: draft.childId,
      label: draft.label,
      prompt: draft.prompt,
      alarmTime: draft.alarmTime,
      weekdays: draft.weekdays,
      enabled: draft.enabled,
      snoozeMinutes: draft.snoozeMinutes,
    );
    _alarms.add(alarm);
    return alarm;
  }

  @override
  Future<Alarm> updateAlarm(String alarmId, AlarmDraft draft) async {
    lastDraft = draft;
    final alarm = Alarm(
      id: alarmId,
      childId: draft.childId,
      label: draft.label,
      prompt: draft.prompt,
      alarmTime: draft.alarmTime,
      weekdays: draft.weekdays,
      enabled: draft.enabled,
      snoozeMinutes: draft.snoozeMinutes,
    );
    final index = _alarms.indexWhere((item) => item.id == alarmId);
    if (index >= 0) {
      _alarms[index] = alarm;
    } else {
      _alarms.add(alarm);
    }
    return alarm;
  }

  @override
  Future<void> deleteAlarm(String alarmId) async {
    deletedIds.add(alarmId);
    _alarms.removeWhere((item) => item.id == alarmId);
  }
}
