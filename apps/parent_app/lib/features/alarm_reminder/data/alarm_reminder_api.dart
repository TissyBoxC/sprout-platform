import '../../../core/network/api_client.dart';
import '../domain/alarm_reminder.dart';

/// Guardian-facing alarm reminder contracts. Every call is family-scoped on the
/// platform, so the client never sends an owner id.
abstract interface class AlarmReminderApi {
  /// Lists every alarm owned by the signed-in family.
  Future<List<Alarm>> fetchAlarms();

  /// Creates an alarm and returns the stored entry.
  Future<Alarm> createAlarm(AlarmDraft draft);

  /// Replaces an alarm and returns the stored entry.
  Future<Alarm> updateAlarm(String alarmId, AlarmDraft draft);

  /// Deletes one alarm owned by the signed-in family.
  Future<void> deleteAlarm(String alarmId);

  /// Lists the bounded firing history for the signed-in family.
  Future<List<AlarmEvent>> fetchEvents({int limit = 0});
}

/// Production implementation backed by the authenticated API client.
class HttpAlarmReminderApi implements AlarmReminderApi {
  const HttpAlarmReminderApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<List<Alarm>> fetchAlarms() async {
    final response = await _apiClient.get('/api/v1/alarm-reminders');
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing alarm reminder data');
    }
    final alarms = data['alarms'];
    if (alarms is! List) {
      return const <Alarm>[];
    }
    return alarms
        .whereType<Map>()
        .map((item) => Alarm.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<Alarm> createAlarm(AlarmDraft draft) async {
    final response = await _apiClient.post(
      '/api/v1/alarm-reminders',
      body: draft.toJson(),
    );
    return _requiredAlarm(response['data']);
  }

  @override
  Future<Alarm> updateAlarm(String alarmId, AlarmDraft draft) async {
    final response = await _apiClient.put(
      '/api/v1/alarm-reminders/${Uri.encodeComponent(alarmId)}',
      body: draft.toJson(),
    );
    return _requiredAlarm(response['data']);
  }

  @override
  Future<void> deleteAlarm(String alarmId) async {
    await _apiClient.delete(
      '/api/v1/alarm-reminders/${Uri.encodeComponent(alarmId)}',
    );
  }

  @override
  Future<List<AlarmEvent>> fetchEvents({int limit = 0}) async {
    final response = await _apiClient.get(
      '/api/v1/alarm-reminders/events',
      queryParameters: <String, String>{if (limit > 0) 'limit': '$limit'},
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing alarm event data');
    }
    final events = data['events'];
    if (events is! List) {
      return const <AlarmEvent>[];
    }
    return events
        .whereType<Map>()
        .map((item) => AlarmEvent.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }
}

Alarm _requiredAlarm(Object? data) {
  if (data is! Map) {
    throw const FormatException('missing alarm reminder');
  }
  final envelope = Map<String, Object?>.from(data);
  final alarm = envelope['alarm'] ?? envelope;
  if (alarm is! Map) {
    throw const FormatException('missing alarm reminder');
  }
  return Alarm.fromJson(Map<String, Object?>.from(alarm));
}
