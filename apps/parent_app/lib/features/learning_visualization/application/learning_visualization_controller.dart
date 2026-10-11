import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/learning_visualization_api.dart';
import '../domain/learning_visualization.dart';

/// Complete state rendered by the learning-visualization page.
class LearningVisualizationState {
  const LearningVisualizationState({
    required this.policy,
    required this.devices,
  });

  final LearningVisualizationPolicy policy;
  final List<LearningVisualizationDevice> devices;

  bool get isEmpty => devices.isEmpty;

  int get runningCount => devices.where((device) => device.running).length;

  int get outdatedCount =>
      devices.where((device) => device.supported && !device.upToDate).length;
}

/// Owns the family learning-visualization policy so the page can read, publish
/// and refresh it without holding a device session.
class LearningVisualizationController
    extends AsyncNotifier<LearningVisualizationState> {
  late final LearningVisualizationApi _api;

  @override
  Future<LearningVisualizationState> build() async {
    _api = ref.read(learningVisualizationApiProvider);
    return _load();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  /// Publishes a new policy revision and reloads the page state.
  Future<LearningVisualizationPolicy> publishPolicy(
    LearningVisualizationDraft draft,
  ) async {
    final policy = await _api.publishPolicy(draft);
    await refresh();
    return policy;
  }

  Future<LearningVisualizationState> _load() async {
    final policy = await _api.fetchPolicy();
    final devices = await _api.fetchDevices();
    return LearningVisualizationState(policy: policy, devices: devices);
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final learningVisualizationApiProvider = Provider<LearningVisualizationApi>((
  ref,
) {
  return HttpLearningVisualizationApi(ref.read(apiClientProvider));
});

final learningVisualizationControllerProvider =
    AsyncNotifierProvider<
      LearningVisualizationController,
      LearningVisualizationState
    >(LearningVisualizationController.new);
