import '../../../core/network/api_client.dart';
import '../domain/vision_task.dart';

/// Guardian-facing camera and vision contract.
abstract interface class VisionApi {
  Future<VisionCapability> fetchCapability(String deviceId);

  Future<List<VisionTask>> listTasks(String deviceId);

  Future<List<VisionResult>> listResults(String deviceId);

  Future<VisionTask> createTask(
    String deviceId, {
    required String taskType,
    String prompt = '',
  });

  Future<void> cancelTask(String deviceId, String taskId);
}

/// Production implementation backed by the authenticated API client.
class HttpVisionApi implements VisionApi {
  const HttpVisionApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<VisionCapability> fetchCapability(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/vision/capability',
    );
    return VisionCapability.fromJson(_data(response));
  }

  @override
  Future<List<VisionTask>> listTasks(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/vision/tasks',
    );
    return _taskList(_data(response)['tasks']);
  }

  @override
  Future<List<VisionResult>> listResults(String deviceId) async {
    final response = await _apiClient.get(
      '/api/v1/devices/$deviceId/vision/results',
    );
    final results = _data(response)['results'];
    if (results is! List) {
      return const <VisionResult>[];
    }
    return results
        .whereType<Map>()
        .map(
          (item) => VisionResult.fromJson(Map<String, Object?>.from(item)),
        )
        .toList(growable: false);
  }

  @override
  Future<VisionTask> createTask(
    String deviceId, {
    required String taskType,
    String prompt = '',
  }) async {
    final response = await _apiClient.post(
      '/api/v1/devices/$deviceId/vision/tasks',
      body: <String, Object?>{'task_type': taskType, 'prompt': prompt},
    );
    return VisionTask.fromJson(_data(response));
  }

  @override
  Future<void> cancelTask(String deviceId, String taskId) async {
    await _apiClient.post(
      '/api/v1/devices/$deviceId/vision/tasks/$taskId/cancel',
    );
  }
}

List<VisionTask> _taskList(Object? value) {
  if (value is! List) {
    return const <VisionTask>[];
  }
  return value
      .whereType<Map>()
      .map((item) => VisionTask.fromJson(Map<String, Object?>.from(item)))
      .toList(growable: false);
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing vision data');
  }
  return Map<String, Object?>.from(data);
}
