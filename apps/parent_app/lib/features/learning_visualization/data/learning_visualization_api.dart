import '../../../core/network/api_client.dart';
import '../domain/learning_visualization.dart';

/// Guardian-facing learning-visualization contracts. Every call is
/// family-scoped on the platform, so the client never sends an owner id.
abstract interface class LearningVisualizationApi {
  /// Reads the family policy, or the platform default when none is set.
  Future<LearningVisualizationPolicy> fetchPolicy();

  /// Publishes a new family policy revision and returns the stored policy.
  Future<LearningVisualizationPolicy> publishPolicy(
    LearningVisualizationDraft draft,
  );

  /// Lists each family device's latest bounded visualization status.
  Future<List<LearningVisualizationDevice>> fetchDevices();
}

/// Production implementation backed by the authenticated API client.
class HttpLearningVisualizationApi implements LearningVisualizationApi {
  const HttpLearningVisualizationApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<LearningVisualizationPolicy> fetchPolicy() async {
    final response = await _apiClient.get(
      '/api/v1/learning-visualization/policy',
    );
    return _requiredPolicy(response['data']);
  }

  @override
  Future<LearningVisualizationPolicy> publishPolicy(
    LearningVisualizationDraft draft,
  ) async {
    final response = await _apiClient.put(
      '/api/v1/learning-visualization/policy',
      body: draft.toJson(),
    );
    return _requiredPolicy(response['data']);
  }

  @override
  Future<List<LearningVisualizationDevice>> fetchDevices() async {
    final response = await _apiClient.get(
      '/api/v1/learning-visualization/devices',
    );
    final data = response['data'];
    if (data is! Map) {
      throw const FormatException('missing learning visualization data');
    }
    final devices = data['devices'];
    if (devices is! List) {
      return const <LearningVisualizationDevice>[];
    }
    return devices
        .whereType<Map>()
        .map(
          (item) => LearningVisualizationDevice.fromJson(
            Map<String, Object?>.from(item),
          ),
        )
        .toList(growable: false);
  }
}

LearningVisualizationPolicy _requiredPolicy(Object? data) {
  if (data is! Map) {
    throw const FormatException('missing learning visualization policy');
  }
  final envelope = Map<String, Object?>.from(data);
  final policy = envelope['policy'] ?? envelope;
  if (policy is! Map) {
    throw const FormatException('missing learning visualization policy');
  }
  return LearningVisualizationPolicy.fromJson(
    Map<String, Object?>.from(policy),
  );
}
