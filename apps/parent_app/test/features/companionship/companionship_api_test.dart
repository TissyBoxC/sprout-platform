import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/companionship/data/companionship_api.dart';
import 'package:parent_app/features/companionship/domain/companionship.dart';

void main() {
  test('parses the growth record envelope and forwards the child filter',
      () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'growth_records': <Object?>[
            <String, Object?>{
              'id': 'record-1',
              'child_id': 'child-1',
              'category': 'milestone',
              'title': '第一次自己穿鞋',
              'note': '花了很久但很认真',
              'recorded_at': '2026-10-09T00:00:00Z',
            },
          ],
        },
      },
    );
    final api = HttpCompanionshipApi(client);

    final records = await api.fetchGrowthRecords(childId: 'child-1');

    expect(records, hasLength(1));
    expect(records.single.category, GrowthCategory.milestone);
    expect(records.single.title, '第一次自己穿鞋');
    expect(client.lastQueryParameters, <String, String>{'child_id': 'child-1'});
  });

  test('omits the child filter when listing the whole family', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'growth_records': <Object?>[]},
      },
    );
    final api = HttpCompanionshipApi(client);

    final records = await api.fetchGrowthRecords();

    expect(records, isEmpty);
    expect(client.lastQueryParameters, isEmpty);
  });

  test('creates a growth record with the category wire value', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'growth_record': <String, Object?>{
            'id': 'record-1',
            'child_id': 'child-1',
            'category': 'encouragement',
            'title': '主动整理玩具',
            'note': '',
            'recorded_at': '2026-10-09T00:00:00Z',
          },
        },
      },
    );
    final api = HttpCompanionshipApi(client);

    final created = await api.createGrowthRecord(
      childId: 'child-1',
      category: GrowthCategory.encouragement,
      title: '主动整理玩具',
      note: '',
    );

    expect(created.category, GrowthCategory.encouragement);
    expect(client.lastPostPath, '/api/v1/companionship/growth-records');
    expect(client.lastPostBody, <String, Object?>{
      'child_id': 'child-1',
      'category': 'encouragement',
      'title': '主动整理玩具',
      'note': '',
    });
  });

  test('deletes a growth record on its encoded path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{'data': <String, Object?>{}},
    );
    final api = HttpCompanionshipApi(client);

    await api.deleteGrowthRecord('record 1');

    expect(client.deletePaths, <String>[
      '/api/v1/companionship/growth-records/record%201',
    ]);
  });

  test('parses the habit reminder envelope including weekdays', () async {
    final api = HttpCompanionshipApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'habit_reminders': <Object?>[
              <String, Object?>{
                'id': 'reminder-1',
                'child_id': 'child-1',
                'label': '刷牙',
                'prompt': '该刷牙啦',
                'reminder_time': '07:30',
                'weekdays': <Object?>[1, 3, 5],
                'enabled': true,
              },
            ],
          },
        },
      ),
    );

    final reminders = await api.fetchHabitReminders();

    expect(reminders, hasLength(1));
    expect(reminders.single.label, '刷牙');
    expect(reminders.single.weekdays, <int>[1, 3, 5]);
    expect(reminders.single.enabled, isTrue);
    expect(formatWeekdays(reminders.single.weekdays), '周一、周三、周五');
  });

  test('posts a reminder draft as its JSON body', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'habit_reminder': <String, Object?>{
            'id': 'reminder-1',
            'child_id': 'child-1',
            'label': '喝水',
            'prompt': '',
            'reminder_time': '09:00',
            'weekdays': <Object?>[1, 2, 3, 4, 5, 6, 7],
            'enabled': true,
          },
        },
      },
    );
    final api = HttpCompanionshipApi(client);

    await api.createHabitReminder(
      const HabitReminderDraft(
        childId: 'child-1',
        label: '喝水',
        prompt: '',
        reminderTime: '09:00',
        weekdays: [1, 2, 3, 4, 5, 6, 7],
        enabled: true,
      ),
    );

    expect(client.lastPostPath, '/api/v1/companionship/habit-reminders');
    expect(client.lastPostBody, <String, Object?>{
      'child_id': 'child-1',
      'label': '喝水',
      'prompt': '',
      'reminder_time': '09:00',
      'weekdays': <int>[1, 2, 3, 4, 5, 6, 7],
      'enabled': true,
    });
  });

  test('collapses a full week of weekdays to 每天', () {
    expect(formatWeekdays(const [1, 2, 3, 4, 5, 6, 7]), '每天');
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
