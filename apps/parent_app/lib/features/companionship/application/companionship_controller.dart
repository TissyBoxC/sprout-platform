import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/companionship_api.dart';
import '../domain/companionship.dart';

/// One child the guardian may attach a record or reminder to.
class CompanionshipChild {
  const CompanionshipChild({required this.id, required this.name});

  final String id;
  final String name;
}

/// Complete state rendered by the companionship page.
class CompanionshipState {
  const CompanionshipState({
    required this.records,
    required this.reminders,
    this.childFilter,
  });

  const CompanionshipState.initial()
    : records = const [],
      reminders = const [],
      childFilter = null;

  final List<GrowthRecord> records;
  final List<HabitReminder> reminders;

  /// When set, the timeline is narrowed to one child.
  final String? childFilter;

  bool get isEmpty => records.isEmpty && reminders.isEmpty;

  /// Distinct child ids present in the current timeline, in first-seen order.
  List<String> get recordChildIds {
    final seen = <String>{};
    final ids = <String>[];
    for (final record in records) {
      if (record.childId.isNotEmpty && seen.add(record.childId)) {
        ids.add(record.childId);
      }
    }
    return ids;
  }
}

/// Owns the growth timeline and habit reminders so the page can refresh,
/// filter, and mutate both without holding a device session.
class CompanionshipController extends AsyncNotifier<CompanionshipState> {
  late CompanionshipApi _api;
  String? _childFilter;

  @override
  Future<CompanionshipState> build() async {
    _api = ref.read(companionshipApiProvider);
    _childFilter = null;
    return _load();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  /// Narrows the timeline to one child, or clears the filter with `null`.
  Future<void> filterByChild(String? childId) async {
    final normalized = (childId != null && childId.isNotEmpty) ? childId : null;
    if (normalized == _childFilter) {
      return;
    }
    _childFilter = normalized;
    await refresh();
  }

  /// Appends one growth record and reloads the timeline.
  Future<GrowthRecord> createGrowthRecord({
    required String childId,
    required GrowthCategory category,
    required String title,
    required String note,
  }) async {
    final created = await _api.createGrowthRecord(
      childId: childId,
      category: category,
      title: title,
      note: note,
    );
    await refresh();
    return created;
  }

  /// Deletes one growth record and reloads the timeline.
  Future<void> deleteGrowthRecord(String recordId) async {
    await _api.deleteGrowthRecord(recordId);
    await refresh();
  }

  /// Creates one reminder and reloads the list.
  Future<HabitReminder> createHabitReminder(HabitReminderDraft draft) async {
    final created = await _api.createHabitReminder(draft);
    await refresh();
    return created;
  }

  /// Replaces one reminder and reloads the list.
  Future<HabitReminder> updateHabitReminder(
    String reminderId,
    HabitReminderDraft draft,
  ) async {
    final updated = await _api.updateHabitReminder(reminderId, draft);
    await refresh();
    return updated;
  }

  /// Deletes one reminder and reloads the list.
  Future<void> deleteHabitReminder(String reminderId) async {
    await _api.deleteHabitReminder(reminderId);
    await refresh();
  }

  Future<CompanionshipState> _load() async {
    final records = await _api.fetchGrowthRecords(childId: _childFilter);
    final reminders = await _api.fetchHabitReminders();
    return CompanionshipState(
      records: records,
      reminders: reminders,
      childFilter: _childFilter,
    );
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final companionshipApiProvider = Provider<CompanionshipApi>((ref) {
  return HttpCompanionshipApi(ref.read(apiClientProvider));
});

final companionshipControllerProvider =
    AsyncNotifierProvider<CompanionshipController, CompanionshipState>(
      CompanionshipController.new,
    );
