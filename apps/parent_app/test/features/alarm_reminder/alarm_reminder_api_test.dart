import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/alarm_reminder/data/alarm_reminder_api.dart';
import 'package:parent_app/features/alarm_reminder/domain/alarm_reminder.dart';

void main() {
  test('parses the alarm envelope including weekdays and snooze', () async {
    final api = HttpAlarmReminderApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'alarms': <Object?>[
              <String, Object?>{
                'id': 'alarm-1',
                'child_id': 'child-1',
                'label': '起床',
                'prompt': '该起床啦',
                'alarm_time': '07:30',
                'weekdays': <Object?>[1, 3, 5],
                'enabled': true,
                'snooze_minutes': 10,
                'created_at': '2026-10-09T00:00:00Z',
                'updated_at': '2026-10-09T00:00:00Z',
              },
            ],
          },
        },
      ),
    );

    final alarms = await api.fetchAlarms();

    expect(alarms, hasLength(1));
    expect(alarms.single.label, '起床');
    expect(alarms.single.alarmTime, '07:30');
    expect(alarms.single.weekdays, <int>[1, 3, 5]);
    expect(alarms.single.snoozeMinutes, 10);
    expect(alarms.single.enabled, isTrue);
  });

  test('defaults snooze to five minutes when the field is missing', () async {
    final api = HttpAlarmReminderApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'alarms': <Object?>[
              <String, Object?>{
                'id': 'alarm-1',
                'child_id': 'child-1',
                'label': '午睡',
                'prompt': '',
                'alarm_time': '13:00',
                'weekdays': <Object?>[],
                'enabled': false,
              },
            ],
          },
        },
      ),
    );

    final alarms = await api.fetchAlarms();

    expect(alarms.single.snoozeMinutes, 5);
    expect(alarms.single.enabled, isFalse);
    expect(formatWeekdays(alarms.single.weekdays), '每天');
  });

  test('creates an alarm with the draft JSON body', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'alarm': <String, Object?>{
            'id': 'alarm-1',
            'child_id': 'child-1',
            'label': '刷牙',
            'prompt': '',
            'alarm_time': '20:00',
            'weekdays': <Object?>[1, 2, 3, 4, 5],
            'enabled': true,
            'snooze_minutes': 5,
          },
        },
      },
    );
    final api = HttpAlarmReminderApi(client);

    await api.createAlarm(
      const AlarmDraft(
        childId: 'child-1',
        label: '刷牙',
        prompt: '',
        alarmTime: '20:00',
        weekdays: [1, 2, 3, 4, 5],
        enabled: true,
        snoozeMinutes: 5,
      ),
    );

    expect(client.lastPostPath, '/api/v1/alarm-reminders');
    expect(client.lastPostBody, <String, Object?>{
      'child_id': 'child-1',
      'label': '刷牙',
      'prompt': '',
      'alarm_time': '20:00',
      'weekdays': <int>[1, 2, 3, 4, 5],
      'enabled': true,
      'snooze_minutes': 5,
    });
  });

  test('updates an alarm on its encoded path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'alarm': <String, Object?>{
            'id': 'alarm 1',
            'child_id': 'child-1',
            'label': '起床',
            'prompt': '',
            'alarm_time': '06:45',
            'weekdays': <Object?>[1],
            'enabled': true,
            'snooze_minutes': 5,
          },
        },
      },
    );
    final api = HttpAlarmReminderApi(client);

    final updated = await api.updateAlarm(
      'alarm 1',
      const AlarmDraft(
        childId: 'child-1',
        label: '起床',
        prompt: '',
        alarmTime: '06:45',
        weekdays: [1],
        enabled: true,
        snoozeMinutes: 5,
      ),
    );

    expect(updated.alarmTime, '06:45');
    expect(client.lastPutPath, '/api/v1/alarm-reminders/alarm%201');
  });

  test('deletes an alarm on its encoded path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{'data': <String, Object?>{}},
    );
    final api = HttpAlarmReminderApi(client);

    await api.deleteAlarm('alarm 1');

    expect(client.deletePaths, <String>['/api/v1/alarm-reminders/alarm%201']);
  });

  test('parses firing events and forwards the limit', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'events': <Object?>[
            <String, Object?>{
              'id': 'event-1',
              'alarm_id': 'alarm-1',
              'device_id': 'device-1',
              'alarm_date': '2026-10-09',
              'state': 'snoozed',
              'reported_at': '2026-10-09T07:35:00Z',
            },
          ],
        },
      },
    );
    final api = HttpAlarmReminderApi(client);

    final events = await api.fetchEvents(limit: 20);

    expect(events, hasLength(1));
    expect(events.single.state, AlarmState.snoozed);
    expect(events.single.alarmDate, '2026-10-09');
    expect(client.lastQueryParameters, <String, String>{'limit': '20'});
  });

  test('collapses a full week and an empty set to 每天', () {
    expect(formatWeekdays(const [1, 2, 3, 4, 5, 6, 7]), '每天');
    expect(formatWeekdays(const []), '每天');
    expect(formatWeekdays(const [5, 1, 3]), '周一、周三、周五');
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  Map<String, String>? lastQueryParameters;
  String? lastPostPath;
  Object? lastPostBody;
  String? lastPutPath;
  Object? lastPutBody;
  final List<String> deletePaths = <String>[];

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    lastQueryParameters = queryParameters;
    return response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    lastPostPath = path;
    lastPostBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    lastPutPath = path;
    lastPutBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> delete(String path) async {
    deletePaths.add(path);
    return response;
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }
}
