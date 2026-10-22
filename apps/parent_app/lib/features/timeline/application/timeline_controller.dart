import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../../family/application/family_controller.dart';
import '../data/timeline_api.dart';
import '../domain/timeline.dart';

/// Resolves the signed-in family before the timeline can be addressed.
final timelineFamilyIdProvider = FutureProvider<String>((ref) async {
  final family = await ref.watch(familyControllerProvider.future);
  return family.capability.familyId;
});

final timelineApiProvider = Provider<TimelineApi?>((ref) {
  final familyId = ref.watch(timelineFamilyIdProvider).value;
  if (familyId == null || familyId.isEmpty) {
    return null;
  }
  return HttpTimelineApi(ref.watch(apiClientProvider), familyId);
});

/// The children whose timeline can be read.
///
/// The platform refuses a timeline read without a child, so this list is also
/// what decides whether the screen can ask for anything at all.
final timelineChildrenProvider = FutureProvider<List<ChildProfile>>((ref) {
  return ref.watch(childProfileControllerProvider.future);
});

/// One choice the guardian makes on the screen, held so a rebuild keeps it.
class _TimelineChoice extends Notifier<int> {
  _TimelineChoice(this._initial);

  final int _initial;

  @override
  int build() => _initial;

  /// Replaces the choice. Riverpod 3 offers no string or index state provider,
  /// so the screen calls this instead of assigning `state` from a widget.
  void select(int value) => state = value;
}

/// The index of the chosen child in [timelineChildrenProvider], or -1 when
/// nothing has been chosen yet.
///
/// The platform refuses a timeline read without a child, so -1 is a real state
/// rather than a default: the screen asks the guardian to choose instead of
/// sending a request that is known to fail.
final timelineSelectedChildIndexProvider =
    NotifierProvider<_TimelineChoice, int>(() => _TimelineChoice(-1));

/// The child the choice points at, or empty when nothing is chosen.
final timelineSelectedChildIdProvider = Provider<String>((ref) {
  final index = ref.watch(timelineSelectedChildIndexProvider);
  final children = ref.watch(timelineChildrenProvider).value;
  if (children == null || index < 0 || index >= children.length) {
    return '';
  }
  return children[index].childId;
});

/// The state the page watches. A page without focus reads nothing; the real
/// values come back when the page says it is visible again.
final timelineModelProvider = Provider<TimelineState>((ref) {
  final binding = ref.watch(timelineBindingProvider);
  if (!binding.isForeground) {
    return const TimelineState();
  }
  return ref.watch(timelineControllerProvider);
});

/// Whether the page is on top, and which API it may use.
class TimelineBinding {
  const TimelineBinding({required this.api, this.isForeground = true});

  final TimelineApi? api;
  final bool isForeground;
}

final timelineBindingProvider = Provider<TimelineBinding>((ref) {
  return TimelineBinding(api: ref.watch(timelineApiProvider));
});

/// What the 成长时间轴 screen shows right now.
class TimelineState {
  const TimelineState({
    this.childId = '',
    this.events = const <TimelineEvent>[],
    this.milestones,
    this.nextCursor = '',
    this.isLoading = false,
    this.isLoadingMore = false,
    this.error,
    this.loadMoreError,
  });

  final String childId;
  final List<TimelineEvent> events;
  final TimelineMilestones? milestones;

  /// The cursor the next page continues from; empty means the list reached
  /// the newest entry and no further read will be made.
  final String nextCursor;
  final bool isLoading;
  final bool isLoadingMore;

  /// A failure that stopped the first page.
  final Object? error;

  /// A failure that stopped one more page while the list stayed readable.
  final Object? loadMoreError;

  bool get hasMore => nextCursor.isNotEmpty;

  bool get isEmpty => events.isEmpty;

  TimelineState copyWith({
    String? childId,
    List<TimelineEvent>? events,
    TimelineMilestones? milestones,
    bool clearMilestones = false,
    String? nextCursor,
    bool? isLoading,
    bool? isLoadingMore,
    Object? error,
    bool clearError = false,
    Object? loadMoreError,
    bool clearLoadMoreError = false,
  }) {
    return TimelineState(
      childId: childId ?? this.childId,
      events: events ?? this.events,
      milestones: clearMilestones ? null : (milestones ?? this.milestones),
      nextCursor: nextCursor ?? this.nextCursor,
      isLoading: isLoading ?? this.isLoading,
      isLoadingMore: isLoadingMore ?? this.isLoadingMore,
      error: clearError ? null : (error ?? this.error),
      loadMoreError: clearLoadMoreError
          ? null
          : (loadMoreError ?? this.loadMoreError),
    );
  }
}

/// Reads one child's timeline, one page at a time.
///
/// Pagination follows the platform's cursor rather than an offset, and an empty
/// cursor ends the list for good: no further read is issued once the platform
/// says there is nothing newer, so a screen left open cannot spin.
class TimelineController extends Notifier<TimelineState> {
  static const _pageSize = 30;

  @override
  TimelineState build() {
    final binding = ref.watch(timelineBindingProvider);
    if (!binding.isForeground) {
      return const TimelineState();
    }
    final api = binding.api;
    final selected = ref.watch(timelineSelectedChildIdProvider);
    if (api == null || selected.isEmpty) {
      // Without a child there is nothing the platform will answer, so no
      // request is made and the screen asks the guardian to choose one.
      return TimelineState(childId: selected);
    }
    _loadFirstPage(api, selected);
    return TimelineState(childId: selected, isLoading: true);
  }

  /// The API this notifier is using, kept so a page can be requested again.
  TimelineApi? _api;

  /// The generation of the newest first-page read. A reply from an older
  /// generation is dropped, which is what keeps one child's events from landing
  /// in another child's list when the guardian switches quickly.
  int _generation = 0;

  final bool _isMounted = true;

  Future<void> _loadFirstPage(TimelineApi api, String childId) async {
    _api = api;
    final generation = ++_generation;
    try {
      final page = await api.fetchEvents(childId: childId, limit: _pageSize);
      if (!_isMounted || generation != _generation) {
        return;
      }
      state = TimelineState(
        childId: childId,
        events: _deduplicated(page.events),
        nextCursor: page.nextCursor,
        milestones: state.milestones,
        isLoading: false,
      );
      // The counters travel with the list, so a failed counter read cannot hide
      // the events the guardian already has. They are read from an empty card
      // on purpose: counters from the previous child must not stand in for
      // this one's while the read is in flight.
      state = state.copyWith(clearMilestones: true);
      await _loadMilestones(api, childId, generation);
    } on Object catch (error) {
      if (!_isMounted || generation != _generation) {
        return;
      }
      state = state.copyWith(isLoading: false, error: error);
    }
  }

  Future<void> _loadMilestones(
    TimelineApi api,
    String childId,
    int generation,
  ) async {
    try {
      final milestones = await api.fetchMilestones(childId: childId);
      if (!_isMounted || generation != _generation) {
        return;
      }
      state = state.copyWith(milestones: milestones);
    } on Object {
      // The counters are context, not the record itself. A failure leaves the
      // list readable and the counters absent, which the screen explains.
      if (!_isMounted || generation != _generation) {
        return;
      }
      state = state.copyWith(clearMilestones: true);
    }
  }

  /// Loads the next page.
  ///
  /// It returns immediately when a page is already in flight or the platform
  /// has said there is nothing newer, so a button pressed twice cannot read the
  /// same cursor twice.
  Future<void> loadMore() async {
    final api = _api ?? ref.read(timelineBindingProvider).api;
    final cursor = state.nextCursor;
    if (api == null ||
        cursor.isEmpty ||
        state.isLoadingMore ||
        state.isLoading) {
      return;
    }
    final generation = _generation;
    state = state.copyWith(isLoadingMore: true, clearLoadMoreError: true);
    try {
      final page = await api.fetchEvents(
        childId: state.childId,
        before: cursor,
        limit: _pageSize,
      );
      if (!_isMounted || generation != _generation) {
        return;
      }
      state = state.copyWith(
        events: _deduplicated(<TimelineEvent>[...state.events, ...page.events]),
        nextCursor: page.nextCursor,
        isLoadingMore: false,
      );
    } on Object catch (error) {
      if (!_isMounted || generation != _generation) {
        return;
      }
      state = state.copyWith(isLoadingMore: false, loadMoreError: error);
    }
  }

  /// Reads the first page again, for the retry button.
  Future<void> refresh() async {
    final api = _api ?? ref.read(timelineBindingProvider).api;
    if (api == null || state.childId.isEmpty) {
      return;
    }
    state = state.copyWith(isLoading: true, clearError: true);
    await _loadFirstPage(api, state.childId);
  }

  /// Drops an entry that arrived twice, keyed on the platform's stable id.
  ///
  /// Two pages can only overlap if the stored timeline changed between reads;
  /// showing the same card twice would still read like a double count.
  List<TimelineEvent> _deduplicated(List<TimelineEvent> events) {
    final seen = <String>{};
    final unique = <TimelineEvent>[];
    for (final event in events) {
      if (seen.add(event.eventId)) {
        unique.add(event);
      }
    }
    return unique;
  }
}

final timelineControllerProvider =
    NotifierProvider<TimelineController, TimelineState>(TimelineController.new);
