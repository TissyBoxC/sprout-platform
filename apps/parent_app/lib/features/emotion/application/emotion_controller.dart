import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../../family/application/family_controller.dart';
import '../data/emotion_api.dart';
import '../domain/emotion.dart';
import '../presentation/emotion_copy.dart';

/// Resolves the signed-in family before any aggregate can be addressed.
final emotionFamilyIdProvider = FutureProvider<String>((ref) async {
  final family = await ref.watch(familyControllerProvider.future);
  return family.capability.familyId;
});

final emotionApiProvider = Provider<EmotionApi?>((ref) {
  final familyId = ref.watch(emotionFamilyIdProvider).value;
  if (familyId == null || familyId.isEmpty) {
    return null;
  }
  return HttpEmotionApi(ref.watch(apiClientProvider), familyId);
});

/// The children the distribution can be limited to.
///
/// It reuses the child profile list, so the names a guardian picks here are the
/// same ones the rest of the app shows.
final emotionChildrenProvider = FutureProvider<List<ChildProfile>>((ref) {
  return ref.watch(childProfileControllerProvider.future);
});

/// One choice the guardian makes on the screen, held so a rebuild keeps it.
class _EmotionChoice extends Notifier<int> {
  _EmotionChoice(this._initial);

  final int _initial;

  @override
  int build() => _initial;

  /// Replaces the choice. Riverpod 3 offers no string or index state provider,
  /// so the screen calls this instead of assigning `state` from a widget.
  void select(int value) => state = value;
}

/// The selected window in days.
final emotionWindowProvider = NotifierProvider<_EmotionChoice, int>(
  () => _EmotionChoice(EmotionSummaryDefaults.windowDays),
);

/// The index of the child in [emotionChildrenProvider], or -1 for the whole
/// family.
///
/// It stores a position rather than an identifier because Riverpod 3 offers no
/// string state provider, and an index survives the one case that matters here:
/// the list is read before the choice is used, so the position always resolves.
final emotionSelectedChildIndexProvider = NotifierProvider<_EmotionChoice, int>(
  () => _EmotionChoice(-1),
);

/// The child the choice currently points at, or empty for the whole family.
final emotionSelectedChildIdProvider = Provider<String>((ref) {
  final index = ref.watch(emotionSelectedChildIndexProvider);
  final children = ref.watch(emotionChildrenProvider).value;
  if (children == null || index < 0 || index >= children.length) {
    return '';
  }
  return children[index].childId;
});

/// The window the screen shows, narrowed to what the platform accepts.
final emotionEffectiveWindowProvider = Provider<int>((ref) {
  final days = ref.watch(emotionWindowProvider);
  if (days < 1 || days > 90) {
    return EmotionSummaryDefaults.windowDays;
  }
  return days;
});

/// The values the screen falls back to when the platform reports none.
abstract final class EmotionSummaryDefaults {
  static const windowDays = 7;
  static const maxRetentionDays = 90;
}

/// One binding of a family to the operations the screen can call.
class EmotionBinding {
  const EmotionBinding({required this.api, this.isForeground = true});

  final EmotionApi? api;

  /// True while the screen is on top. It decides whether the summary is read
  /// at all: a screen that is not showing data must not hold a future open.
  final bool isForeground;
}

/// The switch and the aggregate for one (child, window) pair.
///
/// It is one notifier rather than two providers because the screen's states are
/// coupled: turning sensing on has to reveal the distribution, and a refusal
/// caused by a turned-off switch must land on the switch rather than on the
/// list.
class EmotionController extends Notifier<EmotionState> {
  @override
  EmotionState build() {
    final binding = ref.watch(emotionBindingProvider);
    if (!binding.isForeground) {
      // A hidden page still builds so its notifier exists; it just reads
      // nothing until the page says it is visible again.
      return const EmotionState(isLoading: true);
    }
    final api = binding.api;
    if (api == null) {
      return const EmotionState(
        isLoading: true,
        settings: EmotionSettings(
          isEnabled: false,
          retentionDays: EmotionSummaryDefaults.windowDays,
          summaryDefaultDays: EmotionSummaryDefaults.windowDays,
          maxRetentionDays: EmotionSummaryDefaults.maxRetentionDays,
        ),
      );
    }
    final childId = ref.watch(emotionSelectedChildIdProvider);
    final days = ref.watch(emotionEffectiveWindowProvider);
    _load(api, childId: childId, days: days);
    return const EmotionState(isLoading: true);
  }

  /// The API this notifier is using, kept so a save can report a refusal.
  EmotionApi? _api;

  /// True while this notifier is attached. Every await is followed by a check,
  /// because a guardian can leave the screen while a read is in flight.
  final bool _isMounted = true;

  /// Reads the switch, then the aggregate only when sensing is on.
  ///
  /// [notice] is a confirmation to keep beside whatever the read finds; it is
  /// carried as a kind rather than as a sentence so the copy stays in one file.
  Future<void> _load(
    EmotionApi api, {
    required String childId,
    required int days,
    EmotionNotice? notice,
  }) async {
    _api = api;
    try {
      final settings = await api.fetchSettings();
      if (!_isMounted) {
        return;
      }
      if (!settings.isEnabled) {
        state = EmotionState(
          isLoading: false,
          availability: EmotionAvailability.disabled,
          settings: settings,
          notice: notice,
        );
        return;
      }
      final summary = await api.fetchSummary(childId: childId, days: days);
      if (!_isMounted) {
        return;
      }
      state = EmotionState(
        isLoading: false,
        availability: EmotionAvailability.enabled,
        settings: settings,
        summary: summary,
        notice: notice,
      );
    } on Object catch (error) {
      if (!_isMounted) {
        return;
      }
      // A family that turned sensing off elsewhere answers the summary with a
      // refusal about the switch. Showing that as the switch's state is
      // truthful; showing it as a failed list would blame the wrong thing.
      if (error is EmotionRefused && error.refusal == EmotionRefusal.disabled) {
        state = EmotionState(
          isLoading: false,
          availability: EmotionAvailability.disabled,
          notice: notice,
        );
        return;
      }
      state = EmotionState(isLoading: false, error: error);
    }
  }

  /// Turns sensing on and shows the first distribution as soon as it is there.
  Future<void> enable() async {
    await _saveSettings(isEnabled: true);
  }

  /// Turns sensing off. The screen asks for confirmation before calling this.
  Future<void> disable() async {
    await _saveSettings(isEnabled: false, notice: EmotionNotice.disabled);
  }

  /// Changes how long samples are kept. A rejected value keeps the stored one.
  Future<void> setRetentionDays(int days) async {
    final api = _api ?? ref.read(emotionBindingProvider).api;
    if (api == null || days <= 0) {
      return;
    }
    final previous = state.settings;
    state = state.copyWith(isSavingRetention: true, clearError: true);
    try {
      final updated = await api.updateSettings(
        isEnabled: state.settings?.isEnabled ?? true,
        retentionDays: days,
      );
      if (!_isMounted) {
        return;
      }
      state = state.copyWith(
        settings: updated,
        isSavingRetention: false,
        clearNotice: true,
      );
    } on Object catch (error) {
      if (!_isMounted) {
        return;
      }
      state = state.copyWith(
        settings: previous,
        isSavingRetention: false,
        error: error,
      );
    }
  }

  Future<void> _saveSettings({
    required bool isEnabled,
    EmotionNotice? notice,
  }) async {
    final api = _api ?? ref.read(emotionBindingProvider).api;
    if (api == null) {
      return;
    }
    state = state.copyWith(isSaving: true, clearError: true);
    try {
      await api.updateSettings(isEnabled: isEnabled);
      if (!_isMounted) {
        return;
      }
      // The switch reply carries the configuration but no aggregate, so both
      // are read again. The read is what confirms the stored state, which is
      // why nothing here assumes the save succeeded just because the reply
      // arrived: a family that turned sensing off on another phone must see
      // that here too.
      state = EmotionState(isLoading: true, notice: notice);
      await _load(
        api,
        childId: ref.read(emotionSelectedChildIdProvider),
        days: ref.read(emotionEffectiveWindowProvider),
        notice: notice,
      );
    } on Object catch (error) {
      if (!_isMounted) {
        return;
      }
      // A refusal lands back on the screen the guardian is already on, with
      // its sentence beside the control, rather than replacing the whole page.
      state = EmotionState(
        isLoading: false,
        availability: state.availability,
        settings: state.settings,
        notice: notice,
        error: error,
      );
    }
  }

  /// Reads the selected child and window again, for a retry or a refresh.
  Future<void> refresh() async {
    final api = _api ?? ref.read(emotionBindingProvider).api;
    if (api == null) {
      return;
    }
    state = state.copyWith(isLoading: true, clearError: true);
    await _load(
      api,
      childId: ref.read(emotionSelectedChildIdProvider),
      days: ref.read(emotionEffectiveWindowProvider),
    );
  }
}

final emotionControllerProvider =
    NotifierProvider<EmotionController, EmotionState>(EmotionController.new);

/// Decides whether the screen may read anything.
///
/// The page overrides this with its own visibility, which is what keeps a
/// paused page from holding a request open. It defaults to the real binding so
/// production never needs an override.
final emotionBindingProvider = Provider<EmotionBinding>((ref) {
  // Reading the API through a provider that rebuilds on family change keeps the
  // binding honest without the screen having to pass it down.
  final api = ref.watch(emotionApiProvider);
  return EmotionBinding(api: api);
});

/// The state the page watches.
///
/// It carries the binding into the notifier's key so a page that reports itself
/// hidden rebuilds the notifier with a request it will not issue, and a page
/// that reports itself visible again rebuilds it with a fresh read.
final emotionModelProvider = Provider<EmotionState>((ref) {
  final binding = ref.watch(emotionBindingProvider);
  if (!binding.isForeground) {
    return const EmotionState(isLoading: true);
  }
  return ref.watch(emotionControllerProvider);
});

/// A confirmation the screen shows after a saved change.
enum EmotionNotice { enabled, disabled }

/// What the 情绪感知 screen shows right now.
class EmotionState {
  const EmotionState({
    this.availability = EmotionAvailability.loading,
    this.settings,
    this.summary,
    this.isLoading = false,
    this.isSaving = false,
    this.isSavingRetention = false,
    this.error,
    this.notice,
  });

  final EmotionAvailability availability;
  final EmotionSettings? settings;
  final EmotionSummary? summary;
  final bool isLoading;
  final bool isSaving;
  final bool isSavingRetention;

  /// A refusal or a transport failure, already safe to show.
  final Object? error;

  /// A confirmation the guardian can read, such as "已关闭".
  final EmotionNotice? notice;

  bool get isEnabled => availability == EmotionAvailability.enabled;

  bool get hasSummary => summary != null;

  EmotionState copyWith({
    EmotionAvailability? availability,
    EmotionSettings? settings,
    EmotionSummary? summary,
    bool clearSummary = false,
    bool? isLoading,
    bool? isSaving,
    bool? isSavingRetention,
    Object? error,
    bool clearError = false,
    EmotionNotice? notice,
    bool clearNotice = false,
  }) {
    return EmotionState(
      availability: availability ?? this.availability,
      settings: settings ?? this.settings,
      summary: clearSummary ? null : (summary ?? this.summary),
      isLoading: isLoading ?? this.isLoading,
      isSaving: isSaving ?? this.isSaving,
      isSavingRetention: isSavingRetention ?? this.isSavingRetention,
      error: clearError ? null : (error ?? this.error),
      notice: clearNotice ? null : (notice ?? this.notice),
    );
  }
}
