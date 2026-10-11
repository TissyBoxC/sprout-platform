import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/companionship/application/companionship_controller.dart';
import 'package:parent_app/features/companionship/data/companionship_api.dart';
import 'package:parent_app/features/companionship/domain/companionship.dart';

void main() {
  test('loads growth records and reminders on first build', () async {
    final api = _FakeCompanionshipApi(
      records: [
        _record('record-1', 'child-1', GrowthCategory.milestone),
        _record('record-2', 'child-2', GrowthCategory.habit),
      ],
      reminders: [_reminder('reminder-1', 'child-1')],
    );
    final container = ProviderContainer(
      overrides: [companionshipApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(companionshipControllerProvider.future);

    expect(state.records, hasLength(2));
    expect(state.reminders, hasLength(1));
    expect(state.recordChildIds, ['child-1', 'child-2']);
    expect(state.childFilter, isNull);
    expect(api.lastChildFilter, isNull);
  });

  test('filters the timeline to one child and clears the filter again',
      () async {
    final api = _FakeCompanionshipApi(
      records: [_record('record-1', 'child-1', GrowthCategory.milestone)],
    );
    final container = ProviderContainer(
      overrides: [companionshipApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(companionshipControllerProvider.future);
    final controller = container.read(
      companionshipControllerProvider.notifier,
    );

    await controller.filterByChild('child-1');
    expect(api.lastChildFilter, 'child-1');
    expect(
      container.read(companionshipControllerProvider).value?.childFilter,
      'child-1',
    );

    // Filtering by the same child is a no-op: no extra request is sent.
    final before = api.growthCalls;
    await controller.filterByChild('child-1');
    expect(api.growthCalls, before);

    await controller.filterByChild(null);
    expect(api.lastChildFilter, isNull);
  });

  test('creates a growth record then reloads the timeline', () async {
    final api = _FakeCompanionshipApi(records: const []);
    final container = ProviderContainer(
      overrides: [companionshipApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(companionshipControllerProvider.future);
    final controller = container.read(
      companionshipControllerProvider.notifier,
    );
    final created = await controller.createGrowthRecord(
      childId: 'child-1',
      category: GrowthCategory.encouragement,
      title: '主动整理玩具',
      note: '',
    );

    expect(created.title, '主动整理玩具');
    final state = container.read(companionshipControllerProvider).value;
    expect(state?.records, hasLength(1));
  });

  test('deletes a growth record then reloads', () async {
    final api = _FakeCompanionshipApi(
      records: [_record('record-1', 'child-1', GrowthCategory.milestone)],
    );
    final container = ProviderContainer(
      overrides: [companionshipApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(companionshipControllerProvider.future);
    final controller = container.read(
      companionshipControllerProvider.notifier,
    );
    await controller.deleteGrowthRecord('record-1');

    expect(api.deletedRecordIds, ['record-1']);
    expect(
      container.read(companionshipControllerProvider).value?.records,
      isEmpty,
    );
  });

  test('creates and updates a habit reminder', () async {
    final api = _FakeCompanionshipApi(records: const []);
    final container = ProviderContainer(
      overrides: [companionshipApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(companionshipControllerProvider.future);
    final controller = container.read(
      companionshipControllerProvider.notifier,
    );
    final created = await controller.createHabitReminder(
      const HabitReminderDraft(
        childId: 'child-1',
        label: '刷牙',
        prompt: '该刷牙啦',
        reminderTime: '07:30',
        weekdays: [1, 3, 5],
        enabled: true,
      ),
    );
    expect(created.label, '刷牙');

    final updated = await controller.updateHabitReminder(
      created.id,
      const HabitReminderDraft(
        childId: 'child-1',
        label: '刷牙',
        prompt: '该刷牙啦',
        reminderTime: '20:00',
        weekdays: [2, 4, 6],
        enabled: false,
      ),
    );
    expect(updated.reminderTime, '20:00');
    expect(updated.enabled, isFalse);
  });

  test('propagates a load failure as an error state', () async {
    final api = _FakeCompanionshipApi(records: const [], failLoad: true);
    final container = ProviderContainer(
      overrides: [companionshipApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final subscription = container.listen(
      companionshipControllerProvider,
      (_, _) {},
    );
    addTearDown(subscription.close);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(
      container.read(companionshipControllerProvider).hasError,
      isTrue,
    );
  });
}

GrowthRecord _record(String id, String childId, GrowthCategory category) {
  return GrowthRecord(
    id: id,
    childId: childId,
    category: category,
    title: '记录 $id',
    note: '',
    recordedAt: DateTime.utc(2026, 10, 9, 12),
  );
}

HabitReminder _reminder(String id, String childId) {
  return HabitReminder(
    id: id,
    childId: childId,
    label: '提醒 $id',
    prompt: '',
    reminderTime: '08:00',
    weekdays: const [1, 2, 3, 4, 5, 6, 7],
    enabled: true,
  );
}

class _FakeCompanionshipApi implements CompanionshipApi {
  _FakeCompanionshipApi({
    required List<GrowthRecord> records,
    List<HabitReminder> reminders = const [],
    this.failLoad = false,
  })  : _records = [...records],
        _reminders = [...reminders];

  final List<GrowthRecord> _records;
  final List<HabitReminder> _reminders;
  final bool failLoad;
  int growthCalls = 0;
  String? lastChildFilter;
  final List<String> deletedRecordIds = [];
  int _sequence = 0;

  @override
  Future<List<GrowthRecord>> fetchGrowthRecords({String? childId}) async {
    growthCalls += 1;
    lastChildFilter = childId;
    if (failLoad) {
      throw Exception('network down');
    }
    if (childId == null) {
      return List<GrowthRecord>.unmodifiable(_records);
    }
    return List<GrowthRecord>.unmodifiable(
      _records.where((record) => record.childId == childId),
    );
  }

  @override
  Future<GrowthRecord> createGrowthRecord({
    required String childId,
    required GrowthCategory category,
    required String title,
    required String note,
  }) async {
    _sequence += 1;
    final record = GrowthRecord(
      id: 'record-new-$_sequence',
      childId: childId,
      category: category,
      title: title,
      note: note,
      recordedAt: DateTime.utc(2026, 10, 10),
    );
    _records.insert(0, record);
    return record;
  }

  @override
  Future<void> deleteGrowthRecord(String recordId) async {
    deletedRecordIds.add(recordId);
    _records.removeWhere((record) => record.id == recordId);
  }

  @override
  Future<List<HabitReminder>> fetchHabitReminders() async =>
      List<HabitReminder>.unmodifiable(_reminders);

  @override
  Future<HabitReminder> createHabitReminder(HabitReminderDraft draft) async {
    _sequence += 1;
    final reminder = HabitReminder(
      id: 'reminder-new-$_sequence',
      childId: draft.childId,
      label: draft.label,
      prompt: draft.prompt,
      reminderTime: draft.reminderTime,
      weekdays: draft.weekdays,
      enabled: draft.enabled,
    );
    _reminders.add(reminder);
    return reminder;
  }

  @override
  Future<HabitReminder> updateHabitReminder(
    String reminderId,
    HabitReminderDraft draft,
  ) async {
    final index = _reminders.indexWhere((item) => item.id == reminderId);
    final reminder = HabitReminder(
      id: reminderId,
      childId: draft.childId,
      label: draft.label,
      prompt: draft.prompt,
      reminderTime: draft.reminderTime,
      weekdays: draft.weekdays,
      enabled: draft.enabled,
    );
    if (index >= 0) {
      _reminders[index] = reminder;
    } else {
      _reminders.add(reminder);
    }
    return reminder;
  }

  @override
  Future<void> deleteHabitReminder(String reminderId) async {
    _reminders.removeWhere((item) => item.id == reminderId);
  }
}
