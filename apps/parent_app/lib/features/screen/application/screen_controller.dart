import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/screen_api.dart';
import '../domain/screen.dart';

/// The family screen policy, catalog, point-to-read dictionary and the last
/// resolved video plan for one device.
class ScreenState {
  const ScreenState({
    required this.policy,
    required this.catalog,
    required this.dictionary,
  });

  final ScreenPolicy policy;
  final ScreenCatalog catalog;
  final List<PointReadEntry> dictionary;

  ScreenState copyWith({
    ScreenPolicy? policy,
    ScreenCatalog? catalog,
    List<PointReadEntry>? dictionary,
  }) {
    return ScreenState(
      policy: policy ?? this.policy,
      catalog: catalog ?? this.catalog,
      dictionary: dictionary ?? this.dictionary,
    );
  }
}

/// Owns the guardian's screen surface settings for the family.
class ScreenController extends AsyncNotifier<ScreenState> {
  late ScreenApi _api;

  @override
  Future<ScreenState> build() async {
    _api = ref.read(screenApiProvider);
    final result = await _api.fetchPolicy();
    final dictionary = await _api.fetchDictionary();
    return ScreenState(
      policy: result.$1,
      catalog: result.$2,
      dictionary: dictionary,
    );
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(build);
  }

  Future<ScreenPolicy> savePolicy(ScreenPolicy policy) async {
    final updated = await _api.updatePolicy(policy);
    final current = state.value;
    if (current != null) {
      state = AsyncValue.data(current.copyWith(policy: updated));
    }
    return updated;
  }

  Future<PointReadEntry> putEntry({
    required String term,
    required String text,
  }) async {
    final entry = await _api.putDictionaryEntry(term: term, text: text);
    final current = state.value;
    if (current != null) {
      final next = <PointReadEntry>[
        for (final existing in current.dictionary)
          if (existing.term != entry.term) existing,
        entry,
      ];
      state = AsyncValue.data(current.copyWith(dictionary: next));
    }
    return entry;
  }

  Future<void> deleteEntry(String entryId) async {
    await _api.deleteDictionaryEntry(entryId);
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncValue.data(
      current.copyWith(
        dictionary: current.dictionary
            .where((entry) => entry.id != entryId)
            .toList(growable: false),
      ),
    );
  }

  Future<VideoPlaybackPlan> resolvePlayback({
    required String deviceId,
    required int bitrateKbps,
    required int fps,
  }) {
    return _api.resolvePlayback(
      deviceId: deviceId,
      bitrateKbps: bitrateKbps,
      fps: fps,
    );
  }

  Future<VideoPlaybackPlan> fetchPlayback(String deviceId) {
    return _api.fetchPlayback(deviceId);
  }

  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final screenApiProvider = Provider<ScreenApi>((ref) {
  return HttpScreenApi(ref.read(apiClientProvider));
});

final screenControllerProvider =
    AsyncNotifierProvider<ScreenController, ScreenState>(ScreenController.new);
