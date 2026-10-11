import '../../../core/network/api_client.dart';
import '../domain/companionship.dart';

/// Guardian-facing companionship contracts: the growth timeline and habit
/// reminders. Every call is family-scoped on the platform, so the client never
/// sends an owner id.
abstract interface class CompanionshipApi {
  /// Lists the growth timeline, newest first.
  ///
  /// Passing [childId] narrows the result to one child.
  Future<List<GrowthRecord>> fetchGrowthRecords({String? childId});

  /// Appends one growth record and returns the stored entry.
  Future<GrowthRecord> createGrowthRecord({
    required String childId,
    required GrowthCategory category,
    required String title,
    required String note,
  });

  /// Deletes one growth record owned by the signed-in family.
  Future<void> deleteGrowthRecord(String recordId);

  /// Lists every habit reminder in the family.
  Future<List<HabitReminder>> fetchHabitReminders();

  /// Creates a reminder and returns the stored entry.
  Future<HabitReminder> createHabitReminder(HabitReminderDraft draft);

  /// Replaces a reminder and returns the stored entry.
  Future<HabitReminder> updateHabitReminder(
    String reminderId,
    HabitReminderDraft draft,
  );

  /// Deletes one habit reminder owned by the signed-in family.
  Future<void> deleteHabitReminder(String reminderId);
}

/// Production implementation backed by the authenticated API client.
class HttpCompanionshipApi implements CompanionshipApi {
  const HttpCompanionshipApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<List<GrowthRecord>> fetchGrowthRecords({String? childId}) async {
    final response = await _apiClient.get(
      '/api/v1/companionship/growth-records',
      queryParameters: <String, String>{
        if (childId != null && childId.isNotEmpty) 'child_id': childId,
      },
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing growth record data');
    }
    final records = data['growth_records'];
    if (records is! List) {
      return const <GrowthRecord>[];
    }
    return records
        .whereType<Map>()
        .map((item) => GrowthRecord.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<GrowthRecord> createGrowthRecord({
    required String childId,
    required GrowthCategory category,
    required String title,
    required String note,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/companionship/growth-records',
      body: <String, Object?>{
        'child_id': childId,
        'category': category.apiValue,
        'title': title,
        'note': note,
      },
    );
    return _requiredGrowthRecord(response['data']);
  }

  @override
  Future<void> deleteGrowthRecord(String recordId) async {
    await _apiClient.delete(
      '/api/v1/companionship/growth-records/${Uri.encodeComponent(recordId)}',
    );
  }

  @override
  Future<List<HabitReminder>> fetchHabitReminders() async {
    final response = await _apiClient.get(
      '/api/v1/companionship/habit-reminders',
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing habit reminder data');
    }
    final reminders = data['habit_reminders'];
    if (reminders is! List) {
      return const <HabitReminder>[];
    }
    return reminders
        .whereType<Map>()
        .map((item) => HabitReminder.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<HabitReminder> createHabitReminder(HabitReminderDraft draft) async {
    final response = await _apiClient.post(
      '/api/v1/companionship/habit-reminders',
      body: draft.toJson(),
    );
    return _requiredHabitReminder(response['data']);
  }

  @override
  Future<HabitReminder> updateHabitReminder(
    String reminderId,
    HabitReminderDraft draft,
  ) async {
    final response = await _apiClient.put(
      '/api/v1/companionship/habit-reminders/${Uri.encodeComponent(reminderId)}',
      body: draft.toJson(),
    );
    return _requiredHabitReminder(response['data']);
  }

  @override
  Future<void> deleteHabitReminder(String reminderId) async {
    await _apiClient.delete(
      '/api/v1/companionship/habit-reminders/${Uri.encodeComponent(reminderId)}',
    );
  }
}

GrowthRecord _requiredGrowthRecord(Object? data) {
  if (data is! Map) {
    throw const FormatException('missing growth record');
  }
  final envelope = Map<String, Object?>.from(data);
  final record = envelope['growth_record'] ?? envelope;
  if (record is! Map) {
    throw const FormatException('missing growth record');
  }
  return GrowthRecord.fromJson(Map<String, Object?>.from(record));
}

HabitReminder _requiredHabitReminder(Object? data) {
  if (data is! Map) {
    throw const FormatException('missing habit reminder');
  }
  final envelope = Map<String, Object?>.from(data);
  final reminder = envelope['habit_reminder'] ?? envelope;
  if (reminder is! Map) {
    throw const FormatException('missing habit reminder');
  }
  return HabitReminder.fromJson(Map<String, Object?>.from(reminder));
}
