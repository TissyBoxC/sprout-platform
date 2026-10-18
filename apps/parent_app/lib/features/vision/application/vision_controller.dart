import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/vision_api.dart';
import '../domain/vision_task.dart';

/// Combined guardian view of one device's camera capability, tasks, and
/// short-lived results.
class VisionOverview {
  const VisionOverview({
    required this.capability,
    required this.tasks,
    required this.results,
  });

  final VisionCapability capability;
  final List<VisionTask> tasks;
  final List<VisionResult> results;
}

/// Owns the guardian's camera and vision view for one device.
class VisionController extends AsyncNotifier<VisionOverview> {
  VisionController(this.deviceId);

  final String deviceId;
  late VisionApi _api;

  @override
  Future<VisionOverview> build() async {
    _api = ref.read(visionApiProvider);
    return _load(deviceId);
  }

  Future<VisionOverview> _load(String deviceId) async {
    final capability = await _api.fetchCapability(deviceId);
    final tasks = await _api.listTasks(deviceId);
    final results = await _api.listResults(deviceId);
    return VisionOverview(
      capability: capability,
      tasks: tasks,
      results: results,
    );
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(() => _load(deviceId));
  }

  /// Creates one task. The platform enforces guardian consent, so a refused
  /// request surfaces a guardian-facing message and keeps the current state.
  Future<VisionTask> createTask({
    required String taskType,
    String prompt = '',
  }) async {
    final task = await _api.createTask(
      deviceId,
      taskType: taskType,
      prompt: prompt,
    );
    await refresh();
    return task;
  }

  Future<void> cancelTask(String taskId) async {
    await _api.cancelTask(deviceId, taskId);
    await refresh();
  }

  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final visionApiProvider = Provider<VisionApi>((ref) {
  return HttpVisionApi(ref.read(apiClientProvider));
});

final visionControllerProvider =
    AsyncNotifierProvider.family<VisionController, VisionOverview, String>(
      VisionController.new,
    );
